package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestOpArgv(t *testing.T) {
	src, _ := sourceBySlug("ign-wiki")
	argv, err := fetchArgv(src, "cyberpunk-2077/night-city", "/depot/gamemap/fmg-archive")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"go", "run", "./tools/crawl",
		"-archive", "/depot/gamemap/fmg-archive",
		"-ign", "cyberpunk-2077/night-city",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("fetch argv %v, want %v", argv, want)
	}
	if _, err := fetchArgv(src, "not a target", "/depot/gamemap/fmg-archive"); err == nil {
		t.Error("a bad target built an argv")
	}

	argv = tilesArgv("/depot/gamemap/fmg-archive")
	want = []string{"go", "run", "./tools/tiles", "-source", "/depot/gamemap/fmg-archive", "-output", "build/tiles"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("tiles argv %v, want %v", argv, want)
	}

	argv, err = generateArgv("/depot/gamemap/fmg-archive", "/library/bundles")
	if err != nil {
		t.Fatal(err)
	}
	want = []string{
		"go", "run", "./tools/generate",
		"-source", "/depot/gamemap",
		"-tiles", "build/tiles/index.json",
		"-bundles", "/library/bundles",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("generate argv %v, want %v", argv, want)
	}
	// tools/generate finds captures under a directory literally named
	// fmg-archive; pointing it anywhere else must refuse, not guess.
	if _, err := generateArgv("/depot/gamemap/archive", "/library/bundles"); err == nil {
		t.Error("generate accepted an archive that is not an fmg-archive")
	}
}

func TestOpsHandlerRefusals(t *testing.T) {
	server, dir := testServer(t)
	ts := newTestSite(t, server)
	post := func(form string, header ...string) (int, string) {
		t.Helper()
		request, err := http.NewRequest("POST", ts.URL+"/ops/run", strings.NewReader(form))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for at := 0; at+1 < len(header); at += 2 {
			request.Header.Set(header[at], header[at+1])
		}
		response, err := ts.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(body)
	}

	// Without a repository and an archive nothing may run, whatever is asked.
	if status, _ := post("op=tiles"); status != 400 {
		t.Errorf("op without a repo answered %d", status)
	}
	server.shop.repo = t.TempDir()
	if status, _ := post("op=tiles"); status != 400 {
		t.Errorf("op without an archive answered %d", status)
	}

	server.shop.archive = filepath.Join(dir, "fmg-archive")
	if status, _ := post("op=vanish"); status != 400 {
		t.Errorf("unknown op answered %d", status)
	}
	if status, _ := post("op=fetch&source=nowhere&target=x"); status != 400 {
		t.Errorf("unknown source answered %d", status)
	}
	if status, _ := post("op=fetch&source=mapgenie&target=bad/target"); status != 400 {
		t.Errorf("bad target answered %d", status)
	}
	// A cross-site POST carries a foreign Origin and must be turned away.
	if status, _ := post("op=tiles", "Origin", "http://evil.example"); status != 403 {
		t.Errorf("foreign origin answered %d", status)
	}
}

func TestOpsHandlerStreams(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go binary on PATH")
	}
	server, dir := testServer(t)
	ts := newTestSite(t, server)
	// The repo points at an empty directory, so the tool run fails at once --
	// but its refusal must arrive through the page as streamed text, which
	// is the whole wiring under test.
	server.shop.repo = t.TempDir()
	server.shop.archive = filepath.Join(dir, "fmg-archive")

	response, err := ts.Client().Post(ts.URL+"/ops/run", "application/x-www-form-urlencoded",
		strings.NewReader("op=generate"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if response.StatusCode != 200 {
		t.Fatalf("streamed op answered %d: %s", response.StatusCode, text)
	}
	if !strings.Contains(text, "go run ./tools/generate") {
		t.Errorf("stream does not announce its command: %q", text)
	}
	if !strings.Contains(text, "cartograph: ") {
		t.Errorf("stream does not carry the failure: %q", text)
	}
}

func TestStreamCommand(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go binary on PATH")
	}
	var out bytes.Buffer
	if err := streamCommand(context.Background(), &out, t.TempDir(), []string{"go", "env", "GOOS"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != runtime.GOOS {
		t.Errorf("streamed %q, want %q", got, runtime.GOOS)
	}

	out.Reset()
	err := streamCommand(context.Background(), &out, t.TempDir(), []string{"go", "definitely-not-a-subcommand"})
	if err == nil {
		t.Fatal("a failing command reported no error")
	}
	if out.Len() == 0 {
		t.Error("the failing command's own words were not streamed")
	}
}
