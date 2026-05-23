package server

import (
	"testing"
)

func TestParseAPIKeys_MultiKeyCommaSeparated(t *testing.T) {
	t.Setenv("DOUBLE_GIS_API_KEYS", "k1,k2,k3")
	t.Setenv("DOUBLE_GIS_API_KEY", "")

	keys, err := parseAPIKeys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(keys), 3; got != want {
		t.Fatalf("len(keys) = %d, want %d", got, want)
	}
	if keys[0] != "k1" || keys[1] != "k2" || keys[2] != "k3" {
		t.Fatalf("keys = %v, want [k1 k2 k3]", keys)
	}
}

func TestParseAPIKeys_TrimsWhitespaceAndSkipsBlanks(t *testing.T) {
	t.Setenv("DOUBLE_GIS_API_KEYS", "  k1 , ,k2,   ")
	t.Setenv("DOUBLE_GIS_API_KEY", "")

	keys, err := parseAPIKeys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" {
		t.Fatalf("keys = %v, want [k1 k2]", keys)
	}
}

func TestParseAPIKeys_FallsBackToSingleKeyVar(t *testing.T) {
	t.Setenv("DOUBLE_GIS_API_KEYS", "")
	t.Setenv("DOUBLE_GIS_API_KEY", "legacy")

	keys, err := parseAPIKeys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 || keys[0] != "legacy" {
		t.Fatalf("keys = %v, want [legacy]", keys)
	}
}

func TestParseAPIKeys_MultiKeyTakesPrecedenceOverSingle(t *testing.T) {
	t.Setenv("DOUBLE_GIS_API_KEYS", "new1,new2")
	t.Setenv("DOUBLE_GIS_API_KEY", "legacy")

	keys, err := parseAPIKeys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "new1" || keys[1] != "new2" {
		t.Fatalf("keys = %v, want [new1 new2]", keys)
	}
}

func TestParseAPIKeys_ReturnsErrorWhenNothingConfigured(t *testing.T) {
	t.Setenv("DOUBLE_GIS_API_KEYS", "")
	t.Setenv("DOUBLE_GIS_API_KEY", "")

	if _, err := parseAPIKeys(); err == nil {
		t.Fatal("expected error when both env vars are empty")
	}
}

func TestParseAPIKeys_ReturnsErrorWhenMultiVarIsOnlyDelimiters(t *testing.T) {
	t.Setenv("DOUBLE_GIS_API_KEYS", ", , ,")
	t.Setenv("DOUBLE_GIS_API_KEY", "")

	if _, err := parseAPIKeys(); err == nil {
		t.Fatal("expected error when DOUBLE_GIS_API_KEYS has no real values")
	}
}
