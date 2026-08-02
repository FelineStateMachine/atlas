package app

import "testing"

// The two facts a legend row carries about what a collection looks like: the
// colour it wears, and where its artwork lives. Both had to be checked against
// the seam rather than against themselves, because the disagreement they are
// here to prevent is between two surfaces drawing one world.

// The seam's ladder is `collection.color || collection.iconColor ||
// paletteColor(ordinal)` (render/chart/styles.ts, collectionColor). A curated
// volume declares a colour and any ladder agrees with any other; an enriched
// one -- a city -- declares none, and the fallback is the whole question.
func TestCollectionColorIsTheSeamsLadder(t *testing.T) {
	cases := []struct {
		name       string
		collection collectionModel
		want       string
	}{
		{
			name:       "a declared colour wins",
			collection: collectionModel{ID: "1496244488", Color: "#6984F2", Index: 3},
			want:       "#6984F2",
		},
		{
			name:       "the older spelling is still a declaration",
			collection: collectionModel{ID: "1496244488", IconColor: "#E25598", Index: 3},
			want:       "#E25598",
		},
		{
			name:       "an undeclared colour is the palette by payload order",
			collection: collectionModel{ID: "1496244488", Index: 0},
			want:       palette[0],
		},
		{
			name:       "and it keeps counting round the wheel",
			collection: collectionModel{ID: "39191589", Index: len(palette) + 2},
			want:       palette[2],
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := collectionColor(&tt.collection); got != tt.want {
				t.Errorf("collectionColor = %q, want %q", got, tt.want)
			}
		})
	}
}

// The bend-or fixture's own collection: a city's Historic Resources, which
// declares no colour and carries a standard glyph. It is the case the reader
// reported -- a green swatch beside a blue pin -- and it is first in the
// payload, so the seam draws it in the wheel's first colour.
func TestACityCollectionWearsTheColourTheMapDraws(t *testing.T) {
	historic := &collectionModel{ID: "1496244488", Title: "Historic Resources", Index: 0}
	if got, want := collectionColor(historic), "#4fb3d5"; got != want {
		t.Errorf("Historic Resources wears %q; the chart draws it %q", got, want)
	}
	if colorFor(historic.ID) == collectionColor(historic) {
		t.Skip("the feature hash happens to agree here, so this world proves nothing")
	}
}

// The seam names an asset one way (render/data/plane.ts, iconURL) and the
// legend has to name it the same way, or one picture is fetched twice and the
// row and the pins it stands for are drawn from two downloads.
func TestIconAssetURLIsSpelledTheWayTheSeamSpellsIt(t *testing.T) {
	const base = "/data/v/fallout/abc123abc123"
	cases := []struct {
		asset string
		want  string
	}{
		{"vault.svg", base + "/icons/vault.svg"},
		{"std--maki-monument.svg", base + "/icons/std--maki-monument.svg"},
		// The seam's own case, from render/test/markers.test.ts: the
		// separators stay separators, the brackets stay brackets, and the
		// space goes on the wire encoded.
		{"signs/Vault 101 (Ext).png", base + "/icons/signs/Vault%20101%20(Ext).png"},
		{"a&b.svg", base + "/icons/a%26b.svg"},
	}
	for _, tt := range cases {
		if got := iconAssetURL(base, tt.asset); got != tt.want {
			t.Errorf("iconAssetURL(%q) = %q, want %q", tt.asset, got, tt.want)
		}
	}
	if got := iconAssetURL(base, ""); got != "" {
		t.Errorf("a collection with no artwork named %q; it has no URL to give", got)
	}
	if got := iconAssetURL("", "vault.svg"); got != "" {
		t.Errorf("an asset with no build to hang off named %q", got)
	}
}
