// Package i18n translates the backend's messages (errors shown in the app,
// the macOS menu, the browser build's sign-in pages). As in the frontend,
// the Korean text is the key: T("연결되어 있지 않습니다") is that text in
// Korean and its entry in the English table (en.go) in English. Format
// strings are keys too, so an English entry keeps the same verbs.
package i18n

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

var (
	cur      atomic.Value // "ko" or "en"
	explicit atomic.Bool  // chosen in the app (else guessed)
)

func init() { cur.Store(guess()) }

// guess is the language before the app says: the environment's locale
// (English without one).
func guess() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if v == "C" || v == "POSIX" || strings.HasPrefix(v, "C.") {
				break
			}
			return Normalize(v)
		}
	}
	return "en"
}

// Normalize maps a locale ("ko_KR.UTF-8", "en-US", …) to "ko" or "en".
func Normalize(l string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "ko") {
		return "ko"
	}
	return "en"
}

// Set chooses the language and reports whether it changed.
func Set(lang string) bool {
	lang = Normalize(lang)
	explicit.Store(true)
	return cur.Swap(lang) != lang
}

func Lang() string { return cur.Load().(string) }

// Chosen reports whether the language was set in the app (not guessed).
func Chosen() bool { return explicit.Load() }

// FromAccept picks ko or en from an Accept-Language header.
func FromAccept(h string) string {
	for _, part := range strings.Split(h, ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if tag == "" || tag == "*" {
			continue
		}
		return Normalize(tag)
	}
	return Lang()
}

// In is ko in the given language.
func In(lang, ko string) string {
	if lang == "en" {
		if s, ok := en[ko]; ok {
			return s
		}
	}
	return ko
}

func T(ko string) string                 { return In(Lang(), ko) }
func Sprintf(ko string, a ...any) string { return fmt.Sprintf(T(ko), a...) }
func Error(ko string) error              { return errors.New(T(ko)) }
func Errorf(ko string, a ...any) error   { return fmt.Errorf(T(ko), a...) }
