package main

import (
	"context"
	"net/url"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Tabs detached into their own windows. The main window keeps every tab
// (and saves it in the workspace); a tab window shows one of them until it
// is closed or put back. Window and tab talk through events:
//
//	tab:sync     tab window → main   the tab's state changed
//	tab:restore  tab window → main   put the tab back into the main window
//	tab:close    tab window → main   the tab was closed
//	tab-window:close  Go → tab window   the window's close button was clicked
type tabWindows struct {
	mu       sync.Mutex
	wins     map[string]*application.WebviewWindow
	state    map[string]string // the tab as the window should open it
	closing  map[string]bool   // allowed to close (no "put back" round trip)
	quitting bool
}

func tabWindowName(tabID string) string { return "tab:" + tabID }

// OpenTabWindow shows the tab in a window of its own; state is the tab's
// saved form (JSON), read back by the window with TabWindowState.
func (a *App) OpenTabWindow(tabID, title, state string) {
	tw := &a.tabWins
	tw.mu.Lock()
	if w := tw.wins[tabID]; w != nil {
		tw.mu.Unlock()
		w.Show()
		w.Focus()
		return
	}
	tw.state[tabID] = state
	tw.mu.Unlock()

	w := application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             tabWindowName(tabID),
		Title:            title,
		Width:            1100,
		Height:           760,
		MinWidth:         520,
		MinHeight:        360,
		BackgroundColour: application.NewRGB(30, 31, 34),
		URL:              "/?tab=" + url.QueryEscape(tabID),
	})
	// The close button puts the tab back (the page sends its latest state
	// first), unless the tab itself asked to close or the app is quitting.
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		tw.mu.Lock()
		allow := tw.quitting || tw.closing[tabID]
		tw.mu.Unlock()
		if !allow {
			e.Cancel()
			application.Get().Event.Emit("tab-window:close", tabID)
		}
	})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		tw.mu.Lock()
		delete(tw.wins, tabID)
		delete(tw.state, tabID)
		delete(tw.closing, tabID)
		tw.mu.Unlock()
	})
	tw.mu.Lock()
	tw.wins[tabID] = w
	tw.mu.Unlock()
}

// TabWindowState is the tab a tab window shows (JSON, "" if unknown).
func (a *App) TabWindowState(tabID string) string {
	a.tabWins.mu.Lock()
	defer a.tabWins.mu.Unlock()
	return a.tabWins.state[tabID]
}

// CloseTabWindow closes the tab's window without asking it to put the tab back.
func (a *App) CloseTabWindow(tabID string) {
	tw := &a.tabWins
	tw.mu.Lock()
	w := tw.wins[tabID]
	if w != nil {
		tw.closing[tabID] = true
	}
	tw.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

// FocusTabWindow brings the tab's window to the front; false if it has none.
func (a *App) FocusTabWindow(tabID string) bool {
	a.tabWins.mu.Lock()
	w := a.tabWins.wins[tabID]
	a.tabWins.mu.Unlock()
	if w == nil {
		return false
	}
	if w.IsMinimised() {
		w.UnMinimise()
	}
	w.Show()
	w.Focus()
	return true
}

// quitWithMain makes closing the main window quit the app, tab windows and all.
func (a *App) quitWithMain(main *application.WebviewWindow) {
	main.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.tabWins.mu.Lock()
		a.tabWins.quitting = true
		a.tabWins.mu.Unlock()
		go application.Get().Quit()
	})
}

// windowOf is the window a binding call came from (the main window if unknown).
func (a *App) windowOf(ctx context.Context) *application.WebviewWindow {
	if w, ok := ctx.Value(application.WindowKey).(*application.WebviewWindow); ok && w != nil {
		return w
	}
	return a.window
}
