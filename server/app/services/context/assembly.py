"""
Context assembly module for building LLM input messages.

This module handles the assembly of context components into a unified message
format for LLM processing. It implements the "one big message" pattern where
character settings, background story, and recent messages are combined into
a single prompt.

The assembly process includes:
- Character settings (personality, style, identity) integration
- Background story formatting with metadata
- Recent message formatting with sequence numbers
"""

from typing import List, Dict, Any, Optional
from langchain_core.messages import HumanMessage, AIMessage, BaseMessage
from app.logger import logger


class ContextAssemblyService:
    """
    Service for assembling context components into LLM-ready messages.
    
    This service implements the context assembly strategy where:
    - Character settings define the agent's personality and style
    - Summary and recent messages are decoupled (may overlap)
    - Metadata labels help LLM understand the scope of each component
    - Background story provides compressed historical context
    - Recent messages provide precise details for current context
    """
    
    # Template for full context with background story and character settings
    ONE_BIG_MESSAGE_TEMPLATE = """{character_section}Here is the background information about the conversation:

[Conversation Summary (compressed from messages 1-{summary_version})]
{background_story}

---

[Recent Conversation (messages {recent_start}-{recent_end})]
{dialog_section}

---

Please respond directly to the user. Do not use parenthetical action descriptions or non-verbal content."""

    # Template for simple context without background story (V < N case)
    ONE_BIG_MESSAGE_NO_STORY_TEMPLATE = """{character_section}[Conversation Record (messages {recent_start}-{recent_end})]
{dialog_section}

---

Please respond directly to the user. Do not use parenthetical action descriptions or non-verbal content."""

    @staticmethod
    def _format_character_section(character_settings: Optional[str]) -> str:
        """
        Format character settings section for the prompt.
        
        Args:
            character_settings: Agent's personality, style, and identity settings
            
        Returns:
            Formatted character section string
        """
        if not character_settings:
            return ""
        return f"[Your Character]\n{character_settings}\n\n---\n\n"

    @staticmethod
    def assemble_context(
        character_settings: Optional[str],
        background_story: Optional[str],
        recent_messages: List[Dict[str, Any]],
        summary_version: Optional[int] = None,
        recent_start: int = 1,
        recent_end: int = 1
    ) -> List[BaseMessage]:
        """
        Assemble context into one big message for LLM processing.
        
        This method combines character settings, background story, and recent messages
        into a unified message format. The background story and recent messages
        are decoupled - they may overlap in coverage.
        
        Args:
            character_settings: Agent's personality, style, and identity settings
            background_story: Background narrative from summary + RAG segments
            recent_messages: Recent completed messages (including trigger_message as the latest)
            summary_version: Version number of the summary (covers messages 1 to summary_version)
            recent_start: Starting message sequence number for recent messages
            recent_end: Ending message sequence number for recent messages
            
        Returns:
            List of LangChain messages ready for LLM processing
        """
        messages = []

        # Format character settings section
        character_section = ContextAssemblyService._format_character_section(character_settings)

        # Format recent messages into dialog section
        dialog_lines = []
        for msg in recent_messages:
            role = "User" if msg["role"] == "user" else "You"
            dialog_lines.append(f"{role}: {msg['content']}")
        dialog_section = "\n".join(dialog_lines)

        # Choose template based on whether background story exists
        if background_story and summary_version:
            one_big_message = ContextAssemblyService.ONE_BIG_MESSAGE_TEMPLATE.format(
                character_section=character_section,
                background_story=background_story,
                dialog_section=dialog_section,
                summary_version=summary_version,
                recent_start=recent_start,
                recent_end=recent_end
            )
        else:
            one_big_message = ContextAssemblyService.ONE_BIG_MESSAGE_NO_STORY_TEMPLATE.format(
                character_section=character_section,
                dialog_section=dialog_section,
                recent_start=recent_start,
                recent_end=recent_end
            )

        # Add the one big message as a HumanMessage
        messages.append(HumanMessage(content=one_big_message))

        logger.info(f"Assembled context with {len(messages)} messages")
        return messages

