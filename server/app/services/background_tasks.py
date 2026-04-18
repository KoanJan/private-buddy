from sqlalchemy.orm import Session
from app.models.session import Session as SessionModel, SESSION_STATUS_STREAMING, SESSION_STATUS_IDLE
from app.models.message import Message, MESSAGE_STATUS_STREAMING, MESSAGE_STATUS_COMPLETED, MESSAGE_STATUS_FAILED
from app.services.llm_service import LLMService
from app.services.data_service import DataService
from app.services.connection_manager import manager
from app.database import SessionLocal
from app.logger import logger
import asyncio


async def process_chat_task(
    session_id: int,
    user_message_id: int,
    ai_message_id: int
):
    db = SessionLocal()
    try:
        logger.info(f"Starting background chat task for session {session_id}")
        
        session = DataService.get_session(db, session_id)
        if not session:
            return
        
        agent = DataService.get_agent(db, session.agent_id)
        if not agent:
            ai_msg = db.query(Message).filter(Message.id == ai_message_id).first()
            if ai_msg:
                ai_msg.status = MESSAGE_STATUS_FAILED
                ai_msg.content = "Error: Agent not found"
                db.commit()
            return
        
        llm_config = DataService.get_llm_config(db, agent.llm_config_id)
        if not llm_config:
            ai_msg = db.query(Message).filter(Message.id == ai_message_id).first()
            if ai_msg:
                ai_msg.status = MESSAGE_STATUS_FAILED
                ai_msg.content = "Error: LLM config not found"
                db.commit()
            return
        
        history = DataService.get_message_history(db, session_id, ai_message_id)
        
        user_msg = db.query(Message).filter(Message.id == user_message_id).first()
        if not user_msg:
            logger.error(f"User message {user_message_id} not found")
            return
        
        chat_model = LLMService.create_chat_model(llm_config)
        langchain_messages = LLMService.build_messages(
            agent.system_prompt if agent.system_prompt else None,
            history,
            user_msg.content
        )
        
        logger.info("Starting LLM stream in background...")
        full_response = ""
        
        async for chunk in chat_model.astream(langchain_messages):
            if chunk.content:
                full_response += chunk.content
                
                ai_msg = db.query(Message).filter(Message.id == ai_message_id).first()
                if ai_msg:
                    ai_msg.content = full_response
                    db.commit()
                
                await manager.notify(session_id, {
                    'type': 'chunk',
                    'content': chunk.content
                })
        
        ai_msg = db.query(Message).filter(Message.id == ai_message_id).first()
        if ai_msg:
            ai_msg.status = MESSAGE_STATUS_COMPLETED
            ai_msg.content = full_response
            db.commit()
        
        session = DataService.get_session(db, session_id)
        if session:
            session.status = SESSION_STATUS_IDLE
            db.commit()
        
        await manager.notify(session_id, {'type': 'done'})
        
        logger.info(f"Background chat task completed for session {session_id}, response length: {len(full_response)}")
        
    except Exception as e:
        logger.error(f"Error in background chat task: {str(e)}", exc_info=True)
        
        try:
            ai_msg = db.query(Message).filter(Message.id == ai_message_id).first()
            if ai_msg:
                ai_msg.status = MESSAGE_STATUS_FAILED
                ai_msg.content = f"Error: {str(e)}"
                db.commit()
            
            session = DataService.get_session(db, session_id)
            if session:
                session.status = SESSION_STATUS_IDLE
                db.commit()
            
            await manager.notify(session_id, {
                'type': 'error',
                'message': str(e)
            })
        except Exception as inner_e:
            logger.error(f"Error handling failure: {str(inner_e)}", exc_info=True)
    
    finally:
        db.close()