package authoring

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFeatureAdaptersRefuseSilentlyIncompleteResponses(t *testing.T) {
	project, err := LoadProject(filepath.Join("..", "..", "examples", "sample-region"+ProjectExtension))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		adapter string
		body    string
		mention string
	}{
		{
			adapter: "arcgis-feature-service",
			body:    `{"type":"FeatureCollection","features":[],"exceededTransferLimit":true}`,
			mention: "transfer limit",
		},
		{
			adapter: "ogc-api-features",
			body:    `{"type":"FeatureCollection","features":[],"numberReturned":0,"links":[{"rel":"next","href":"https://example.invalid/page/2"}]}`,
			mention: "next page",
		},
		{
			adapter: "ogc-api-features",
			body:    `{"type":"FeatureCollection","features":[],"numberReturned":1}`,
			mention: "count differs",
		},
		{
			adapter: "ogc-api-features",
			body:    `{"type":"FeatureCollection","features":[],"numberMatched":1,"numberReturned":0}`,
			mention: "incomplete",
		},
	}
	for _, test := range tests {
		t.Run(test.adapter+" "+test.mention, func(t *testing.T) {
			source := project.Sources[0]
			source.Adapter = test.adapter
			_, err := observe(project, 0, source, Capture{}, []byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.mention) {
				t.Fatalf("observe = %v, want %q", err, test.mention)
			}
		})
	}
}
