"""
Workspace manager for agent execution isolation.

Each session gets an isolated workspace directory under the configured root.
All files created during agent execution are confined to this directory.

Workspace structure (0.0.8):
    ~/PBD_trial_electron/
        db/                         -- SQLite database + vector store (vectors.db)
        workspace/
            1/                      -- session_id=1 workspace
                .meta/
                    task.md         -- rewritten task requirements (system-managed)
                    notes.md        -- agent's structured working notes
                output/             -- LLM working directory (deliverables + temp files)
            2/                      -- session_id=2 workspace
            ...
        avatars/                    -- agent avatar images

Design principles:
- .meta/ is system-managed, LLM should NOT directly modify files here
- output/ is the LLM's working directory for all file operations
- notes.md uses append-only structured format for traceability

The workspace root defaults to ~/PrivateBuddyData/workspace if not configured.
"""

import os
from datetime import datetime
from pathlib import Path
from typing import List, Literal, Optional

from app.config import get_settings
from app.logger import logger

# Note entry types for structured logging
NOTE_TYPE_OBSERVATION = "observation"
NOTE_TYPE_DECISION = "decision"
NOTE_TYPE_FINDING = "finding"
NOTE_TYPE_CORRECTION = "correction"
NOTE_TYPE_PROGRESS = "progress"

NOTE_TYPES = Literal["observation", "decision", "finding", "correction", "progress"]


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
    return Path(settings.data_root) / "workspace"


def get_avatars_dir() -> Path:
    """
    Get the avatars directory for agent avatar images.

    Defaults to ~/PrivateBuddyData/avatars. Creates the directory
    if it does not exist.

    Returns:
        Path to the avatars directory.
    """
    settings = get_settings()
    avatars_dir = Path(settings.data_root) / "avatars"
    avatars_dir.mkdir(parents=True, exist_ok=True)
    return avatars_dir


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


def get_meta_dir(session_id: int) -> Path:
    """
    Get the .meta directory path for a session.

    Args:
        session_id: The session's database ID.

    Returns:
        Absolute path to the .meta directory.
    """
    return get_session_workspace(session_id) / ".meta"


def get_output_dir(session_id: int) -> Path:
    """
    Get the output directory path for a session.

    This is the LLM's working directory for all file operations.

    Args:
        session_id: The session's database ID.

    Returns:
        Absolute path to the output directory.
    """
    return get_session_workspace(session_id) / "output"


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


def init_session_workspace(session_id: int, rewritten_requirement: str) -> Path:
    """
    Initialize workspace structure for agent execution.

    Creates:
    - .meta/task.md with rewritten task requirement
    - .meta/notes.md as empty file (structured, append-only)
    - output/ directory (LLM's working directory)

    If workspace already exists (from previous execution in same session):
    - .meta/task.md: append rewritten requirement (tasks may evolve)
    - .meta/notes.md: keep as-is (agent's memory across executions)
    - output/: keep as-is (previous deliverables)

    Args:
        session_id: The session's database ID.
        rewritten_requirement: Task requirement already rewritten by
            TaskRequirementRewriter. Raw user messages should NOT be
            passed here -- rewrite first, then call this function.

    Returns:
        Absolute path to the session's workspace directory.
    """
    workspace = ensure_session_workspace(session_id)

    # Create .meta/ directory
    meta_dir = workspace / ".meta"
    meta_dir.mkdir(exist_ok=True)

    # .meta/task.md: append rewritten requirement with timestamp
    task_file = meta_dir / "task.md"
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    if task_file.exists():
        with open(task_file, "a") as f:
            f.write(f"\n\n---\n\n## [{timestamp}] Task Update\n\n{rewritten_requirement}")
    else:
        with open(task_file, "w") as f:
            f.write(f"# Task\n\n{rewritten_requirement}")

    # .meta/notes.md: create empty file if not exists
    notes_file = meta_dir / "notes.md"
    if not notes_file.exists():
        notes_file.write_text("# Agent Notes\n\nStructured log of agent's work progress.\n\n")

    # output/ directory (LLM's working directory)
    output_dir = workspace / "output"
    output_dir.mkdir(exist_ok=True)

    return workspace


