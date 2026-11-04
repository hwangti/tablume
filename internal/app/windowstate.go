package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tablume/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The main window opens at the size it was closed at (maximised if it was).
// The position isn't kept: the window opens centred, which is always on a
// screen that is there now.

type windowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised,omitempty"`
}

func windowStatePath() string {
	dir, _ := store.Dir()
	return filepath.Join(dir, "window.json")
}

// loadWindowState applies the saved size to the main window's options.
func loadWindowState(o *application.WebviewWindowOptions) {
	b, err := os.ReadFile(windowStatePath())
	if err != nil {
		return
	}
	var s windowState
	if json.Unmarshal(b, &s) != nil {
		return
	}
	if s.Width >= o.MinWidth && s.Height >= o.MinHeight && s.Width <= 10000 && s.Height <= 10000 {
		o.Width, o.Height = s.Width, s.Height
	}
	if s.Maximised {
		o.StartState = application.WindowStateMaximised
	}
}

// keepWindowState saves the main window's size as it changes (a moment
// after, so a drag writes once). The size of a maximised (or full-screen)
// window is the one it had before.
func keepWindowState(w *application.WebviewWindow) {
	var (
		mu    sync.Mutex
		state windowState
		timer *time.Timer
	)
	save := func() {
		mu.Lock()
		s := state
		mu.Unlock()
		if s.Width == 0 {
			return
		}
		if b, err := json.Marshal(s); err == nil {
			_ = os.WriteFile(windowStatePath(), b, 0o600)
		}
	}
	note := func(*application.WindowEvent) {
		if w.IsMinimised() {
			return
		}
		max := w.IsMaximised()
		b := w.Bounds()
		mu.Lock()
		defer mu.Unlock()
		state.Maximised = max
		if !max && !w.IsFullscreen() && b.Width > 0 && b.Height > 0 {
			state.Width, state.Height = b.Width, b.Height
		}
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(500*time.Millisecond, save)
	}
	w.OnWindowEvent(events.Common.WindowShow, note)
	w.OnWindowEvent(events.Common.WindowDidResize, note)
	// (a hook runs before the window goes; it only writes what is known)
	w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) { save() })
}
