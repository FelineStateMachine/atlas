package hostenv_test

import (
	"errors"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

func TestValidName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"app.json", true},
		{"volume.tunic.json", true},
		{"volume.zelda-tears-of-the-kingdom.json", true},
		{"volume.mojave_wasteland.json", true},
		{"", false},
		{".hidden", false},
		{"-leading", false},
		{"_leading", false},
		{"volume/tunic.json", false},
		{"..", false},
		{"Volume.json", false},
		{"volume tunic.json", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := hostenv.ValidName(tt.name)
			if tt.ok && err != nil {
				t.Errorf("ValidName(%q) = %v, want admitted", tt.name, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("ValidName(%q) was admitted", tt.name)
			}
		})
	}
}

func TestMemorySessions(t *testing.T) {
	store := hostenv.NewMemorySessions()

	if _, err := store.Load("app.json"); !errors.Is(err, hostenv.ErrNoSession) {
		t.Fatalf("loading a record nobody wrote = %v, want ErrNoSession", err)
	}
	if err := store.Save("app.json", []byte(`{"volume":"tunic"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("volume.tunic.json", []byte(`{"world":"world"}`)); err != nil {
		t.Fatal(err)
	}
	held, err := store.Load("app.json")
	if err != nil || string(held) != `{"volume":"tunic"}` {
		t.Fatalf("Load = %q, %v", held, err)
	}

	// A record handed back is a copy: a caller that scribbles on what it read
	// has not edited the store.
	held[0] = '!'
	again, err := store.Load("app.json")
	if err != nil || string(again) != `{"volume":"tunic"}` {
		t.Fatalf("the store was edited through a returned slice: %q, %v", again, err)
	}

	names, err := store.Names()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "app.json" || names[1] != "volume.tunic.json" {
		t.Fatalf("Names = %v, want the two records sorted", names)
	}

	// Deleting is the contract the file-backed store answers too: the record
	// goes, its neighbours stay, and a record that was never held is already
	// what the caller asked for.
	if err := store.Delete("volume.tunic.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("volume.tunic.json"); !errors.Is(err, hostenv.ErrNoSession) {
		t.Errorf("the record survived a delete: %v", err)
	}
	if names, err := store.Names(); err != nil || len(names) != 1 || names[0] != "app.json" {
		t.Errorf("Names = %v, %v, want the pointer left standing", names, err)
	}
	if err := store.Delete("volume.tunic.json"); err != nil {
		t.Errorf("deleting a record twice = %v, want a quiet success", err)
	}

	if err := store.Save("volume/escape.json", nil); err == nil {
		t.Error("a name with a separator was accepted")
	}
	if err := store.Delete("volume/escape.json"); err == nil {
		t.Error("a name with a separator was accepted for deletion")
	}
}
