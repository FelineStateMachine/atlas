package authoring

import "testing"

func TestFeatureValueCoalescesTheFirstPresentScalar(t *testing.T) {
	t.Parallel()
	feature := geoJSONFeature{Properties: map[string]any{
		"NAME": nil,
		"OID":  "road-42",
	}}
	value, held := featureValue(feature, "coalesce(properties.NAME, properties.OID)")
	if !held || scalarString(value) != "road-42" {
		t.Fatalf("coalesced value = %v, %v", value, held)
	}
	if _, held := featureValue(feature, "coalesce(properties.NAME)"); held {
		t.Fatal("malformed coalesce expression was accepted")
	}
}
