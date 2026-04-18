from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session
from typing import AsyncGenerator
from app.database import get_db
from app.services.chat_service import ChatService
from app.logger import logger
import json

router = APIRouter(prefix="/api/chat", tags=["chat-sse"])


@router.get("/stream/{session_id}")
async def chat_stream(
    session_id: int,
    message: str,
    db: Session = Depends(get_db)
):
    """
    SSE streaming chat endpoint
    """
    logger.info(f"SSE stream request - session_id: {session_id}, message: {message}")
    
    async def event_generator() -> AsyncGenerator[bytes, None]:
        try:
            logger.info(f"Starting chat stream for session {session_id}")
            
            yield b": connected\n\n"
            
            async for chunk in ChatService.chat(db, session_id, message):
                logger.debug(f"Sending chunk: {chunk[:50]}...")
                data = json.dumps({'type': 'chunk', 'content': chunk}, ensure_ascii=False)
                yield f"data: {data}\n\n".encode('utf-8')
            
            logger.info(f"Chat stream completed for session {session_id}")
            done_data = json.dumps({'type': 'done'}, ensure_ascii=False)
            yield f"data: {done_data}\n\n".encode('utf-8')
        except Exception as e:
            logger.error(f"Error in chat stream: {str(e)}", exc_info=True)
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