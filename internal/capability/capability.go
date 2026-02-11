// Package capability defines the token claims and constraints
// used by the Strata capability system.
package capability

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Capability represents a signed capability token's claims.
type Capability struct {
	ID          string      `json:"jti"`
	Subject     string      `json:"sub"`
	IssuedAt    time.Time   `json:"iat"`
	ExpiresAt   time.Time   `json:"exp"`
	Service     string      `json:"service"`
	Actions     []string    `json:"actions"`
	Rights      []string    `json:"rights,omitempty"`
	Constraints Constraints `json:"constraints"`
}

// Constraints limits what a capability token may access.
type Constraints struct {
	PathPrefix string `json:"path_prefix,omitempty"`
	RateLimit  string `json:"rate_limit,omitempty"`
}

// NewCapability creates a capability with a random ID and the given parameters.
func NewCapability(service string, actions []string, constraints Constraints, ttl time.Duration) *Capability {
	id := make([]byte, 16)
	rand.Read(id)
	now := time.Now()
	return &Capability{
		ID:          hex.EncodeToString(id),
		Subject:     "capability",
		IssuedAt:    now,
		ExpiresAt:   now.Add(ttl),
		Service:     service,
		Actions:     actions,
		Constraints: constraints,
	}
}

func (c *Capability) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

func (c *Capability) HasAction(action string) bool {
	for _, a := range c.Actions {
		if a == action {
			return true
		}
	}
	return false
}
