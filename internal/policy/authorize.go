// Package policy centralizes authorization decisions for all Strata services.
// Services call Authorize() instead of performing ad-hoc permission checks.
package policy

import (
	"fmt"
	"strings"

	"github.com/Gao-OS/StrataOS/internal/capability"
)

// Error codes matching api/protocol.md v0.3.1.
const (
	CodeUnauthenticated   = 2
	CodePermissionDenied  = 3
	CodeResourceExhausted = 7
)

// PolicyError is returned by Authorize when access is denied.
type PolicyError struct {
	Code    int
	Name    string
	Message string
}

func (e *PolicyError) Error() string {
	return e.Message
}

// Authorize checks whether claims permit the requested method.
// ctx may contain method-specific context (e.g. "path" for filesystem operations).
// Returns nil if authorized, or *PolicyError describing the denial.
func Authorize(claims *capability.Capability, method string, ctx map[string]any) error {
	if claims == nil {
		return &PolicyError{
			Code:    CodeUnauthenticated,
			Name:    "UNAUTHENTICATED",
			Message: "token required",
		}
	}

	// Parse method into service.action.
	parts := strings.SplitN(method, ".", 2)
	if len(parts) != 2 {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("invalid method format: %q", method),
		}
	}
	service, action := parts[0], parts[1]

	// Token must be scoped to the correct service.
	if claims.Service != service {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("token not valid for service %q", service),
		}
	}

	// Check fully-qualified rights (preferred) or legacy actions (fallback).
	if !hasRight(claims.Rights, method) && !hasAction(claims.Actions, action) {
		return &PolicyError{
			Code:    CodePermissionDenied,
			Name:    "PERMISSION_DENIED",
			Message: fmt.Sprintf("method %q not permitted", method),
		}
	}

	// Enforce constraints.
	return enforceConstraints(claims, ctx)
}

func hasRight(rights []string, required string) bool {
	for _, r := range rights {
		if r == required {
			return true
		}
	}
	return false
}

func hasAction(actions []string, required string) bool {
	for _, a := range actions {
		if a == required {
			return true
		}
	}
	return false
}
