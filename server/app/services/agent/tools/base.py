"""
Base tool abstraction for the task agent system.

All tools must inherit from Tool and implement the execute method.
Tools are registered by name and provide their OpenAI function schema
for LLM tool calling.
"""

from abc import ABC, abstractmethod
from typing import Any, Dict


class Tool(ABC):
    """
    Abstract base class for agent tools.

    Each tool has a unique name, an OpenAI-compatible function definition schema,
    and an execute method that performs the actual work.
    """

    @property
    @abstractmethod
    def name(self) -> str:
        """Unique identifier for this tool."""
        ...

    @property
    @abstractmethod
    def schema(self) -> Dict[str, Any]:
        """
        OpenAI function calling schema.

        Must follow the format:
        {
            "type": "function",
            "function": {
                "name": "...",
                "description": "...",
                "parameters": {
                    "type": "object",
                    "properties": {...},
                    "required": [...]
                }
            }
        }
        """
        ...

    @abstractmethod
    async def execute(self, **kwargs) -> str:
        """
        Execute the tool with the given arguments.

        Args:
            **kwargs: Tool-specific arguments matching the schema.

        Returns:
            String representation of the tool result.
        """
        ...
