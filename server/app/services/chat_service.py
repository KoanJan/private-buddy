from sqlalchemy.orm import Session
from typing import List, Dict, Optional, AsyncGenerator
from app.models.session import Session as SessionModel
from app.models.message import Message
from app.models.llm_config import LLMConfig
from app.services.llm_service import LLMService
from app.services.data_service import DataService
from app.logger import logger
import time


class ChatService:
    @staticmethod
    async def chat(
        db: Session,
        session_id: int,
        user_message: str
    ) -> AsyncGenerator[str, None]:
        logger.info(f"ChatService.chat called - session_id: {session_id}, message: {user_message[:50]}...")
        
        try:
            session = DataService.get_session(db, session_id)
            if not session:
                raise ValueError("Session not found")
            
            logger.info(f"Session found: {session.title}")
            
            llm_config = None
            if session.llm_config_id:
                llm_config = DataService.get_llm_config(db, session.llm_config_id)
            
            if not llm_config:
                raise ValueError("LLM config not found")
            
            logger.info(f"Using LLM config: {llm_config.name}")
            
            history = DataService.get_message_history(db, session_id)
            
            chat_model = LLMService.create_chat_model(llm_config)
            langchain_messages = LLMService.build_messages(
                session.system_prompt,
                history,
                user_message
            )
            
            logger.info("Starting LLM stream...")
            full_response = ""
            chunk_count = 0
            start_time = time.time()
            
            async for chunk in chat_model.astream(langchain_messages):
                if chunk.content:
                    chunk_count += 1
                    chunk_time = time.time() - start_time
                    logger.debug(f"Chunk #{chunk_count} at {chunk_time:.2f}s: {chunk.content[:30]}...")
                    full_response += chunk.content
                    yield chunk.content
            
            total_time = time.time() - start_time
            logger.info(f"LLM stream completed - {chunk_count} chunks in {total_time:.2f}s, response length: {len(full_response)}")
            
            user_msg = Message(
                session_id=session_id,
                role="user",
                content=user_message
            )
            db.add(user_msg)
            
            assistant_msg = Message(
                session_id=session_id,
                role="assistant",
                content=full_response
            )
            db.add(assistant_msg)
            db.commit()
            
            logger.info("Messages saved to database")
            
        except Exception as e:
            logger.error(f"Error in ChatService.chat: {str(e)}", exc_info=True)
            raise