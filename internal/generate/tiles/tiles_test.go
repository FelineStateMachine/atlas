package tiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalName(t *testing.T) {
	tests := []struct {
		name, volume, pyramid, want string
	}{
		{"the volume's own prefix comes off", "tunic", "tunic__world", "world"},
		{"a variant keeps what makes it one", "gta5", "gta5__los-santos__road", "los-santos__road"},
		{"an unprefixed pyramid is left alone", "tunic", "somebody-else", "somebody-else"},
		{"one underscore is not the separator", "tunic", "tunic_world", "tunic_world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LocalName(tt.volume, tt.pyramid); got != tt.want {
				t.Errorf("LocalName(%q, %q) = %q, want %q", tt.volume, tt.pyramid, got, tt.want)
			}
		})
	}
}

// TestStampPart pins the name a pyramid's derivation stamp weighs into a
// bundle's stamp under. It is one part per pyramid however many tiles it holds,
// which is what lets a rebuild notice an unchanged raster without reading one.
func TestStampPart(t *testing.T) {
	if got := StampPart("world"); got != "tiles/world" {
		t.Errorf("StampPart(\"world\") = %q", got)
	}
	if strings.ContainsAny(StampPart("world"), " \n") {
		t.Error("a part name may hold neither a space nor a newline; they separate the summed form")
	}
}

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, IndexName)
	write(t, index, `{"tileSize":256,"size":8192,"lenses":[
		{"sourcePath":"tunic/world/default-v2","assetPath":"tunic__world","stamp":"aaa",
		 "minZoom":0,"maxZoom":7,"fullZoom":5,"sourceZoom":15,
		 "grid":{"sourceZoom":13,"firstTile":4064},
		 "formats":["jpg"],"interpolate":true,"background":"#000000"},
		{"sourcePath":"other/world/v1","assetPath":"tunic__world__aligned-other","stamp":"bbb",
		 "alignedWith":"tunic/world/default-v2","name":"Other",
		 "grid":{"sourceZoom":13,"firstTile":4064},"formats":["jpg"]}
	]}`)
	write(t, filepath.Join(dir, "tunic__world", "0", "0", "0.jpg"), "a")
	write(t, filepath.Join(dir, "tunic__world", "10", "1", "2.jpg"), "b")
	write(t, filepath.Join(dir, "tunic__world", "2", "1", "2.jpg"), "c")

	set, err := Open(index)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if set.TileSize != 256 || set.Size != 8192 {
		t.Errorf("grid is %d/%d", set.TileSize, set.Size)
	}

	native, ok := set.Native("tunic/world/default-v2")
	if !ok {
		t.Fatal("the native pyramid was not found")
	}
	if native.Name != "tunic__world" || native.Stamp != "aaa" || native.Window.FirstTile != 4064 {
		t.Errorf("native pyramid is %+v", native)
	}

	aligned := set.Aligned("tunic/world/default-v2")
	if len(aligned) != 1 || aligned[0].LensName != "Other" {
		t.Errorf("aligned pyramids are %+v", aligned)
	}
	if _, ok := set.Native("nothing/here"); ok {
		t.Error("an underived tile set answered")
	}

	t.Run("tiles come out in the order a bundle writes them", func(t *testing.T) {
		list, err := set.Tiles(native)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"0/0/0.jpg", "10/1/2.jpg", "2/1/2.jpg"}
		if len(list) != len(want) {
			t.Fatalf("%d tiles, want %d", len(list), len(want))
		}
		for i, name := range want {
			if list[i].Name != name {
				t.Errorf("tile %d is %q, want %q (a walk is lexical, so level 10 precedes level 2)",
					i, list[i].Name, name)
			}
		}
	})

	t.Run("names", func(t *testing.T) {
		names := set.Names()
		if len(names) != 2 || names[0] != "tunic__world" {
			t.Errorf("names are %v", names)
		}
	})
}

func TestOpenRefusals(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"no grid", `{"lenses":[]}`, "declares no tile grid"},
		{"not JSON", `{`, "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), IndexName)
			write(t, path, tt.body)
			if _, err := Open(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Open = %v, want a refusal naming %q", err, tt.want)
			}
		})
	}
	t.Run("no register at all", func(t *testing.T) {
		if _, err := Open(filepath.Join(t.TempDir(), IndexName)); err == nil {
			t.Fatal("a missing register was accepted")
		}
	})
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
