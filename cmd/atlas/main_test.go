package main

import (
	"strings"
	"testing"
)

// TestCommandTable holds the one property the table exists for: what this
// binary can do is visible in one place, each subcommand named once and
// described.
func TestCommandTable(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands() {
		if c.name == "" || c.run == nil {
			t.Errorf("a subcommand with no name or no body: %+v", c)
			continue
		}
		if seen[c.name] {
			t.Errorf("two subcommands are called %q", c.name)
		}
		seen[c.name] = true
		if c.summary == "" {
			t.Errorf("%s has no summary; the table is the help text", c.name)
		}
		if strings.HasPrefix(c.name, "-") {
			t.Errorf("%q reads as a flag", c.name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("the binary does nothing")
	}
	for _, name := range []string{"compose", "serve", "translate"} {
		if !seen[name] {
			t.Errorf("the table does not carry %s", name)
		}
	}
}

func TestRunRefusals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"nothing at all", nil, "no subcommand"},
		{"a subcommand nobody wrote", []string{"levitate"}, "unknown subcommand"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run(%v) = %v, want a refusal naming %q", tt.args, err, tt.want)
			}
		})
	}
}

// TestSubcommandsRefuseMissingInputs: every lane subcommand names what it needs
// rather than guessing, because guessing an archive root is how a build reads
// somebody else's data.
func TestSubcommandsRefuseMissingInputs(t *testing.T) {
	tests := []struct {
		name string
		run  func([]string) error
		want string
	}{
		{"compose", runCompose, "-archive and -tiles"},
		{"translate", runTranslate, "-archive is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("%s with no flags = %v, want a refusal naming %q", tt.name, err, tt.want)
			}
		})
	}
}
