"""
Agent service module - the main entry point for agent execution.

This module provides the single public interface for the agent system:
    service = AgentService(db)
    delivery = await service.execute(task_requirement, llm_config, ...) -> AgentDelivery

Design principles:
- Input: task requirement (structured, not raw user message)
- Output: final delivery (success result or failure with reason)
- Internal isolation: all process info is hidden from the outside
- No pollution of the chat system

The agent service is self-contained and autonomous. It creates its own
Agent Loop, LLM client, tools, and context manager for each execution.
Nothing from the internal execution leaks into the chat context.
"""

from pathlib import Path
from typing import List, Optional

from pydantic import BaseModel
from sqlalchemy.orm import Session as DBSession

from app.models.llm_config import LLMConfig
from app.services.agent.agent_loop import AgentLoop
from app.services.agent.llm_client import AgentLLMClient
from app.services.agent.tools.base import Tool
from app.services.agent.tools.bash import BashTool
from app.services.agent.tools.web_search import WebSearchTool
from app.config import get_settings
from app.logger import logger


class AgentDelivery(BaseModel):
    """
    The final output of an agent execution.

    A delivery is always produced - either a successful result
    or a failure with a reason. Both are legitimate outcomes.

    Attributes:
        status: "success" or "failure"
        result: The agent output (present on success)
        reason: The failure explanation (present on failure)
    """

    status: str
    result: Optional[str] = None
    reason: Optional[str] = None


