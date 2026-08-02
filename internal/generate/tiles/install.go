package tiles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Installing a run's worth of pyramids.
//
// The sequence is what makes a run that dies partway safe. Each pyramid is
// derived under a temporary name and arrives by one rename, so a reader of the
// tile set never sees a half-written one; pyramids the archive no longer offers
// are taken out; and the register is written last, so until the very end it
// still names the stamps of what is actually on disk. A run interrupted anywhere
// leaves a register whose stamps disagree with a plan, and the next run derives
// exactly those pyramids again.

// Register is a tile set's index as it is written.
type Register struct {
	TileSize int       `json:"tileSize"`
	Size     int       `json:"size"`
	Pyramids []Pyramid `json:"lenses"`
}

// Carry decides whether a pyramid the last run left can stand: its stamp
// matches, it is still under the same name, and the directory is still there.
// Keeping one is doing nothing at all.
func (s *Set) Carry(plan Plan, stamp string) (Pyramid, bool) {
	if s == nil {
		return Pyramid{}, false
	}
	// By name rather than by tile set: an aligned variant is registered under
	// the picture it hangs on, so a lookup through the tile set it was
	// resampled from would never find one and every warp would be derived
	// again on every run.
	existing, held := s.byName[plan.Name]
	if !held || existing.TileSet != plan.TileSet {
		return Pyramid{}, false
	}
	if existing.Stamp == "" || existing.Stamp != stamp {
		return Pyramid{}, false
	}
	if _, err := os.Stat(filepath.Join(s.dir, existing.Name)); err != nil {
		// Whatever the last run left is not there to be kept.
		return Pyramid{}, false
	}
	return existing, true
}

// Install moves the pyramids that were derived into the tile set, takes out any
// the archive no longer offers, and writes the register last.
func Install(temp, dir string, register Register, derived map[string]bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create tile set: %w", err)
	}
	names := make([]string, 0, len(derived))
	for name := range derived {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target := filepath.Join(dir, name)
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("replace %s: %w", name, err)
		}
		if err := os.Rename(filepath.Join(temp, name), target); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}

	wanted := make(map[string]bool, len(register.Pyramids))
	for _, pyramid := range register.Pyramids {
		wanted[pyramid.Name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || wanted[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("remove %s: %w", entry.Name(), err)
		}
	}

	sort.Slice(register.Pyramids, func(i, j int) bool {
		return register.Pyramids[i].TileSet < register.Pyramids[j].TileSet
	})
	data, err := json.Marshal(register)
	if err != nil {
		return err
	}
	staged := filepath.Join(temp, IndexName)
	if err := os.WriteFile(staged, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", staged, err)
	}
	return os.Rename(staged, filepath.Join(dir, IndexName))
}
