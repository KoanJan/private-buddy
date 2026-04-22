from pydantic import BaseModel
from typing import Optional
from datetime import datetime


class EmbeddingConfigBase(BaseModel):
    name: str
    model_id: str
    base_url: str
    api_key: str
    description: str = ''


class EmbeddingConfigCreate(EmbeddingConfigBase):
    pass


class EmbeddingConfigUpdate(BaseModel):
    name: Optional[str] = None
    model_id: Optional[str] = None
    base_url: Optional[str] = None
    api_key: Optional[str] = None
    description: Optional[str] = None


class EmbeddingConfigResponse(EmbeddingConfigBase):
    id: int
    created_at: datetime
    updated_at: Optional[datetime] = None

    class Config:
        from_attributes = True
