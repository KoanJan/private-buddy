"""
Chat service module for direct chat processing.

This module provides a simple chat service that handles direct LLM interactions
without the context engineering pipeline.

Note: This service is deprecated. Use background_tasks.process_chat_task instead
for the full context engineering pipeline.
"""

from sqlalchemy.orm import Session
from typing import AsyncGenerator
from app.models.message import Message
from app.services.llm import LLMService
from app.services.data_service import DataService
from app.services.llm.llm_logger import TokenUsageLogger
from app.logger import logger


class ChatService:
    """
    Service for direct chat processing without context engineering.
    
    Note: This service is deprecated. Use background_tasks.process_chat_task
    instead for the full context engineering pipeline with summary, RAG, etc.
    """
    
    @staticmethod
    async def chat(
        db: Session,
        session_id: int,
        user_message: str
    ) -> AsyncGenerator[str, None]:
        """
        Process a chat message and stream the LLM response.
        
        This method handles direct LLM interactions without the context
        engineering pipeline. It loads the session's agent to get character
        settings and LLM configuration.
        
        Args:
            db: Database session
            session_id: Session ID for the chat
            user_message: The user's message text
            
        Yields:
            Chunks of the LLM response as they are generated
        """
        logger.info(f"ChatService.chat called - session_id: {session_id}, message: {user_message[:50]}...")
        
        try:
            session = DataService.get_session(db, session_id)
            if not session:
                raise ValueError("Session not found")
            
            logger.info(f"Session found: {session.title}")
            
            # Load agent to get character_settings and llm_config_id
            agent = DataService.get_agent(db, session.agent_id)
            if not agent:
                raise ValueError("Agent not found")
            
            llm_config = None
            if agent.llm_config_id:
                llm_config = DataService.get_llm_config(db, agent.llm_config_id)
            
            if not llm_config:
                raise ValueError("LLM config not found")
            
            logger.info(f"Using LLM config: {llm_config.name}")
            
            history = DataService.get_message_history(db, session_id)
            
            chat_model = LLMService.create_chat_model(llm_config)
            langchain_messages = LLMService.build_messages(
                agent.character_settings if agent.character_settings else None,
                history,
                user_message
            )
            
            logger.info("Starting LLM stream...")
            full_response = ""
            
            async for chunk in chat_model.astream(langchain_messages, config={"callbacks": [TokenUsageLogger()]}):
                if chunk.content:
                    full_response += chunk.content
                    yield chunk.content
            
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
