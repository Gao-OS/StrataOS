package capability

import (
	"testing"
	"time"
)

func TestNewCapability(t *testing.T) {
	cap := NewCapability("fs", []string{"open", "read"}, Constraints{PathPrefix: "/tmp"}, time.Hour)

	if cap.ID == "" {
		t.Error("ID should be non-empty")
	}
	if len(cap.ID) != 32 { // 16 bytes hex-encoded = 32 chars
		t.Errorf("ID length = %d, want 32", len(cap.ID))
	}
	if cap.Service != "fs" {
		t.Errorf("Service = %q, want %q", cap.Service, "fs")
	}
	if len(cap.Actions) != 2 {
		t.Errorf("Actions len = %d, want 2", len(cap.Actions))
	}
	if cap.Subject != "capability" {
		t.Errorf("Subject = %q, want %q", cap.Subject, "capability")
	}
	if cap.Constraints.PathPrefix != "/tmp" {
		t.Errorf("PathPrefix = %q, want %q", cap.Constraints.PathPrefix, "/tmp")
	}
}

func TestNewCapability_UniqueIDs(t *testing.T) {
	a := NewCapability("fs", nil, Constraints{}, time.Hour)
	b := NewCapability("fs", nil, Constraints{}, time.Hour)
	if a.ID == b.ID {
		t.Error("two capabilities should have different IDs")
	}
}

func TestCapability_IsExpired(t *testing.T) {
	expired := &Capability{
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if !expired.IsExpired() {
		t.Error("should be expired")
	}

	valid := &Capability{
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if valid.IsExpired() {
		t.Error("should not be expired")
	}
}

func TestCapability_ActionsField(t *testing.T) {
	cap := &Capability{
		Actions: []string{"open", "read", "list"},
	}
	if len(cap.Actions) != 3 {
		t.Errorf("Actions len = %d, want 3", len(cap.Actions))
	}
	if cap.Actions[0] != "open" {
		t.Errorf("Actions[0] = %q, want %q", cap.Actions[0], "open")
	}
}

func TestCapability_RightsField(t *testing.T) {
	cap := &Capability{
		Rights: []string{"fs.open", "fs.read"},
	}
	if len(cap.Rights) != 2 {
		t.Errorf("Rights len = %d, want 2", len(cap.Rights))
	}
}

func TestConstraints_ZeroValue(t *testing.T) {
	c := Constraints{}
	if c.PathPrefix != "" {
		t.Errorf("PathPrefix should be empty, got %q", c.PathPrefix)
	}
	if c.RateLimit != "" {
		t.Errorf("RateLimit should be empty, got %q", c.RateLimit)
	}
}
