// Package runtime implements the Agent Runtime: the event-driven execution
// engine that transforms an Agent from a passive configuration object into
// an active, stateful entity with its own lifecycle.
//
// This file provides the RuntimeManager which manages AgentRuntime instances
// across all agents in the system. It serves as the bridge between the
// handler layer and the per-agent event loops.
package runtime

import (
	"sync"

	applogger "private-buddy-server/internal/logger"
)

// RuntimeManager manages AgentRuntime instances for all agents.
// Thread-safe. Each agent gets exactly one runtime.
type RuntimeManager struct {
	mu             sync.RWMutex
	runtimes       map[int64]*AgentRuntime // agentID -> runtime
	onStatusChange func(agentID, sessionID int64, status int)
}

// NewRuntimeManager creates a new runtime manager.
func NewRuntimeManager(onStatusChange func(agentID, sessionID int64, status int)) *RuntimeManager {
	return &RuntimeManager{
		runtimes:       make(map[int64]*AgentRuntime),
		onStatusChange: onStatusChange,
	}
}

// GetOrCreateRuntime returns the runtime for an agent, creating one if needed.
func (rm *RuntimeManager) GetOrCreateRuntime(agentID int64) *AgentRuntime {
	rm.mu.RLock()
	if rt, ok := rm.runtimes[agentID]; ok {
		rm.mu.RUnlock()
		return rt
	}
	rm.mu.RUnlock()

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Double-check after acquiring write lock
	if rt, ok := rm.runtimes[agentID]; ok {
		return rt
	}

	rt := StartAgentRuntime(agentID, rm.onStatusChange)
	rm.runtimes[agentID] = rt
	return rt
}

// SendEvent sends an event to the appropriate agent runtime.
// Creates the runtime if it doesn't exist yet.
func (rm *RuntimeManager) SendEvent(agentID int64, event AgentEvent) {
	rt := rm.GetOrCreateRuntime(agentID)
	rt.SendEvent(event)
}

// StopAll stops all agent runtimes. Called during graceful shutdown.
func (rm *RuntimeManager) StopAll() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Currently runtimes use background contexts that can't be cancelled.
	// Future iterations will add proper cancellation.
	applogger.L.Info("Stopping all agent runtimes", "count", len(rm.runtimes))
	rm.runtimes = make(map[int64]*AgentRuntime)
}

// GlobalRuntimeManager is the singleton runtime manager for the application.
// Initialized during application startup.
var GlobalRuntimeManager *RuntimeManager

// InitGlobalRuntimeManager initializes the global runtime manager.
func InitGlobalRuntimeManager(onStatusChange func(agentID, sessionID int64, status int)) {
	GlobalRuntimeManager = NewRuntimeManager(onStatusChange)
	applogger.L.Info("Global runtime manager initialized")
}
