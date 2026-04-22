"""
Embedding config API endpoints.

This module provides REST API endpoints for embedding configuration management,
including CRUD operations with reference handling on deletion.
"""

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from typing import List
from app.database import get_db
from app.models.embedding_config import EmbeddingConfig
from app.models.agent import Agent
from app.schemas.embedding_config import (
    EmbeddingConfigCreate,
    EmbeddingConfigUpdate,
    EmbeddingConfigResponse
)
from app.utils.crud import CRUDBase
from app.logger import logger

router = APIRouter(prefix="/api/embedding-configs", tags=["embedding-configs"])
crud = CRUDBase[EmbeddingConfig, EmbeddingConfigCreate, EmbeddingConfigUpdate](
    EmbeddingConfig, "Embedding config"
)


@router.post("/", response_model=EmbeddingConfigResponse)
def create_embedding_config(
    config: EmbeddingConfigCreate,
    db: Session = Depends(get_db)
):
    return crud.create(db, config)


@router.get("/", response_model=List[EmbeddingConfigResponse])
def list_embedding_configs(
    skip: int = 0,
    limit: int = 100,
    db: Session = Depends(get_db)
):
    return crud.get_multi(db, skip, limit)


@router.get("/{config_id}", response_model=EmbeddingConfigResponse)
def get_embedding_config(
    config_id: int,
    db: Session = Depends(get_db)
):
    return crud.get(db, config_id)


@router.put("/{config_id}", response_model=EmbeddingConfigResponse)
def update_embedding_config(
    config_id: int,
    config_update: EmbeddingConfigUpdate,
    db: Session = Depends(get_db)
):
    db_config = crud.get(db, config_id)
    return crud.update(db, db_config, config_update)


@router.delete("/{config_id}")
def delete_embedding_config(
    config_id: int,
    db: Session = Depends(get_db)
):
    """
    Delete an embedding configuration.
    
    This endpoint handles references from agents by resetting their
    embedding_config_id to 0 (default) before deletion. This is safe
    because embedding_config_id=0 means "use default embedding config".
    
    This follows the project rule: data constraints should be handled
    at the application layer, not at the database layer.
    """
    logger.info(f"Deleting embedding config {config_id}")
    
    # Verify config exists
    config = crud.get(db, config_id)
    
    # Reset embedding_config_id to 0 for all referencing agents
    updated_agents = db.query(Agent).filter(
        Agent.embedding_config_id == config_id
    ).update({"embedding_config_id": 0}, synchronize_session=False)
    
    if updated_agents > 0:
        logger.info(f"Reset embedding_config_id to 0 for {updated_agents} agent(s)")
    
    # Delete the config
    db.delete(config)
    db.commit()
    
    logger.info(f"Embedding config {config_id} deleted successfully")
    return {"message": "Embedding config deleted successfully"}
