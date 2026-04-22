"""
Narrative generation module for creating background stories.

This module handles the generation of background narratives from summary
and RAG segments. The narrative provides a coherent context for the LLM
to understand the conversation history.

The narrative generation process:
1. Combine summary content (if available)
2. Combine relevant RAG segments (if available)
3. Use LLM to generate a coherent third-person narrative
"""

from typing import Optional, Dict, Any, List
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI
from app.models.llm_config import LLMConfig
from app.services.llm.llm_logger import TokenUsageLogger
from app.logger import logger


class NarrativeService:
    """
    Service for generating background narratives from context components.
    
    This service transforms structured context (summary + segments) into
    a flowing narrative that helps the LLM understand conversation history.
    """
    
    NARRATIVE_PROMPT = """You are a conversation background narrative assistant. Generate a coherent background narrative based on the following information.

{summary_section}

{segments_section}

Integrate the above information into a coherent background narrative with the following requirements:
1. Use third-person perspective
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
            temperature=0.3  # Higher temperature for more creative narrative
        )

    @staticmethod
    async def generate_background_story(
        llm_config: LLMConfig,
        summary: Optional[Dict[str, Any]],
        relevant_segments: List[Dict[str, Any]]
    ) -> Optional[str]:
        """
        Generate a background story from summary and RAG segments.
        
        This method combines the summary and relevant segments into a prompt
        and uses LLM to generate a coherent third-person narrative.
        
        Args:
            llm_config: LLM configuration for narrative generation
            summary: Summary dictionary with 'version' and 'content' keys
            relevant_segments: List of RAG segment dictionaries with 'content' key
            
        Returns:
            Generated background story text, or None if no input provided
        """
        # Return early if no context available
        if not summary and not relevant_segments:
            return None

        # Build summary section
        summary_section = ""
        if summary:
            summary_section = f"[Conversation Summary]\n{summary['content']}"

        # Build segments section
        segments_section = ""
        if relevant_segments:
            segments_text = "\n".join([
                f"- {seg['content']}"
                for seg in relevant_segments
            ])
            segments_section = f"[Relevant Historical Segments]\n{segments_text}"

        # Format prompt
        prompt = NarrativeService.NARRATIVE_PROMPT.format(
            summary_section=summary_section,
            segments_section=segments_section
        )

        try:
            chat_model = NarrativeService.create_chat_model(llm_config)
            messages = [
                HumanMessage(content=prompt)
            ]

            response = await chat_model.ainvoke(messages, config={"callbacks": [TokenUsageLogger()]})
            background_story = response.content.strip()

            logger.info(f"Generated background story, length: {len(background_story)}")
            return background_story

        except Exception as e:
            logger.error(f"Failed to generate background story: {str(e)}", exc_info=True)
            return None
