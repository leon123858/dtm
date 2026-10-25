package mem

import (
	"context"
	"dtm/adapters/db/db"
	"errors"
	"time"
)

type authEntry struct {
	binding string
	data    []byte
	expires time.Time
}
type inMemoryAuthDBWrapper struct{ *memoryState }

func NewInMemoryAuthDBWrapper() db.AuthDBWrapper {
	return &inMemoryAuthDBWrapper{newMemoryState()}
}
func (m *inMemoryAuthDBWrapper) PutLock(_ context.Context, kind, key, binding string, data []byte, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := kind + ":" + key
	if _, ok := m.entries[k]; ok {
		return errors.New("duplicate auth entry")
	}
	m.entries[k] = authEntry{binding, append([]byte(nil), data...), expires}
	return nil
}
func (m *inMemoryAuthDBWrapper) TakeLock(_ context.Context, kind, key, binding string, now time.Time) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := kind + ":" + key
	e, ok := m.entries[k]
	if !ok || !e.expires.After(now) || e.binding != binding {
		return nil, db.ErrInvalidAuthEntry
	}
	delete(m.entries, k)
	return e.data, nil
}
func (m *inMemoryAuthDBWrapper) CleanupLock(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.entries {
		if !e.expires.After(now) {
			delete(m.entries, k)
		}
	}
	return nil
}
