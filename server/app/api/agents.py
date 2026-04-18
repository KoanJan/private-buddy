from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from typing import List
from app.database import get_db
from app.models.agent import Agent
from app.models.session import Session as SessionModel
from app.schemas.agent import (
    AgentCreate,
    AgentUpdate,
    AgentResponse,
    AgentWithSessions
)
from app.utils.crud import CRUDBase

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
            system_prompt=agent.system_prompt,
            llm_config_id=agent.llm_config_id,
            description=agent.description,
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
    return crud.delete(db, agent_id)