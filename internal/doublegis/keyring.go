package doublegis

import (
	"sync"
	"time"
)

type keyState struct {
	value         string
	cooldownUntil time.Time
}

type keyring struct {
	mu      sync.Mutex
	keys    []keyState
	current int
}

func newKeyring(values []string) *keyring {
	if len(values) == 0 {
		panic("doublegis: newKeyring requires at least one key")
	}
	states := make([]keyState, len(values))
	for i, v := range values {
		states[i] = keyState{value: v}
	}
	return &keyring{keys: states}
}

func (k *keyring) size() int {
	return len(k.keys)
}

func (k *keyring) primary() string {
	return k.keys[0].value
}

// acquire returns the value of the current key plus its index, advancing past
// any keys whose cooldown has not yet expired. ok=false means every key is
// currently cooling down.
func (k *keyring) acquire() (string, int, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()

	now := time.Now()
	for i := 0; i < len(k.keys); i++ {
		idx := (k.current + i) % len(k.keys)
		if k.keys[idx].cooldownUntil.After(now) {
			continue
		}
		k.current = idx
		return k.keys[idx].value, idx, true
	}
	return "", 0, false
}

// markRateLimited sets a cooldown on the key at the given index. If that key
// was the current sticky choice, the pointer is advanced. Safe to call
// concurrently and idempotently for the same key.
func (k *keyring) markRateLimited(index int, retryAfter time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if index < 0 || index >= len(k.keys) {
		return
	}
	k.keys[index].cooldownUntil = time.Now().Add(retryAfter)
	if k.current == index {
		k.current = (k.current + 1) % len(k.keys)
	}
}
