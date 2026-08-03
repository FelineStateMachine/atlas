package curation

import (
	"strings"
	"testing"
)

// TestEmbeddedCorpus holds the shipped curation to what it is curating. Every
// row here is an editorial decision carried over from the tree these tables
// were extracted from, and a row that disappears is a volume quietly composed
// differently, so each is named rather than counted.
func TestEmbeddedCorpus(t *testing.T) {
	tables, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	t.Run("shared window", func(t *testing.T) {
		if tables.Window.SourceZoom != 13 || tables.Window.FirstTile != 4064 {
			t.Errorf("shared window is %+v, want zoom 13 from tile 4064", tables.Window)
		}
	})

	t.Run("world order", func(t *testing.T) {
		preferred := tables.PreferredWorlds("fallout-new-vegas")
		if len(preferred) != 1 || preferred[0] != "mojave-wasteland" {
			t.Errorf("fallout-new-vegas prefers %v", preferred)
		}
		if tables.PreferredWorlds("tunic") != nil {
			t.Errorf("tunic is curated for order and should not be")
		}
		if !tables.NewestFirst("bend-or") {
			t.Errorf("bend-or is a version history and should read newest first")
		}
		if tables.NewestFirst("tunic") {
			t.Errorf("tunic is not a version history")
		}
	})

	t.Run("icon outset", func(t *testing.T) {
		tests := []struct {
			volume, world, want string
		}{
			{"clair-obscur-expedition-33", "anything", "dark"},
			{"cyberpunk-2077", "night-city", "dark"},
			{"fallout-new-vegas", "mojave-wasteland", "dark"},
			{"fallout76", "appalachia", "dark"},
			{"la-noire", "los-angeles", "dark"},
			{"mars", "global", "dark"},
			{"sonic-frontiers", "anything", "dark"},
			{"skyrim", "skyrim", "dark"},
			{"skyrim", "solstheim", "dark"},
			{"skyrim", "somewhere-else", ""},
			{"tunic", "world", ""},
		}
		for _, tt := range tests {
			if got := tables.IconOutset(tt.volume, tt.world); got != tt.want {
				t.Errorf("IconOutset(%q, %q) = %q, want %q", tt.volume, tt.world, got, tt.want)
			}
		}
	})

	t.Run("shard", func(t *testing.T) {
		tests := []struct {
			volume, world string
			want          ShardMode
		}{
			{"fallout-new-vegas", "mojave-wasteland", ShardIntoWorlds},
			{"zelda-tears-of-the-kingdom", "hyrule", ShardIntoLenses},
			{"fallout-new-vegas", "sierra-madre", ShardNone},
			{"tunic", "world", ShardNone},
		}
		for _, tt := range tests {
			if got := tables.Shard(tt.volume, tt.world); got != tt.want {
				t.Errorf("Shard(%q, %q) = %q, want %q", tt.volume, tt.world, got, tt.want)
			}
		}
	})

	t.Run("collection equivalents", func(t *testing.T) {
		want := map[string]string{
			"ripper-doc":     "ripperdoc",
			"tarot-card":     "tarot-graffiti",
			"gun-shop":       "weapon-shop",
			"clothes-shop":   "clothing-vendor",
			"medicine-shop":  "medpoint",
			"melee-shop":     "melee-vendor",
			"netrunner-shop": "netrunner",
		}
		for key, shared := range want {
			if got := tables.CollectionEquivalent("cyberpunk-2077", key); got != shared {
				t.Errorf("cyberpunk-2077 %q meets under %q, want %q", key, got, shared)
			}
		}
		if got := tables.CollectionEquivalent("tunic", "fox_shrine"); got != "" {
			t.Errorf("tunic curates an equivalence it should not: %q", got)
		}
		if got := tables.CollectionEquivalent("cyberpunk-2077", "what"); got != "" {
			t.Errorf("an uncurated key answered %q", got)
		}
	})
}

func TestParseRefusals(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"a schema from the future", `{"schema":99}`, "schema 99"},
		{"no shared window", `{"schema":1}`, "no shared tile window"},
		{
			"an unknown shard mode",
			`{"schema":1,"window":{"sourceZoom":13,"firstTile":4064},
			  "shard":{"worlds":{"a/b":"halves"}}}`,
			"shard mode \"halves\"",
		},
		{
			"a shard key naming no world",
			`{"schema":1,"window":{"sourceZoom":13,"firstTile":4064},
			  "shard":{"worlds":{"a":"worlds"}}}`,
			"names no world",
		},
		{
			"an outset key naming no world",
			`{"schema":1,"window":{"sourceZoom":13,"firstTile":4064},
			  "iconOutset":{"byWorld":{"a":"dark"}}}`,
			"names no world",
		},
		{"not JSON at all", `{`, "decode curation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFrom([]byte(tt.data))
			if err == nil {
				t.Fatalf("accepted, want a refusal naming %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("refused with %q, want it to name %q", err, tt.want)
			}
		})
	}
}

// TestProseKeysAreNotVolumes holds the file's one habit: every section carries
// its reasoning beside its data, and a prose key must never be read as an
// entry.
func TestProseKeysAreNotVolumes(t *testing.T) {
	tables, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := tables.CollectionEquivalent("what", "anything"); got != "" {
		t.Errorf("the equivalents table read its own prose as a volume: %q", got)
	}
}
