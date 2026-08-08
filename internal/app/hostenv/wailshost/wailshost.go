// Package wailshost is the window-shaped half of the desktop host: the native
// file picker, and the window handle a picker has to be asked for through.
//
// It is the one package in the application that knows what a webview is. The
// handler asks [hostenv.Hostenv.PickFile] for a file; the OS host turns that
// into an [oshost.Picker] call; this turns that call into a WKOpenPanel, a GTK
// chooser or a Windows common dialog, depending on what the binary was built
// for. Everything else the desktop host needs -- the library of .atlas files,
// the session records -- is [oshost]'s and is the same code the headless host
// runs, so the two hosts differ by exactly this file (issue #5 §3.3).
//
// It lives under hostenv because that is where the contract says host
// implementations live: depcheck's hostenv analyzer permits the toolkit import
// below here and nowhere else in internal/app.
package wailshost

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/logging"
	"log/slog"
)

// Window is the live window, or the absence of one.
//
// A Wails context exists only between startup and shutdown, and a native
// dialog can only be asked for through it. [Window.Opened] takes it at
// startup -- it is the shell's OnStartup, spelled to be passed directly --
// and [Window.Pick] reads it. The zero value is a host whose window has not
// opened yet, which is a state a request can genuinely arrive in: the page is
// served over the same asset server that the window is still starting.
type Window struct {
	live     atomic.Pointer[context.Context]
	mu       sync.Mutex
	install  func(name string, content io.Reader) error
	refresh  func(context.Context)
	pending  []string
	flushing bool
}

// Accept gives every native file-open surface the application's one install
// command. Paths remain a window concern and never enter the application.
func (w *Window) Accept(install func(name string, content io.Reader) error) {
	w.mu.Lock()
	w.install = install
	w.mu.Unlock()
}

// QueuePaths accepts launch arguments and second-instance open events. Before
// startup they wait; after startup they are installed immediately.
func (w *Window) QueuePaths(paths []string) {
	w.mu.Lock()
	w.pending = append(w.pending, atlasPaths(paths)...)
	live := w.live.Load() != nil
	w.mu.Unlock()
	if live {
		w.flush()
	}
}

// Opened records the window context and enables native drag/drop over the
// entire application surface.
func (w *Window) Opened(ctx context.Context) {
	w.live.Store(&ctx)
	w.mu.Lock()
	if w.refresh == nil {
		w.refresh = wailsruntime.WindowReload
	}
	w.mu.Unlock()
	wailsruntime.OnFileDrop(ctx, func(_, _ int, paths []string) { w.QueuePaths(paths) })
	w.flush()
}

func (w *Window) flush() {
	w.mu.Lock()
	if w.flushing {
		w.mu.Unlock()
		return
	}
	w.flushing = true
	w.mu.Unlock()
	installed := false
	for {
		w.mu.Lock()
		paths := append([]string(nil), w.pending...)
		w.pending = nil
		install := w.install
		refresh := w.refresh
		if len(paths) == 0 || install == nil {
			if install == nil {
				w.pending = append(paths, w.pending...)
			}
			w.flushing = false
			w.mu.Unlock()
			if installed && refresh != nil {
				if live := w.live.Load(); live != nil {
					refresh(*live)
				}
			}
			return
		}
		w.mu.Unlock()
		for _, path := range paths {
			file, err := os.Open(path)
			if err == nil {
				err = install(filepath.Base(path), file)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil {
				slog.Warn("native file open refused", logging.Op("install"), logging.Path(path), slog.Any("error", err))
			} else {
				installed = true
			}
		}
	}
}

func atlasPaths(paths []string) []string {
	accepted := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.Ext(path) == ".atlas" {
			accepted = append(accepted, path)
		}
	}
	return accepted
}

// Pick puts the native file picker in front of the reader and opens what they
// chose.
//
// The request's own context is deliberately unused: a native panel is modal
// on the window, not on the HTTP request that raised it, and a reader who
// navigates away mid-dialog should get their file picker dismissed by
// dismissing it rather than by a cancelled fetch. What the panel needs is the
// window's context, which is the one this holds.
//
// The three answers of the contract are all reachable: a file, ErrNoSelection
// when the reader closed the panel with nothing chosen, and ErrNotAvailable
// before there is a window to be modal on -- refused rather than queued,
// because "there is no window yet" is an answer and a hung import is not.
func (w *Window) Pick(context.Context) (io.ReadCloser, string, error) {
	held := w.live.Load()
	if held == nil {
		return nil, "", fmt.Errorf("choosing a bundle: %w", hostenv.ErrNotAvailable)
	}
	chosen, err := wailsruntime.OpenFileDialog(*held, wailsruntime.OpenDialogOptions{
		Title: "Add a volume",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Atlas volumes (*.atlas)", Pattern: "*.atlas"},
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("choosing a bundle: %w", err)
	}
	if chosen == "" {
		return nil, "", hostenv.ErrNoSelection
	}
	file, err := os.Open(chosen)
	if err != nil {
		return nil, "", fmt.Errorf("opening the chosen bundle: %w", err)
	}
	// The store is handed a stream and the name it was offered under, never a
	// path: the name is what the installed file is called, and where it came
	// from is the picker's business and ends here.
	return file, filepath.Base(chosen), nil
}
