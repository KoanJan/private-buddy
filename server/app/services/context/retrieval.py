"""
Context retrieval module for gathering conversation context.

This module handles the retrieval of context components needed for
LLM processing:
- Recent messages from the session
- RAG-based relevant segments from vector store
- Latest summary from the summary system

The retrieval process supports both RAG-enabled and RAG-disabled modes.
"""

from sqlalchemy.orm import Session
from typing import List, Dict, Any, Optional
from app.models.message import Message
from app.models.embedding_config import EmbeddingConfig
from app.services.context.summary import SummaryService
from app.services.context.vector_store import VectorStoreService
from app.services.data_service import DataService
from app.logger import logger


class RetrievalService:
    """
    Service for retrieving context components for chat processing.
    
    This service coordinates the retrieval of:
    - Recent messages (with optional status filter)
    - RAG segments (if embedding is configured)
    - Latest summary (if available)
    """
    
    @staticmethod
    def get_embedding_config_for_session(db: Session, session_id: int) -> Optional[EmbeddingConfig]:
        """
        Get the embedding configuration for a session's agent.
        
        Args:
            db: Database session
            session_id: Session ID
            
        Returns:
            EmbeddingConfig if configured, None otherwise
        """
        session = DataService.get_session(db, session_id)
        if not session:
            return None

        agent = DataService.get_agent(db, session.agent_id)
        if not agent:
            return None

        # Check if agent has embedding config (ID > 0 means configured)
        if agent.embedding_config_id and agent.embedding_config_id > 0:
            return db.query(EmbeddingConfig).filter(
                EmbeddingConfig.id == agent.embedding_config_id
            ).first()

        return None

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
        
        # Apply status filter if provided
        if status is not None:
            query = query.filter(Message.status == status)
        
        # Get messages in reverse order (newest first), then reverse
        messages = query.order_by(Message.id.desc()).limit(limit).all()
        messages = list(reversed(messages))

        return [
            {"role": msg.role, "content": msg.content, "id": msg.id}
            for msg in messages
        ]

    @staticmethod
    def get_context_without_rag(
        db: Session,
        session_id: int,
        recent_count: int = 10
    ) -> Dict[str, Any]:
        """
        Get context without RAG retrieval.
        
        Used for queries that don't need RAG (e.g., greetings, chitchat).
        Only retrieves recent messages and latest summary.
        
        Args:
            db: Database session
            session_id: Session ID
            recent_count: Number of recent messages to retrieve
            
        Returns:
            Dictionary with 'recent_messages', 'relevant_segments', and 'summary'
        """
        result = {
            "recent_messages": [],
            "relevant_segments": [],
            "summary": None
        }

        from app.models.message import MESSAGE_STATUS_COMPLETED
        
        # Get recent completed messages
        result["recent_messages"] = RetrievalService.get_recent_messages(
            db, session_id, recent_count, status=MESSAGE_STATUS_COMPLETED
        )

        # Get latest summary (may be older than current message count)
        latest_summary = SummaryService.get_latest_summary(db, session_id)
        if latest_summary:
            result["summary"] = {
                "version": latest_summary.version,
                "content": latest_summary.content
            }

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
        
        Args:
            db: Database session
            session_id: Session ID
            query: Processed query for RAG search
            recent_count: Number of recent messages to retrieve
            rag_count: Number of RAG segments to retrieve
            
        Returns:
            Dictionary with 'recent_messages', 'relevant_segments', 
            'summary', and 'has_embedding' keys
        """
        result = {
            "recent_messages": [],
            "relevant_segments": [],
            "summary": None,
            "has_embedding": False
        }

        from app.models.message import MESSAGE_STATUS_COMPLETED
        
        # Get recent completed messages
        result["recent_messages"] = RetrievalService.get_recent_messages(
            db, session_id, recent_count, status=MESSAGE_STATUS_COMPLETED
        )

        # Perform RAG retrieval if embedding is configured
        embedding_config = RetrievalService.get_embedding_config_for_session(db, session_id)
        if embedding_config:
            result["has_embedding"] = True
            try:
                vector_store = VectorStoreService.get_instance(session_id, embedding_config)

                # Search for relevant segments
                search_results = vector_store.search(query, k=rag_count)
                result["relevant_segments"] = search_results
                logger.info(f"RAG retrieved {len(search_results)} segments for session {session_id}")
            except Exception as e:
                logger.error(f"RAG retrieval failed: {str(e)}", exc_info=True)

        # Get latest summary (may be older than current message count)
        latest_summary = SummaryService.get_latest_summary(db, session_id)
        if latest_summary:
            result["summary"] = {
                "version": latest_summary.version,
                "content": latest_summary.content
            }

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
        # Check if embedding is configured
        embedding_config = RetrievalService.get_embedding_config_for_session(db, session_id)
        if not embedding_config:
            logger.info(f"No embedding config for session {session_id}, skipping indexing")
            return False

        # Load messages to index
        messages = db.query(Message).filter(
            Message.id.in_(message_ids),
            Message.session_id == session_id
        ).all()

        if not messages:
            logger.warning(f"No messages found for indexing in session {session_id}")
            return False

        try:
            vector_store = VectorStoreService.get_instance(session_id, embedding_config)

            # Prepare data for indexing
            contents = [msg.content for msg in messages]
            metadatas = [
                {"role": msg.role, "message_id": msg.id}
                for msg in messages
            ]

            # Add to vector store
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
