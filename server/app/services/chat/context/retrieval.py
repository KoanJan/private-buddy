"""
Context retrieval module for gathering conversation context.

This module handles the retrieval of context components needed for
LLM processing:
- Recent messages from the session
- RAG-based relevant segments from vector store
- Latest summary from the summary system
- Cached narrative from the summary record

The retrieval process uses built-in BGE-base-zh embedding model (768 dimensions).
Narrative is retrieved from the summary record's narrative field (cached,
generated in background alongside summary), eliminating the need for
real-time narrative generation during chat processing.
"""

from sqlalchemy.orm import Session
from typing import List, Dict, Any, Optional
from app.models.message import Message
from app.services.chat.context.summary import SummaryService
from app.services.chat.context.vector_store import VectorStoreService
from app.services.data_service import DataService
from app.logger import logger


class RetrievalService:
    """
    Service for retrieving context components for chat processing.
    
    This service coordinates the retrieval of:
    - Recent messages (with optional status filter)
    - RAG segments (using built-in BGE-base-zh embedding)
    - Latest summary (if available)
    - Cached narrative (from summary record, if available)
    """

    @staticmethod
    def get_recent_messages(
        db: Session,
        session_id: int,
        limit: int = 10,
        status: Optional[int] = None
    ) -> List[Dict[str, Any]]:
        """
        Get recent messages from a session.
        
        Messages are returned in chronological order (oldest first).
        Optionally filter by message status.
        
        Args:
            db: Database session
            session_id: Session ID
            limit: Maximum number of messages to retrieve
            status: Optional message status filter
            
        Returns:
            List of message dictionaries with 'role', 'content', and 'id' keys
        """
        query = db.query(Message).filter(
            Message.session_id == session_id
        )
        
        if status is not None:
            query = query.filter(Message.status == status)
        
        messages = query.order_by(Message.id.desc()).limit(limit).all()
        messages = list(reversed(messages))

        return [
            {"role": msg.role, "content": msg.content, "id": msg.id}
            for msg in messages
        ]

    @staticmethod
    def _build_summary_and_narrative(
        latest_summary
    ) -> tuple[Optional[Dict[str, Any]], Optional[str]]:
        """
        Extract summary dict and cached narrative from a HistoricalSummary record.
        
        Narrative retrieval follows the same versioning policy as summary:
        get the latest available version without requiring alignment with
        current message count. If narrative field is empty (not yet generated
        or generation failed), returns None for narrative.
        
        Args:
            latest_summary: HistoricalSummary record, or None
            
        Returns:
            Tuple of (summary_dict, narrative_text).
            summary_dict is None if no summary exists.
            narrative_text is None if no narrative exists or field is empty.
        """
        if not latest_summary:
            return None, None

        summary_dict = {
            "version": latest_summary.version,
            "content": latest_summary.content
        }
        narrative = latest_summary.narrative if latest_summary.narrative else None

        return summary_dict, narrative

    @staticmethod
    def get_context_without_rag(
        db: Session,
        session_id: int,
        recent_count: int = 10
    ) -> Dict[str, Any]:
        """
        Get context without RAG retrieval.
        
        Used for queries that don't need RAG (e.g., greetings, chitchat).
        Retrieves recent messages, latest summary, and cached narrative.
        
        Args:
            db: Database session
            session_id: Session ID
            recent_count: Number of recent messages to retrieve
            
        Returns:
            Dictionary with 'recent_messages', 'relevant_segments', 'summary',
            and 'narrative' keys
        """
        result: Dict[str, Any] = {
            "recent_messages": [],
            "relevant_segments": [],
            "summary": None,
            "narrative": None
        }

        from app.models.message import MESSAGE_STATUS_COMPLETED
        
        result["recent_messages"] = RetrievalService.get_recent_messages(
            db, session_id, recent_count, status=MESSAGE_STATUS_COMPLETED
        )

        latest_summary = SummaryService.get_latest_summary(db, session_id)
        result["summary"], result["narrative"] = RetrievalService._build_summary_and_narrative(latest_summary)

        return result

    @staticmethod
    def get_context_for_chat(
        db: Session,
        session_id: int,
        query: str,
        recent_count: int = 10,
        rag_count: int = 5
    ) -> Dict[str, Any]:
        """
        Get full context for chat processing with RAG.
        
        This method retrieves all context components:
        1. Recent messages from the session
        2. RAG segments relevant to the query (if embedding configured)
        3. Latest summary (if available)
        4. Cached narrative from summary record (if available)
        
        Args:
            db: Database session
            session_id: Session ID
            query: Processed query for RAG search
            recent_count: Number of recent messages to retrieve
            rag_count: Number of RAG segments to retrieve
            
        Returns:
            Dictionary with 'recent_messages', 'relevant_segments', 
            'summary', 'narrative', and 'has_embedding' keys
        """
        result: Dict[str, Any] = {
            "recent_messages": [],
            "relevant_segments": [],
            "summary": None,
            "narrative": None
        }

        from app.models.message import MESSAGE_STATUS_COMPLETED
        
        result["recent_messages"] = RetrievalService.get_recent_messages(
            db, session_id, recent_count, status=MESSAGE_STATUS_COMPLETED
        )

        try:
            vector_store = VectorStoreService.get_instance(session_id)
            search_results = vector_store.search(query, k=rag_count)
            result["relevant_segments"] = search_results
            logger.info(f"RAG retrieved {len(search_results)} segments for session {session_id}")
        except Exception as e:
            logger.error(f"RAG retrieval failed: {str(e)}", exc_info=True)

        latest_summary = SummaryService.get_latest_summary(db, session_id)
        result["summary"], result["narrative"] = RetrievalService._build_summary_and_narrative(latest_summary)

        return result

    @staticmethod
    def index_messages(
        db: Session,
        session_id: int,
        message_ids: List[int]
    ) -> bool:
        """
        Index messages in the vector store for RAG retrieval.
        
        This method adds messages to the vector store after they are
        completed, enabling future RAG retrieval.
        
        Args:
            db: Database session
            session_id: Session ID
            message_ids: List of message IDs to index
            
        Returns:
            True if indexing succeeded, False otherwise
        """
        messages = db.query(Message).filter(
            Message.id.in_(message_ids),
            Message.session_id == session_id
        ).all()

        if not messages:
            logger.warning(f"No messages found for indexing in session {session_id}")
            return False

        try:
            vector_store = VectorStoreService.get_instance(session_id)

            contents = [msg.content for msg in messages]
            metadatas = [
                {"role": msg.role, "message_id": msg.id}
                for msg in messages
            ]

            vector_store.add_messages(
                message_ids=[msg.id for msg in messages],
                contents=contents,
                metadatas=metadatas
            )

            logger.info(f"Indexed {len(messages)} messages for session {session_id}")
            return True

        except Exception as e:
            logger.error(f"Failed to index messages: {str(e)}", exc_info=True)
            return False
