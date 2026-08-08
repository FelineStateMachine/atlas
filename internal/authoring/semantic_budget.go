package authoring

import (
	"fmt"
	"math"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

type semanticCounts struct {
	features  int64
	positions int64
}

func enforceSemanticBudgets(volume vnext.Volume, budgets BuildBudgets) error {
	counts, err := countSemantics(volume)
	if err != nil {
		return err
	}
	if counts.features > budgets.Features {
		return fmt.Errorf("semantic output emits %d features, budget is %d", counts.features, budgets.Features)
	}
	if counts.positions > budgets.GeometryPositions {
		return fmt.Errorf("semantic output emits %d geometry positions, budget is %d", counts.positions, budgets.GeometryPositions)
	}
	return nil
}

func countSemantics(volume vnext.Volume) (semanticCounts, error) {
	var counts semanticCounts
	for _, world := range volume.Worlds {
		for _, set := range world.FeatureSets {
			var err error
			counts.features, err = checkedSemanticAdd(counts.features, len(set.Features))
			if err != nil {
				return semanticCounts{}, fmt.Errorf("count emitted features: %w", err)
			}
			for _, feature := range set.Features {
				for _, part := range feature.Geometry.Parts {
					for _, ring := range part.Rings {
						counts.positions, err = checkedSemanticAdd(counts.positions, len(ring))
						if err != nil {
							return semanticCounts{}, fmt.Errorf("count emitted geometry positions: %w", err)
						}
					}
				}
			}
		}
	}
	return counts, nil
}

func checkedSemanticAdd(total int64, count int) (int64, error) {
	if count < 0 || uint64(count) > math.MaxInt64 {
		return 0, fmt.Errorf("count overflows int64")
	}
	increment := int64(count)
	if total > math.MaxInt64-increment {
		return 0, fmt.Errorf("total overflows int64")
	}
	return total + increment, nil
}
