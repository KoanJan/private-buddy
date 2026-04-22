from sqlalchemy import Column, Integer, String, DateTime
from sqlalchemy.orm import relationship
from sqlalchemy.sql import func
from app.database import Base


SESSION_STATUS_STREAMING = 0
SESSION_STATUS_IDLE = 1


class Session(Base):
    __tablename__ = "sessions"

    id = Column(Integer, primary_key=True, index=True)
    title = Column(String(255), nullable=False, default='')
    agent_id = Column(Integer, nullable=False, index=True)
    status = Column(Integer, default=SESSION_STATUS_IDLE, nullable=False)
    created_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now(), nullable=False)

    messages = relationship(
        "Message",
        back_populates="session",
        primaryjoin="Session.id == Message.session_id",
        foreign_keys="Message.session_id"
    )