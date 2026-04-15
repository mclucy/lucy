package state

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateVersionAcceptsV1(t *testing.T) {
	if err := ValidateVersion("v1"); err != nil {
		t.Fatalf("expected v1 to validate, got %v", err)
	}
}

func TestValidateVersionRejectsUnsupportedVersion(t *testing.T) {
	err := ValidateVersion("v2")
	if err == nil {
		t.Fatal("expected unsupported version error")
	}

	var stateErr StateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("expected StateError, got %T", err)
	}
	if stateErr.Kind != ErrVersionUnsupported {
		t.Fatalf("expected kind %q, got %q", ErrVersionUnsupported, stateErr.Kind)
	}
	if !strings.Contains(err.Error(), "v2") || !strings.Contains(err.Error(), SupportedVersion) {
		t.Fatalf("expected clear version message, got %q", err.Error())
	}
	if !IsVersionError(err) {
		t.Fatal("expected IsVersionError to report true")
	}
}

func TestValidateVersionRejectsEmptyVersionAsMalformed(t *testing.T) {
	err := ValidateVersion("")
	if err == nil {
		t.Fatal("expected malformed version error")
	}

	var stateErr StateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("expected StateError, got %T", err)
	}
	if stateErr.Kind != ErrMalformed {
		t.Fatalf("expected kind %q, got %q", ErrMalformed, stateErr.Kind)
	}
	if stateErr.Field != "version" {
		t.Fatalf("expected field version, got %q", stateErr.Field)
	}
}
