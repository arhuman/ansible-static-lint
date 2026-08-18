package yamlscan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The .tokens goldens were generated with pyyaml 6 (BaseLoader.get_token) via
// the reference dumper described in docs/design/static-yaml-and-var-naming.md.
// They pin the pyyaml mark equivalence the yamllint port depends on; a
// mismatch means the vendored scanner drifted from pyyaml semantics.
func TestTokensMatchPyyamlGoldens(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.yml"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".yml")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			golden, err := os.ReadFile(filepath.Join("testdata", name+".tokens"))
			if err != nil {
				t.Fatal(err)
			}
			got := dumpTokens(string(src))
			if got != string(golden) {
				t.Errorf("token stream diverges from pyyaml golden\ngot:\n%s\nwant:\n%s", got, golden)
			}
		})
	}
}

func TestTokensStopAtScannerError(t *testing.T) {
	toks := Tokens("\tkey: {\n\t\tbroken")
	for _, tok := range toks {
		if tok.Kind == StreamEnd {
			t.Fatalf("scanner error input must not reach StreamEnd, got %v", toks)
		}
	}
}
