package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// CycleStatus carries the result of cycle detection after a tool call.
type CycleStatus struct {
	// Warning is appended to the tool result when a cyclical pattern is detected.
	// Empty string means no warning.
	Warning string
	// Blocked triggers a forced checkpoint at the next iteration.
	// The tool should refuse further calls until the work is reset.
	Blocked bool
	// Reason is a human-readable block reason, used in the checkpoint prompt.
	Reason string
}

// NoCycleDetected is the zero-value CycleStatus, returned when no cycle is detected.
var NoCycleDetected = CycleStatus{}

// Cycle detection thresholds.
const (
	// warnThreshold: consecutive identical (args, result) pairs before warning.
	warnThreshold = 3
	// blockThreshold: consecutive identical (args, result) pairs before blocking.
	blockThreshold = 8
)

// CycleDetector tracks whether a tool receives the same input and produces
// the same output repeatedly. It does not interpret the content of args or
// result — both are treated as opaque values. Same (args, result) pair
// repeating N times consecutively is a cycle, regardless of whether the
// result represents success or failure.
//
// Tools embed this struct. The CycleDetect method is automatically promoted
// to satisfy the Tool interface. No initialization is needed — zero values
// are correct defaults.
type CycleDetector struct {
	lastSignature string
	count         int
	blocked       bool
	blockReason   string
}

// CycleDetect checks whether the current (args, result) pair matches the
// previous one. Consecutive identical pairs increment the counter;
// any difference resets it. When the counter reaches warnThreshold,
// a warning is returned. When it reaches blockThreshold, a block is returned.
//
// This method is promoted to the embedding tool, satisfying the Tool interface.
func (d *CycleDetector) CycleDetect(args map[string]interface{}, result string) CycleStatus {
	if d.blocked {
		return CycleStatus{Blocked: true, Reason: d.blockReason}
	}

	sig := signature(args, result)
	if sig == d.lastSignature {
		d.count++
	} else {
		d.count = 0
	}
	d.lastSignature = sig

	if d.count >= blockThreshold {
		d.blocked = true
		d.blockReason = fmt.Sprintf("same call repeated %d times", d.count)
		return CycleStatus{Blocked: true, Reason: d.blockReason}
	}

	if d.count >= warnThreshold {
		return CycleStatus{
			Warning: formatWarning(d.count),
		}
	}

	return NoCycleDetected
}

// signature computes a stable hash of the args + result combination.
// args are JSON-marshalled for stable serialization; result is used as-is.
func signature(args map[string]interface{}, result string) string {
	normalizedArgs, _ := json.Marshal(args)
	return hashString(string(normalizedArgs) + ":" + result)
}

// hashString computes a SHA256 hex digest of the input string.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// formatWarning constructs a loop warning message that guides the agent
// to self-reflect rather than command it to stop.
func formatWarning(count int) string {
	return fmt.Sprintf(
		"[Loop Warning: repeated_call; count=%d]\n"+
			"The same tool call has produced the same result %d times in a row.\n"+
			"Ask yourself: Is repeating this making progress? Could a different\n"+
			"approach work? Perhaps the root cause is elsewhere.",
		count, count,
	)
}
