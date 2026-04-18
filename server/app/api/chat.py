from fastapi import APIRouter, Depends, HTTPException, BackgroundTasks
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session
from typing import AsyncGenerator, Optional
from app.database import get_db
from app.models.session import Session as SessionModel, SESSION_STATUS_STREAMING, SESSION_STATUS_IDLE
from app.models.message import Message, MESSAGE_STATUS_STREAMING, MESSAGE_STATUS_COMPLETED
from app.models.agent import Agent
from app.services.connection_manager import manager
from app.services.background_tasks import process_chat_task
from app.services.data_service import DataService
from app.logger import logger
import json
import asyncio

router = APIRouter(prefix="/api/chat", tags=["chat"])


@router.post("/new")
async def create_and_send(
    message: str,
    agent_id: Optional[int] = None,
    title: Optional[str] = None,
    background_tasks: BackgroundTasks = None,
    db: Session = Depends(get_db)
):
    logger.info(f"Create new session and send message: {message[:50]}...")
    
    if not title:
        title = message[:50] + ("..." if len(message) > 50 else "")
    
    if not agent_id:
        default_agent = db.query(Agent).first()
        if not default_agent:
            raise HTTPException(status_code=500, detail="No default agent found")
        agent_id = default_agent.id
    
    session = SessionModel(
        title=title,
        agent_id=agent_id,
        status=SESSION_STATUS_STREAMING
    )
    db.add(session)
    db.flush()
    
    user_msg = Message(
        session_id=session.id,
        role="user",
        content=message,
        status=MESSAGE_STATUS_COMPLETED
    )
    db.add(user_msg)
    db.flush()
    
    ai_msg = Message(
        session_id=session.id,
        role="assistant",
        content="",
        status=MESSAGE_STATUS_STREAMING
    )
    db.add(ai_msg)
    db.commit()
    
    logger.info(f"Created session {session.id}, messages - user: {user_msg.id}, ai: {ai_msg.id}")
    
    background_tasks.add_task(
        process_chat_task,
        session.id,
        user_msg.id,
        ai_msg.id
    )
    
    return {
        "session_id": session.id,
        "user_message_id": user_msg.id,
        "ai_message_id": ai_msg.id
    }


@router.post("/send/{session_id}")
async def send_message(
    session_id: int,
    message: str,
    background_tasks: BackgroundTasks,
    db: Session = Depends(get_db)
):
    logger.info(f"Send message request - session_id: {session_id}, message: {message[:50]}...")
    
    session = DataService.get_session(db, session_id)
    if not session:
        raise HTTPException(status_code=404, detail="Session not found")
    
    if session.status == SESSION_STATUS_STREAMING:
        logger.warning(f"Session {session_id} is already streaming, rejecting message")
        raise HTTPException(status_code=400, detail="Session is busy, please wait for current response to complete")
    
    user_msg = Message(
        session_id=session_id,
        role="user",
        content=message,
        status=MESSAGE_STATUS_COMPLETED
    )
    db.add(user_msg)
    db.flush()
    
    ai_msg = Message(
        session_id=session_id,
        role="assistant",
        content="",
        status=MESSAGE_STATUS_STREAMING
    )
    db.add(ai_msg)
    db.flush()
    
    session.status = SESSION_STATUS_STREAMING
    db.commit()
    
    logger.info(f"Created messages - user: {user_msg.id}, ai: {ai_msg.id}")
    
    background_tasks.add_task(
        process_chat_task,
        session_id,
        user_msg.id,
        ai_msg.id
    )
    
    return {
        "user_message_id": user_msg.id,
        "ai_message_id": ai_msg.id
    }


@router.get("/stream/{session_id}")
async def stream_messages(
    session_id: int,
    db: Session = Depends(get_db)
):
    logger.info(f"SSE stream request for session {session_id}")
    
    session = DataService.get_session(db, session_id)
    if not session:
        raise HTTPException(status_code=404, detail="Session not found")
    
    async def event_generator() -> AsyncGenerator[bytes, None]:
        try:
            yield b": connected\n\n"
            
            streaming_msg = db.query(Message).filter(
                Message.session_id == session_id,
                Message.status == MESSAGE_STATUS_STREAMING
            ).order_by(Message.created_at.desc()).first()
            
            if streaming_msg:
                logger.info(f"Found streaming message {streaming_msg.id}, sending existing content: {len(streaming_msg.content)} chars")
                data = json.dumps({
                    'type': 'existing',
                    'content': streaming_msg.content,
                    'message_id': streaming_msg.id
                }, ensure_ascii=False)
                yield f"data: {data}\n\n".encode('utf-8')
            
            queue = await manager.connect(session_id)
            
            try:
                while True:
                    try:
                        message = await asyncio.wait_for(queue.get(), timeout=30.0)
                        data = json.dumps(message, ensure_ascii=False)
                        yield f"data: {data}\n\n".encode('utf-8')
                        
                        if message.get('type') in ['done', 'error']:
                            logger.info(f"Stream completed for session {session_id}")
                            break
                    except asyncio.TimeoutError:
                        yield b": heartbeat\n\n"
                        
            finally:
                await manager.disconnect(session_id, queue)
                
        except Exception as e:
            logger.error(f"Error in SSE stream: {str(e)}", exc_info=True)
            error_data = json.dumps({'type': 'error', 'message': str(e)}, ensure_ascii=False)
            yield f"data: {error_data}\n\n".encode('utf-8')
    
    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream; charset=utf-8",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
            "Access-Control-Allow-Origin": "*",
        }
    )