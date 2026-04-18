from sqlalchemy import Column, Integer, String, Text, DateTime
from sqlalchemy.orm import relationship
from sqlalchemy.sql import func
from app.database import Base


class Agent(Base):
    __tablename__ = "agents"

    id = Column(Integer, primary_key=True, index=True)
    name = Column(String(255), nullable=False)
    system_prompt = Column(Text, nullable=False, default='')
    llm_config_id = Column(Integer, nullable=False, index=True)
    description = Column(Text, nullable=False, default='')
    created_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)
    updated_at = Column(DateTime(timezone=True), onupdate=func.now())

    llm_config = relationship(
        "LLMConfig",
        back_populates="agents",
        primaryjoin="Agent.llm_config_id == LLMConfig.id",
        foreign_keys="Agent.llm_config_id"
    )