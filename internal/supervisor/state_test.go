package supervisor

import "testing"

func TestStateString(t *testing.T) {
	tests := []struct {
		state ServiceState
		want  string
	}{
		{Declared, "Declared"},
		{Starting, "Starting"},
		{Healthy, "Healthy"},
		{Crashed, "Crashed"},
		{Restarting, "Restarting"},
		{Stopped, "Stopped"},
		{Quarantined, "Quarantined"},
		{ServiceState(99), "Unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("ServiceState(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestCanTransition_Valid(t *testing.T) {
	valid := [][2]ServiceState{
		{Declared, Starting},
		{Starting, Healthy},
		{Starting, Crashed},
		{Healthy, Crashed},
		{Healthy, Stopped},
		{Crashed, Restarting},
		{Crashed, Quarantined},
		{Restarting, Starting},
		{Quarantined, Starting},
		{Stopped, Starting},
	}
	for _, pair := range valid {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("expected %s → %s to be valid", pair[0], pair[1])
		}
	}
}

func TestCanTransition_Invalid(t *testing.T) {
	invalid := [][2]ServiceState{
		{Declared, Healthy},
		{Declared, Crashed},
		{Starting, Stopped},
		{Starting, Restarting},
		{Healthy, Starting},
		{Healthy, Declared},
		{Crashed, Healthy},
		{Crashed, Stopped},
		{Restarting, Healthy},
		{Restarting, Crashed},
		{Quarantined, Healthy},
		{Stopped, Healthy},
		{Stopped, Crashed},
	}
	for _, pair := range invalid {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("expected %s → %s to be invalid", pair[0], pair[1])
		}
	}
}
