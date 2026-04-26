"""
Agent Loop implementation for the task execution system.

Implements the ReAct (Thought -> Action -> Observation) pattern:
1. LLM receives the current context and decides what to do
2. If tool_calls: execute tools and feed results back
3. If stop: return the final content as delivery
4. If length: truncate context and continue
5. Repeat until stop or max_iterations reached

The loop records every iteration to the interactions table
for observability and frontend display.
"""

import json
from typing import Dict, List, Optional

from sqlalchemy.orm import Session as DBSession

from app.models.interaction import Interaction, INTERACTION_TYPE_REQUEST, INTERACTION_TYPE_RESPONSE
from app.services.agent.llm_client import AgentLLMClient
from app.services.agent.context.manager import ContextManager
from app.services.agent.tools.base import Tool
from app.logger import logger


DEFAULT_MAX_ITERATIONS = 90
DEFAULT_SYSTEM_PROMPT = """You are a helpful AI agent that can execute tasks using tools.

Available tools:
- bash: Execute shell commands
- web_search: Search the web for information

When using tools:
1. Think step by step about what you need to do
2. Choose the appropriate tool
3. Execute the tool and observe the result
4. Continue until the task is complete

Always verify your actions by checking the results.

When the task is complete, provide a clear and concise summary of what was accomplished.
If the task cannot be completed, explain why and what was attempted."""


