package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Gao-OS/StrataOS/internal/capability"
)

const v2PublicHeader = "v2.public."

// pae implements Pre-Authentication Encoding per the PASETO specification.
// It binds the header, message, and footer together to prevent tampering.
func pae(pieces ...[]byte) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(len(pieces)))
	for _, p := range pieces {
		lenBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(lenBuf, uint64(len(p)))
		buf = append(buf, lenBuf...)
		buf = append(buf, p...)
	}
	return buf
}

// Sign creates a PASETO v2.public token from a capability and ed25519 private key.
func Sign(cap *capability.Capability, key ed25519.PrivateKey) (string, error) {
	message, err := json.Marshal(cap)
	if err != nil {
		return "", fmt.Errorf("marshal capability: %w", err)
	}

	m2 := pae([]byte(v2PublicHeader), message, []byte{})
	sig := ed25519.Sign(key, m2)

	body := make([]byte, len(message)+ed25519.SignatureSize)
	copy(body, message)
	copy(body[len(message):], sig)

	token := v2PublicHeader + base64.RawURLEncoding.EncodeToString(body)
	return token, nil
}

// Verify validates a PASETO v2.public token and returns the embedded capability.
func Verify(token string, key ed25519.PublicKey) (*capability.Capability, error) {
	if !strings.HasPrefix(token, v2PublicHeader) {
		return nil, fmt.Errorf("invalid token header")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(token[len(v2PublicHeader):])
	if err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	if len(decoded) < ed25519.SignatureSize {
		return nil, fmt.Errorf("token too short")
	}

	message := decoded[:len(decoded)-ed25519.SignatureSize]
	sig := decoded[len(decoded)-ed25519.SignatureSize:]

	m2 := pae([]byte(v2PublicHeader), message, []byte{})
	if !ed25519.Verify(key, m2, sig) {
		return nil, fmt.Errorf("invalid signature")
	}

	var cap capability.Capability
	if err := json.Unmarshal(message, &cap); err != nil {
		return nil, fmt.Errorf("unmarshal capability: %w", err)
	}
	return &cap, nil
}
