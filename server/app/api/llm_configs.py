"""
LLM config API endpoints.

This module provides REST API endpoints for LLM configuration management,
including CRUD operations with reference checking on deletion.
"""

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session
from typing import List
from app.database import get_db
from app.models.llm_config import LLMConfig
from app.models.agent import Agent
from app.schemas.llm_config import (
    LLMConfigCreate,
    LLMConfigUpdate,
    LLMConfigResponse
)
from app.utils.crud import CRUDBase
from app.logger import logger

router = APIRouter(prefix="/api/llm-configs", tags=["llm-configs"])
crud = CRUDBase[LLMConfig, LLMConfigCreate, LLMConfigUpdate](
    LLMConfig, "LLM config"
)


@router.post("/", response_model=LLMConfigResponse)
def create_llm_config(
    config: LLMConfigCreate,
    db: Session = Depends(get_db)
):
    return crud.create(db, config)


@router.get("/", response_model=List[LLMConfigResponse])
def list_llm_configs(
    skip: int = 0,
    limit: int = 100,
    db: Session = Depends(get_db)
):
    return crud.get_multi(db, skip, limit)


@router.get("/{config_id}", response_model=LLMConfigResponse)
def get_llm_config(
    config_id: int,
    db: Session = Depends(get_db)
):
    return crud.get(db, config_id)


@router.put("/{config_id}", response_model=LLMConfigResponse)
def update_llm_config(
    config_id: int,
    config_update: LLMConfigUpdate,
    db: Session = Depends(get_db)
):
    db_config = crud.get(db, config_id)
    return crud.update(db, db_config, config_update)


@router.delete("/{config_id}")
def delete_llm_config(
    config_id: int,
    db: Session = Depends(get_db)
):
    """
    Delete an LLM configuration.
    
    This endpoint checks if the configuration is referenced by any agents
    before deletion. If referenced, deletion is rejected with a list of
    referencing agents.
    
    This follows the project rule: data constraints should be handled
    at the application layer, not at the database layer.
    """
    logger.info(f"Attempting to delete LLM config {config_id}")
    
    # Verify config exists
    config = crud.get(db, config_id)
    
    # Check if any agents reference this config
    referencing_agents = db.query(Agent).filter(
        Agent.llm_config_id == config_id
    ).all()
    
    if referencing_agents:
        agent_names = [agent.name for agent in referencing_agents]
        logger.warning(f"Cannot delete LLM config {config_id}: referenced by agents {agent_names}")
        raise HTTPException(
            status_code=400,
            detail=f"Cannot delete LLM config: it is referenced by {len(referencing_agents)} agent(s): {', '.join(agent_names)}"
        )
    
    # Safe to delete
    db.delete(config)
    db.commit()
    
    logger.info(f"LLM config {config_id} deleted successfully")
    return {"message": "LLM config deleted successfully"}
