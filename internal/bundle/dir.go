package bundle

import (
	"os"
	"path/filepath"
)

// appID mirrors the identity main.go gives the framework. It lives here so
// the build tools, which are separate programs, resolve the same library the
// running application serves from.
const appID = "dev.felinestatemachine.atlas"

// DefaultDir is the central .atlas library: the bundles directory under the
// application's own data directory. The application arrives at the same path
// through the framework; the bundler installs straight into it, so a dev run
// needs no pointing at anything -- and a running application, watching this
// directory, picks a fresh build up the moment it lands.
func DefaultDir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, appID, "bundles"), nil
}
