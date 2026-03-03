// Package registry implements an in-memory service registry for Strata.
// Services register their endpoints and other services resolve them by name.
package registry

import (
	"fmt"
	"sync"
)

// Entry represents a registered service endpoint.
type Entry struct {
	Service  string `json:"service"`
	Endpoint string `json:"endpoint"`
	APIv     int    `json:"api_v"`
}

// Registry is a thread-safe in-memory service registry.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// New creates an empty registry.
func New() *Registry {
	return &Registry{
		entries: make(map[string]*Entry),
	}
}

// Register adds or updates a service entry.
func (r *Registry) Register(service, endpoint string, apiv int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[service] = &Entry{
		Service:  service,
		Endpoint: endpoint,
		APIv:     apiv,
	}
}

// Resolve looks up a service by name.
// Returns the entry and true if found, nil and false otherwise.
func (r *Registry) Resolve(service string) (*Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[service]
	if !ok {
		return nil, false
	}
	// Return a copy to avoid data races.
	copy := *e
	return &copy, true
}

// List returns all registered entries.
func (r *Registry) List() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, *e)
	}
	return result
}

// Remove unregisters a service. Returns an error if not found.
func (r *Registry) Remove(service string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[service]; !ok {
		return fmt.Errorf("service %q not registered", service)
	}
	delete(r.entries, service)
	return nil
}
