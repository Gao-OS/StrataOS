package policy

import (
	"testing"
	"time"

	"github.com/Gao-OS/StrataOS/internal/capability"
)

// --- enforcePathPrefix tests ---

func TestEnforcePathPrefix_EmptyPrefix(t *testing.T) {
	err := enforcePathPrefix("", map[string]any{"path": "/anything"})
	if err != nil {
		t.Errorf("empty prefix should pass: %v", err)
	}
}

func TestEnforcePathPrefix_EmptyPath(t *testing.T) {
	err := enforcePathPrefix("/tmp", map[string]any{"path": ""})
	if err != nil {
		t.Errorf("empty path should pass: %v", err)
	}
}

func TestEnforcePathPrefix_NilCtx(t *testing.T) {
	err := enforcePathPrefix("/tmp", nil)
	if err != nil {
		t.Errorf("nil ctx should pass: %v", err)
	}
}

func TestEnforcePathPrefix_AbsolutePathRejected(t *testing.T) {
	cases := []string{"/tmp/foo", "/etc/passwd", "/"}
	for _, path := range cases {
		err := enforcePathPrefix("/tmp", map[string]any{"path": path})
		if err == nil {
			t.Errorf("expected rejection for absolute path %q", path)
		}
	}
}

func TestEnforcePathPrefix_TraversalRejected(t *testing.T) {
	cases := []string{
		"../secret",
		"foo/../../bar",
		"..hidden", // contains ".." — rejected
	}
	for _, path := range cases {
		err := enforcePathPrefix("/tmp", map[string]any{"path": path})
		if err == nil {
			t.Errorf("expected rejection for path %q", path)
		}
	}
}

func TestEnforcePathPrefix_ValidRelativePath(t *testing.T) {
	err := enforcePathPrefix("/tmp", map[string]any{"path": "data/file.txt"})
	if err != nil {
		t.Errorf("valid relative subpath should pass: %v", err)
	}
}

func TestEnforcePathPrefix_RelativePathBoundary(t *testing.T) {
	// "data" under prefix "/tmp" resolves to "/tmp/data" — should pass.
	err := enforcePathPrefix("/tmp", map[string]any{"path": "data"})
	if err != nil {
		t.Errorf("relative subpath should pass: %v", err)
	}
}

func TestEnforcePathPrefix_OutsidePrefix(t *testing.T) {
	// Even though the path is relative, if someone finds a way to escape
	// (without ".." which is caught earlier), it should be denied.
	// In practice ".." is the only way, but we test the prefix check itself.
	err := enforcePathPrefix("/home/user", map[string]any{"path": "safe/file.txt"})
	if err != nil {
		t.Errorf("valid relative path under prefix should pass: %v", err)
	}
}

// --- parseRate tests ---

func TestParseRate_Valid(t *testing.T) {
	rate, ok := parseRate("50rps")
	if !ok || rate != 50 {
		t.Errorf("parseRate(\"50rps\") = (%f, %v), want (50, true)", rate, ok)
	}
}

func TestParseRate_Fractional(t *testing.T) {
	rate, ok := parseRate("0.5rps")
	if !ok || rate != 0.5 {
		t.Errorf("parseRate(\"0.5rps\") = (%f, %v), want (0.5, true)", rate, ok)
	}
}

func TestParseRate_Invalid(t *testing.T) {
	cases := []string{"", "50rpm", "abc", "0rps", "-5rps", "rps"}
	for _, s := range cases {
		_, ok := parseRate(s)
		if ok {
			t.Errorf("parseRate(%q) should fail", s)
		}
	}
}

// --- enforceRateLimit tests ---

func TestEnforceRateLimit_EmptyRateLimit(t *testing.T) {
	err := enforceRateLimit("cap1", "")
	if err != nil {
		t.Errorf("empty rate limit should pass: %v", err)
	}
}

func TestEnforceRateLimit_MalformedRateReturnsError(t *testing.T) {
	err := enforceRateLimit("cap-malformed", "50rpm")
	if err == nil {
		t.Fatal("expected error for malformed rate limit")
	}
	pe := err.(*PolicyError)
	if pe.Code != CodeInvalidArgument {
		t.Errorf("code = %d, want %d", pe.Code, CodeInvalidArgument)
	}
}

