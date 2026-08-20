package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Functions whose first string argument is a source sentence. A source that
// never reaches the table is not an error at runtime — it silently renders in
// English — so nothing but a check like this one will find it.
var translatable = map[string][]int{
	"T":          {1},    // i18n.T(lang, source, …)
	"setReason":  {0},    // selfupdate
	"skip":       {0},    // advisor
	"errf":       {1},    // selfupdate, after the wrapped cause
	"writeError": {3, 4}, // writeError(w, r, status, message, hint)
}

// Sources deliberately left untranslated: pass-through formats that carry an
// already-rendered message rather than a sentence of their own.
var notSentences = map[string]bool{"%s": true, "%v": true, "%w": true}

func TestEverySourceStringHasATranslation(t *testing.T) {
	root := filepath.Join("..", "..")
	var missing []string
	seen := map[string]bool{}

	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "internal/i18n") {
			return nil
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			idxs, wanted := translatable[calleeName(call.Fun)]
			if !wanted {
				return true
			}
			for _, idx := range idxs {
				if len(call.Args) <= idx {
					continue
				}
				src, ok := constString(call.Args[idx])
				if !ok || src == "" || notSentences[src] || seen[src] {
					continue
				}
				seen[src] = true
				if _, found := da[src]; !found {
					missing = append(missing, path+": "+strconv.Quote(src))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range missing {
		t.Errorf("no Danish translation for %s", m)
	}
	if len(seen) == 0 {
		t.Fatal("found no source strings at all; the walk is not reaching the code")
	}
	t.Logf("checked %d source strings", len(seen))
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// constString reconstructs a string literal, including one written as several
// literals joined with +, which is how the long sentences are wrapped.
func constString(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, okl := constString(v.X)
		r, okr := constString(v.Y)
		return l + r, okl && okr
	}
	return "", false
}
