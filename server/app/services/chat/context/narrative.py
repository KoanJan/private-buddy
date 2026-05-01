"""
Narrative generation module for creating background stories.

This module handles the generation of background narratives. Two generation
modes are supported:

1. Cached narrative (from summary only, no segments):
   - Generated in background immediately after summary generation
   - Stored in historical_summaries.narrative field alongside the summary
   - Retrieved at chat time without LLM call (major performance gain)
   - Segments are handled as an independent section in context assembly

2. Legacy real-time narrative (from summary + segments):
   - Generated on-the-fly during chat processing
   - Segments are naturally integrated into the narrative
   - Kept for backward compatibility but no longer used in main flow

Narrative Perspective Design:
- Uses internal focalization (agent's viewpoint) rather than external focalization
- The agent is addressed as "You" to enhance immersion and continuity
- This helps the LLM naturally continue the conversation rather than "retell" it
"""

from typing import Optional, Dict, Any, List
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI
from app.models.llm_config import LLMConfig
from app.logger import logger


class NarrativeService:
    """
    Service for generating background narratives from context components.
    
    This service transforms structured context into a flowing narrative
    that helps the LLM understand conversation history.
    """

    CACHED_NARRATIVE_PROMPT = """You are a conversation background narrative assistant. Generate a coherent background narrative based on the summary.

Summary:
{summary_content}

Requirements:
1. Use second-person perspective (address the agent as "You"). For example: "You have been discussing X with the user. The user mentioned..."
2. Preserve ALL key information from the summary
3. Transform the summary into a flowing narrative
4. Do NOT add interpretations, judgments, or assumptions
5. Maintain information fidelity

IMPORTANT: The narrative MUST preserve the original language of the conversation.
- If the conversation is in Chinese, write the narrative in Chinese.
- If the conversation is in English, write the narrative in English.
- If the conversation contains multiple languages, the narrative may also contain multiple languages.
- Do NOT translate between languages. Maintain information fidelity.

Output only the narrative content."""

    NARRATIVE_PROMPT = """You are a conversation background narrative assistant. Generate a coherent background narrative based on the following information.

{summary_section}

{segments_section}

Integrate the above information into a coherent background narrative with the following requirements:
1. Use second-person perspective (address the agent as "You"). For example: "You have been discussing X with the user. The user mentioned..."
2. Preserve key information and context
3. The narrative should be coherent and flowing, not a simple list
4. Output only the narrative content, without additional explanations

IMPORTANT: The narrative MUST preserve the original language of the conversation.
- If the conversation is in Chinese, write the narrative in Chinese.
- If the conversation is in English, write the narrative in English.
- If the conversation contains multiple languages, the narrative may also contain multiple languages.
- Do NOT translate between languages. Maintain information fidelity."""

    @staticmethod
    def create_chat_model(llm_config: LLMConfig) -> ChatOpenAI:
        """
        Create a ChatOpenAI instance from LLM configuration.

        Args:
            llm_config: LLM configuration containing model ID, API key, and base URL

        Returns:
            Configured ChatOpenAI instance with moderate temperature for creative output
        """
        return ChatOpenAI(
            model=llm_config.model_id,
            openai_api_base=llm_config.base_url,
            openai_api_key=llm_config.api_key,
            temperature=0.3
        )

    @staticmethod
    async def generate_narrative_from_summary(
        llm_config: LLMConfig,
        summary_content: str
    ) -> Optional[str]:
        """
        Generate a background narrative from summary content only.

        This is the cached narrative generation method, called in background
        immediately after summary generation. The narrative is stored alongside
        the summary and retrieved at chat time without LLM call.

        Args:
            llm_config: LLM configuration for narrative generation
            summary_content: The summary text to transform into narrative

        Returns:
            Generated narrative text, or None if generation failed
        """
        if not summary_content:
            return None

        prompt = NarrativeService.CACHED_NARRATIVE_PROMPT.format(
            summary_content=summary_content
        )

        try:
            chat_model = NarrativeService.create_chat_model(llm_config)
            messages = [HumanMessage(content=prompt)]

            response = await chat_model.ainvoke(messages)
            narrative = response.content.strip()

            logger.info(f"Generated cached narrative from summary, length: {len(narrative)}")
            return narrative

        except Exception as e:
            logger.error(f"Failed to generate cached narrative: {str(e)}", exc_info=True)
            return None

    @staticmethod
    async def generate_background_story(
        llm_config: LLMConfig,
        summary: Optional[Dict[str, Any]],
        relevant_segments: List[Dict[str, Any]]
    ) -> Optional[str]:
        """
        Generate a background story from summary and RAG segments.

        Legacy real-time generation method. Segments are naturally integrated
        into the narrative for maximum coherence, but this requires an LLM
        call during chat processing (40-66s bottleneck).

        Args:
            llm_config: LLM configuration for narrative generation
            summary: Summary dictionary with 'version' and 'content' keys
            relevant_segments: List of RAG segment dictionaries with 'content' key

        Returns:
            Generated background story text, or None if no input provided
        """
        if not summary and not relevant_segments:
            return None

        summary_section = ""
        if summary:
            summary_section = f"[Conversation Summary]\n{summary['content']}"

        segments_section = ""
        if relevant_segments:
            segments_text = "\n".join([
                f"- {seg['content']}"
                for seg in relevant_segments
            ])
            segments_section = f"[Relevant Historical Segments]\n{segments_text}"

        prompt = NarrativeService.NARRATIVE_PROMPT.format(
            summary_section=summary_section,
            segments_section=segments_section
        )

        try:
            chat_model = NarrativeService.create_chat_model(llm_config)
            messages = [HumanMessage(content=prompt)]

            response = await chat_model.ainvoke(messages)
            background_story = response.content.strip()

            logger.info(f"Generated background story, length: {len(background_story)}")
            return background_story

        except Exception as e:
            logger.error(f"Failed to generate background story: {str(e)}", exc_info=True)
            return None
