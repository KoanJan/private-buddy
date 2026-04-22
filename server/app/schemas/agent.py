"""
Agent schemas for API request/response validation.

Defines the Pydantic models for agent creation, update, and response.
"""

from pydantic import BaseModel
from typing import Optional, List
from datetime import datetime


class AgentBase(BaseModel):
    """Base schema for agent with common fields."""
    name: str
    character_settings: str  # Agent's personality, style, identity
    llm_config_id: int
    embedding_config_id: int = 0
    description: str = ""


class AgentCreate(AgentBase):
    """Schema for creating a new agent."""
    pass


class AgentUpdate(BaseModel):
    """Schema for updating an existing agent. All fields are optional."""
    name: Optional[str] = None
    character_settings: Optional[str] = None
    llm_config_id: Optional[int] = None
    embedding_config_id: Optional[int] = None
    description: Optional[str] = None


class AgentResponse(AgentBase):
    """Schema for agent response with additional metadata."""
    id: int
    created_at: datetime
    updated_at: Optional[datetime] = None

    class Config:
        from_attributes = True


class SessionBrief(BaseModel):
    """Brief session information for agent's session list."""
    id: int
    title: str
    status: int
    created_at: datetime
    updated_at: Optional[datetime] = None

    class Config:
        from_attributes = True


class AgentWithSessions(AgentResponse):
    """Agent response with associated sessions."""
    sessions: List[SessionBrief] = []
