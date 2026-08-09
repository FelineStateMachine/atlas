package workbench

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/authoring"
	"github.com/FelineStateMachine/atlas/internal/workbench/oprunner"
)

const maxUSAreaFormBytes = 32 << 10

func (w *Workbench) handleProjectUSArea(rw http.ResponseWriter, request *http.Request) {
	if err := oprunner.CheckOrigin(request); err != nil {
		http.Error(rw, err.Error(), http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(rw, request.Body, maxUSAreaFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	profile, err := usAreaProfileFromForm(request)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	project, _, err := authoring.NewAreaProject(profile)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	manifest, err := authoring.MarshalProjectYAML(project)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateAndInstallManifest(w.targets.Project, manifest); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(rw, request, "/project?notice=Official+U.S.+profile+installed", http.StatusSeeOther)
}

func usAreaProfileFromForm(request *http.Request) (authoring.AreaProfile, error) {
	profile := authoring.DefaultAreaProfile()
	fields := []struct {
		name string
		at   int
	}{
		{name: "west", at: 0}, {name: "south", at: 1}, {name: "east", at: 2}, {name: "north", at: 3},
	}
	for _, field := range fields {
		value, err := strconv.ParseFloat(request.FormValue(field.name), 64)
		if err != nil {
			return authoring.AreaProfile{}, fmt.Errorf("%s coordinate is invalid", field.name)
		}
		profile.Bounds[field.at] = value
	}
	detail, err := strconv.Atoi(request.FormValue("detail"))
	if err != nil {
		return authoring.AreaProfile{}, fmt.Errorf("detail zoom is invalid")
	}
	profile.DetailZoom = detail
	profile.IncludeTopo = request.FormValue("topo") == "true"
	profile.IncludeRoads = request.FormValue("roads") == "true"
	profile.IncludeHydro = request.FormValue("hydro") == "true"
	profile.IncludeCounties = request.FormValue("counties") == "true"
	if err := presentationFromForm(request, &profile.Presentation); err != nil {
		return authoring.AreaProfile{}, err
	}
	return profile, nil
}

func presentationFromForm(request *http.Request, presentation *authoring.AreaPresentation) error {
	presentation.RoadLabel = request.FormValue("road-label")
	presentation.HydroLabel = request.FormValue("hydro-label")
	presentation.CountyLabel = request.FormValue("county-label")
	presentation.RoadColor = request.FormValue("road-color")
	presentation.HydroColor = request.FormValue("hydro-color")
	presentation.HydroFill = request.FormValue("hydro-fill")
	presentation.CountyColor = request.FormValue("county-color")
	presentation.RoadVisible = request.FormValue("road-visible") == "true"
	presentation.HydroVisible = request.FormValue("hydro-visible") == "true"
	presentation.CountyVisible = request.FormValue("county-visible") == "true"
	orders := []struct {
		name string
		to   *int64
	}{
		{name: "road-order", to: &presentation.RoadOrder},
		{name: "hydro-order", to: &presentation.HydroOrder},
		{name: "county-order", to: &presentation.CountyOrder},
	}
	for _, order := range orders {
		value, err := strconv.ParseInt(request.FormValue(order.name), 10, 64)
		if err != nil {
			return fmt.Errorf("%s is invalid", order.name)
		}
		*order.to = value
	}
	return nil
}
