"""
API endpoints for agent-world interaction records.

Provides:
- GET /api/interactions?agent_msg_id={id} - Get interactions for an agent message
- GET /api/messages/{message_id}/interaction-status - Get has_interactions status
"""

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from typing import List
from pydantic import BaseModel
from datetime import datetime

from app.database import get_db
from app.models.interaction import Interaction, INTERACTION_TYPE_REQUEST, INTERACTION_TYPE_RESPONSE
from app.models.message import Message, HAS_INTERACTIONS_PENDING, HAS_INTERACTIONS_EXISTS, HAS_INTERACTIONS_NONE
from app.logger import logger

router = APIRouter(prefix="/api", tags=["interactions"])


class InteractionResponse(BaseModel):
    """Response schema for a single interaction record."""
    id: int
    session_id: int
    user_msg_id: int
    agent_msg_id: int
    iteration: int
    type: int
    timestamp: datetime
    data: str
    created_at: datetime

    class Config:
        from_attributes = True


class InteractionListResponse(BaseModel):
    """Response schema for a list of interaction records."""
    interactions: List[InteractionResponse]


class InteractionStatusResponse(BaseModel):
    """Response schema for message interaction status."""
    has_interactions: int


@router.get("/interactions", response_model=InteractionListResponse)
def get_interactions(
    agent_msg_id: int,
    db: Session = Depends(get_db),
):
    """
    Get interaction records for an agent message.

    Results are ordered by iteration, then type.
    """
    interactions = db.query(Interaction).filter(
        Interaction.agent_msg_id == agent_msg_id
    ).order_by(Interaction.iteration, Interaction.type).all()

    logger.info(f"Get interactions: agent_msg_id={agent_msg_id}, count={len(interactions)}")

    return InteractionListResponse(interactions=interactions)


@router.get("/messages/{message_id}/interaction-status", response_model=InteractionStatusResponse)
def get_interaction_status(
    message_id: int,
    db: Session = Depends(get_db),
):
    """
    Get the has_interactions status for a message.

    Returns:
        has_interactions: 0=pending, 1=has interactions, 2=no interactions
    """
    message = db.query(Message).filter(Message.id == message_id).first()
    if not message:
        raise HTTPException(status_code=404, detail="Message not found")

    return InteractionStatusResponse(has_interactions=message.has_interactions)
