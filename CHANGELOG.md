# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [0.0.14] - 2026-06-09

### Added
- **Agent Runtime**: new runtime architecture with AgentRuntime/Work/Manager, implementing ReAct loop with Bash and WebSearch tools, SSE-based status push, and participant status tracking (idle/working)
- **Agent Status Bar**: chat window top bar showing agent avatars with animated status indicators (green=idle, pulsing blue=working), agent name on hover
- **Message Draft Model**: interaction records now associated with drafts instead of messages, enabling proper message isolation between user-agent and agent-world boundaries
- **Participant Session Model**: tracks session participants with type/role/status, supporting future multi-agent scenarios
- **Pre-release Data Reset**: 0.0.x versions automatically wipe user data on version change to avoid schema migration issues

### Changed
- **Enum Storage Convention**: all enum fields across 4 tables (participant_sessions, messages, documents, knowledge_bases) migrated from string to int, with database rebuild migration and string→int value mapping
- **Message Role**: `role` field changed from string ("user"/"assistant") to int (1/2) across backend models, API schemas, and frontend types
- **Interaction Query**: API adjusted to query interactions via draft_id instead of user_msg_id + agent_msg_id

### Fixed
- **Database Migration Detection**: enum migration check now correctly handles VARCHAR columns (not just TEXT), fixing silent migration skip
- **Message List Styling**: CSS class mapping updated after role type change from string to int

### Fixed
- **LLM Hallucination Prevention**: discard tool_call reasoning content in TaskLoop to prevent internal process information from leaking into chat layer, which caused the chat LLM to misinterpret reasoning (e.g., "the command is correct") as accomplished facts (e.g., "the service is running")
- **Service Degradation Fix**: relax trigger message check in assembleSimpleContext from "must be the latest completed message" to "must exist in the completed messages list", fixing false degradation in concurrent/multi-agent scenarios
- **SQLite ID Reuse Prevention**: add ensureAutoIncrement to rebuild tables with AUTOINCREMENT keyword, ensuring primary key IDs are strictly monotonically increasing and never reused after row deletion
- **Goroutine Leak on Session Deletion**: add TaskCancelManager to cancel running processChatTask goroutines when their session is deleted, preventing stale goroutines from overwriting data

### Changed
- **Message Rendering**: remove streaming chunk rendering in favor of whole-message updates with loading spinner, eliminating chunk interleaving issues in multi-agent scenarios
- **SSE Connection Management**: establish persistent SSE connection per session instead of per-message, with auto-reconnect on session switch

## [0.0.12] - 2026-06-05

### Changed
- **Session List UI**: flattened two-level Collapse structure to single list sorted by updated_at, agent avatar displayed on left side of each session item
- **New Session Button**: toolbar-style MessageCircle icon with agent dropdown selection

### Fixed
- **HNSW Index**: catch panic when adding node at runtime via safeAddToGraph, consistent with batch build behavior


## [0.0.11] - 2026-05-11

### Added
- **Knowledge Base**: full KB lifecycle management — create, delete, document upload, async processing pipeline (extraction → chunking → embedding → indexing), per-KB isolated SQLite vector storage
- **HNSW Index**: dual-mode indexing (Flat → HNSW) with auto-switching by vector count, CAS-based concurrent transition, lazy loading, and startup recovery
- **RAG Integration**: Agent binds knowledge bases; chat pipeline retrieves from bound KBs concurrently and injects results into context
- **Knowledge Base UI**: list/detail views with document upload, status tracking, and KB binding in Agent config

### Changed
- **Chat Pipeline**: reorganized with KB retrieval step between preprocessing and context assembly
- **Document Deletion**: Document records hard-deleted, chunks soft-deleted with `deleted_count` tracking

### Fixed
- **Chat Window**: SSE reconnection on page refresh during streaming, race condition on session switch, temp-to-real session transition preserving streaming state


## [0.0.10] - 2026-05-08

### Added
- ResizableCard component for draggable card-based UI layout

### Changed
- BashTool: use `cmd /c` on Windows instead of `bash -c`
- Embedding option label: "Default" → "Not used"
- Chat input placeholder: "Finally remembered me? Shift+Enter for new line"
- Replace console.log with logger in api.ts

