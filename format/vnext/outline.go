package vnext

import "fmt"

// Outline restores the complete semantic catalog except feature rows,
// relationships, provenance payloads, and asset bytes. It is the page-zero
// entry point for clients that subsequently demand-page visible features and
// tiles from the compiled Atlas.
func (reader *Reader) Outline() (Volume, error) {
	tables := make(map[string]Table, len(tableNames())-3)
	for _, name := range tableNames() {
		if name == featureTableName || name == relationshipTableName || name == provenanceTableName {
			continue
		}
		table, err := reader.Table(name)
		if err != nil {
			return Volume{}, err
		}
		tables[name] = table
	}
	root := tables[volumeTableName]
	if root.Rows != 1 {
		return Volume{}, fmt.Errorf("volume table has %d rows", root.Rows)
	}
	volume := Volume{ID: requiredString(root, 0, "volume.id"), Title: requiredString(root, 0, "volume.title")}
	worlds, worldIndex, err := decodeWorlds(tables)
	if err != nil {
		return Volume{}, err
	}
	volume.Worlds = worlds
	if _, err := decodeFeatureSets(&volume, tables, worldIndex); err != nil {
		return Volume{}, err
	}
	if err := decodeWorldDetails(&volume, tables, worldIndex); err != nil {
		return Volume{}, err
	}
	volume.Assets = decodeAssetOutline(tables[assetTableName])
	return volume, nil
}

func decodeAssetOutline(table Table) []Asset {
	assets := make([]Asset, table.Rows)
	for row := 0; row < table.Rows; row++ {
		assets[row] = Asset{
			ID: requiredString(table, row, "asset.id"), MediaType: requiredString(table, row, "asset.mediaType"),
			Path: requiredString(table, row, "asset.path"), Provenance: optionalStringAt(table, row, "asset.provenance"),
		}
	}
	return assets
}
