"""
Summary generation and management module.

This module handles the generation and retrieval of conversation summaries.
Summaries are versioned to track the evolution of conversation context.

Summary Generation Rules:
- V < N: No summary generated (not enough messages)
- N <= V < 2N: Full summary using all messages (1 to V) with empty baseline
- V >= 2N: Incremental summary using baseline (V-N) + recent messages (V-N+1 to V)

After summary content is generated, a narrative is immediately generated from
the summary content. Both are written to the database in a single atomic
operation, ensuring no intermediate state exists where summary exists but
narrative is empty. This eliminates the real-time narrative generation
bottleneck during chat processing while maintaining data consistency.

The summary system supports:
- Recursive baseline generation when baseline is missing
- Asynchronous generation that doesn't block chat responses
- Version tracking for context assembly
- Atomic write of summary + narrative
"""

from sqlalchemy.orm import Session
from typing import Optional
from app.models.historical_summary import HistoricalSummary
from app.models.message import Message
from app.models.llm_config import LLMConfig
from app.services.llm import LLMService
from app.services.chat.context.narrative import NarrativeService
from app.logger import logger
from langchain_core.messages import HumanMessage


class SummaryService:
    """
    Service for managing conversation summaries.
    
    This service handles:
    - Summary retrieval by version
    - Summary generation with LLM
    - Recursive baseline generation
    - Message formatting for summary prompts
    - Atomic write of summary + cached narrative
    """
    
    SUMMARY_PROMPT = """You are a conversation summary assistant. Generate a new summary based on the conversation history and baseline summary.

Baseline summary (if exists):
{baseline_summary}

Recent conversation:
{recent_messages}

Generate a concise but complete summary that includes key information, decisions, and context from the conversation. The summary should help understand the background for subsequent conversations.

IMPORTANT: The summary MUST preserve the original language of the conversation.
- If the conversation is in Chinese, write the summary in Chinese.
- If the conversation is in English, write the summary in English.
- If the conversation contains multiple languages, the summary may also contain multiple languages.
- Do NOT translate between languages. Maintain information fidelity."""

    @staticmethod
    def get_summary(db: Session, session_id: int, version: int) -> Optional[HistoricalSummary]:
        """
        Get a specific summary by session ID and version.
        
        Args:
            db: Database session
            session_id: Session ID
            version: Summary version number
            
        Returns:
            HistoricalSummary if found, None otherwise
        """
        return db.query(HistoricalSummary).filter(
            HistoricalSummary.session_id == session_id,
            HistoricalSummary.version == version
        ).first()

    @staticmethod
    def get_latest_summary(db: Session, session_id: int) -> Optional[HistoricalSummary]:
        """
        Get the latest summary for a session.
        
        Used during context assembly to get the most recent summary
        available, even if it's older than the current message count.
        The narrative field is included in the returned record.
        
        Args:
            db: Database session
            session_id: Session ID
            
        Returns:
            Latest HistoricalSummary if any exists, None otherwise
        """
        return db.query(HistoricalSummary).filter(
            HistoricalSummary.session_id == session_id
        ).order_by(HistoricalSummary.version.desc()).first()

    @staticmethod
    def get_messages_by_range(
        db: Session,
        session_id: int,
        start_seq: int,
        end_seq: int
    ) -> list[Message]:
        """
        Get messages by session-internal sequence numbers (not global IDs).
        
        Messages are ordered by their global ID, which corresponds to
        their insertion order. The sequence numbers are 1-based.
        
        Args:
            db: Database session
            session_id: Session ID
            start_seq: Start sequence number (1-based, inclusive)
            end_seq: End sequence number (inclusive)
            
        Returns:
            List of Message objects in chronological order
        """
        return db.query(Message).filter(
            Message.session_id == session_id
        ).order_by(Message.id.asc()).offset(start_seq - 1).limit(end_seq - start_seq + 1).all()

    @staticmethod
    def create_summary(
        db: Session,
        session_id: int,
        version: int,
        content: str,
        narrative: str = ""
    ) -> HistoricalSummary:
        """
        Create and persist a new summary with narrative.
        
        Both summary content and narrative are written atomically in a single
        database operation. This ensures no intermediate state exists where
        summary is available but narrative is empty.
        
        Args:
            db: Database session
            session_id: Session ID
            version: Version number (equals message count at generation time)
            content: Summary content text
            narrative: Cached narrative text
            
        Returns:
            Created HistoricalSummary instance
        """
        summary = HistoricalSummary(
            session_id=session_id,
            version=version,
            content=content,
            narrative=narrative
        )
        db.add(summary)
        db.commit()
        db.refresh(summary)
        return summary

    @staticmethod
    def format_messages_for_summary(messages: list[Message]) -> str:
        """
        Format messages for the summary prompt.
        
        Converts message objects into a human-readable format suitable
        for LLM summarization.
        
        Args:
            messages: List of Message objects
            
        Returns:
            Formatted string with role prefixes
        """
        formatted = []
        for msg in messages:
            role = "User" if msg.role == "user" else "Assistant"
            formatted.append(f"{role}: {msg.content}")
        return "\n\n".join(formatted)

    @staticmethod
    async def generate_summary(
        db: Session,
        session_id: int,
        version: int,
        llm_config: LLMConfig,
        window_size: int
    ) -> Optional[HistoricalSummary]:
        """
        Generate a summary for the specified version.
        
        This method implements the summary generation logic:
        1. Check if summary already exists (idempotent)
        2. Validate version >= window_size
        3. Determine generation strategy based on version:
           - N <= V < 2N: Full summary with all messages
           - V >= 2N: Incremental with baseline + recent messages
        4. Recursively generate missing baseline if needed
        5. Call LLM to generate summary content
        6. Generate cached narrative from summary content
        7. Atomically persist summary + narrative in a single write
        
        Atomic write design: summary and narrative are both generated before
        writing to the database. This eliminates the intermediate state where
        summary exists but narrative is empty, avoiding race conditions during
        concurrent reads. If narrative generation fails, the entire operation
        is aborted and no record is written — the next trigger will retry.
        
        Args:
            db: Database session
            session_id: Session ID
            version: Version number (message count V)
            llm_config: LLM configuration for generation
            window_size: Summary window size (N)
            
        Returns:
            Generated HistoricalSummary if successful, None otherwise
        """
        # Check if summary already exists (idempotent)
        existing = SummaryService.get_summary(db, session_id, version)
        if existing:
            logger.info(f"Summary already exists for session {session_id}, version {version}")
            return existing

        # Validate minimum version
        if version < window_size:
            logger.info(f"Version {version} < window_size {window_size}, skipping summary generation")
            return None

        # Branch 1: N <= V < 2N - Full summary with all messages
        if version < 2 * window_size:
            messages = SummaryService.get_messages_by_range(db, session_id, 1, version)
            if not messages:
                logger.warning(f"No messages found for session {session_id}, range 1-{version}")
                return None

            messages_text = SummaryService.format_messages_for_summary(messages)
            prompt = SummaryService.SUMMARY_PROMPT.format(
                baseline_summary="(No baseline summary, this is the first summary)",
                recent_messages=messages_text
            )
            
        # Branch 2: V >= 2N - Incremental summary with baseline
        else:
            baseline_version = version - window_size
            
            # Get or recursively generate baseline summary
            baseline_summary = SummaryService.get_summary(db, session_id, baseline_version)
            if not baseline_summary:
                logger.info(f"Baseline summary not found for session {session_id}, version {baseline_version}, generating recursively...")
                baseline_summary = await SummaryService.generate_summary(
                    db, session_id, baseline_version, llm_config, window_size
                )

            baseline_text = baseline_summary.content if baseline_summary else "(No baseline summary)"

            # Get recent messages for the window
            start_seq = version - window_size + 1
            end_seq = version
            messages = SummaryService.get_messages_by_range(db, session_id, start_seq, end_seq)

            if not messages:
                logger.warning(f"No messages found for session {session_id}, seq range {start_seq}-{end_seq}")
                return None

            messages_text = SummaryService.format_messages_for_summary(messages)
            prompt = SummaryService.SUMMARY_PROMPT.format(
                baseline_summary=baseline_text,
                recent_messages=messages_text
            )

        # Generate summary content using LLM
        try:
            chat_model = LLMService.create_chat_model(llm_config)
            langchain_messages = [
                HumanMessage(content=prompt)
            ]

            response = await chat_model.ainvoke(langchain_messages)
            summary_content = response.content

            logger.info(f"Generated summary content for session {session_id}, version {version}")

            # Generate cached narrative from summary content (before DB write)
            narrative_result = await NarrativeService.generate_narrative_from_summary(
                llm_config, summary_content
            )
            if not narrative_result:
                logger.error(f"Narrative generation failed for session {session_id}, version {version}, aborting atomic write")
                return None

            # Atomically persist summary + narrative in a single write
            summary = SummaryService.create_summary(
                db, session_id, version, summary_content, narrative_result
            )

            logger.info(f"Atomically created summary+record for session {session_id}, version {version}")

            return summary

        except Exception as e:
            logger.error(f"Failed to generate summary: {str(e)}", exc_info=True)
            return None
