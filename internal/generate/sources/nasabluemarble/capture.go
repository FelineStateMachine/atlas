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
	// Features is the vector half of the capture: country borders and primary
	// capitals, distilled from Natural Earth's public-domain files at crawl
	// time, coordinates verbatim.
	Features features `json:"features"`
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

// features is the vector publication the capture marries to the raster: which
// edition it was, the digests its files carried, and the distillation itself.
type features struct {
	Edition       string    `json:"edition"`
	BordersSHA256 string    `json:"bordersSha256"`
	PlacesSHA256  string    `json:"placesSha256"`
	Countries     []country `json:"countries"`
	Capitals      []capital `json:"capitals"`
}

// country is one country as the capture keeps it: identity, the continent the
// publication files it under, its label point, and its rings -- polygons, then
// rings, then positions, longitude first, split at the antimeridian as the
// publication splits them.
type country struct {
	Name      string           `json:"name"`
	A3        string           `json:"a3"`
	Continent string           `json:"continent"`
	LabelLon  float64          `json:"labelLon"`
	LabelLat  float64          `json:"labelLat"`
	Polygons  [][][][2]float64 `json:"polygons"`
}

// capital is one primary capital as the capture keeps it.
type capital struct {
	Name    string  `json:"name"`
	Country string  `json:"country"`
	A3      string  `json:"a3"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}
