"""
Search engine configuration model.

Stores a single search engine configuration (id=1).
When is_active is False or api_key is empty, the search tool is not available.
"""

from sqlalchemy import Column, Integer, String, DateTime, Text, Boolean
from sqlalchemy.sql import func

from app.database import Base


class SearchConfig(Base):
    """
    Search engine configuration model.

    Only one record exists (id=1). When is_active is False or api_key is empty,
    the WebSearchTool will not be added to the agent's tool list.
    """

    __tablename__ = "search_config"

    id = Column(Integer, primary_key=True, default=1)
    provider = Column(String(50), nullable=False, default='tavily')
    api_key = Column(String(255), nullable=False, default='')
    description = Column(Text, nullable=False, default='')
    is_active = Column(Boolean, nullable=False, default=False)
    updated_at = Column(DateTime(timezone=True), onupdate=func.now())

    def is_available(self) -> bool:
        """
        Check if the search engine is available for use.

        Returns:
            True if is_active is True and api_key is not empty.
        """
        return self.is_active and bool(self.api_key)
