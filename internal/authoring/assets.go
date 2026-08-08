package authoring

import (
	"fmt"
	"os"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func attachAssets(project Project, plan Plan, captures map[string]Capture, cache *Cache, volume *vnext.Volume) error {
	for _, asset := range project.Assets {
		planned, ok := assetPlan(plan, asset.ID)
		if !ok {
			return fmt.Errorf("asset %s has no planned request", asset.ID)
		}
		capture := captures[planned.ID]
		data, err := os.ReadFile(cache.BlobPath(capture.Body))
		if err != nil {
			return fmt.Errorf("read captured asset %s: %w", asset.ID, err)
		}
		volume.Assets = append(volume.Assets, vnext.Asset{
			ID: asset.ID, MediaType: asset.MediaType, Data: data, Provenance: asset.Provenance,
		})
	}
	return nil
}

func assetPlan(plan Plan, asset string) (PlannedRequest, bool) {
	for _, request := range plan.Requests {
		if request.Kind == RequestAsset && request.Asset == asset {
			return request, true
		}
	}
	return PlannedRequest{}, false
}
