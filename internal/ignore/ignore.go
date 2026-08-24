// Package ignore reads ansible-lint's `.ansible-lint-ignore` file, which
// silences named rules on named files from outside the linted source.
//
// It exists because `# noqa` cannot reach everything: a finding reported
// against a file as a whole has no line to carry the comment. The file is also
// how a repository adopts a linter without fixing everything first, which is
// why its two forms differ in strength. A bare entry keeps its finding and
// marks it as known; an entry qualified `skip` removes it outright.
//
// The parser reproduces upstream's, quirks included. See
// docs/adr/0006-ignore-file-semantics.md for which oddities are deliberate and
// where astl knowingly departs.
package ignore

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/arhuman/ansible-static-lint/internal/rules"
	"github.com/arhuman/ansible-static-lint/internal/safeio"
)

// Filenames are the two names ansible-lint tries, in its order (IGNORE_FILE in
// its loaders.py). The first that exists wins and the second is not merged into
// it, the same way config files resolve.
var Filenames = []string{
	".ansible-lint-ignore",
	".config/ansible-lint-ignore.txt",
}

// entry is one (path, rule) pair named by the file. The path is stored exactly
// as written: upstream applies no normalization on this side, so `./play.yml`
// matches nothing, while the finding it was meant to silence prints as
// `play.yml`.
type entry struct {
	path string
	tag  string
}

// Rules is a parsed ignore file: which rule is ignored on which path, and
// whether that entry carried the `skip` qualifier.
type Rules struct {
	entries map[entry]bool
}

// Load reads the ignore file that applies to a run. A named override is read as
// given; otherwise the two default names are tried under dir. No file at all
// yields empty rules, which is not an error: most repositories have none.
//
// dir is a parameter rather than the process directory so that a test can point
// the search at somewhere it controls. The CLI passes ".", which is where
// ansible-lint looks and nowhere else: there is deliberately no walk up to a
// repository root, so a file one directory up is not read however much
// upstream's documentation implies otherwise.
func Load(dir, override string) (Rules, error) {
	if override != "" {
		return loadFile(override)
	}
	for _, name := range Filenames {
		r, err := loadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return r, err
	}
	return Rules{}, nil
}

func loadFile(path string) (Rules, error) {
	data, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		return Rules{}, err
	}
	return parse(path, data)
}

// errNoRule reports a line naming a path and nothing else. Upstream raises an
// IndexError its own handler does not catch, so it dies with a traceback;
// naming the file and line is the same refusal, said usefully.
var errNoRule = errors.New("no rule id after the path")

// parse reads every line of an ignore file. Lines are split the way Python's
// text mode splits them, where a lone carriage return also ends a line.
func parse(name string, data []byte) (Rules, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	r := Rules{entries: make(map[entry]bool)}
	for i, line := range strings.Split(text, "\n") {
		if err := r.add(line); err != nil {
			return Rules{}, fmt.Errorf("%s:%d: %w", name, i+1, err)
		}
	}
	return r, nil
}

// add records one line, or reports why it cannot be read.
func (r Rules) add(line string) error {
	// Upstream cuts at the first '#' anywhere on the line, not at a space
	// followed by one, and offers no escape. A path containing '#' is
	// therefore unusable. That is the format, not an oversight to repair.
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	// Any run of whitespace separates, which is also why a path containing a
	// space cannot be expressed at all.
	fields := strings.Fields(line)
	switch len(fields) {
	case 0:
		return nil
	case 1:
		return errNoRule
	}
	skip := false
	// Exactly three, not three or more: upstream reads the qualifier only from
	// a three-field line, so a fourth field silently disarms the `skip` on it.
	if len(fields) == 3 {
		var err error
		if skip, err = qualifier(fields[2]); err != nil {
			return err
		}
	}
	k := entry{path: fields[0], tag: rules.Canonical(fields[1])}
	// One path may carry the same rule twice, once qualified and once not.
	// Upstream keeps both in a set and answers with whichever it happens to
	// iterate first, so its verdict there is unstable; skip wins here.
	r.entries[k] = r.entries[k] || skip
	return nil
}

// qualifier reads the third column, where `skip` is the only word upstream
// defines. It is comma-separated for a vocabulary that never grew.
func qualifier(s string) (bool, error) {
	skip := false
	for _, q := range strings.Split(s, ",") {
		if q != "skip" {
			return false, fmt.Errorf("unknown qualifier %q, only \"skip\" is defined", q)
		}
		skip = true
	}
	return skip, nil
}

// Apply resolves a run's findings against the ignore file.
//
// The two forms differ, and that difference is the point of the file. A bare
// entry keeps its finding and marks it, so a run still prints a line for
// something already known; a `skip` entry drops it, so nothing is printed at
// all. Marking is also what lets the run pass, because a marked finding reports
// at warning level and warnings do not fail a run.
//
// Matching is exact on both columns: `yaml` does not cover `yaml[indentation]`,
// and a rule id upstream does not know is simply an entry that never fires.
func (r Rules) Apply(findings []rules.Finding) []rules.Finding {
	if len(r.entries) == 0 {
		return findings
	}
	out := findings[:0]
	for _, f := range findings {
		skip, ok := r.entries[entry{path: f.Path, tag: f.Tag}]
		if !ok {
			out = append(out, f)
			continue
		}
		if skip {
			continue
		}
		f.Ignored = true
		f.Warning = true
		out = append(out, f)
	}
	return out
}
