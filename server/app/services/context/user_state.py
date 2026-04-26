"""
User state inference module for detecting user's emotional, purposive, and situational context.

This module infers the user's current state from recent conversation messages
using LLM structured output. The inferred state is injected into the prompt's
instruction area as natural language to guide the agent's response strategy.

Three-dimensional model:
- Emotion: user's current emotional state (affects response tone)
- Purpose: user's current conversational goal (affects response content direction)
- Situation: user's physical context (affects response constraints)

Intent type is implicitly derived from purpose + situation, not modeled separately.
"""

from typing import Optional, List, Dict, Any, Literal
from pydantic import BaseModel, Field
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI
from app.models.llm_config import LLMConfig
from app.services.llm.llm_logger import TokenUsageLogger
from app.logger import logger


class UserState(BaseModel):
    """
    Structured model for inferred user state.

    Field descriptions serve dual purpose:
    1. Guide LLM structured output generation
    2. Provide natural language fragments for prompt template assembly
    """
    emotion: Literal["calm", "anxious", "frustrated", "urgent", "curious"] = Field(
        description="The user's current emotional state: "
                    "'calm' for relaxed or neutral, "
                    "'anxious' for worried or uneasy, "
                    "'frustrated' for annoyed or impatient (e.g. repeated failed attempts), "
                    "'urgent' for time-pressured or emergency, "
                    "'curious' for inquisitive or exploratory"
    )
    purpose: Literal["seek_help", "seek_advice", "seek_confirmation", "express_feeling", "casual_chat"] = Field(
        description="The user's current conversational goal: "
                    "'seek_help' for needing a solution or fix, "
                    "'seek_advice' for wanting recommendations or guidance, "
                    "'seek_confirmation' for validating a decision or understanding, "
                    "'express_feeling' for sharing emotions without expecting solutions, "
                    "'casual_chat' for social or non-goal-oriented conversation"
    )
    situation: str = Field(
        default="unknown",
        description="Brief natural language description of the user's physical context "
                    "if inferable from the conversation, such as time of day, device, "
                    "environment, or activity. Use 'unknown' if not inferable. "
                    "Examples: 'at work on desktop', 'late evening on mobile', "
                    "'in a meeting', 'commuting'"
    )
    needs_world_interaction: bool = Field(
        description="Whether the user's request requires interaction with the external world: "
                    "true if the request needs tools, real-time information, file operations, "
                    "or any action beyond the LLM's parametric knowledge; "
                    "false if the LLM can answer directly from its training data"
    )

    def to_natural_language(self) -> str:
        """
        Convert the structured user state into a natural language description
        suitable for injection into the prompt's instruction area.
        """
        emotion_map = {
            "calm": "calm and relaxed",
            "anxious": "anxious or worried",
            "frustrated": "frustrated or impatient",
            "urgent": "under time pressure or in urgency",
            "curious": "curious and exploratory",
        }
        purpose_map = {
            "seek_help": "seeking help with a problem",
            "seek_advice": "looking for advice or recommendations",
            "seek_confirmation": "seeking confirmation or validation",
            "express_feeling": "expressing feelings without expecting solutions",
            "casual_chat": "engaging in casual conversation",
        }

        parts = [
            f"The user appears {emotion_map[self.emotion]}",
            f"is {purpose_map[self.purpose]}",
        ]
        if self.situation and self.situation != "unknown":
            parts.append(f"and is likely {self.situation}")
        if self.needs_world_interaction:
            parts.append("and needs to interact with the external world (tools, real-time data, or file operations)")

        return ", ".join(parts) + "."


class UserStateService:
    """
    Service for inferring user state from recent conversation messages.

    Uses LLM structured output (with_structured_output) to produce a
    UserState instance. On failure, returns None, allowing graceful
    degradation — the main chat flow continues without user state.
    """

    INFERENCE_PROMPT = """Based on the following recent conversation, infer the user's current state.

Recent conversation:
{recent_messages}

Analyze the user's emotional tone, conversational purpose, and any clues about their physical situation.

Also determine if the user's request requires interaction with the external world:
- Set needs_world_interaction=true if the request needs: real-time information (news, weather, stock prices), 
  file operations (create, modify, delete files), code execution, web searches, or any tool usage.
- Set needs_world_interaction=false if the LLM can answer directly from its training data 
  (general knowledge, advice, explanations, casual conversation)."""

    @staticmethod
    def _create_chat_model(llm_config: LLMConfig) -> ChatOpenAI:
        """
        Create a ChatOpenAI instance from LLM configuration.

        Args:
            llm_config: LLM configuration containing model ID, API key, and base URL

        Returns:
            Configured ChatOpenAI instance with low temperature for consistent outputs
        """
        return ChatOpenAI(
            model=llm_config.model_id,
            openai_api_base=llm_config.base_url,
            openai_api_key=llm_config.api_key,
            temperature=0.1
        )

    @staticmethod
    def _format_recent_messages(recent_messages: List[Dict[str, Any]]) -> str:
        """
        Format recent messages into a text block for the inference prompt.

        Args:
            recent_messages: List of message dicts with 'role' and 'content' keys

        Returns:
            Formatted dialog text
        """
        dialog_lines = []
        for msg in recent_messages:
            role = "User" if msg["role"] == "user" else "Assistant"
            dialog_lines.append(f"{role}: {msg['content']}")
        return "\n".join(dialog_lines)

    @staticmethod
    async def infer_user_state(
        llm_config: LLMConfig,
        recent_messages: List[Dict[str, Any]]
    ) -> Optional[UserState]:
        """
        Infer the user's current state from recent conversation messages.

        Args:
            llm_config: LLM configuration for the inference call
            recent_messages: Recent conversation messages with 'role' and 'content' keys

        Returns:
            UserState instance if inference succeeds, None otherwise
        """
        if not recent_messages:
            return None

        try:
            chat_model = UserStateService._create_chat_model(llm_config)
            structured_model = chat_model.with_structured_output(UserState)

            dialog_text = UserStateService._format_recent_messages(recent_messages)
            prompt = UserStateService.INFERENCE_PROMPT.format(recent_messages=dialog_text)
            messages = [HumanMessage(content=prompt)]

            result = await structured_model.ainvoke(
                messages, config={"callbacks": [TokenUsageLogger()]}
            )

            logger.info(f"Inferred user state: emotion={result.emotion}, purpose={result.purpose}, situation={result.situation}, needs_world_interaction={result.needs_world_interaction}")
            return result

        except Exception as e:
            logger.error(f"Failed to infer user state: {str(e)}", exc_info=True)
            return None
