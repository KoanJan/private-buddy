from sqlalchemy.orm import Session
from typing import List, Dict, Optional
from app.models.session import Session as SessionModel
from app.models.message import Message
from app.models.agent import Agent
from app.models.llm_config import LLMConfig
from app.logger import logger


class DataService:
    @staticmethod
    def get_session(db: Session, session_id: int) -> Optional[SessionModel]:
        session = db.query(SessionModel).filter(SessionModel.id == session_id).first()
        if not session:
            logger.error(f"Session {session_id} not found")
        return session

    @staticmethod
    def get_agent(db: Session, agent_id: int) -> Optional[Agent]:
        agent = db.query(Agent).filter(Agent.id == agent_id).first()
        if not agent:
            logger.error(f"Agent {agent_id} not found")
        return agent

    @staticmethod
    def get_llm_config(db: Session, llm_config_id: int) -> Optional[LLMConfig]:
        llm_config = db.query(LLMConfig).filter(LLMConfig.id == llm_config_id).first()
        if not llm_config:
            logger.error(f"LLM config {llm_config_id} not found")
        return llm_config

    @staticmethod
    def get_message_history(
        db: Session,
        session_id: int,
        before_message_id: Optional[int] = None
    ) -> List[Dict[str, str]]:
        query = db.query(Message).filter(
            Message.session_id == session_id
        )
        
        if before_message_id:
            query = query.filter(Message.id < before_message_id)
        
        messages = query.order_by(Message.created_at.asc()).all()
        
        history = [
            {"role": msg.role, "content": msg.content}
            for msg in messages
        ]
        
        logger.info(f"Loaded {len(history)} historical messages for session {session_id}")
        return history
