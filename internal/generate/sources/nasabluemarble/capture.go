package nasabluemarble

// The archived capture, as the crawler wrote it. A Blue Marble capture is one
// pinned publication: NASA Earth Observatory's global, cloud-free, true-color
// base map, taken whole as a single image and cut into the reference level the
// deriver folds down. The capture body records the product's identity and the
// derivation policy the crawler applied, so a reader of the archive can say
// exactly what these tiles are and how they came to be -- and so a policy
// change reads as a new capture rather than as the same one wearing new bytes.
//
// These are the only declarations in the tree permitted to carry this
// publisher's vocabulary. Fields are declared in the order they marshal.

type capture struct {
	Source string `json:"source"`
	Body   string `json:"body"`
	// Product names the publication as NASA titles it, and Credit is the line
	// NASA Earth Observatory asks republishers to carry.
	Product string `json:"product"`
	Credit  string `json:"credit"`
	// AssetSHA256 is the digest of the source image the tiles were derived
	// from -- the capture's whole claim to identity. The asset's URL rides the
	// snapshot index as provenance, never this body.
	AssetSHA256 string `json:"assetSha256"`
	// Width and Height are the source image's pixels.
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Map    mosaic     `json:"map"`
	Derive derivation `json:"derive"`
}

// mosaic is the pyramid as captured: the deepest square-world level the source
// was cut to, and the encoding its tiles wear. LayerTitle names the picture the
// way a person knows it, which is what a lens picker shows.
type mosaic struct {
	MaxZoom    int    `json:"maxZoom"`
	Extension  string `json:"extension"`
	LayerTitle string `json:"layerTitle"`
}

// derivation is the policy the crawler cut the reference level under. It is
// provenance a person reads and identity the capture hash carries; nothing
// downstream depends on it.
type derivation struct {
	Resampler   string `json:"resampler"`
	JPEGQuality int    `json:"jpegQuality"`
}