### Fixed
- Agent avatar not persisted when creating with avatar selected


## [0.0.9] - 2026-05-06

### Added
- **Electron Desktop Application**: cross-platform desktop packaging for macOS, Windows, and Linux with one-click build commands (`npm run dist:mac|win|linux`)
- **Go Backend**: complete rewrite of backend from Python to Go with native cross-compilation support (GOOS/GOARCH)
- **Dynamic Port Allocation**: Electron main process automatically assigns free port to Go server, frontend dynamically resolves via IPC
- **Custom Title Bar**: VS Code-style title bar with `titleBarStyle: 'hidden'` + `titleBarOverlay`, native window controls (traffic lights on macOS, min/max/close on Win/Linux) overlaid on web content
- **Splash Screen**: loading screen during Go server startup with health check polling
- **IPC Bridge**: secure communication between renderer and main process via contextBridge (`getServerPort`, `getAppVersion`, `getPlatform`, `onBackendStatus`, `onBackendError`)

### Changed
- **Backend Stack**: FastAPI → Gin, SQLAlchemy → GORM, langchain-openai → go-openai, numpy → custom cosine similarity
- **SQLite Driver**: switched to pure-Go `modernc.org/sqlite` (no CGO required) enabling hassle-free cross-compilation
- **Service Scripts**: `start.sh`/`stop.sh`/`restart.sh` converted from Python (uvicorn) to Go binary management with PID file tracking
- **Vite Config**: `base: './'` for relative paths, `outDir` to project-level `web-dist/` for electron-builder packaging
- **Header Styling**: height reduced from 56px to 38px, logo/icon/text scaled down to match native window controls

### Removed
- **Python Backend**: entire `server/app/` directory (API, services, models, schemas) deleted
- **Python Dependencies**: `pyproject.toml`, `setup.sh`, `venv/` no longer needed
- **Manual Database Init**: `server/database/` removed (Go uses GORM AutoMigrate)
- **Unused Assets**: `web/public/icons.svg` (social icon sprite with no references)

### Performance
- Backend binary size reduced from ~100 MB+ (PyInstaller) to ~33 MB (go build)
- Startup time reduced from 2-5 seconds (Python interpreter) to <1 second (native binary)
- Cross-platform builds now possible from a single macOS machine


## [0.0.8] - 2026-05-01

### Changed
- **Database Migration: MySQL → SQLite**: replaced MySQL with SQLite for desktop application compatibility, eliminating the need for users to install and configure a database server
- **Engine Configuration**: removed MySQL-specific `pool_pre_ping` and `pool_recycle`, added SQLite `check_same_thread=False` and WAL mode pragma
- **SQL Migration Scripts**: consolidated all incremental SQL files (0.0.1 through 0.0.7) into a single `full_init.sql` for fresh database initialization
- **Database Initialization**: `init_db.sh` now supports `init` (full) and `upgrade` (incremental) modes, uses `sqlite3` client
- **ORM Models**: removed `comment=` parameters (SQLite does not support column comments); fixed `SearchConfig.updated_at` to be NOT NULL with server_default
- **Task Loop LLM Configuration**: checkpoint client now uses agent's LLM config instead of separate environment variables, eliminating redundant configuration
- **Data Directory**: unified all application data under `~/PrivateBuddyData/` (db, chroma, workspace, avatars)
- **Environment Variables**: simplified to `DATA_ROOT` only; `DATABASE_URL`, `CHROMA_PERSIST_DIR`, and `LLM_*` variables removed

### Removed
- **pymysql dependency**: no longer needed after SQLite migration
- **LLM environment variables**: `LLM_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY` replaced by database-stored agent LLM configs
- **migrate_mysql_to_sqlite.py**: one-time migration script deleted after use

### Added
- **Auto data directory creation**: `PrivateBuddyData/db` directory automatically created on application startup
- **SQLite PRAGMA configuration**: WAL journal mode and foreign keys enabled on connection
- **Database Version Tracking**: `db_versions` table and `DBVersion` model for schema version management
- **Version API**: `GET /api/version` endpoint returns database schema version from `db_versions` table
- **Upgrade SQL Directory**: `sql/upgrade/` for incremental schema changes in future versions


