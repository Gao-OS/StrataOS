package supervisor

import "time"

// BackoffConfig controls exponential backoff for crash restarts.
type BackoffConfig struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

// DefaultBackoff returns a sensible default: 1s base, 30s cap.
func DefaultBackoff() BackoffConfig {
	return BackoffConfig{
		BaseDelay: 1 * time.Second,
		MaxDelay:  30 * time.Second,
	}
}

// ComputeDelay returns the backoff delay for the given crash count.
// Sequence: base * 2^(count-1), capped at max.
// A crashCount of 0 or 1 returns BaseDelay.
func ComputeDelay(crashCount int, cfg BackoffConfig) time.Duration {
	if crashCount <= 1 {
		return cfg.BaseDelay
	}
	delay := cfg.BaseDelay
	for i := 1; i < crashCount; i++ {
		delay *= 2
		if delay > cfg.MaxDelay {
			return cfg.MaxDelay
		}
	}
	return delay
}
