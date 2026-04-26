"""
Search configuration schemas.
"""

from datetime import datetime
from typing import Optional

from pydantic import BaseModel


class SearchConfigBase(BaseModel):
    """Base schema for search configuration."""

    provider: str = "tavily"
    api_key: str = ""
    description: str = ""
    is_active: bool = False


class SearchConfigUpdate(BaseModel):
    """Schema for updating search configuration."""

    provider: Optional[str] = None
    api_key: Optional[str] = None
    description: Optional[str] = None
    is_active: Optional[bool] = None


class SearchConfigResponse(BaseModel):
    """Schema for search configuration response."""

    id: int
    provider: str
    api_key: str
    description: str
    is_active: bool
    updated_at: Optional[datetime] = None

    class Config:
        from_attributes = True
