// Package experience implements the self-evolving experience system for agents.
//
// Private experiences (agent_experiences) are auto-generated from task execution
// notes via LLM reflection. They belong to individual agents and serve as personal
// cognitive assets — similar to entity_profiles in the memory system.
//
// Public experiences are externally injected knowledge accessible to all agents,
// analogous to knowledge_bases. They are created by ingesting external skill files
// (SKILL.md) via LLM refinement and serve as a "library" agents can search and
// potentially learn from (learning mechanism is a TODO for future iterations).
//
// Key API:
//   - Init: sets the shared embedding service reference (must be called at startup).
//   - SearchExperiences: semantic search over an agent's private experiences.
//   - CheckReflection: heartbeat callback that distills reusable experience from
//     task execution notes via LLM reflection.
package experience
