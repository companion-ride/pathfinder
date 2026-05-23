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
