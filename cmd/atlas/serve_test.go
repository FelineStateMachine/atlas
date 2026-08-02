package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where the library is comes from three places in a fixed order, and getting
// that order wrong means a dev loop silently serves the wrong library.
func TestLibraryDir(t *testing.T) {
	tests := []struct {
		name    string
		flagged string
		env     string
		want    string
	}{
		{name: "the flag wins", flagged: "/tmp/one", env: "/tmp/two", want: "/tmp/one"},
		{name: "then the environment", env: "/tmp/two", want: "/tmp/two"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(bundlesDirEnv, tt.env)
			got, err := libraryDir(tt.flagged)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("libraryDir(%q) with %s=%q = %q, want %q",
					tt.flagged, bundlesDirEnv, tt.env, got, tt.want)
			}
		})
	}

	t.Run("then the application's own data directory", func(t *testing.T) {
		t.Setenv(bundlesDirEnv, "")
		got, err := libraryDir("")
		if err != nil {
			t.Skipf("this machine has no config directory: %v", err)
		}
		base, _ := os.UserConfigDir()
		if want := filepath.Join(base, appIdentifier, "bundles"); got != want {
			t.Errorf("libraryDir(\"\") = %q, want %q", got, want)
		}
		if !strings.HasSuffix(got, "bundles") {
			t.Errorf("the fallback library is %q", got)
		}
	})
}
