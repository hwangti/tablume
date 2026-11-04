// Command version sets the release version in every file that carries one,
// or checks that they agree:
//
//	go run ./build/version 0.9.1   (wails3 task version VERSION=0.9.1)
//	go run ./build/version -check
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A spot in a file that holds the version: the pattern's first group is the
// text before it, the second the version itself.
type spot struct {
	file string
	re   *regexp.Regexp
	fmt  func(v string) string // how the file writes it
}

func plain(v string) string { return v }

// The Windows manifest wants four numbers.
func four(v string) string { return v + ".0" }

var spots = []spot{
	{"frontend/package.json", regexp.MustCompile(`(?s)\A(\{\s*"name":\s*"[^"]*",\s*(?:"private":\s*\w+,\s*)?"version":\s*")([^"]*)`), plain},
	{"frontend/package-lock.json", regexp.MustCompile(`(?s)\A(\{\s*"name":\s*"[^"]*",\s*"version":\s*")([^"]*)`), plain},
	{"frontend/package-lock.json", regexp.MustCompile(`(?s)("packages":\s*\{\s*"":\s*\{\s*"name":\s*"[^"]*",\s*"version":\s*")([^"]*)`), plain},
	{"build/darwin/Info.plist", regexp.MustCompile(`(<key>CFBundleVersion</key>\s*<string>)([^<]*)`), plain},
	{"build/darwin/Info.plist", regexp.MustCompile(`(<key>CFBundleShortVersionString</key>\s*<string>)([^<]*)`), plain},
	{"build/darwin/Info.dev.plist", regexp.MustCompile(`(<key>CFBundleVersion</key>\s*<string>)([^<]*)`), plain},
	{"build/darwin/Info.dev.plist", regexp.MustCompile(`(<key>CFBundleShortVersionString</key>\s*<string>)([^<]*)`), plain},
	{"build/windows/info.json", regexp.MustCompile(`("file_version":\s*")([^"]*)`), plain},
	{"build/windows/info.json", regexp.MustCompile(`("ProductVersion":\s*")([^"]*)`), plain},
	{"build/windows/wails.exe.manifest", regexp.MustCompile(`(<assemblyIdentity type="win32" name="com\.wails\.Tablume" version=")([^"]*)`), four},
}

// Plain x.y.z: what macOS and Windows accept as a version number.
var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func main() {
	check := flag.Bool("check", false, "check that every file has the version in VERSION")
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	var err error
	switch {
	case *check && flag.NArg() == 0:
		err = Check(*root)
	case !*check && flag.NArg() == 1:
		err = Set(*root, flag.Arg(0))
	default:
		err = errors.New("usage: version x.y.z | version -check")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "version:", err)
		os.Exit(1)
	}
}

// Read returns the version in the VERSION file.
func Read(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "VERSION"))
	return strings.TrimSpace(string(b)), err
}

// Set writes v into VERSION and every spot.
func Set(root, v string) error {
	if !semver.MatchString(v) {
		return fmt.Errorf("%q: want x.y.z", v)
	}
	files := map[string]string{}
	for _, s := range spots {
		text, ok := files[s.file]
		if !ok {
			b, err := os.ReadFile(filepath.Join(root, s.file))
			if err != nil {
				return err
			}
			text = string(b)
		}
		if !s.re.MatchString(text) {
			return fmt.Errorf("%s: version not found (%s)", s.file, s.re)
		}
		done := false
		files[s.file] = s.re.ReplaceAllStringFunc(text, func(m string) string {
			if done {
				return m
			}
			done = true
			g := s.re.FindStringSubmatch(m)
			return g[1] + s.fmt(v) + m[len(g[0]):]
		})
	}
	for f, text := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte(text), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(v+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("version", v)
	return nil
}

// Check reports every spot whose version differs from VERSION.
func Check(root string) error {
	v, err := Read(root)
	if err != nil {
		return err
	}
	if !semver.MatchString(v) {
		return fmt.Errorf("VERSION %q: want x.y.z", v)
	}
	var bad []string
	for _, s := range spots {
		b, err := os.ReadFile(filepath.Join(root, s.file))
		if err != nil {
			return err
		}
		g := s.re.FindSubmatch(b)
		switch {
		case g == nil:
			bad = append(bad, s.file+": version not found")
		case string(g[2]) != s.fmt(v):
			bad = append(bad, fmt.Sprintf("%s: %s, VERSION is %s", s.file, g[2], v))
		}
	}
	if bad != nil {
		return fmt.Errorf("out of step (run `wails3 task version VERSION=%s`):\n  %s", v, strings.Join(bad, "\n  "))
	}
	return nil
}
