package policy

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Gao-OS/StrataOS/internal/capability"
)

// enforceConstraints checks all constraints from the capability against ctx.
func enforceConstraints(claims *capability.Capability, ctx map[string]any) error {
	if err := enforcePathPrefix(claims.Constraints.PathPrefix, ctx); err != nil {
		return err
	}
	if err := enforceRateLimit(claims.ID, claims.Constraints.RateLimit); err != nil {
		return err
	}
	return nil
}

// enforcePathPrefix ensures ctx["path"] is within the allowed prefix.
// Rejects ".." traversal attempts.
func enforcePathPrefix(prefix string, ctx map[string]any) error {
	if prefix == "" {
		return nil
	}
	path, _ := ctx["path"].(string)
	if path == "" {
		return nil
	}

	if strings.Contains(path, "..") {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: "path traversal not allowed",
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("cannot resolve path: %v", err),
		}
	}
	absPrefix, err := filepath.Abs(prefix)
	if err != nil {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("cannot resolve prefix: %v", err),
		}
	}

	if absPath != absPrefix && !strings.HasPrefix(absPath, absPrefix+string(filepath.Separator)) {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("path %s outside allowed prefix %s", absPath, absPrefix),
		}
	}
	return nil
}

// In-memory token bucket rate limiter, keyed by cap_id.
var globalLimiter = &rateLimiter{
	buckets: make(map[string]*bucket),
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	rate   float64
	last   time.Time
}

// parseRate parses "50rps" into 50.0 tokens/second.
func parseRate(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "rps") {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(s, "rps"), 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func enforceRateLimit(capID, rateLimit string) error {
	if rateLimit == "" {
		return nil
	}
	rate, ok := parseRate(rateLimit)
	if !ok {
		return nil
	}

	globalLimiter.mu.Lock()
	defer globalLimiter.mu.Unlock()

	b, exists := globalLimiter.buckets[capID]
	if !exists {
		b = &bucket{tokens: rate, rate: rate, last: time.Now()}
		globalLimiter.buckets[capID] = b
	}

	// Refill tokens based on elapsed time.
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.rate {
		b.tokens = b.rate
	}
	b.last = now

	if b.tokens < 1 {
		return &PolicyError{
			Code:    CodeResourceExhausted,
			Name:    "RESOURCE_EXHAUSTED",
			Message: "rate limit exceeded",
		}
	}
	b.tokens--
	return nil
}
