package sources

import (
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// TestRegistryIsComplete holds every registered source to what a document, a
// ledger and a source card need from it. A source that cannot say who it is
// produces documents nothing downstream can attribute.
func TestRegistryIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, source := range All() {
		about := source.Describe()
		t.Run(about.Name, func(t *testing.T) {
			if about.Name == "" {
				t.Fatal("a source with no name")
			}
			if seen[about.Name] {
				t.Fatalf("two sources are registered as %q", about.Name)
			}
			seen[about.Name] = true
			if about.Label == "" {
				t.Error("no label for a person to read")
			}
			if about.Attribution == "" && about.License == "" {
				t.Error("neither a licence nor an attribution; a volume owes the people whose work it carries")
			}
			switch about.IDSpace {
			case doc.IDSpaceNative, doc.IDSpaceDerived:
			default:
				t.Errorf("id space %q is neither %q nor %q",
					about.IDSpace, doc.IDSpaceNative, doc.IDSpaceDerived)
			}
			if strings.ToLower(about.Name) != about.Name {
				t.Errorf("name %q is not a slug", about.Name)
			}
		})
	}
	if len(seen) == 0 {
		t.Fatal("no source is registered")
	}
}

func TestFor(t *testing.T) {
	for _, name := range Names() {
		if _, err := For(name); err != nil {
			t.Errorf("For(%q): %v", name, err)
		}
	}
	if _, err := For("nobody"); err == nil {
		t.Error("an unregistered source answered")
	}
}
