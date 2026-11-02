package main

import "github.com/wailsapp/wails/v3/pkg/application"

// macMenu is the macOS menu bar, with the Korean titles macOS itself uses.
// Without an Edit menu, Cmd+C / Cmd+V don't reach the webview on macOS.
// Zoom is left to the page (Cmd +/-/0), so there is no View menu.
func macMenu() *application.Menu {
	role := func(r application.Role, label string) *application.MenuItem {
		return application.NewRole(r).SetLabel(label)
	}
	sep := application.NewMenuItemSeparator

	app := application.NewMenuFromItems(
		role(application.About, "Tablume에 관하여"),
		sep(),
		role(application.ServicesMenu, "서비스"),
		sep(),
		role(application.Hide, "Tablume 가리기"),
		role(application.HideOthers, "기타 가리기"),
		role(application.UnHide, "모두 보기"),
		sep(),
		role(application.Quit, "Tablume 종료"),
	)
	speech := application.NewMenuFromItems(
		role(application.StartSpeaking, "말하기 시작"),
		role(application.StopSpeaking, "말하기 중단"),
	)
	// No Delete item: its plain Backspace shortcut would be a menu key
	// equivalent and could take the key away from the editor and the grid.
	edit := application.NewMenuFromItems(
		role(application.Undo, "실행 취소"),
		role(application.Redo, "실행 복귀"),
		sep(),
		role(application.Cut, "오려두기"),
		role(application.Copy, "복사하기"),
		role(application.Paste, "붙여넣기"),
		role(application.PasteAndMatchStyle, "붙여넣고 스타일 일치시키기"),
		role(application.SelectAll, "전체 선택"),
		sep(),
		application.NewSubmenu("말하기", speech),
	)
	window := application.NewMenuFromItems(
		role(application.Minimise, "최소화"),
		role(application.Zoom, "확대/축소"),
		sep(),
		role(application.Front, "모두 앞으로 가져오기"),
	)

	return application.NewMenuFromItems(
		application.NewSubmenu("Tablume", app),
		application.NewSubmenu("편집", edit),
		application.NewSubmenu("윈도우", window),
	)
}