def read_task_md(session_id: int) -> str:
    """
    Read the full content of .meta/task.md for a session.

    Args:
        session_id: The session's database ID.

    Returns:
        Full text content of task.md, or empty string if not found.
    """
    task_file = get_meta_dir(session_id) / "task.md"
    if not task_file.exists():
        return ""
    return task_file.read_text()


def read_notes_md(session_id: int) -> str:
    """
    Read the full content of .meta/notes.md for a session.

    Args:
        session_id: The session's database ID.

    Returns:
        Full text content of notes.md, or empty string if not found.
    """
    notes_file = get_meta_dir(session_id) / "notes.md"
    if not notes_file.exists():
        return ""
    return notes_file.read_text()


def append_note(
    session_id: int,
    entry_type: NOTE_TYPES,
    content: str,
    references: Optional[List[str]] = None,
    conflicts_with: Optional[str] = None,
) -> None:
    """
    Append a structured note entry to notes.md.

    This is the core function for the append-only notes system.
    Each entry has:
    - Timestamp: when the entry was written
    - Type: observation/decision/finding/correction/progress
    - Content: the main note text
    - References: optional links to workspace files
    - Conflict marker: optional note about conflicting with a previous entry

    Args:
        session_id: The session's database ID.
        entry_type: Type of the note entry.
        content: The main content of the note.
        references: Optional list of file paths in workspace (relative to output/).
        conflicts_with: Optional timestamp or identifier of conflicting entry.
    """
    notes_file = get_meta_dir(session_id) / "notes.md"
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    # Build the entry
    lines = [
        f"## [{timestamp}] {entry_type.upper()}",
        "",
        content,
    ]

    if references:
        lines.append("")
        lines.append("**References:**")
        for ref in references:
            lines.append(f"- `{ref}`")

    if conflicts_with:
        lines.append("")
        lines.append(f"⚠️ **Conflicts with:** {conflicts_with}")
        lines.append("_See above for the previous entry that this corrects or supersedes._")

    lines.append("")
    lines.append("---")
    lines.append("")

    # Append to file
    with open(notes_file, "a") as f:
        f.write("\n".join(lines))

    logger.info(
        f"Note appended for session {session_id}: type={entry_type}, "
        f"content_len={len(content)}, refs={len(references or [])}"
    )


def write_notes_md(session_id: int, content: str) -> None:
    """
    Write content to notes.md for a session.

    This overwrites the entire file. Used by WriteNotesTool
    during mandatory notes checkpoints.

    WARNING: This is a legacy function for backward compatibility.
    Prefer append_note() for structured, traceable notes.

    Args:
        session_id: The session's database ID.
        content: The content to write to notes.md.
    """
    notes_file = get_meta_dir(session_id) / "notes.md"
    notes_file.write_text(content)
    logger.info(f"notes.md written for session {session_id}: {len(content)} chars")


def trim_notes_md(session_id: int) -> None:
    """
    Trim notes.md if it exceeds the configured max chars.

    Implements a sliding window strategy: keeps the newest entries
    (at the bottom of the file) and discards the oldest entries
    (at the top). This ensures the agent always has access to
    the most recent context, which is most relevant for ongoing
    decision-making.

    Truncation is aligned to entry boundaries (## [timestamp])
    to avoid splitting an entry in the middle.

    Args:
        session_id: The session's database ID.
    """
    settings = get_settings()
    max_chars = settings.notes_max_chars

    notes_file = get_meta_dir(session_id) / "notes.md"
    if not notes_file.exists():
        return
    content = notes_file.read_text()
    if len(content) <= max_chars:
        return

    trimmed = content[-max_chars:]
    entry_boundary = trimmed.find("\n## [")
    if entry_boundary > 0:
        trimmed = trimmed[entry_boundary + 1:]

    notes_file.write_text("[notes.md trimmed: older entries discarded]\n\n" + trimmed)
    logger.info(
        f"notes.md trimmed for session {session_id}: "
        f"{len(content)} -> {len(trimmed)} chars"
    )


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