class AgentService:
    """
    Self-contained agent execution service.

    This is the only public interface of the agent module.
    It accepts a task requirement and an LLM configuration,
    runs the agent loop internally, and returns an AgentDelivery.

    Usage:
        service = AgentService(db)
        delivery = await service.execute(
            task_requirement="Find the latest Python release version",
            llm_config=llm_config,
        )
        # delivery.status == "success" or "failure"
        # delivery.result or delivery.reason
    """

    def __init__(self, db: DBSession):
        """
        Initialize AgentService with database session.

        Args:
            db: Database session for writing interaction records and loading configs.
        """
        self._db = db

    async def execute(
        self,
        task_requirement: str,
        llm_config: LLMConfig,
        max_iterations: Optional[int] = None,
        system_prompt: Optional[str] = None,
        workspace: Optional[Path] = None,
        delivery_type: Optional[str] = None,
        session_id: Optional[int] = None,
        user_msg_id: Optional[int] = None,
        agent_msg_id: Optional[int] = None,
    ) -> AgentDelivery:
        """
        Execute an agent task and return the delivery.

        This method is the single entry point for agent execution.
        It creates all necessary components internally and runs
        the agent loop to completion.

        Args:
            task_requirement: The task description to execute.
            llm_config: LLM configuration for the agent.
            max_iterations: Override for max loop iterations.
            system_prompt: Override for the agent's system prompt.
            workspace: If set, BashTool will be confined to this directory.
            delivery_type: Expected delivery type ("text" or "file").
                          Affects the system prompt to guide the agent.
            session_id: Session ID for interaction records.
            user_msg_id: User message ID that triggered execution.
            agent_msg_id: Agent message ID for the delivery target.

        Returns:
            AgentDelivery with status, result (on success), and reason (on failure).
        """
        settings = get_settings()
        effective_max_iterations = max_iterations or settings.task_max_iterations

        logger.info(
            f"AgentService.execute: task_len={len(task_requirement)}, "
            f"model={llm_config.model_id}, "
            f"max_iterations={effective_max_iterations}, "
            f"workspace={workspace}, delivery_type={delivery_type}, "
            f"session_id={session_id}, agent_msg_id={agent_msg_id}"
        )

        try:
            tools = self._create_tools(workspace=workspace)
            tool_schemas = [t.schema for t in tools]
            has_web_search = any(isinstance(t, WebSearchTool) for t in tools)

            effective_system_prompt = system_prompt or self._build_system_prompt(
                workspace=workspace,
                delivery_type=delivery_type,
                has_web_search=has_web_search,
            )

            llm_client = AgentLLMClient(
                llm_config=llm_config,
                tool_schemas=tool_schemas,
            )

            try:
                agent_loop = AgentLoop(
                    llm_client=llm_client,
                    tools=tools,
                    max_iterations=effective_max_iterations,
                    system_prompt=effective_system_prompt,
                    db=self._db,
                    session_id=session_id,
                    user_msg_id=user_msg_id,
                    agent_msg_id=agent_msg_id,
                )

                result = await agent_loop.run(task_requirement)

                delivery = AgentDelivery(
                    status=result["status"],
                    result=result.get("result"),
                    reason=result.get("reason"),
                )

                logger.info(
                    f"AgentService.execute completed: status={delivery.status}, "
                    f"result_len={len(delivery.result or '')}, "
                    f"reason_len={len(delivery.reason or '')}"
                )

                return delivery
            finally:
                await llm_client.close()

        except Exception as e:
            logger.error(
                f"AgentService.execute unexpected error: {str(e)}",
                exc_info=True,
            )
            return AgentDelivery(
                status="failure",
                reason=f"Unexpected error during task execution: {str(e)}",
            )

    def _create_tools(
        self,
        workspace: Optional[Path] = None,
    ) -> List[Tool]:
        """
        Create the tool set for the agent.

        Args:
            workspace: If set, BashTool is confined to this directory.

        Returns:
            List of Tool instances. WebSearchTool is only included
            if search is configured and active.
        """
        tools: List[Tool] = [BashTool(workspace=workspace)]

        from app.services.search import SearchService
        search_config = SearchService.get_config(self._db)
        if search_config and search_config.is_available():
            tools.append(WebSearchTool(search_config=search_config))
            logger.info("WebSearchTool added to agent tools")
        else:
            logger.info("WebSearchTool not available (not configured or disabled)")

        return tools

    @staticmethod
    def _build_system_prompt(
        workspace: Optional[Path] = None,
        delivery_type: Optional[str] = None,
        has_web_search: bool = True,
    ) -> str:
        """
        Build a system prompt based on workspace and delivery type.

        Args:
            workspace: The agent's workspace directory.
            delivery_type: Expected delivery type ("text" or "file").
            has_web_search: Whether web_search tool is available.

        Returns:
            A system prompt string tailored to the task configuration.
        """
        parts = [
            "You are a helpful AI agent that can execute tasks using tools.",
            "",
            "Available tools:",
            "- bash: Execute shell commands",
        ]

        if has_web_search:
            parts.append("- web_search: Search the web for information")

        parts.extend([
            "",
            "CRITICAL: Before calling any tool, you MUST first explain your reasoning",
            "in the content field. Describe what you plan to do and why.",
            "Only after explaining your thought process, make the tool call.",
            "",
            "When using tools:",
            "1. Think step by step about what you need to do",
            "2. Explain your reasoning in the content field",
            "3. Choose the appropriate tool",
            "4. Execute the tool and observe the result",
            "5. Continue until the task is complete",
            "",
            "Always verify your actions by checking the results.",
        ])

        if workspace:
            parts.extend([
                "",
                f"IMPORTANT: Your workspace directory is: {workspace}",
                "All files you create MUST be within this directory.",
                "Do not write files to any other location.",
            ])

        if delivery_type == "file":
            parts.extend([
                "",
                "DELIVERY TYPE: file",
                "The user expects file deliverables (code, documents, etc.).",
                "Create the required files in your workspace directory.",
                "When finished, list all created files and provide a summary.",
            ])
        elif delivery_type == "text":
            parts.extend([
                "",
                "DELIVERY TYPE: text",
                "The user expects a text answer as the deliverable.",
                "Provide a clear, concise text response.",
                "You may use tools to gather information, but the final",
                "output should be a direct text answer.",
            ])

        parts.extend([
            "",
            "When the task is complete, provide a clear and concise summary of what was accomplished.",
            "If the task cannot be completed, explain why and what was attempted.",
        ])

        return "\n".join(parts)
