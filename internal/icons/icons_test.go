package icons

import (
	"strings"
	"testing"
)

func TestStandardResolvesVendoredNames(t *testing.T) {
	data, asset, err := Standard("maki/mountain")
	if err != nil {
		t.Fatal(err)
	}
	if asset != "std--maki-mountain.svg" {
		t.Fatalf("asset name: %q", asset)
	}
	if !strings.Contains(string(data), "<svg") {
		t.Fatal("resolved bytes are not an svg")
	}
}

func TestStandardRefusesStrangers(t *testing.T) {
	for _, ref := range []string{"maki/skyscraper", "lucide/mountain", "mountain", ""} {
		if _, _, err := Standard(ref); err == nil {
			t.Errorf("%q resolved", ref)
		}
	}
}
