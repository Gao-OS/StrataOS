// Package auth handles ed25519 key management, PASETO v2.public token
// signing/verification, and capability revocation.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	return &KeyPair{Public: pub, Private: priv}, nil
}

// WritePublicKey writes the base64-encoded public key to path.
func (kp *KeyPair) WritePublicKey(path string) error {
	encoded := base64.StdEncoding.EncodeToString(kp.Public)
	return os.WriteFile(path, []byte(encoded), 0644)
}

// LoadPublicKey reads a base64-encoded ed25519 public key from path.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: %d", len(decoded))
	}
	return ed25519.PublicKey(decoded), nil
}
