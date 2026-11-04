package main

import (
	"tablume/internal/i18n"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// macMenu is the macOS menu bar, with the titles macOS itself uses (in the
// app's language; made again when it changes).
// Without an Edit menu, Cmd+C / Cmd+V don't reach the webview on macOS.
// Zoom is left to the page (Cmd +/-/0), so there is no View menu.
func macMenu() *application.Menu {
	role := func(r application.Role, label string) *application.MenuItem {
		return application.NewRole(r).SetLabel(label)
	}
	sep := application.NewMenuItemSeparator

	app := application.NewMenuFromItems(
		role(application.About, i18n.T("Tablume에 관하여")),
		sep(),
		role(application.ServicesMenu, i18n.T("서비스")),
		sep(),
		role(application.Hide, i18n.T("Tablume 가리기")),
		role(application.HideOthers, i18n.T("기타 가리기")),
		role(application.UnHide, i18n.T("모두 보기")),
		sep(),
		role(application.Quit, i18n.T("Tablume 종료")),
	)
	speech := application.NewMenuFromItems(
		role(application.StartSpeaking, i18n.T("말하기 시작")),
		role(application.StopSpeaking, i18n.T("말하기 중단")),
	)
	// No Delete item: its plain Backspace shortcut would be a menu key
	// equivalent and could take the key away from the editor and the grid.
	edit := application.NewMenuFromItems(
		role(application.Undo, i18n.T("실행 취소")),
		role(application.Redo, i18n.T("실행 복귀")),
		sep(),
		role(application.Cut, i18n.T("오려두기")),
		role(application.Copy, i18n.T("복사하기")),
		role(application.Paste, i18n.T("붙여넣기")),
		role(application.PasteAndMatchStyle, i18n.T("붙여넣고 스타일 일치시키기")),
		role(application.SelectAll, i18n.T("전체 선택")),
		sep(),
		application.NewSubmenu(i18n.T("말하기"), speech),
	)
	window := application.NewMenuFromItems(
		role(application.Minimise, i18n.T("최소화")),
		role(application.Zoom, i18n.T("확대/축소")),
		sep(),
		role(application.Front, i18n.T("모두 앞으로 가져오기")),
	)

	return application.NewMenuFromItems(
		application.NewSubmenu("Tablume", app),
		application.NewSubmenu(i18n.T("편집"), edit),
		application.NewSubmenu(i18n.T("윈도우"), window),
	)
}
