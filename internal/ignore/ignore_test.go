package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// sample is the run an ignore file is resolved against. Two findings share a
// path so that an entry naming one cannot be shown to work by silencing both.
func sample() []rules.Finding {
	return []rules.Finding{
		{Path: "play.yml", Line: 2, Tag: "name[play]", Message: "All plays should be named."},
		{Path: "play.yml", Line: 4, Tag: "name[missing]", Message: "All tasks should be named."},
		{Path: "other.yml", Line: 1, Tag: "yaml[indentation]", Message: "wrong indentation"},
	}
}

// applied renders what an ignore file did to sample(): a finding it dropped is
// absent, one it marked carries " ignored". Written as one string per finding
// so a case states the whole verdict rather than a count.
func applied(t *testing.T, content string) []string {
	t.Helper()
	r, err := parse("ignore", []byte(content))
	if err != nil {
		t.Fatalf("parse(%q): %v", content, err)
	}
	var got []string
	for _, f := range r.Apply(sample()) {
		if !f.Ignored {
			got = append(got, f.Tag)
			continue
		}
		// The mark is what demotes the finding; a marked finding that still
		// failed the run would defeat the whole point of the file.
		if !f.Warning {
			t.Errorf("%s is ignored but not a warning", f.Tag)
		}
		got = append(got, f.Tag+" ignored")
	}
	return got
}

