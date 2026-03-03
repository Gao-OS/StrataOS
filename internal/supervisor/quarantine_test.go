package supervisor

import (
	"testing"
	"time"
)

func TestShouldQuarantine_BelowThreshold(t *testing.T) {
	cfg := DefaultQuarantine()
	now := time.Now()
	crashes := []time.Time{
		now.Add(-30 * time.Second),
		now.Add(-20 * time.Second),
		now.Add(-10 * time.Second),
	}
	if ShouldQuarantine(crashes, cfg) {
		t.Error("expected no quarantine with 3 crashes (threshold 5)")
	}
}

func TestShouldQuarantine_AtThreshold(t *testing.T) {
	cfg := DefaultQuarantine()
	now := time.Now()
	crashes := []time.Time{
		now.Add(-90 * time.Second),
		now.Add(-60 * time.Second),
		now.Add(-30 * time.Second),
		now.Add(-15 * time.Second),
		now.Add(-5 * time.Second),
	}
	if !ShouldQuarantine(crashes, cfg) {
		t.Error("expected quarantine with 5 crashes within 2 minutes")
	}
}

func TestShouldQuarantine_OldCrashesExpire(t *testing.T) {
	cfg := DefaultQuarantine()
	now := time.Now()
	crashes := []time.Time{
		now.Add(-10 * time.Minute), // old, outside window
		now.Add(-9 * time.Minute),  // old
		now.Add(-8 * time.Minute),  // old
		now.Add(-30 * time.Second), // recent
		now.Add(-10 * time.Second), // recent
	}
	if ShouldQuarantine(crashes, cfg) {
		t.Error("expected no quarantine: only 2 crashes within window")
	}
}

func TestShouldQuarantine_EmptyCrashes(t *testing.T) {
	cfg := DefaultQuarantine()
	if ShouldQuarantine(nil, cfg) {
		t.Error("expected no quarantine with no crashes")
	}
}

func TestShouldQuarantine_ZeroMaxCrashes(t *testing.T) {
	cfg := QuarantineConfig{MaxCrashes: 0, Window: time.Minute}
	now := time.Now()
	crashes := []time.Time{now}
	if ShouldQuarantine(crashes, cfg) {
		t.Error("expected no quarantine with MaxCrashes=0")
	}
}
