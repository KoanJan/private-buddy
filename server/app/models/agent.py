from sqlalchemy import Column, Integer, String, Text, DateTime
from sqlalchemy.orm import relationship
from sqlalchemy.sql import func
from app.database import Base


class Agent(Base):
    """
    Agent model representing an AI assistant configuration.
    
    An agent defines the behavior and capabilities of an AI assistant,
    including its character settings (personality, style, identity)
    and the LLM/embedding configurations to use.
    """
    __tablename__ = "agents"

    id = Column(Integer, primary_key=True, index=True)
    name = Column(String(255), nullable=False)
    character_settings = Column(Text, nullable=False, default='')  # Agent's personality, style, identity
    llm_config_id = Column(Integer, nullable=False, index=True)
    embedding_config_id = Column(Integer, nullable=False, default=0, index=True)
    description = Column(Text, nullable=False, default='')
    created_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now(), nullable=False)

    llm_config = relationship(
        "LLMConfig",
        back_populates="agents",
        primaryjoin="Agent.llm_config_id == LLMConfig.id",
        foreign_keys="Agent.llm_config_id"
    )