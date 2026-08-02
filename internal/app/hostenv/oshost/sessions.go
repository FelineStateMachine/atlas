package oshost

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// Sessions keeps session records as files in one directory, one file per
// record, named exactly as the application named them.
//
// A record is written whole through a staged file and a rename, so a reader
// arriving mid-write sees the old record or the new one and never half of
// either. Nothing here knows what a record means: the schema belongs to the
// application and is documented in docs/app.md.
type Sessions struct{ dir string }

// NewSessions answers for dir, creating it if it is not there yet. A host
// with nowhere to write should use hostenv.NewMemorySessions instead of
// pointing this at a directory it cannot keep.
func NewSessions(dir string) (*Sessions, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("open session directory: %w", err)
	}
	return &Sessions{dir: dir}, nil
}

// Dir is where the records are kept.
func (s *Sessions) Dir() string { return s.dir }

// Load reads a record, or reports hostenv.ErrNoSession.
func (s *Sessions) Load(name string) ([]byte, error) {
	path, err := s.path(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, hostenv.ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("read session %s: %w", name, err)
	}
	return data, nil
}

// Save writes a record whole.
func (s *Sessions) Save(name string, data []byte) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(s.dir, ".writing-*")
	if err != nil {
		return fmt.Errorf("write session %s: %w", name, err)
	}
	defer os.Remove(staged.Name())
	if _, err := staged.Write(data); err != nil {
		staged.Close()
		return fmt.Errorf("write session %s: %w", name, err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("write session %s: %w", name, err)
	}
	if err := os.Chmod(staged.Name(), 0o644); err != nil {
		return fmt.Errorf("write session %s: %w", name, err)
	}
	if err := os.Rename(staged.Name(), path); err != nil {
		return fmt.Errorf("write session %s: %w", name, err)
	}
	return nil
}

// Names lists the records held, sorted. The staged files a concurrent save
// leaves behind are not records and are not listed.
func (s *Sessions) Names() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || hostenv.ValidName(entry.Name()) != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// path is where a record lives. The name is held to hostenv.ValidName before
// it is joined to anything, which is what makes a record name safe to build
// out of a volume slug that arrived in a URL.
func (s *Sessions) path(name string) (string, error) {
	if err := hostenv.ValidName(name); err != nil {
		return "", fmt.Errorf("session %q: %w", name, err)
	}
	return filepath.Join(s.dir, name), nil
}
