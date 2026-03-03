package supervisor

import "time"

// QuarantineConfig controls when a service is quarantined.
type QuarantineConfig struct {
	MaxCrashes int           // crashes within Window to trigger quarantine
	Window     time.Duration // sliding window duration
}

// DefaultQuarantine returns a sensible default: 5 crashes in 2 minutes.
func DefaultQuarantine() QuarantineConfig {
	return QuarantineConfig{
		MaxCrashes: 5,
		Window:     2 * time.Minute,
	}
}

// ShouldQuarantine returns true if the number of crashes within the window
// meets or exceeds the threshold.
func ShouldQuarantine(crashes []time.Time, cfg QuarantineConfig) bool {
	if cfg.MaxCrashes <= 0 {
		return false
	}
	now := time.Now()
	cutoff := now.Add(-cfg.Window)
	count := 0
	for _, t := range crashes {
		if t.After(cutoff) {
			count++
		}
	}
	return count >= cfg.MaxCrashes
}
