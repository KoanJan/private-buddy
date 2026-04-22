"""
Session API endpoints.

This module provides REST API endpoints for session management,
including CRUD operations with proper cascade deletion handling.
"""

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from typing import List
from app.database import get_db
from app.models.session import Session as SessionModel
from app.models.message import Message
from app.models.historical_summary import HistoricalSummary
from app.schemas.session import (
    SessionCreate,
    SessionUpdate,
    SessionResponse
)
from app.utils.crud import CRUDBase
from app.logger import logger

router = APIRouter(prefix="/api/sessions", tags=["sessions"])
crud = CRUDBase[SessionModel, SessionCreate, SessionUpdate](
    SessionModel, "Session"
)


@router.post("/", response_model=SessionResponse)
def create_session(
    session: SessionCreate,
    db: Session = Depends(get_db)
):
    return crud.create(db, session)


@router.get("/", response_model=List[SessionResponse])
def list_sessions(
    skip: int = 0,
    limit: int = 100,
    db: Session = Depends(get_db)
):
    return crud.get_multi(db, skip, limit)


@router.get("/{session_id}", response_model=SessionResponse)
def get_session(
    session_id: int,
    db: Session = Depends(get_db)
):
    return crud.get(db, session_id)


@router.put("/{session_id}", response_model=SessionResponse)
def update_session(
    session_id: int,
    session_update: SessionUpdate,
    db: Session = Depends(get_db)
):
    db_session = crud.get(db, session_id)
    return crud.update(db, db_session, session_update)


@router.delete("/{session_id}")
def delete_session(
    session_id: int,
    db: Session = Depends(get_db)
):
    """
    Delete a session and all its associated data.
    
    This endpoint handles cascade deletion at the application layer:
    1. Delete all historical summaries for the session
    2. Delete all messages for the session
    3. Delete the session itself
    
    This follows the project rule: data constraints should be handled
    at the application layer, not at the database layer.
    """
    logger.info(f"Deleting session {session_id} and its associated data")
    
    # Verify session exists
    session = crud.get(db, session_id)
    
    # Delete historical summaries (application-layer cascade)
    deleted_summaries = db.query(HistoricalSummary).filter(
        HistoricalSummary.session_id == session_id
    ).delete()
    logger.info(f"Deleted {deleted_summaries} historical summaries for session {session_id}")
    
    # Delete messages (application-layer cascade)
    deleted_messages = db.query(Message).filter(
        Message.session_id == session_id
    ).delete()
    logger.info(f"Deleted {deleted_messages} messages for session {session_id}")
    
    # Delete the session itself
    db.delete(session)
    db.commit()
    
    logger.info(f"Session {session_id} deleted successfully")
    return {"message": "Session deleted successfully"}
