from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from typing import List
from app.database import get_db
from app.models.session import Session as SessionModel
from app.schemas.session import (
    SessionCreate,
    SessionUpdate,
    SessionResponse
)
from app.utils.crud import CRUDBase

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
    return crud.delete(db, session_id)