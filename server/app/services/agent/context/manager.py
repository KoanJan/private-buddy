"""
Context manager for the task agent's internal message history.

Manages the message list within a single task execution.
All messages (system, user, assistant, tool) are kept internally
and never leak outside the task boundary.

This module implements the "context memory" component of the
"two tools + one memory" design principle.
"""

from typing import Any, Dict, List, Optional

from app.logger import logger


class ContextManager:
    """
    Manages the internal message history for a single task execution.

    Messages follow the OpenAI chat completion format:
    - system: { role, content }
    - user: { role, content }
    - assistant: { role, content } or { role, tool_calls }
    - tool: { role, tool_call_id, content }

    All messages are session-scoped and isolated from the chat system.
    """

    def __init__(self, system_prompt: Optional[str] = None):
        """
        Initialize the context manager.

        Args:
            system_prompt: Optional system prompt to prepend to the message list.
        """
        self._messages: List[Dict[str, Any]] = []
        if system_prompt:
            self._messages.append({
                "role": "system",
                "content": system_prompt,
            })

    @property
    def messages(self) -> List[Dict[str, Any]]:
        """Get the current message list (read-only reference)."""
        return self._messages

    def add_user_message(self, content: str) -> None:
        """
        Add a user message to the context.

        Args:
            content: The user's message text.
        """
        self._messages.append({"role": "user", "content": content})
        logger.debug(f"ContextManager: added user message, total={len(self._messages)}")

    def add_assistant_message(self, content: str) -> None:
        """
        Add an assistant text message to the context.

        Args:
            content: The assistant's response text.
        """
        self._messages.append({"role": "assistant", "content": content})
        logger.debug(f"ContextManager: added assistant message, total={len(self._messages)}")

    def add_assistant_tool_calls(
        self, tool_calls: List[Dict[str, Any]], content: str = ""
    ) -> None:
        """
        Add an assistant message with tool calls to the context.

        Args:
            tool_calls: List of tool call objects from the LLM response.
                Each tool call has: id, type, function.name, function.arguments
            content: The model's reasoning/thoughts before the tool call.
                This field records the model's thought process for observability.
        """
        msg: Dict[str, Any] = {
            "role": "assistant",
            "tool_calls": tool_calls,
        }
        if content:
            msg["content"] = content
        self._messages.append(msg)
        logger.debug(
            f"ContextManager: added assistant tool_calls ({len(tool_calls)} calls), "
            f"thoughts_len={len(content)}, total={len(self._messages)}"
        )

    def add_tool_result(
        self, tool_call_id: str, content: str
    ) -> None:
        """
        Add a tool execution result to the context.

        Args:
            tool_call_id: The ID of the tool call this result corresponds to.
            content: The tool execution result as a string.
        """
        self._messages.append({
            "role": "tool",
            "tool_call_id": tool_call_id,
            "content": content,
        })
        logger.debug(f"ContextManager: added tool result, total={len(self._messages)}")

    def get_message_count(self) -> int:
        """Get the total number of messages in the context."""
        return len(self._messages)

    def get_total_token_estimate(self) -> int:
        """
        Rough estimate of total tokens in the context.

        Uses a simple heuristic: ~4 characters per token for English,
        ~2 characters per token for mixed CJK content.
        This is a conservative estimate for monitoring purposes only.
        """
        total_chars = sum(
            len(str(msg.get("content", ""))) + len(str(msg.get("tool_calls", "")))
            for msg in self._messages
        )
        return total_chars // 3

    def truncate_earliest_messages(self, keep_count: int) -> None:
        """
        Remove the earliest messages, keeping the most recent ones.

        Preserves the system message if present.

        Args:
            keep_count: Number of most recent messages to keep (excluding system).
        """
        if len(self._messages) <= keep_count:
            return

        system_msg = None
        if self._messages and self._messages[0]["role"] == "system":
            system_msg = self._messages[0]

        remaining = self._messages[-keep_count:] if not system_msg else self._messages[-(keep_count):]

        self._messages = [system_msg] + remaining if system_msg else remaining
        logger.info(
            f"ContextManager: truncated to {len(self._messages)} messages "
            f"(keep_count={keep_count})"
        )