class AgentLoop:
    """
    ReAct-style agent loop for autonomous task execution.

    The loop iterates:
    - Call LLM with current context
    - If LLM returns tool_calls: execute tools, append results, continue
    - If LLM returns stop: deliver the content
    - If LLM returns length: truncate context, continue
    - If max_iterations reached: deliver failure with reason

    Every iteration is recorded to the interactions table with:
    - type=1 (request): the messages sent to the LLM
    - type=2 (response): the LLM output (content, tool_calls, finish_reason)
    """

    def __init__(
        self,
        llm_client: AgentLLMClient,
        tools: List[Tool],
        max_iterations: int = DEFAULT_MAX_ITERATIONS,
        system_prompt: Optional[str] = None,
        db: Optional[DBSession] = None,
        session_id: Optional[int] = None,
        user_msg_id: Optional[int] = None,
        agent_msg_id: Optional[int] = None,
    ):
        """
        Initialize the agent loop.

        Args:
            llm_client: LLM client with tool binding support.
            tools: List of available tools.
            max_iterations: Maximum number of loop iterations.
            system_prompt: System prompt for the agent.
            db: Database session for writing interaction records.
                If None, interactions are not persisted.
            session_id: Session ID for interaction records.
            user_msg_id: User message ID that triggered execution.
            agent_msg_id: Agent message ID for the delivery target.
        """
        self._llm_client = llm_client
        self._tool_registry: Dict[str, Tool] = {t.name: t for t in tools}
        self._max_iterations = max_iterations
        self._system_prompt = system_prompt or DEFAULT_SYSTEM_PROMPT
        self._db = db
        self._session_id = session_id
        self._user_msg_id = user_msg_id
        self._agent_msg_id = agent_msg_id

    def _write_interaction(self, iteration: int, interaction_type: int, data: dict) -> None:
        """
        Write an interaction record to the database.

        Silently skips if database session is not configured.

        Args:
            iteration: The iteration number.
            interaction_type: INTERACTION_TYPE_REQUEST or INTERACTION_TYPE_RESPONSE.
            data: The data payload to store as JSON.
        """
        if not self._db or not self._session_id:
            return

        try:
            record = Interaction(
                session_id=self._session_id,
                user_msg_id=self._user_msg_id or 0,
                agent_msg_id=self._agent_msg_id or 0,
                iteration=iteration,
                type=interaction_type,
                data=json.dumps(data, ensure_ascii=False),
            )
            self._db.add(record)
            self._db.commit()
        except Exception as e:
            logger.error(f"Failed to write interaction record: {e}")
            if self._db:
                self._db.rollback()

    async def run(self, task_requirement: str) -> Dict[str, str]:
        """
        Execute the agent loop for a given task requirement.

        This is the main entry point. It runs the ReAct loop until:
        - LLM returns a stop response (success)
        - Max iterations reached (failure)
        - An unrecoverable error occurs (failure)

        Args:
            task_requirement: The task description to execute.

        Returns:
            Dict with:
            - status: "success" or "failure"
            - result: Final content (on success)
            - reason: Failure reason (on failure)
        """
        context = ContextManager(system_prompt=self._system_prompt)
        context.add_user_message(task_requirement)

        logger.info(
            f"AgentLoop starting: task_len={len(task_requirement)}, "
            f"max_iterations={self._max_iterations}, "
            f"tools={list(self._tool_registry.keys())}, "
            f"session_id={self._session_id}, agent_msg_id={self._agent_msg_id}"
        )

        for iteration in range(1, self._max_iterations + 1):
            logger.info(f"AgentLoop iteration {iteration}/{self._max_iterations}")

            self._write_interaction(
                iteration=iteration,
                interaction_type=INTERACTION_TYPE_REQUEST,
                data={"messages": context.messages},
            )

            try:
                response = await self._llm_client.invoke(context.messages)
            except Exception as e:
                logger.error(f"AgentLoop LLM error at iteration {iteration}: {str(e)}")
                return {
                    "status": "failure",
                    "reason": f"LLM invocation failed at iteration {iteration}: {str(e)}",
                }

            finish_reason = response["finish_reason"]
            content = response.get("content", "") or ""
            tool_calls = response.get("tool_calls", []) or []

            self._write_interaction(
                iteration=iteration,
                interaction_type=INTERACTION_TYPE_RESPONSE,
                data={
                    "content": content,
                    "tool_calls": tool_calls,
                    "finish_reason": finish_reason,
                },
            )

            if finish_reason == "stop":
                logger.debug(
                    f"AgentLoop completed at iteration {iteration}: "
                    f"content={repr(content[:500])}"
                )
                return {"status": "success", "result": content}

            if finish_reason == "tool_calls":
                thoughts = content

                if thoughts:
                    logger.info(
                        f"AgentLoop thoughts [iteration {iteration}]: {thoughts[:500]}"
                    )

                context.add_assistant_tool_calls(tool_calls, content=thoughts)

                for tc in tool_calls:
                    tool_name = tc["function"]["name"]
                    tool_args = json.loads(tc["function"]["arguments"])

                    logger.info(f"Executing tool: {tool_name}, args: {json.dumps(tool_args, ensure_ascii=False)[:200]}")

                    tool_result = await self._execute_tool_call(tc)

                    logger.debug(
                        f"AgentLoop tool result: tool={tool_name}, "
                        f"result={repr(tool_result[:500])}"
                    )
                    context.add_tool_result(
                        tool_call_id=tc["id"],
                        content=tool_result,
                    )

                continue

            if finish_reason == "length":
                logger.warning(
                    f"AgentLoop context length limit at iteration {iteration}, "
                    f"truncating earliest messages"
                )
                context.truncate_earliest_messages(keep_count=20)
                continue

        logger.warning(
            f"AgentLoop reached max_iterations={self._max_iterations}"
        )
        return {
            "status": "failure",
            "reason": f"Task did not complete within {self._max_iterations} iterations",
        }

    async def _execute_tool_call(self, tool_call: Dict) -> str:
        """
        Execute a single tool call and return the result.

        Args:
            tool_call: Dict with id, function.name, function.arguments.

        Returns:
            Tool execution result as a string.
        """
        tool_name = tool_call["function"]["name"]
        arguments_str = tool_call["function"]["arguments"]

        try:
            arguments = json.loads(arguments_str)
        except json.JSONDecodeError as e:
            logger.error(f"Invalid tool arguments JSON: {arguments_str}")
            return f"Error: invalid arguments format - {str(e)}"

        tool = self._tool_registry.get(tool_name)
        if not tool:
            logger.error(f"Unknown tool: {tool_name}")
            return f"Error: unknown tool '{tool_name}'"

        logger.info(f"Executing tool: {tool_name}, args: {arguments_str[:200]}")

        try:
            result = await tool.execute(**arguments)
            return result
        except Exception as e:
            logger.error(
                f"Tool execution error: tool={tool_name}, error={str(e)}",
                exc_info=True,
            )
            return f"Error executing tool '{tool_name}': {str(e)}"
