# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


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
