package app

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/logging"
	"github.com/FelineStateMachine/atlas/internal/minting"
)

const maxMintFormBytes = 32 << 10

func (a *App) handleMintPreview(w http.ResponseWriter, r *http.Request) {
	if a.minter == nil {
		http.NotFound(w, r)
		return
	}
	request, err := parseMintRequest(w, r)
	if err != nil {
		a.writeMintPreview(w, newMintView(request, minting.Preview{}, err), http.StatusUnprocessableEntity)
		return
	}
	preview, err := a.minter.Preview(request)
	status := http.StatusOK
	if err != nil {
		status = http.StatusUnprocessableEntity
	}
	a.writeMintPreview(w, newMintView(request, preview, err), status)
}

func (a *App) handleMint(w http.ResponseWriter, r *http.Request) {
	if a.minter == nil {
		http.NotFound(w, r)
		return
	}
	request, err := parseMintRequest(w, r)
	if err != nil {
		a.writeMintPreview(w, newMintView(request, minting.Preview{}, err), http.StatusUnprocessableEntity)
		return
	}
	result, err := a.minter.Mint(r.Context(), request, func(event minting.Event) {
		slog.Info(event.Message, logging.Op("mint"), slog.String("stage", event.Stage))
	})
	if err != nil {
		a.writeMintPreview(w, newMintView(request, minting.Preview{}, err), http.StatusUnprocessableEntity)
		return
	}
	changed, err := a.env.Volumes().Rescan()
	if err != nil {
		http.Error(w, "the minted Atlas was built but the library could not be refreshed", http.StatusInternalServerError)
		return
	}
	a.announce(changed)
	destination := "/v/" + result.Slug + "/" + result.World
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", destination)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func parseMintRequest(w http.ResponseWriter, r *http.Request) (minting.Request, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMintFormBytes)
	if err := r.ParseForm(); err != nil {
		return minting.Request{}, fmt.Errorf("read mint choices: %w", err)
	}
	request := minting.Request{Title: strings.TrimSpace(r.FormValue("title"))}
	if request.Title == "" || len([]rune(request.Title)) > 80 {
		return request, fmt.Errorf("name the Atlas with 1 to 80 characters")
	}
	for index, field := range []string{"west", "south", "east", "north"} {
		value, err := strconv.ParseFloat(r.FormValue(field), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return request, fmt.Errorf("%s coordinate is invalid", field)
		}
		request.Bounds[index] = value
	}
	detail, err := strconv.Atoi(r.FormValue("detail"))
	if err != nil {
		return request, fmt.Errorf("detail zoom is invalid")
	}
	request.DetailZoom = detail
	request.Topo = r.FormValue("topo") == "true"
	request.Roads = r.FormValue("roads") == "true"
	request.Hydro = r.FormValue("hydro") == "true"
	request.Counties = r.FormValue("counties") == "true"
	request.RoadLabel = strings.TrimSpace(r.FormValue("road-label"))
	request.HydroLabel = strings.TrimSpace(r.FormValue("hydro-label"))
	request.CountyLabel = strings.TrimSpace(r.FormValue("county-label"))
	request.RoadColor = strings.TrimSpace(r.FormValue("road-color"))
	request.HydroColor = strings.TrimSpace(r.FormValue("hydro-color"))
	request.HydroFill = strings.TrimSpace(r.FormValue("hydro-fill"))
	request.CountyColor = strings.TrimSpace(r.FormValue("county-color"))
	return request, nil
}

func newMintView(request minting.Request, preview minting.Preview, err error) MintView {
	view := MintView{
		Available: true, Request: request, Preview: preview,
		Pixels: strconv.FormatInt(preview.Pixels, 10) + " px",
		Tiles:  strconv.FormatInt(preview.RasterTiles, 10) + " raster tiles",
		Groups: strconv.Itoa(preview.Requests) + " source groups",
		Bytes:  strconv.FormatInt((preview.EstimatedBytes+(1<<20)-1)/(1<<20), 10) + " MiB",
	}
	if err != nil {
		view.Error = err.Error()
	}
	return view
}

func (a *App) writeMintPreview(w http.ResponseWriter, view MintView, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	a.writePage(w, "mint-preview", View{Mint: view})
}
