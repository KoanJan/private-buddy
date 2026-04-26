"""
Search service module.

Provides search engine configuration management.
"""

from typing import Optional

from sqlalchemy.orm import Session

from app.models.search_config import SearchConfig
from app.logger import logger


class SearchService:
    """
    Service for managing search engine configuration.

    The search_config table contains only one record (id=1).
    This service provides methods to get and update this configuration.
    """

    @staticmethod
    def get_config(db: Session) -> Optional[SearchConfig]:
        """
        Get the search engine configuration.

        Args:
            db: Database session.

        Returns:
            SearchConfig instance or None if not found.
        """
        config = db.query(SearchConfig).filter(SearchConfig.id == 1).first()
        if not config:
            logger.warning("SearchConfig not found, creating default")
            config = SearchConfig(
                id=1,
                provider='tavily',
                api_key='',
                description='',
                is_active=False
            )
            db.add(config)
            db.commit()
            db.refresh(config)
        return config

    @staticmethod
    def update_config(
        db: Session,
        provider: Optional[str] = None,
        api_key: Optional[str] = None,
        description: Optional[str] = None,
        is_active: Optional[bool] = None,
    ) -> SearchConfig:
        """
        Update the search engine configuration.

        Args:
            db: Database session.
            provider: Search provider (tavily, duckduckgo).
            api_key: API key for the search provider.
            description: Description of the configuration.
            is_active: Whether the search engine is enabled.

        Returns:
            Updated SearchConfig instance.
        """
        config = SearchService.get_config(db)

        if provider is not None:
            config.provider = provider
        if api_key is not None:
            config.api_key = api_key
        if description is not None:
            config.description = description
        if is_active is not None:
            config.is_active = is_active

        db.commit()
        db.refresh(config)

        logger.info(
            f"SearchConfig updated: provider={config.provider}, "
            f"is_active={config.is_active}, has_api_key={bool(config.api_key)}"
        )

        return config
