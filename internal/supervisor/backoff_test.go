package supervisor

import (
	"testing"
	"time"
)

func TestComputeDelay(t *testing.T) {
	cfg := BackoffConfig{
		BaseDelay: 1 * time.Second,
		MaxDelay:  30 * time.Second,
	}
	tests := []struct {
		crashes int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 30 * time.Second}, // capped
		{7, 30 * time.Second}, // stays capped
		{100, 30 * time.Second},
	}
	for _, tt := range tests {
		got := ComputeDelay(tt.crashes, cfg)
		if got != tt.want {
			t.Errorf("ComputeDelay(%d) = %v, want %v", tt.crashes, got, tt.want)
		}
	}
}

func TestComputeDelay_CustomConfig(t *testing.T) {
	cfg := BackoffConfig{
		BaseDelay: 500 * time.Millisecond,
		MaxDelay:  5 * time.Second,
	}
	if got := ComputeDelay(1, cfg); got != 500*time.Millisecond {
		t.Errorf("crash 1: got %v, want 500ms", got)
	}
	if got := ComputeDelay(4, cfg); got != 4*time.Second {
		t.Errorf("crash 4: got %v, want 4s", got)
	}
	if got := ComputeDelay(5, cfg); got != 5*time.Second {
		t.Errorf("crash 5: got %v, want 5s (capped)", got)
	}
}
