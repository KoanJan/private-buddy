from sqlalchemy import Column, Integer, String, Text, DateTime
from sqlalchemy.orm import relationship
from datetime import datetime
from app.database import Base
from .base import LOCALTIME


class Agent(Base):
    """
    Agent model representing an AI assistant configuration.
    
    An agent defines the behavior and capabilities of an AI assistant,
    including its character settings (personality, style, identity)
    and the LLM configuration to use.
    
    Note: Embedding is now built-in using BGE-base-zh model (768 dimensions).
    No user configuration is required or supported.
    """
    __tablename__ = "agents"

    id = Column(Integer, primary_key=True, index=True)
    name = Column(String(255), nullable=False)
    character_settings = Column(Text, nullable=False, default='')  # Agent's personality, style, identity
    llm_config_id = Column(Integer, nullable=False, index=True)
    description = Column(Text, nullable=False, default='')
    avatar = Column(String(500), nullable=False, default='')  # Relative path under PrivateBuddyData/avatars/
    created_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, nullable=False)
    updated_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, onupdate=datetime.now, nullable=False)

    llm_config = relationship(
        "LLMConfig",
        back_populates="agents",
        primaryjoin="Agent.llm_config_id == LLMConfig.id",
        foreign_keys="Agent.llm_config_id"
    )