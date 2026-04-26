"""
Workspace manager for agent execution isolation.

Each session gets an isolated workspace directory under the configured root.
All files created during agent execution are confined to this directory.

Workspace structure:
    ~/PrivateBuddyData/
        workspace/
            1/          -- session_id=1 workspace
            2/          -- session_id=2 workspace
            ...

The workspace root defaults to ~/PrivateBuddyData/workspace if not configured.
"""

import os
from pathlib import Path

from app.config import get_settings
from app.logger import logger


DEFAULT_DATA_ROOT = Path.home() / "PrivateBuddyData"


def get_workspace_root() -> Path:
    """
    Get the workspace root directory.

    If not configured via WORKSPACE_ROOT env var, defaults to
    ~/PrivateBuddyData/workspace.

    Returns:
        Path to the workspace root directory.
    """
    settings = get_settings()
    if settings.workspace_root:
        return Path(settings.workspace_root)
    return DEFAULT_DATA_ROOT / "workspace"


def ensure_session_workspace(session_id: int) -> Path:
    """
    Ensure a workspace directory exists for the given session.

    The directory name is the session_id for simplicity and isolation.
    If the directory already exists, it is returned as-is
    (no content is cleared).

    Args:
        session_id: The session's database ID.

    Returns:
        Absolute path to the session's workspace directory.
    """
    root = get_workspace_root()
    root.mkdir(parents=True, exist_ok=True)

    workspace = root / str(session_id)
    workspace.mkdir(parents=True, exist_ok=True)

    abs_path = workspace.resolve()
    logger.info(f"Workspace ensured for session {session_id}: {abs_path}")
    return abs_path


def get_session_workspace(session_id: int) -> Path:
    """
    Get the workspace path for a session without creating it.

    Args:
        session_id: The session's database ID.

    Returns:
        Absolute path to the session's workspace directory.
    """
    root = get_workspace_root()
    return (root / str(session_id)).resolve()


def remove_session_workspace(session_id: int) -> bool:
    """
    Remove the workspace directory for a session.

    Args:
        session_id: The session's database ID.

    Returns:
        True if the directory was removed, False if it didn't exist.
    """
    workspace = get_session_workspace(session_id)
    if not workspace.exists():
        return False

    import shutil
    shutil.rmtree(workspace)
    logger.info(f"Workspace removed for session {session_id}: {workspace}")
    return True


def is_within_workspace(path: str, workspace: Path) -> bool:
    """
    Check if a given path is within the workspace.

    This is used by BashTool to enforce directory confinement.

    Args:
        path: The path to check (can be relative or absolute).
        workspace: The workspace path.

    Returns:
        True if the resolved path is within the workspace.
    """
    try:
        resolved = Path(path).resolve()
        workspace_resolved = workspace.resolve()
        return str(resolved).startswith(str(workspace_resolved) + os.sep) or resolved == workspace_resolved
    except (OSError, ValueError):
        return False
