package policy

import (
	"testing"

	"github.com/Gao-OS/StrataOS/internal/capability"
)

func TestAuthorize_NilClaims(t *testing.T) {
	err := Authorize(nil, "fs.open", nil)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("expected *PolicyError, got %T", err)
	}
	if pe.Code != CodeUnauthenticated {
		t.Errorf("code = %d, want %d", pe.Code, CodeUnauthenticated)
	}
	if pe.Name != "UNAUTHENTICATED" {
		t.Errorf("name = %q, want %q", pe.Name, "UNAUTHENTICATED")
	}
}

func TestAuthorize_InvalidMethodFormat(t *testing.T) {
	claims := &capability.Capability{Service: "fs"}
	err := Authorize(claims, "noperiod", nil)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("expected *PolicyError, got %T", err)
	}
	if pe.Code != CodePermissionDenied {
		t.Errorf("code = %d, want %d", pe.Code, CodePermissionDenied)
	}
}

func TestAuthorize_WrongService(t *testing.T) {
	claims := &capability.Capability{
		Service: "identity",
		Rights:  []string{"fs.open"},
	}
	err := Authorize(claims, "fs.open", nil)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("expected *PolicyError, got %T", err)
	}
	if pe.Code != CodePermissionDenied {
		t.Errorf("code = %d, want %d", pe.Code, CodePermissionDenied)
	}
}

func TestAuthorize_MissingRight(t *testing.T) {
	claims := &capability.Capability{
		Service: "fs",
		Rights:  []string{"fs.list"},
	}
	err := Authorize(claims, "fs.open", nil)
	if err == nil {
		t.Fatal("expected error for missing right")
	}
	pe := err.(*PolicyError)
	if pe.Code != CodePermissionDenied {
		t.Errorf("code = %d, want %d", pe.Code, CodePermissionDenied)
	}
}

func TestAuthorize_MissingAction(t *testing.T) {
	claims := &capability.Capability{
		Service: "fs",
		Actions: []string{"list"},
	}
	err := Authorize(claims, "fs.open", nil)
	if err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestAuthorize_GrantedByRight(t *testing.T) {
	claims := &capability.Capability{
		Service: "fs",
		Rights:  []string{"fs.open"},
	}
	if err := Authorize(claims, "fs.open", nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAuthorize_GrantedByLegacyAction(t *testing.T) {
	claims := &capability.Capability{
		Service: "fs",
		Actions: []string{"open"},
	}
	if err := Authorize(claims, "fs.open", nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAuthorize_RightsPreferredOverActions(t *testing.T) {
	// Has the right but not the action — should still pass.
	claims := &capability.Capability{
		Service: "fs",
		Rights:  []string{"fs.open"},
		Actions: []string{"list"},
	}
	if err := Authorize(claims, "fs.open", nil); err != nil {
		t.Errorf("right should be sufficient: %v", err)
	}
}

func TestAuthorize_BothRightsAndActions(t *testing.T) {
	claims := &capability.Capability{
		Service: "fs",
		Rights:  []string{"fs.open"},
		Actions: []string{"open"},
	}
	if err := Authorize(claims, "fs.open", nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAuthorize_EmptyRightsAndActions(t *testing.T) {
	claims := &capability.Capability{
		Service: "fs",
	}
	err := Authorize(claims, "fs.open", nil)
	if err == nil {
		t.Fatal("expected denial with no rights or actions")
	}
}

func TestPolicyError_Error(t *testing.T) {
	pe := &PolicyError{Code: 3, Name: "PERMISSION_DENIED", Message: "denied"}
	if pe.Error() != "denied" {
		t.Errorf("Error() = %q, want %q", pe.Error(), "denied")
	}
}