## [0.0.7] - 2026-05-01

### Added
- **Cached Narrative Generation**: narrative generated alongside summary in background task, stored in `historical_summaries.narrative` field with atomic write, eliminating real-time narrative generation bottleneck during chat processing
- **CachedStaticFiles**: custom StaticFiles class with `Cache-Control: public, max-age=86400` for avatar images, enabling browser-side caching

### Changed
- **Parallel LLM Calls**: User State inference and Query Preprocessing now run concurrently via `asyncio.gather` when V >= N, reducing combined latency from sum to max of both calls
- **Narrative Retrieval**: follows same versioning policy as summary — get latest available version without requiring alignment with current message count
- **Segments Section**: RAG-retrieved segments now rendered as independent section with narrative-style transition in context assembly

### Performance
- Chat response time reduced from 60-90s to 25-50s (V >= N scenarios)
- Avatar HTTP requests eliminated for 24h after first load via browser caching


## [0.0.6] - 2026-05-01

### Added
- **Markdown Rendering**: assistant messages rendered with react-markdown + remark-gfm
- **Custom Agent Avatar**: upload, store, and display custom avatar images for each agent
- **Project Logo**: display favicon.svg in header next to app title
- **Message Time Formatting**: contextual display — same day (time only), yesterday ("Yesterday" + time), older (full date + time) with i18n support
- **Agent Avatar in Chat**: AI messages show the agent's avatar alongside the name
- **LoadingSpinner Component**: braille rotation animation replacing typing dots for streaming messages
- **ConfigIcon Component**: unified icon rendering for agent/LLM/embedding/search/language types
- **CSS Theme Variables**: centralized `--color-*` variables for consistent theming

### Changed
- **Settings Panel**: restructured as right-side drawer with card grid overview instead of main area switching
- **Language Switching**: from dropdown menu to card-based selection
- **Agent List**: removed expand/collapse arrows, added avatar display
- **Settings Labels**: simplified ("LLM Config" → "LLM", "Agent Config" → "Agent", etc.)
- **Inline Colors**: replaced hardcoded hex values with CSS variables across all components

### Fixed
- Scroll-to-top issue when opening historical sessions


## [0.0.5] - 2026-04-30

### Added
- **Finite Working Memory**: iteration window for context visibility, older iterations discarded directly
- **Reader-Oriented Notes**: structured append-only notes.md via write_notes tool, bridging LLM statelessness
- **Forced Checkpoint**: mandatory notes write when distance from last write reaches window boundary
- **Workspace Structure**: `.meta/` (task.md + notes.md) + `output/` two-channel isolation
- **Task Requirement Rewriting**: ambiguous user messages rewritten into clear, self-contained task descriptions

### Changed
- Refactored `agent/` to `task/` for naming consistency with task execution semantics
- Moved chat context logic under `chat/` and created shared DTO module to eliminate circular imports


## [0.0.4] - 2026-04-26

### Added
- **Agent Execution System**: ReAct pattern with minimal tool set (Bash + Web Search)
- **Interaction Boundary**: separate storage for agent-world interactions, isolated from user conversation
- **Search Engine Integration**: configurable Tavily/DuckDuckGo with automatic tool availability detection


## [0.0.3] - 2026-04-24

### Added
- **Narrative Optimization**: internal focalization for background story, cohesive section transitions
- **User State Inference**: three-dimensional model (emotion, purpose, situation) for response strategy guidance


## [0.0.2] - 2026-04-22

### Added
- Context engineering: automatic conversation summary and background narrative generation
- Smart query preprocessing: query classification, rewriting, and clarification
- Character settings: customizable agent personality and style
- RAG integration: retrieve relevant historical context for better responses

### Changed
- Improved context assembly with decoupled summary and recent messages
- Optimized LLM prompts for better multilingual support


## [0.0.1] - 2026-04-17

### Added
- Basic chat functionality with AI agents
- Agent and LLM configuration management
- Session and message history
- SSE streaming for chat responses
- Multi-language support (English and Chinese)
