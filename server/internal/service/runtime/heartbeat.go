package runtime

import (
	"context"

	"private-buddy-server/internal/service/memory"

	applogger "private-buddy-server/internal/logger"
)

// Heartbeat check frequency constants.
const (
	memoryDensityCheckInterval = 6 // Every 6 heartbeat ticks
)

// handleHeartbeat processes a heartbeat tick for periodic maintenance.
//
// The heartbeat handles periodic checks (obligations, memory density).
// Responsiveness is guaranteed by the eventqueue — user messages, scheduled
// events, etc. all trigger agent actions via interrupts. Heartbeat does not
// need to poll for unread messages or drive proactive messaging.
func (r *agentRuntime) handleHeartbeat(ctx context.Context) {
	if len(r.activeWorks) > 0 {
		// Agent is busy — no heartbeat processing needed
		return
	}

	r.heartbeatTick++
	r.idleTicks++

	// Memory density check (every 6 ticks)
	if r.heartbeatTick%memoryDensityCheckInterval == 0 {
		r.checkMemoryDensity(ctx)
	}
}

// checkMemoryDensity runs memory density check: detects when enough long-term
// observations have accumulated around an entity to trigger EntityProfile
// generation.
func (r *agentRuntime) checkMemoryDensity(ctx context.Context) {
	triggered := memory.CheckProfileDensity(ctx, r.agentID)
	if triggered > 0 {
		applogger.Info("memory density check: EntityProfile generation triggered",
			"agent_id", r.agentID,
			"profiles_triggered", triggered,
		)
	} else {
		applogger.Debug("memory density check: no profiles triggered",
			"agent_id", r.agentID,
		)
	}
}
