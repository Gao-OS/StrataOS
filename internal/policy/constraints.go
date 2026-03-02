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
// Rejects absolute paths (protocol requires relative-only) and ".." traversal attempts.
func enforcePathPrefix(prefix string, ctx map[string]any) error {
	if prefix == "" {
		return nil
	}
	path, _ := ctx["path"].(string)
	if path == "" {
		return nil
	}

	// Protocol requires relative paths only.
	if strings.HasPrefix(path, "/") {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: "absolute paths not allowed; use relative paths under path_prefix",
		}
	}

	if strings.Contains(path, "..") {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: "path traversal not allowed",
		}
	}

	// Resolve the full path: prefix + relative path.
	absPrefix, err := filepath.Abs(prefix)
	if err != nil {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("cannot resolve prefix: %v", err),
		}
	}
	absPath, err := filepath.Abs(filepath.Join(prefix, path))
	if err != nil {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("cannot resolve path: %v", err),
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

// staleBucketTTL is the duration after which an unused rate limit bucket is evicted.
const staleBucketTTL = 5 * time.Minute

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

// CodeInvalidArgument matches the protocol error code for bad input.
const CodeInvalidArgument = 1

func enforceRateLimit(capID, rateLimit string) error {
	if rateLimit == "" {
		return nil
	}
	rate, ok := parseRate(rateLimit)
	if !ok {
		return &PolicyError{
			Code:    CodeInvalidArgument,
			Name:    "INVALID_ARGUMENT",
			Message: fmt.Sprintf("unparseable rate limit: %q (expected format: \"50rps\")", rateLimit),
		}
	}

	globalLimiter.mu.Lock()
	defer globalLimiter.mu.Unlock()

	// Lazy eviction of stale buckets to prevent unbounded memory growth.
	now := time.Now()
	for id, b := range globalLimiter.buckets {
		if now.Sub(b.last) > staleBucketTTL {
			delete(globalLimiter.buckets, id)
		}
	}

	b, exists := globalLimiter.buckets[capID]
	if !exists {
		b = &bucket{tokens: rate, rate: rate, last: now}
		globalLimiter.buckets[capID] = b
	}

	// Refill tokens based on elapsed time.
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
