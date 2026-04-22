"""
Data service module for database operations.

This module provides a centralized layer for common database operations,
abstracting away the details of SQLAlchemy queries. It follows the
repository pattern to keep business logic separate from data access.

Main responsibilities:
- Session, agent, and LLM config retrieval
- Message history loading with pagination
"""

from sqlalchemy.orm import Session
from typing import List, Dict, Optional
from app.models.session import Session as SessionModel
from app.models.message import Message
from app.models.agent import Agent
from app.models.llm_config import LLMConfig
from app.logger import logger


class DataService:
    """
    Service for common database operations.
    
    This class provides static methods for retrieving and querying
    database entities, centralizing data access logic.
    """
    
    @staticmethod
    def get_session(db: Session, session_id: int) -> Optional[SessionModel]:
        """
        Get a session by ID.
        
        Args:
            db: Database session
            session_id: Session ID
            
        Returns:
            Session model if found, None otherwise
        """
        session = db.query(SessionModel).filter(SessionModel.id == session_id).first()
        if not session:
            logger.error(f"Session {session_id} not found")
        return session

    @staticmethod
    def get_agent(db: Session, agent_id: int) -> Optional[Agent]:
        """
        Get an agent by ID.
        
        Args:
            db: Database session
            agent_id: Agent ID
            
        Returns:
            Agent model if found, None otherwise
        """
        agent = db.query(Agent).filter(Agent.id == agent_id).first()
        if not agent:
            logger.error(f"Agent {agent_id} not found")
        return agent

    @staticmethod
    def get_llm_config(db: Session, llm_config_id: int) -> Optional[LLMConfig]:
        """
        Get an LLM configuration by ID.
        
        Args:
            db: Database session
            llm_config_id: LLM config ID
            
        Returns:
            LLMConfig model if found, None otherwise
        """
        llm_config = db.query(LLMConfig).filter(LLMConfig.id == llm_config_id).first()
        if not llm_config:
            logger.error(f"LLM config {llm_config_id} not found")
        return llm_config

    @staticmethod
    def get_message_history(
        db: Session,
        session_id: int,
        before_message_id: Optional[int] = None,
        limit: Optional[int] = None
    ) -> List[Dict[str, str]]:
        """
        Get message history for a session.
        
        This method retrieves messages in chronological order, with optional
        pagination by message ID and count limit.
        
        Args:
            db: Database session
            session_id: Session ID
            before_message_id: Get messages before this ID (exclusive)
            limit: Maximum number of messages to retrieve
            
        Returns:
            List of message dictionaries with 'role' and 'content' keys
        """
        query = db.query(Message).filter(
            Message.session_id == session_id
        )
        
        # Filter by message ID if specified
        if before_message_id:
            query = query.filter(Message.id < before_message_id)
        
        # Apply limit and ordering
        if limit:
            # Get newest messages first, then reverse for chronological order
            query = query.order_by(Message.id.desc()).limit(limit)
            messages = list(reversed(query.all()))
        else:
            # Get all messages in chronological order
            messages = query.order_by(Message.created_at.asc()).all()
        
        # Convert to dictionary format
        history = [
            {"role": msg.role, "content": msg.content}
            for msg in messages
        ]
        
        logger.info(f"Loaded {len(history)} historical messages for session {session_id}")
        return history
