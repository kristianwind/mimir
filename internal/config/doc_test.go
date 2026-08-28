package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// A configuration reference that has drifted is worse than none: it is a
// document that looks authoritative and quietly lies. This fails the build
// rather than letting somebody deploy against a variable that was renamed.
func TestEveryVariableIsDocumented(t *testing.T) {
	code, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "CONFIGURATION.md"))
	if err != nil {
		t.Fatalf("the configuration reference is missing, and mimir help points at it: %v", err)
	}

	// In the source they are string literals; in the document they are
	// inline code.
	inCode := names(regexp.MustCompile(`"(MIMIR_[A-Z_]+)"`).FindAllSubmatch(code, -1))
	inDoc := names(regexp.MustCompile("`(MIMIR_[A-Z_]+)`").FindAllSubmatch(doc, -1))

	for _, n := range sorted(inCode) {
		if !inDoc[n] {
			t.Errorf("%s is read by the code and not documented", n)
		}
	}
	for _, n := range sorted(inDoc) {
		if !inCode[n] {
			t.Errorf("%s is documented and nothing reads it", n)
		}
	}
}

func names(matches [][][]byte) map[string]bool {
	out := map[string]bool{}
	for _, m := range matches {
		out[string(m[1])] = true
	}
	return out
}

func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
