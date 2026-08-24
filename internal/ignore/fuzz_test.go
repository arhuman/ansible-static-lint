package ignore

import (
	"strings"
	"testing"
)

// FuzzParse drives the ignore-file parser with arbitrary bytes.
//
// The parser reads a file the linted repository owns, so the property under
// test is that no input makes it panic or hang: every line either records an
// entry or is refused with an error naming its number. The oracle is weaker
// than the parser on purpose, asserting only what cannot be wrong: a parse that
// succeeded recorded no entry with an empty column, and a parse that failed
// said which line it choked on.
func FuzzParse(f *testing.F) {
	f.Add("")
	f.Add("play.yml name[missing]\n")
	f.Add("play.yml name[missing] skip\n")
	f.Add("# comment only\n")
	f.Add("play.yml\n")
	f.Add("play.yml name[play] bogus\n")
	f.Add("play.yml name[play] skip extra\n")
	f.Add("a#b c\r\rd e skip\n")
	f.Add("  \t \n\n\n")

	f.Fuzz(func(t *testing.T, content string) {
		r, err := parse("fuzz", []byte(content))
		if err != nil {
			if !strings.HasPrefix(err.Error(), "fuzz:") {
				t.Errorf("error %q does not name the file and line it refused", err)
			}
			return
		}
		for k := range r.entries {
			if k.path == "" || k.tag == "" {
				t.Errorf("recorded an entry with an empty column: %+v", k)
			}
			if strings.ContainsAny(k.path+k.tag, " \t\n\r#") {
				t.Errorf("recorded an entry holding a separator or comment rune: %+v", k)
			}
		}
		// Applying is part of the surface: a parsed file must never make the
		// filter panic, whatever it holds.
		r.Apply(sample())
	})
}