func TestApply(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "empty file changes nothing",
			content: "",
			want:    []string{"name[play]", "name[missing]", "yaml[indentation]"},
		},
		{
			name:    "a bare entry keeps the finding and marks it",
			content: "play.yml name[missing]\n",
			want:    []string{"name[play]", "name[missing] ignored", "yaml[indentation]"},
		},
		{
			name:    "a skip entry removes the finding",
			content: "play.yml name[missing] skip\n",
			want:    []string{"name[play]", "yaml[indentation]"},
		},
		{
			name:    "comments, blank and whitespace-only lines are skipped",
			content: "# a comment\n\n   \n\t\nplay.yml name[play] # trailing\n",
			want:    []string{"name[play] ignored", "name[missing]", "yaml[indentation]"},
		},
		{
			name:    "any run of whitespace separates the columns",
			content: "  play.yml\t\t name[play]   skip  \n",
			want:    []string{"name[missing]", "yaml[indentation]"},
		},
		{
			name:    "a lone carriage return ends a line",
			content: "play.yml name[play] skip\rplay.yml name[missing] skip\r",
			want:    []string{"yaml[indentation]"},
		},
		{
			name:    "a native rule id resolves to the same rule",
			content: "play.yml name.task-missing skip\n",
			want:    []string{"name[play]", "yaml[indentation]"},
		},
		{
			name:    "skip wins over a bare duplicate, whichever comes first",
			content: "play.yml name[play]\nplay.yml name[play] skip\n",
			want:    []string{"name[missing]", "yaml[indentation]"},
		},
		{
			name:    "skip wins when the bare duplicate comes second",
			content: "play.yml name[play] skip\nplay.yml name[play]\n",
			want:    []string{"name[missing]", "yaml[indentation]"},
		},
		{
			name:    "an unknown rule id is accepted and never fires",
			content: "play.yml foo-bar\n",
			want:    []string{"name[play]", "name[missing]", "yaml[indentation]"},
		},
		{
			name:    "a rule id does not cover its subtags",
			content: "other.yml yaml skip\n",
			want:    []string{"name[play]", "name[missing]", "yaml[indentation]"},
		},
		{
			name:    "an entry is bound to the path that names it",
			content: "other.yml name[play] skip\n",
			want:    []string{"name[play]", "name[missing]", "yaml[indentation]"},
		},
		// The four below reproduce upstream's parser rather than endorse it.
		{
			name:    "a fourth field silently disarms the qualifier",
			content: "play.yml name[play] skip extra\n",
			want:    []string{"name[play] ignored", "name[missing]", "yaml[indentation]"},
		},
		{
			name:    "a comment cuts at the first hash, wherever it sits",
			content: "play.yml name[pl#ay] skip\n",
			want:    []string{"name[play]", "name[missing]", "yaml[indentation]"},
		},
		{
			name:    "a path is matched verbatim, so ./ never resolves",
			content: "./play.yml name[play] skip\n",
			want:    []string{"name[play]", "name[missing]", "yaml[indentation]"},
		},
		{
			name:    "a path holding a space cannot be expressed",
			content: "my play.yml\n",
			want:    []string{"name[play]", "name[missing]", "yaml[indentation]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applied(t, tt.content)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestApplyPathWithSpaceParsesAsAnEntry pins the other half of the previous
// case: `my play.yml` is not a one-field line, it is the path `my` with the
// rule `play.yml`, which is why it parses at all.
func TestApplyPathWithSpaceParsesAsAnEntry(t *testing.T) {
	r, err := parse("ignore", []byte("my play.yml\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := r.entries[entry{path: "my", tag: "play.yml"}]; !ok {
		t.Errorf("entries = %v, want the line split into path %q and rule %q", r.entries, "my", "play.yml")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "a path with no rule id",
			content: "play.yml\n",
			want:    "ignore:1: no rule id after the path",
		},
		{
			name:    "the line number names the offending line",
			content: "# comment\nplay.yml name[play]\nplay.yml\n",
			want:    "ignore:3: no rule id after the path",
		},
		{
			name:    "an unknown qualifier",
			content: "play.yml name[play] warn\n",
			want:    `unknown qualifier "warn"`,
		},
		{
			name:    "an unknown qualifier beside a valid one",
			content: "play.yml name[play] skip,warn\n",
			want:    `unknown qualifier "warn"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse("ignore", []byte(tt.content))
			if err == nil {
				t.Fatalf("parse(%q) = nil, want an error", tt.content)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadFindsTheDefaultName(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".ansible-lint-ignore"), "play.yml name[play] skip\n")

	r, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(r.Apply(sample())); got != 2 {
		t.Errorf("kept %d findings, want 2", got)
	}
}

// TestLoadFallsBackToTheConfigDir is the shape a repository reaches for when it
// keeps its dotfiles under .config, the same reason config discovery reads more
// than one name.
func TestLoadFallsBackToTheConfigDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".config", "ansible-lint-ignore.txt"), "play.yml name[play] skip\n")

	r, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(r.Apply(sample())); got != 2 {
		t.Errorf("kept %d findings, want 2", got)
	}
}

// TestLoadDoesNotMergeTheTwoNames: the first name found wins outright, so a
// repository holding both does not get the union of them.
func TestLoadDoesNotMergeTheTwoNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".ansible-lint-ignore"), "play.yml name[play] skip\n")
	write(t, filepath.Join(dir, ".config", "ansible-lint-ignore.txt"), "play.yml name[missing] skip\n")

	r, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := r.Apply(sample())
	if len(got) != 2 || got[0].Tag != "name[missing]" {
		t.Errorf("kept %v, want only the preferred file to apply", got)
	}
}

func TestLoadWithoutAnyFileIsNotAnError(t *testing.T) {
	r, err := Load(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(r.Apply(sample())); got != 3 {
		t.Errorf("kept %d findings, want all 3", got)
	}
}

// TestLoadDoesNotSearchUpward: upstream resolves both names against the process
// directory only. A file one level up belongs to another project's run.
func TestLoadDoesNotSearchUpward(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".ansible-lint-ignore"), "play.yml name[play] skip\n")
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := Load(nested, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(r.Apply(sample())); got != 3 {
		t.Errorf("kept %d findings, want all 3: the parent's file must not apply", got)
	}
}

func TestLoadOverrideIsReadInPlaceOfTheDefault(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".ansible-lint-ignore"), "play.yml name[play] skip\n")
	named := filepath.Join(dir, "elsewhere.txt")
	write(t, named, "play.yml name[missing] skip\n")

	r, err := Load(dir, named)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := r.Apply(sample())
	if len(got) != 2 || got[0].Tag != "name[play]" {
		t.Errorf("kept %v, want only the named file to apply", got)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
