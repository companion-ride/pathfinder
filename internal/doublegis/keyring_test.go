package doublegis

import (
	"testing"
)

func TestNewKeyring_StoresAllKeys(t *testing.T) {
	k := newKeyring([]string{"a", "b", "c"})
	if k.size() != 3 {
		t.Fatalf("size = %d, want 3", k.size())
	}
}

func TestNewKeyring_PrimaryReturnsFirstKey(t *testing.T) {
	k := newKeyring([]string{"first", "second"})
	if got := k.primary(); got != "first" {
		t.Fatalf("primary = %q, want %q", got, "first")
	}
}

func TestNewKeyring_PanicsOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty key slice")
		}
	}()
	newKeyring(nil)
}
