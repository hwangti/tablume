package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Wails builds the App / Edit / Window menus with fixed English titles.
// Retitle them in Korean (the names macOS itself uses in Korean).
static NSString *tablumeTr(NSString *t) {
	static NSDictionary *m = nil;
	if (m == nil) {
		m = [@{
			@"Hide Others": @"기타 가리기",
			@"Show All": @"모두 보기",
			@"Edit": @"편집",
			@"Undo": @"실행 취소",
			@"Redo": @"실행 복귀",
			@"Cut": @"오려두기",
			@"Copy": @"복사하기",
			@"Paste": @"붙여넣기",
			@"Paste and Match Style": @"붙여넣고 스타일 일치시키기",
			@"Delete": @"삭제",
			@"Select All": @"전체 선택",
			@"Speech": @"말하기",
			@"Start Speaking": @"말하기 시작",
			@"Stop Speaking": @"말하기 중단",
			@"Window": @"윈도우",
			@"Minimize": @"최소화",
			@"Zoom": @"확대/축소",
			@"Full Screen": @"전체 화면",
		} retain];
	}
	NSString *v = m[t];
	if (v != nil) return v;
	if ([t hasPrefix:@"About "]) return [NSString stringWithFormat:@"%@에 관하여", [t substringFromIndex:6]];
	if ([t hasPrefix:@"Hide "]) return [NSString stringWithFormat:@"%@ 가리기", [t substringFromIndex:5]];
	if ([t hasPrefix:@"Quit "]) return [NSString stringWithFormat:@"%@ 종료", [t substringFromIndex:5]];
	return nil;
}

static void tablumeWalk(NSMenu *menu) {
	for (NSMenuItem *it in [menu itemArray]) {
		NSString *n = tablumeTr([it title]);
		if (n != nil) [it setTitle:n];
		NSMenu *sub = [it submenu];
		if (sub != nil) {
			NSString *s = tablumeTr([sub title]);
			if (s != nil) [sub setTitle:s];
			tablumeWalk(sub);
		}
	}
}

static void tablumeLocalizeMenu(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSMenu *m = [NSApp mainMenu];
		if (m != nil) tablumeWalk(m);
	});
}
*/
import "C"

func localizeMenu() { C.tablumeLocalizeMenu() }
