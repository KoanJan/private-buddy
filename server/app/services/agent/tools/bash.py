"""
Bash tool for executing shell commands within a workspace.

Provides the agent with the ability to run shell commands on the local system.
Commands are confined to the task's workspace directory to ensure isolation.
Supports configurable timeout and returns stdout, stderr, and exit code.
"""

import asyncio
import os
from pathlib import Path
from typing import Any, Dict, Optional

from app.services.agent.tools.base import Tool
from app.logger import logger


class BashTool(Tool):
    """
    Tool for executing shell commands via subprocess.

    Returns structured output containing stdout, stderr, and exit code.
    Commands are executed with a configurable timeout to prevent hanging.
    When a workspace is set, commands are executed with CWD set to the
    workspace directory and path traversal is blocked.
    """

    DEFAULT_TIMEOUT = 30000

    def __init__(self, workspace: Optional[Path] = None):
        """
        Initialize BashTool with optional workspace confinement.

        Args:
            workspace: If set, commands run with CWD=workspace and
                       path traversal outside workspace is blocked.
        """
        self._workspace = workspace

    @property
    def name(self) -> str:
        return "bash"

    @property
    def schema(self) -> Dict[str, Any]:
        workspace_hint = ""
        if self._workspace:
            workspace_hint = (
                f" All file operations must be within {self._workspace}. "
                "Do not access paths outside this directory."
            )
        return {
            "type": "function",
            "function": {
                "name": "bash",
                "description": (
                    "Execute a shell command. Use this tool to run commands, "
                    "manage files, and interact with the system." + workspace_hint
                ),
                "parameters": {
                    "type": "object",
                    "properties": {
                        "command": {
                            "type": "string",
                            "description": "The shell command to execute",
                        },
                        "timeout": {
                            "type": "integer",
                            "description": "Timeout in milliseconds (default: 30000)",
                            "default": self.DEFAULT_TIMEOUT,
                        },
                    },
                    "required": ["command"],
                },
            },
        }

    async def execute(self, **kwargs) -> str:
        """
        Execute a shell command and return the result.

        When workspace is set:
        - CWD is set to the workspace directory
        - Commands attempting to traverse outside workspace are blocked

        Args:
            command: The shell command to execute.
            timeout: Timeout in milliseconds (default: 30000).

        Returns:
            JSON string with stdout, stderr, and exit_code fields.
        """
        command = kwargs.get("command", "")
        timeout_ms = kwargs.get("timeout", self.DEFAULT_TIMEOUT)
        timeout_sec = timeout_ms / 1000.0

        if not command:
            return '{"stdout": "", "stderr": "Error: empty command", "exit_code": 1}'

        if self._workspace and self._is_path_traversal(command):
            logger.warning(
                f"BashTool blocked path traversal: {command[:200]}"
            )
            return (
                '{"stdout": "", "stderr": '
                '"Error: command attempts to access paths outside workspace", '
                '"exit_code": 1}'
            )

        cwd = str(self._workspace) if self._workspace else None
        logger.info(
            f"BashTool executing: {command[:200]} "
            f"(timeout: {timeout_ms}ms, cwd: {cwd or 'default'})"
        )

        try:
            process = await asyncio.create_subprocess_shell(
                command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=cwd,
            )

            try:
                stdout_bytes, stderr_bytes = await asyncio.wait_for(
                    process.communicate(), timeout=timeout_sec
                )
            except asyncio.TimeoutError:
                process.kill()
                await process.wait()
                logger.warning(f"BashTool timeout: {command[:100]}")
                return '{"stdout": "", "stderr": "Error: command timed out", "exit_code": -1}'

            stdout = stdout_bytes.decode("utf-8", errors="replace")
            stderr = stderr_bytes.decode("utf-8", errors="replace")
            exit_code = process.returncode if process.returncode is not None else -1

            result = (
                f'{{"stdout": {self._escape(stdout)}, '
                f'"stderr": {self._escape(stderr)}, '
                f'"exit_code": {exit_code}}}'
            )

            logger.info(
                f"BashTool result: exit_code={exit_code}, "
                f"stdout_len={len(stdout)}, stderr_len={len(stderr)}"
            )
            return result

        except Exception as e:
            logger.error(f"BashTool error: {str(e)}", exc_info=True)
            return f'{{"stdout": "", "stderr": "Error: {self._escape_str(str(e))}", "exit_code": 1}}'

    def _is_path_traversal(self, command: str) -> bool:
        """
        Detect obvious path traversal attempts in a command.

        Blocks commands that reference paths outside the workspace
        using absolute paths or parent directory references.

        This is a best-effort check; it cannot guarantee complete
        confinement but catches the most common patterns.

        Args:
            command: The shell command to check.

        Returns:
            True if path traversal is detected.
        """
        if not self._workspace:
            return False

        workspace_str = str(self._workspace)
        parts = command.split()
        for part in parts:
            if part.startswith("/") and not part.startswith(workspace_str):
                if not _is_safe_absolute_path(part, self._workspace):
                    return True
            if ".." in part:
                resolved = _safe_resolve(part, self._workspace)
                if resolved and not str(resolved).startswith(workspace_str):
                    return True
        return False

    @staticmethod
    def _escape(text: str) -> str:
        """Escape a string for safe JSON embedding."""
        escaped = text.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n").replace("\r", "\\r").replace("\t", "\\t")
        return f'"{escaped}"'

    @staticmethod
    def _escape_str(text: str) -> str:
        """Escape a string for safe embedding inside a JSON string value."""
        return text.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n").replace("\r", "\\r").replace("\t", "\\t")


def _is_safe_absolute_path(path_str: str, workspace: Path) -> bool:
    """
    Check if an absolute path is a common system utility or within workspace.

    System commands like /bin/ls, /usr/bin/python are allowed.
    Only file paths outside workspace are blocked.

    Args:
        path_str: The absolute path to check.
        workspace: The workspace path.

    Returns:
        True if the path is considered safe.
    """
    safe_prefixes = (
        "/bin/", "/usr/bin/", "/usr/local/bin/",
        "/sbin/", "/usr/sbin/",
        "/opt/homebrew/",
    )
    for prefix in safe_prefixes:
        if path_str.startswith(prefix):
            return True
    return False


def _safe_resolve(path_str: str, workspace: Path) -> Optional[Path]:
    """
    Safely resolve a path relative to the workspace.

    Args:
        path_str: The path string (may be relative).
        workspace: The workspace path.

    Returns:
        Resolved path or None if resolution fails.
    """
    try:
        if os.path.isabs(path_str):
            return Path(path_str).resolve()
        return (workspace / path_str).resolve()
    except (OSError, ValueError):
        return None
