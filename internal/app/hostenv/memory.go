package hostenv

import (
	"fmt"
	"sort"
	"sync"
)

// MemorySessions keeps session records for as long as the process lives and
// no longer. It is the store a host uses when it has nowhere to write --
// `atlas serve` without a data directory, a test -- and it is here rather than
// in a test file because "state that does not survive the process" is a real
// host decision, not a mock.
//
// It is safe for concurrent use.
type MemorySessions struct {
	mu      sync.Mutex
	records map[string][]byte
}

// NewMemorySessions returns an empty in-memory session store.
func NewMemorySessions() *MemorySessions {
	return &MemorySessions{records: map[string][]byte{}}
}

// Load reads a record, or reports [ErrNoSession].
func (m *MemorySessions) Load(name string) ([]byte, error) {
	if err := ValidName(name); err != nil {
		return nil, fmt.Errorf("session %q: %w", name, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	held, ok := m.records[name]
	if !ok {
		return nil, ErrNoSession
	}
	out := make([]byte, len(held))
	copy(out, held)
	return out, nil
}

// Save writes a record whole.
func (m *MemorySessions) Save(name string, data []byte) error {
	if err := ValidName(name); err != nil {
		return fmt.Errorf("session %q: %w", name, err)
	}
	held := make([]byte, len(data))
	copy(held, data)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[name] = held
	return nil
}

// Names lists the records held, sorted.
func (m *MemorySessions) Names() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.records))
	for name := range m.records {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
