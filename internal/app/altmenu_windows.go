//go:build windows

package app

import "github.com/wailsapp/wails/v3/pkg/application"

const (
	wmSysCommand = 0x0112
	scKeyMenu    = 0xF100
)

// Windows takes Alt pressed and released with no other key in between as
// "go to the menu": the window's system menu then holds the keyboard and
// the wheel until a click. A mouse click doesn't count as another key, so
// Alt+click on a foreign key value (open the row it refers to) ended in
// that state. The app has no menu bar, so Alt (and F10) alone do nothing
// now; Alt+Space still opens the system menu.
func guardAltMenu(o *application.Options) {
	next := o.Windows.WndProcInterceptor
	o.Windows.WndProcInterceptor = func(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
		if msg == wmSysCommand && wParam&0xFFF0 == scKeyMenu && lParam == 0 {
			return 0, true
		}
		if next != nil {
			return next(hwnd, msg, wParam, lParam)
		}
		return 0, false
	}
}
