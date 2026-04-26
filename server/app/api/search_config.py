"""
Search configuration API endpoints.

Provides GET and PUT endpoints for the single search configuration record.
"""

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from app.database import get_db
from app.schemas.search_config import SearchConfigResponse, SearchConfigUpdate
from app.services.search import SearchService

router = APIRouter(prefix="/api/search-config", tags=["search-config"])


@router.get("", response_model=SearchConfigResponse)
def get_search_config(db: Session = Depends(get_db)):
    """
    Get the current search engine configuration.

    Returns:
        SearchConfigResponse with current configuration.
    """
    config = SearchService.get_config(db)
    return config


@router.put("", response_model=SearchConfigResponse)
def update_search_config(
    config_update: SearchConfigUpdate,
    db: Session = Depends(get_db),
):
    """
    Update the search engine configuration.

    Args:
        config_update: Fields to update.

    Returns:
        SearchConfigResponse with updated configuration.
    """
    config = SearchService.update_config(
        db=db,
        provider=config_update.provider,
        api_key=config_update.api_key,
        description=config_update.description,
        is_active=config_update.is_active,
    )
    return config
