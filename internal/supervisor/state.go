// Package supervisor implements the service lifecycle state machine,
// crash recovery with backoff, and quarantine logic.
package supervisor

import "fmt"

// ServiceState represents a service's lifecycle state.
type ServiceState int

const (
	Declared    ServiceState = iota // registered but never started
	Starting                       // process spawned, waiting for readiness
	Healthy                        // socket is reachable
	Crashed                        // process exited unexpectedly
	Restarting                     // backoff elapsed, about to re-start
	Stopped                        // intentionally stopped
	Quarantined                    // too many crashes, requires manual intervention
)

var stateNames = map[ServiceState]string{
	Declared:    "Declared",
	Starting:    "Starting",
	Healthy:     "Healthy",
	Crashed:     "Crashed",
	Restarting:  "Restarting",
	Stopped:     "Stopped",
	Quarantined: "Quarantined",
}

func (s ServiceState) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", int(s))
}

// transitions defines the legal state transitions.
var transitions = map[ServiceState][]ServiceState{
	Declared:    {Starting},
	Starting:    {Healthy, Crashed},
	Healthy:     {Crashed, Stopped},
	Crashed:     {Restarting, Quarantined},
	Restarting:  {Starting},
	Quarantined: {Starting},
	Stopped:     {Starting},
}

// CanTransition returns true if moving from → to is a legal state change.
func CanTransition(from, to ServiceState) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}
