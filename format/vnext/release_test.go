package vnext

import "testing"

func TestReleaseValidation(t *testing.T) {
	t.Parallel()

	valid := Release{
		Title:     "Minimal",
		CreatedAt: "2026-08-01T20:13:08Z",
		Revision:  1,
		Stamp:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Worlds:    1,
	}
	for name, mutate := range map[string]func(*Release){
		"title":       func(release *Release) { release.Title = "" },
		"capture":     func(release *Release) { release.CreatedAt = "yesterday" },
		"revision":    func(release *Release) { release.Revision = -1 },
		"stamp":       func(release *Release) { release.Stamp = "short" },
		"world count": func(release *Release) { release.Worlds = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid release was accepted")
			}
		})
	}
}

func TestFoldIsDeterministicAndRevisionAware(t *testing.T) {
	t.Parallel()

	older := Descriptor{Locator: "older.atlas", Slug: "minimal", CreatedAt: "2026-08-01T20:13:08Z", Revision: 1, Stamp: "a"}
	rebuilt := Descriptor{Locator: "rebuilt.atlas", Slug: "minimal", CreatedAt: older.CreatedAt, Revision: 2, Stamp: "b"}
	newer := Descriptor{Locator: "newer.atlas", Slug: "minimal", CreatedAt: "2026-08-02T20:13:08Z", Revision: 0, Stamp: "c"}

	if winner := Fold([]Descriptor{newer, rebuilt, older})["minimal"]; winner != newer {
		t.Fatalf("winner = %#v, want newest capture", winner)
	}
	if winner := Fold([]Descriptor{rebuilt, older})["minimal"]; winner != rebuilt {
		t.Fatalf("winner = %#v, want revised build", winner)
	}
}
