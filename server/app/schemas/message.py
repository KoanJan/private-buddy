from pydantic import BaseModel
from typing import Optional
from datetime import datetime


class MessageBase(BaseModel):
    session_id: int
    role: str
    content: str


class MessageCreate(BaseModel):
    content: str


class MessageResponse(MessageBase):
    id: int
    status: int
    created_at: datetime
    updated_at: Optional[datetime] = None

    class Config:
        from_attributes = True