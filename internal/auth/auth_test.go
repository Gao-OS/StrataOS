package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gao-OS/StrataOS/internal/capability"
)

// --- Key generation tests ---

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.Public) != ed25519.PublicKeySize {
		t.Errorf("public key size = %d, want %d", len(kp.Public), ed25519.PublicKeySize)
	}
	if len(kp.Private) != ed25519.PrivateKeySize {
		t.Errorf("private key size = %d, want %d", len(kp.Private), ed25519.PrivateKeySize)
	}
}

func TestWriteAndLoadPublicKey(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "identity.pub")

	if err := kp.WritePublicKey(path); err != nil {
		t.Fatalf("WritePublicKey: %v", err)
	}

	loaded, err := LoadPublicKey(path)
	if err != nil {
		t.Fatalf("LoadPublicKey: %v", err)
	}

	if !loaded.Equal(kp.Public) {
		t.Error("loaded key does not match original")
	}
}

func TestLoadPublicKey_NotFound(t *testing.T) {
	_, err := LoadPublicKey("/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadPublicKey_BadBase64(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pub")
	os.WriteFile(path, []byte("not-valid-base64!!!"), 0644)

	_, err := LoadPublicKey(path)
	if err == nil {
		t.Error("expected error for bad base64")
	}
}

func TestLoadPublicKey_WrongSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wrong.pub")
	os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString([]byte("tooshort"))), 0644)

	_, err := LoadPublicKey(path)
	if err == nil {
		t.Error("expected error for wrong key size")
	}
}

// --- PASETO sign/verify tests ---

func TestSignVerify_RoundTrip(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cap := &capability.Capability{
		ID:        "abc123",
		Subject:   "test",
		Service:   "fs",
		Actions:   []string{"open", "read"},
		Rights:    []string{"fs.open", "fs.read"},
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		Constraints: capability.Constraints{
			PathPrefix: "/tmp",
			RateLimit:  "100rps",
		},
	}

	token, err := Sign(cap, kp.Private)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	got, err := Verify(token, kp.Public)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if got.ID != cap.ID {
		t.Errorf("ID = %q, want %q", got.ID, cap.ID)
	}
	if got.Service != cap.Service {
		t.Errorf("Service = %q, want %q", got.Service, cap.Service)
	}
	if len(got.Rights) != len(cap.Rights) {
		t.Errorf("Rights len = %d, want %d", len(got.Rights), len(cap.Rights))
	}
	if got.Constraints.PathPrefix != cap.Constraints.PathPrefix {
		t.Errorf("PathPrefix = %q, want %q", got.Constraints.PathPrefix, cap.Constraints.PathPrefix)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()

	cap := &capability.Capability{
		ID:      "test",
		Service: "fs",
		Actions: []string{"open"},
	}

	token, err := Sign(cap, kp1.Private)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	_, err = Verify(token, kp2.Public)
	if err == nil {
		t.Error("expected error verifying with wrong key")
	}
}

func TestVerify_TamperedToken(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cap := &capability.Capability{
		ID:      "test",
		Service: "fs",
		Actions: []string{"open"},
	}

	token, _ := Sign(cap, kp.Private)

	// Tamper with a byte in the middle of the token body.
	runes := []byte(token)
	// Find a position in the encoded body (after "v2.public.").
	idx := len(v2PublicHeader) + 10
	if idx < len(runes) {
		runes[idx] ^= 0xFF
	}
	tampered := string(runes)

	_, err := Verify(tampered, kp.Public)
	if err == nil {
		t.Error("expected error for tampered token")
	}
}

func TestVerify_InvalidHeader(t *testing.T) {
	_, err := Verify("v1.public.abcdef", ed25519.PublicKey(make([]byte, 32)))
	if err == nil {
		t.Error("expected error for invalid header")
	}
}

func TestVerify_TooShort(t *testing.T) {
	// Valid header but body decodes to fewer than 64 bytes (sig size).
	short := v2PublicHeader + base64.RawURLEncoding.EncodeToString([]byte("short"))
	_, err := Verify(short, ed25519.PublicKey(make([]byte, 32)))
	if err == nil {
		t.Error("expected error for token too short")
	}
}

func TestVerify_BadBase64(t *testing.T) {
	_, err := Verify(v2PublicHeader+"!!!invalid!!!", ed25519.PublicKey(make([]byte, 32)))
	if err == nil {
		t.Error("expected error for bad base64")
	}
}

// --- PAE tests ---

func TestPAE_Empty(t *testing.T) {
	result := pae()
	// Should encode count of 0 pieces as 8 zero bytes.
	if len(result) != 8 {
		t.Errorf("pae() len = %d, want 8", len(result))
	}
}

func TestPAE_Deterministic(t *testing.T) {
	a := pae([]byte("hello"), []byte("world"))
	b := pae([]byte("hello"), []byte("world"))
	if string(a) != string(b) {
		t.Error("pae should be deterministic")
	}
}

func TestPAE_DifferentInputs(t *testing.T) {
	a := pae([]byte("hello"), []byte("world"))
	b := pae([]byte("helloworld"))
	if string(a) == string(b) {
		t.Error("different piece splits should produce different PAE")
	}
}

// --- Revocation list tests ---

func TestRevocationList_Basic(t *testing.T) {
	rl := NewRevocationList()

	if rl.IsRevoked("abc") {
		t.Error("should not be revoked initially")
	}

	rl.Revoke("abc")
	if !rl.IsRevoked("abc") {
		t.Error("should be revoked after Revoke()")
	}

	// Other IDs unaffected.
	if rl.IsRevoked("def") {
		t.Error("unrevoked ID should not be revoked")
	}
}

func TestRevocationList_DoubleRevoke(t *testing.T) {
	rl := NewRevocationList()
	rl.Revoke("abc")
	rl.Revoke("abc") // idempotent
	if !rl.IsRevoked("abc") {
		t.Error("should still be revoked")
	}
}

func TestRevocationList_Concurrent(t *testing.T) {
	rl := NewRevocationList()
	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		for i := 0; i < 1000; i++ {
			rl.Revoke("cap-concurrent")
		}
		close(done)
	}()

	// Reader goroutine (runs concurrently).
	for i := 0; i < 1000; i++ {
		rl.IsRevoked("cap-concurrent")
	}
	<-done
}

// --- Sign with nil capability ---

func TestSign_NilCapability(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, err := Sign(nil, priv)
	// json.Marshal(nil) returns "null" which is valid, but let's ensure no panic.
	if err != nil {
		t.Logf("Sign(nil) returned error (acceptable): %v", err)
	}
}
