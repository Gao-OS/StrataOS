package registry

import (
	"sync"
	"testing"
)

func TestRegisterAndResolve(t *testing.T) {
	r := New()
	r.Register("fs", "unix:///run/strata/fs.sock", 1)

	e, ok := r.Resolve("fs")
	if !ok {
		t.Fatal("expected to resolve fs")
	}
	if e.Service != "fs" || e.Endpoint != "unix:///run/strata/fs.sock" || e.APIv != 1 {
		t.Errorf("unexpected entry: %+v", e)
	}
}

func TestResolveNotFound(t *testing.T) {
	r := New()
	_, ok := r.Resolve("nonexistent")
	if ok {
		t.Error("expected resolve to fail for unregistered service")
	}
}

func TestRegisterOverwrite(t *testing.T) {
	r := New()
	r.Register("fs", "unix:///old.sock", 1)
	r.Register("fs", "unix:///new.sock", 2)

	e, ok := r.Resolve("fs")
	if !ok {
		t.Fatal("expected to resolve fs")
	}
	if e.Endpoint != "unix:///new.sock" || e.APIv != 2 {
		t.Errorf("expected updated entry, got: %+v", e)
	}
}

func TestList(t *testing.T) {
	r := New()
	r.Register("identity", "unix:///run/strata/identity.sock", 1)
	r.Register("fs", "unix:///run/strata/fs.sock", 1)

	entries := r.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	found := map[string]bool{}
	for _, e := range entries {
		found[e.Service] = true
	}
	if !found["identity"] || !found["fs"] {
		t.Errorf("missing expected services: %v", found)
	}
}

func TestListEmpty(t *testing.T) {
	r := New()
	entries := r.List()
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}
}

func TestRemove(t *testing.T) {
	r := New()
	r.Register("fs", "unix:///run/strata/fs.sock", 1)

	if err := r.Remove("fs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := r.Resolve("fs")
	if ok {
		t.Error("expected fs to be removed")
	}
}

func TestRemoveNotFound(t *testing.T) {
	r := New()
	if err := r.Remove("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent service")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := New()
	var wg sync.WaitGroup

	// Concurrent writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "svc"
			r.Register(name, "unix:///sock", i)
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Resolve("svc")
			r.List()
		}()
	}

	wg.Wait()

	// Should still be resolvable after concurrent access.
	_, ok := r.Resolve("svc")
	if !ok {
		t.Error("expected svc to be resolvable after concurrent access")
	}
}
