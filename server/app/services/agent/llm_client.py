"""
LLM client for the agent system.

Wraps LangChain's ChatOpenAI with tool binding support.
Uses ainvoke (non-streaming) for complete response parsing,
which is required to properly handle tool_calls in the ReAct loop.
"""

import json
from typing import Any, Dict, List, Optional

import httpx
from langchain_openai import ChatOpenAI
from langchain_core.messages import AIMessage, BaseMessage, HumanMessage, SystemMessage, ToolMessage

from app.models.llm_config import LLMConfig
from app.logger import logger


class AgentLLMClient:
    """
    LLM client tailored for the agent's ReAct loop.

    Supports:
    - Tool binding via OpenAI function calling
    - Non-streaming invocation for complete tool_calls parsing
    - Message format conversion between internal dict and LangChain format
    """

    def __init__(self, llm_config: LLMConfig, tool_schemas: List[Dict[str, Any]]):
        """
        Initialize the agent LLM client.

        Args:
            llm_config: LLM configuration (model, API key, base URL).
            tool_schemas: List of OpenAI function schemas for tool binding.
        """
        self._http_client = httpx.AsyncClient(
            limits=httpx.Limits(
                max_connections=1,
                max_keepalive_connections=0,
            ),
        )

        self._chat_model = ChatOpenAI(
            model=llm_config.model_id,
            openai_api_base=llm_config.base_url,
            openai_api_key=llm_config.api_key,
            streaming=False,
            http_async_client=self._http_client,
        )

        if tool_schemas:
            self._chat_model = self._chat_model.bind_tools(tool_schemas)

        logger.info(
            f"AgentLLMClient initialized: model={llm_config.model_id}, "
            f"tools={len(tool_schemas)}"
        )

    async def invoke(
        self, messages: List[Dict[str, Any]]
    ) -> Dict[str, Any]:
        """
        Invoke the LLM with the given messages and return a structured result.

        Args:
            messages: List of message dicts in OpenAI chat format.

        Returns:
            Dict with keys:
            - finish_reason: "stop" | "tool_calls" | "length"
            - content: str (assistant text, present when finish_reason is "stop" or "length")
            - tool_calls: list (present when finish_reason is "tool_calls")
        """
        langchain_messages = self._convert_messages(messages)

        msg_summary = [
            {"role": m.get("role"), "content_len": len(str(m.get("content", ""))), "tool_calls": len(m.get("tool_calls", []))}
            for m in messages
        ]
        logger.debug(
            f"AgentLLMClient invoking with {len(messages)} messages, "
            f"detail: {msg_summary}"
        )

        try:
            response: AIMessage = await self._chat_model.ainvoke(langchain_messages)

            has_tool_calls = bool(response.tool_calls)

            if has_tool_calls:
                tool_calls = self._format_tool_calls(response)
                tc_summary = [
                    {"id": tc["id"], "name": tc["function"]["name"], "args": tc["function"]["arguments"][:200]}
                    for tc in tool_calls
                ]
                logger.debug(
                    f"AgentLLMClient response: finish_reason=tool_calls, "
                    f"content={repr(response.content[:500] if response.content else '')}, "
                    f"tool_calls={tc_summary}"
                )
                return {
                    "finish_reason": "tool_calls",
                    "content": response.content or "",
                    "tool_calls": tool_calls,
                }

            finish_reason = getattr(response, "response_metadata", {}).get(
                "finish_reason", "stop"
            )

            if finish_reason == "length":
                logger.debug(
                    f"AgentLLMClient response: finish_reason=length, "
                    f"content={repr(response.content[:500] if response.content else '')}"
                )
                return {
                    "finish_reason": "length",
                    "content": response.content or "",
                    "tool_calls": [],
                }

            logger.debug(
                f"AgentLLMClient response: finish_reason=stop, "
                f"content={repr(response.content[:500] if response.content else '')}"
            )
            return {
                "finish_reason": "stop",
                "content": response.content or "",
                "tool_calls": [],
            }

        except Exception as e:
            logger.error(f"AgentLLMClient invoke error: {str(e)}", exc_info=True)
            raise

    async def close(self) -> None:
        """Close the underlying HTTP client to release connections."""
        try:
            await self._http_client.aclose()
        except Exception:
            pass

    @staticmethod
    def _convert_messages(messages: List[Dict[str, Any]]) -> List[BaseMessage]:
        """
        Convert internal message dicts to LangChain message objects.

        Handles system, user, assistant (with optional tool_calls), and tool messages.

        Args:
            messages: List of message dicts in OpenAI chat format.

        Returns:
            List of LangChain BaseMessage objects.
        """
        result: List[BaseMessage] = []

        for msg in messages:
            role = msg.get("role")

            if role == "system":
                result.append(SystemMessage(content=msg["content"]))
            elif role == "user":
                result.append(HumanMessage(content=msg["content"]))
            elif role == "assistant":
                tool_calls = msg.get("tool_calls")
                if tool_calls:
                    ai_msg = AIMessage(
                        content=msg.get("content", ""),
                        additional_kwargs={"tool_calls": tool_calls},
                    )
                    result.append(ai_msg)
                else:
                    result.append(AIMessage(content=msg.get("content", "")))
            elif role == "tool":
                result.append(
                    ToolMessage(
                        content=msg["content"],
                        tool_call_id=msg.get("tool_call_id", ""),
                    )
                )

        return result

    @staticmethod
    def _format_tool_calls(response: AIMessage) -> List[Dict[str, Any]]:
        """
        Format tool calls from the LLM response into a standardized structure.

        Args:
            response: The AIMessage response containing tool_calls.

        Returns:
            List of tool call dicts with id, function.name, function.arguments.
        """
        formatted = []
        for tc in response.tool_calls:
            formatted.append({
                "id": tc.get("id", ""),
                "type": "function",
                "function": {
                    "name": tc.get("name", ""),
                    "arguments": json.dumps(tc.get("args", {}), ensure_ascii=False),
                },
            })
        return formatted
