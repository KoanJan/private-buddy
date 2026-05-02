"""
Agent API endpoints.

This module provides REST API endpoints for agent management,
including CRUD operations with proper cascade deletion handling.
"""

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from typing import List
from app.database import get_db
from app.models.agent import Agent
from app.models.session import Session as SessionModel
from app.models.message import Message
from app.models.historical_summary import HistoricalSummary
from app.models.interaction import Interaction
from app.schemas.agent import (
    AgentCreate,
    AgentUpdate,
    AgentResponse,
    AgentWithSessions
)
from app.services.task.workspace import remove_session_workspace, get_avatars_dir
from app.utils.crud import CRUDBase
from app.logger import logger

router = APIRouter(prefix="/api/agents", tags=["agents"])
crud = CRUDBase[Agent, AgentCreate, AgentUpdate](Agent, "Agent")


@router.post("/", response_model=AgentResponse)
def create_agent(
    agent: AgentCreate,
    db: Session = Depends(get_db)
):
    return crud.create(db, agent)


@router.get("/", response_model=List[AgentResponse])
def list_agents(
    skip: int = 0,
    limit: int = 100,
    db: Session = Depends(get_db)
):
    return crud.get_multi(db, skip, limit)


@router.get("/with-sessions", response_model=List[AgentWithSessions])
def list_agents_with_sessions(
    db: Session = Depends(get_db)
):
    agents = db.query(Agent).order_by(Agent.updated_at.desc()).all()
    
    if not agents:
        return []
    
    agent_ids = [agent.id for agent in agents]
    all_sessions = db.query(SessionModel).filter(
        SessionModel.agent_id.in_(agent_ids)
    ).order_by(SessionModel.updated_at.desc()).all()
    
    sessions_by_agent = {}
    for session in all_sessions:
        if session.agent_id not in sessions_by_agent:
            sessions_by_agent[session.agent_id] = []
        sessions_by_agent[session.agent_id].append(session)
    
    result = []
    for agent in agents:
        agent_data = AgentWithSessions(
        id=agent.id,
        name=agent.name,
        character_settings=agent.character_settings,
        llm_config_id=agent.llm_config_id,
        description=agent.description,
        avatar=agent.avatar,
        created_at=agent.created_at,
        updated_at=agent.updated_at,
        sessions=sessions_by_agent.get(agent.id, [])
    )
        result.append(agent_data)
    
    return result


@router.get("/{agent_id}", response_model=AgentResponse)
def get_agent(
    agent_id: int,
    db: Session = Depends(get_db)
):
    return crud.get(db, agent_id)


@router.put("/{agent_id}", response_model=AgentResponse)
def update_agent(
    agent_id: int,
    agent_update: AgentUpdate,
    db: Session = Depends(get_db)
):
    db_agent = crud.get(db, agent_id)
    return crud.update(db, db_agent, agent_update)


@router.delete("/{agent_id}")
def delete_agent(
    agent_id: int,
    db: Session = Depends(get_db)
):
    """
    Delete an agent and all its associated data.
    
    This endpoint handles cascade deletion at the application layer:
    1. Find all sessions for this agent
    2. For each session, delete interactions, historical summaries and messages
    3. Delete all sessions for this agent
    4. Remove workspace directories for all sessions
    5. Delete the agent itself
    
    This follows the project rule: data constraints should be handled
    at the application layer, not at the database layer.
    """
    logger.info(f"Deleting agent {agent_id} and its associated data")
    
    # Verify agent exists
    agent = crud.get(db, agent_id)
    
    # Clean up avatar file if exists
    if agent.avatar:
        avatar_path = get_avatars_dir() / agent.avatar
        if avatar_path.exists():
            avatar_path.unlink()
            logger.info(f"Removed avatar file: {agent.avatar}")
    
    # Find all sessions for this agent
    session_ids = [s.id for s in db.query(SessionModel).filter(
        SessionModel.agent_id == agent_id
    ).all()]
    
    if session_ids:
        # Delete interactions for all sessions
        deleted_interactions = db.query(Interaction).filter(
            Interaction.session_id.in_(session_ids)
        ).delete(synchronize_session=False)
        logger.info(f"Deleted {deleted_interactions} interactions for agent {agent_id}")
        
        # Delete historical summaries for all sessions
        deleted_summaries = db.query(HistoricalSummary).filter(
            HistoricalSummary.session_id.in_(session_ids)
        ).delete(synchronize_session=False)
        logger.info(f"Deleted {deleted_summaries} historical summaries for agent {agent_id}")
        
        # Delete messages for all sessions
        deleted_messages = db.query(Message).filter(
            Message.session_id.in_(session_ids)
        ).delete(synchronize_session=False)
        logger.info(f"Deleted {deleted_messages} messages for agent {agent_id}")
        
        # Delete all sessions
        deleted_sessions = db.query(SessionModel).filter(
            SessionModel.agent_id == agent_id
        ).delete()
        logger.info(f"Deleted {deleted_sessions} sessions for agent {agent_id}")
        
        # Remove workspace directories for all sessions
        for sid in session_ids:
            if remove_session_workspace(sid):
                logger.info(f"Removed workspace directory for session {sid}")
    
    # Delete the agent itself
    db.delete(agent)
    db.commit()
    
    logger.info(f"Agent {agent_id} deleted successfully")
    return {"message": "Agent deleted successfully"}
