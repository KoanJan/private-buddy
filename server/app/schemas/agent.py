from pydantic import BaseModel
from typing import Optional, List
from datetime import datetime


class AgentBase(BaseModel):
    name: str
    system_prompt: str
    llm_config_id: int
    description: str


class AgentCreate(AgentBase):
    pass


class AgentUpdate(BaseModel):
    name: Optional[str] = None
    system_prompt: Optional[str] = None
    llm_config_id: Optional[int] = None
    description: Optional[str] = None


class AgentResponse(AgentBase):
    id: int
    created_at: datetime
    updated_at: Optional[datetime] = None

    class Config:
        from_attributes = True


class SessionBrief(BaseModel):
    id: int
    title: str
    status: int
    created_at: datetime
    updated_at: Optional[datetime] = None

    class Config:
        from_attributes = True


class AgentWithSessions(AgentResponse):
    sessions: List[SessionBrief] = []