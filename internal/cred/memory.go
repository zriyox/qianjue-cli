package cred

import (
	"sync"
)

// MemoryStore is the in-memory Store used by tests. It deep-copies records so
// callers cannot mutate stored state through aliases.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
	// FailWith, when set, makes every operation return that error — used to
	// simulate an unavailable system credential store.
	FailWith error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]Record{}}
}

func copyRecord(r Record) Record {
	if r.Scopes != nil {
		r.Scopes = append([]string(nil), r.Scopes...)
	}
	return r
}

func (m *MemoryStore) Get(account string) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return nil, m.FailWith
	}
	r, ok := m.records[account]
	if !ok {
		return nil, ErrNotFound
	}
	out := copyRecord(r)
	return &out, nil
}

func (m *MemoryStore) Set(account string, r *Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return m.FailWith
	}
	if err := r.Validate(); err != nil {
		return err
	}
	m.records[account] = copyRecord(*r)
	return nil
}

func (m *MemoryStore) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return m.FailWith
	}
	delete(m.records, account)
	return nil
}

// Accounts lists stored account names (test helper).
func (m *MemoryStore) Accounts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.records))
	for n := range m.records {
		names = append(names, n)
	}
	return names
}
