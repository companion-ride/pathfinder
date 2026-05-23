package doublegis

import (
	"testing"
	"time"
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

func TestKeyring_AcquireReturnsCurrentKey(t *testing.T) {
	k := newKeyring([]string{"a", "b"})
	key, idx, ok := k.acquire()
	if !ok {
		t.Fatal("acquire returned ok=false on fresh ring")
	}
	if key != "a" || idx != 0 {
		t.Fatalf("acquire = (%q, %d), want (\"a\", 0)", key, idx)
	}
}

func TestKeyring_AcquireIsStickyWhileHealthy(t *testing.T) {
	k := newKeyring([]string{"a", "b", "c"})
	for i := 0; i < 5; i++ {
		key, _, _ := k.acquire()
		if key != "a" {
			t.Fatalf("acquire iter %d = %q, want stable \"a\"", i, key)
		}
	}
}

func TestKeyring_MarkRateLimitedAdvancesCurrent(t *testing.T) {
	k := newKeyring([]string{"a", "b", "c"})
	_, idx, _ := k.acquire()
	k.markRateLimited(idx, time.Minute)

	key, newIdx, ok := k.acquire()
	if !ok {
		t.Fatal("acquire after mark returned ok=false")
	}
	if key != "b" || newIdx != 1 {
		t.Fatalf("acquire after mark = (%q, %d), want (\"b\", 1)", key, newIdx)
	}
}

func TestKeyring_AcquireSkipsCooledDownKeys(t *testing.T) {
	k := newKeyring([]string{"a", "b", "c"})
	k.markRateLimited(0, time.Minute)
	k.markRateLimited(1, time.Minute)

	key, idx, ok := k.acquire()
	if !ok {
		t.Fatal("acquire returned ok=false but \"c\" should be available")
	}
	if key != "c" || idx != 2 {
		t.Fatalf("acquire = (%q, %d), want (\"c\", 2)", key, idx)
	}
}

func TestKeyring_AcquireReturnsOkFalseWhenAllExhausted(t *testing.T) {
	k := newKeyring([]string{"a", "b"})
	k.markRateLimited(0, time.Minute)
	k.markRateLimited(1, time.Minute)

	_, _, ok := k.acquire()
	if ok {
		t.Fatal("acquire returned ok=true but all keys are cooling down")
	}
}

func TestKeyring_CooldownExpiryRestoresKey(t *testing.T) {
	k := newKeyring([]string{"a"})
	k.markRateLimited(0, 10*time.Millisecond)

	if _, _, ok := k.acquire(); ok {
		t.Fatal("acquire returned ok=true while cooldown still active")
	}
	time.Sleep(20 * time.Millisecond)

	key, _, ok := k.acquire()
	if !ok || key != "a" {
		t.Fatalf("acquire after cooldown = (%q, ok=%v), want (\"a\", true)", key, ok)
	}
}

func TestKeyring_MarkRateLimitedIsIdempotent(t *testing.T) {
	k := newKeyring([]string{"a", "b", "c"})
	k.markRateLimited(0, time.Minute)
	k.markRateLimited(0, time.Minute) // second mark of already-cooled key
	k.markRateLimited(0, time.Minute) // third — must not skip past "b"

	key, _, _ := k.acquire()
	if key != "b" {
		t.Fatalf("acquire = %q, want \"b\" (double-mark must not advance past next healthy key)", key)
	}
}
