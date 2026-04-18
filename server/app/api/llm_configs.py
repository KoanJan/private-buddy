from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from typing import List
from app.database import get_db
from app.models.llm_config import LLMConfig
from app.schemas.llm_config import (
    LLMConfigCreate,
    LLMConfigUpdate,
    LLMConfigResponse
)
from app.utils.crud import CRUDBase

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
    return crud.delete(db, config_id)
