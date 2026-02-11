package auth

import "sync"

// RevocationList is a thread-safe in-memory set of revoked capability IDs.
type RevocationList struct {
	mu      sync.RWMutex
	revoked map[string]struct{}
}

func NewRevocationList() *RevocationList {
	return &RevocationList{
		revoked: make(map[string]struct{}),
	}
}

func (rl *RevocationList) Revoke(tokenID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.revoked[tokenID] = struct{}{}
}

func (rl *RevocationList) IsRevoked(tokenID string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	_, ok := rl.revoked[tokenID]
	return ok
}
