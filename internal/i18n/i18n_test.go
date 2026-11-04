package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var hangul = regexp.MustCompile(`[\x{AC00}-\x{D7A3}]`)

// Every Korean string the backend shows has an English entry: the Korean
// arguments of T / Sprintf / Error / Errorf (and In) in the repository, and
// {{T "…"}} in the sign-in page templates.
func TestEnglishEntries(t *testing.T) {
	root, _ := filepath.Abs("../..")
	used := map[string]bool{}
	tmplT := regexp.MustCompile(`\{\{\s*T\s+("(?:[^"\\]|\\.)*")\s*\}\}`)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "frontend" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				name := ""
				if ok {
					name = sel.Sel.Name
				} else if id, ok := n.Fun.(*ast.Ident); ok {
					name = id.Name
				}
				i := 0
				switch name {
				case "T", "Sprintf", "Error", "Errorf":
				case "In":
					i = 1
				default:
					return true
				}
				if ok {
					if x, ok := sel.X.(*ast.Ident); !ok || (x.Name != "i18n" && x.Name != "fmt") {
						return true
					}
				}
				if len(n.Args) > i {
					if lit, ok := n.Args[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil && hangul.MatchString(s) {
							if sel != nil && sel.X.(*ast.Ident).Name == "fmt" {
								t.Errorf("%s: Korean in fmt.%s: %q", p, name, s)
							} else {
								used[s] = true
							}
						}
					}
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if s, err := strconv.Unquote(n.Value); err == nil {
						for _, m := range tmplT.FindAllStringSubmatch(s, -1) {
							k, _ := strconv.Unquote(m[1])
							used[k] = true
						}
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for k := range used {
		if _, ok := en[k]; !ok {
			t.Errorf("no English for %q", k)
		}
	}
	for k := range en {
		if !used[k] {
			t.Errorf("English entry not used: %q", k)
		}
	}
}

func TestIn(t *testing.T) {
	if In("en", "연결되어 있지 않습니다") != "Not connected" || In("ko", "연결되어 있지 않습니다") != "연결되어 있지 않습니다" {
		t.Fatal("In")
	}
	if FromAccept("ko-KR,ko;q=0.9,en;q=0.8") != "ko" || FromAccept("en-US,en;q=0.9") != "en" {
		t.Fatal("FromAccept")
	}
}