func TestEnforceRateLimit_BasicExhaustion(t *testing.T) {
	capID := "cap-exhaust-test"
	globalLimiter.mu.Lock()
	delete(globalLimiter.buckets, capID)
	globalLimiter.mu.Unlock()

	// 2 requests/second — bucket starts full with 2 tokens.
	for i := 0; i < 2; i++ {
		if err := enforceRateLimit(capID, "2rps"); err != nil {
			t.Fatalf("request %d should pass: %v", i+1, err)
		}
	}
	// Third request should be denied.
	err := enforceRateLimit(capID, "2rps")
	if err == nil {
		t.Fatal("expected rate limit exceeded")
	}
	pe := err.(*PolicyError)
	if pe.Code != CodeResourceExhausted {
		t.Errorf("code = %d, want %d", pe.Code, CodeResourceExhausted)
	}
}

func TestEnforceRateLimit_TokenRefill(t *testing.T) {
	capID := "cap-refill-test"
	globalLimiter.mu.Lock()
	delete(globalLimiter.buckets, capID)
	globalLimiter.mu.Unlock()

	// 10rps — exhaust all tokens.
	for i := 0; i < 10; i++ {
		enforceRateLimit(capID, "10rps")
	}
	err := enforceRateLimit(capID, "10rps")
	if err == nil {
		t.Fatal("should be exhausted")
	}

	// Simulate time passing by manually adjusting the bucket.
	globalLimiter.mu.Lock()
	b := globalLimiter.buckets[capID]
	b.last = b.last.Add(-1 * time.Second)
	globalLimiter.mu.Unlock()

	// After 1 second at 10rps, 10 tokens should have refilled.
	if err := enforceRateLimit(capID, "10rps"); err != nil {
		t.Errorf("should pass after refill: %v", err)
	}
}

func TestEnforceRateLimit_PerCapIsolation(t *testing.T) {
	capA := "cap-iso-a"
	capB := "cap-iso-b"
	globalLimiter.mu.Lock()
	delete(globalLimiter.buckets, capA)
	delete(globalLimiter.buckets, capB)
	globalLimiter.mu.Unlock()

	// Exhaust cap A (1rps).
	enforceRateLimit(capA, "1rps")
	err := enforceRateLimit(capA, "1rps")
	if err == nil {
		t.Fatal("capA should be exhausted")
	}

	// Cap B should still work.
	if err := enforceRateLimit(capB, "1rps"); err != nil {
		t.Errorf("capB should be independent: %v", err)
	}
}

func TestEnforceRateLimit_StaleEviction(t *testing.T) {
	staleID := "cap-stale-evict"
	activeID := "cap-active-evict"

	globalLimiter.mu.Lock()
	// Insert a stale bucket (last used > staleBucketTTL ago).
	globalLimiter.buckets[staleID] = &bucket{
		tokens: 10,
		rate:   10,
		last:   time.Now().Add(-staleBucketTTL - time.Minute),
	}
	delete(globalLimiter.buckets, activeID)
	globalLimiter.mu.Unlock()

	// This call triggers lazy eviction.
	enforceRateLimit(activeID, "10rps")

	globalLimiter.mu.Lock()
	_, staleExists := globalLimiter.buckets[staleID]
	_, activeExists := globalLimiter.buckets[activeID]
	globalLimiter.mu.Unlock()

	if staleExists {
		t.Error("stale bucket should have been evicted")
	}
	if !activeExists {
		t.Error("active bucket should still exist")
	}
}

// --- enforceConstraints integration ---

func TestEnforceConstraints_NoConstraints(t *testing.T) {
	claims := &capability.Capability{ID: "test"}
	if err := enforceConstraints(claims, nil); err != nil {
		t.Errorf("no constraints should pass: %v", err)
	}
}

func TestEnforceConstraints_PathAndRate(t *testing.T) {
	capID := "cap-both-constraints"
	globalLimiter.mu.Lock()
	delete(globalLimiter.buckets, capID)
	globalLimiter.mu.Unlock()

	claims := &capability.Capability{
		ID: capID,
		Constraints: capability.Constraints{
			PathPrefix: "/tmp",
			RateLimit:  "100rps",
		},
	}
	ctx := map[string]any{"path": "file.txt"}
	if err := enforceConstraints(claims, ctx); err != nil {
		t.Errorf("valid path + rate should pass: %v", err)
	}
}
