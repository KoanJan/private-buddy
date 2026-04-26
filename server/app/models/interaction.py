"""
Interaction model for agent-world interaction records.

Each interaction captures one step of the ReAct loop:
- type=1 (request): messages sent to the LLM
- type=2 (response): LLM output including thoughts, tool_calls, finish_reason

Interactions are grouped by (session_id, user_msg_id, agent_msg_id, iteration)
to support both frontend display and debugging.
"""

from sqlalchemy import Column, Integer, Text, DateTime, UniqueConstraint, Index
from sqlalchemy.sql import func
from app.database import Base


INTERACTION_TYPE_REQUEST = 1
INTERACTION_TYPE_RESPONSE = 2


class Interaction(Base):
    __tablename__ = "interactions"

    id = Column(Integer, primary_key=True, index=True)
    session_id = Column(Integer, nullable=False, comment="Owning session")
    user_msg_id = Column(Integer, nullable=False, comment="User message that triggered execution")
    agent_msg_id = Column(Integer, nullable=False, comment="Agent message that delivers the result")
    iteration = Column(Integer, nullable=False, comment="Iteration number within the execution")
    type = Column(Integer, nullable=False, comment="1=request, 2=response")
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False, comment="Time of the interaction")
    data = Column(Text, nullable=False, comment="JSON payload")
    created_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)

    __table_args__ = (
        UniqueConstraint(
            "session_id", "user_msg_id", "agent_msg_id", "iteration", "type",
            name="uk_interactions_session_user_agent_iter_type"
        ),
        Index("idx_interactions_session", "session_id"),
        Index("idx_interactions_user_msg", "user_msg_id"),
        Index("idx_interactions_agent_msg", "agent_msg_id"),
        Index("idx_interactions_session_iteration", "session_id", "iteration"),
    )
