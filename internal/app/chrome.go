package app

import (
	"bytes"
	"net/http"
	"time"

	"github.com/FelineStateMachine/atlas/internal/app/assets"
)

// The application's own chrome, served from the binary.
//
// /assets is a different mount from /static, and the difference is the
// deletability principle standing up in the URL space: /static is whatever
// tree a host handed over -- the seam's built bundle, which lands in M6 and
// which the application must work without -- and /assets is the part of the
// page that is the application's own. A build with no seam serves 404 under
// /static and a complete, styled, interactive page under /assets.

// assetBodies is the whole mount. It is two entries because it is two things:
// the cascade and the runtime. Anything else a page needs belongs in one of
// them or in a template.
var assetBodies = map[string]struct {
	kind string
	body func() []byte
}{
	"app.css": {"text/css; charset=utf-8", assets.Stylesheet},
	"htmx.js": {"text/javascript; charset=utf-8", assets.Runtime},
}

// assetEpoch is the modification time every asset reports. The bytes are
// compiled in, so they change exactly when the binary does and never while it
// is running; a fixed instant is the honest answer and gives conditional
// requests something stable to work with.
var assetEpoch = time.Unix(0, 0).UTC()

// handleAsset serves one of the application's own assets.
func (a *App) handleAsset(w http.ResponseWriter, r *http.Request) {
	held, known := assetBodies[r.PathValue("path")]
	if !known {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", held.kind)
	// The chrome is versioned by the binary rather than by a URL, so it is
	// revalidated rather than held forever: a development build swapped
	// under a running page must not serve yesterday's stylesheet.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, r.PathValue("path"), assetEpoch, bytes.NewReader(held.body()))
}
