package yamllint

import (
	"path/filepath"
	"testing"
)

// TestIgnoreMatchesGitSemantics is the test that matters for issue 0006. The
// patterns are git's, not filepath.Match's, and the two disagree exactly where
// real ignore lists live: `test_*yml` must match at any depth, `roles/` must
// swallow a subtree, and `*` must stop at a separator. Getting this wrong fails
// silently, by linting or skipping files nobody looks at.
func TestIgnoreMatchesGitSemantics(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		// The kafka_role case that produced the four false positives.
		{"bare glob matches at depth", []string{"test_*yml"},
			"molecule/default/tests/3.0/test_kafka_topics.yml", true},
		{"bare glob matches at root", []string{"test_*yml"}, "test_a.yml", true},
		{"bare glob does not match a different name", []string{"test_*yml"},
			"molecule/default/converge.yml", false},

		{"directory pattern swallows its subtree", []string{"roles/"},
			"roles/common/tasks/main.yml", true},
		{"directory pattern matches at depth", []string{"roles/"},
			"molecule/x/roles/common/tasks/main.yml", true},
		{"directory pattern does not match a prefix of a name", []string{"roles/"},
			"roles_extra/main.yml", false},

		{"bare name swallows its subtree", []string{".git"}, ".git/config", true},
		{"literal name", []string{".ansible-lint"}, ".ansible-lint", true},
		{"substring glob", []string{"*vault*"}, "group_vars/all/vault.yml", true},

		// A slash anywhere anchors the pattern to the working directory.
		{"anchored pattern matches at root", []string{"zuul.d/*.yaml"}, "zuul.d/a.yaml", true},
		{"anchored pattern does not match at depth", []string{"zuul.d/*.yaml"},
			"nested/zuul.d/a.yaml", false},
		{"leading slash anchors", []string{"/molecule/"}, "molecule/x.yml", true},
		{"leading slash does not match at depth", []string{"/molecule/"},
			"a/molecule/x.yml", false},

		{"globstar spans segments", []string{"molecule/**/tests/"},
			"molecule/default/3.0/tests/a.yml", true},
		{"globstar spans zero segments", []string{"molecule/**/tests/"},
			"molecule/tests/a.yml", true},
		{"leading globstar", []string{"**/*.j2"}, "roles/x/templates/a.j2", true},

		{"star does not cross a separator", []string{"a/*.yml"}, "a/b/c.yml", false},
		{"question mark matches one character", []string{"v?.yml"}, "v1.yml", true},
		{"question mark does not cross a separator", []string{"a?b.yml"}, "a/b.yml", false},

		{"character class", []string{"v[0-9].yml"}, "v3.yml", true},
		{"character class excludes", []string{"v[0-9].yml"}, "vx.yml", false},
		{"negated class does not cross a separator", []string{"a[!x]b.yml"}, "a/b.yml", false},

		// Later patterns win, which is what makes re-inclusion work.
		{"negation re-includes", []string{"*.yml", "!keep.yml"}, "keep.yml", false},
		{"negation leaves others ignored", []string{"*.yml", "!keep.yml"}, "drop.yml", true},
		{"order matters", []string{"!keep.yml", "*.yml"}, "keep.yml", true},

		{"comment is not a pattern", []string{"# roles/"}, "roles/a.yml", false},
		{"blank line is not a pattern", []string{"", "   "}, "a.yml", false},

		{"leading ./ on the path is stripped", []string{"roles/"}, "./roles/a.yml", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseIgnore(c.patterns).match(c.path); got != c.want {
				t.Errorf("patterns %q against %q: got %v, want %v",
					c.patterns, c.path, got, c.want)
			}
		})
	}
}

func TestIgnoreEmptySpecMatchesNothing(t *testing.T) {
	if parseIgnore(nil).match("a.yml") {
		t.Error("an absent ignore list must not skip anything")
	}
	if parseIgnore([]string{"", "# only a comment"}).match("a.yml") {
		t.Error("a list with no usable pattern must not skip anything")
	}
}

// TestPerRuleIgnoreScopesToThatRule pins the distinction between the two
// `ignore` keys: the top-level one silences the whole family for a file, a
// rule's own silences that rule alone and leaves the others reporting.
func TestPerRuleIgnoreScopesToThatRule(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"),
		"---\nrules:\n  line-length:\n    max: 80\n    ignore: |\n      generated/\n  trailing-spaces: enable\n")
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	scoped := cfg.ForFile("generated/a.yml")
	if scoped == nil {
		t.Fatal("a per-rule ignore must not skip the file entirely")
	}
	if scoped.Enabled("line-length") {
		t.Error("line-length names generated/ in its own ignore, so it must be off there")
	}
	if !scoped.Enabled("trailing-spaces") {
		t.Error("every other rule must still run on a file one rule ignores")
	}

	elsewhere := cfg.ForFile("roles/a.yml")
	if !elsewhere.Enabled("line-length") {
		t.Error("the per-rule ignore must not leak to files it does not match")
	}
}

// TestForFileDoesNotMutateTheSharedConfig guards the scoping above: files are
// linted concurrently through one config, so disabling a rule for one path must
// not disable it for every other.
func TestForFileDoesNotMutateTheSharedConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"),
		"---\nrules:\n  line-length:\n    max: 80\n    ignore: |\n      generated/\n")
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	_ = cfg.ForFile("generated/a.yml")
	if !cfg.Enabled("line-length") {
		t.Error("ForFile must not switch the rule off on the config it was called on")
	}
	if !cfg.ForFile("roles/a.yml").Enabled("line-length") {
		t.Error("a later file must be unaffected by an earlier scoped lookup")
	}
}

func TestIgnoreAndIgnoreFromFileAreExclusive(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"),
		"---\nignore: |\n  a/\nignore-from-file: .gitignore\n")
	if _, _, err := Load(dir); err == nil {
		t.Error("setting both ignore and ignore-from-file must be an error, as it is upstream")
	}
}

func TestIgnoreFromFileReadsPatterns(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".gitignore"), "generated/\n*.bak.yml\n")
	write(t, filepath.Join(dir, ".yamllint"), "---\nignore-from-file: .gitignore\n")
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ForFile("generated/a.yml") != nil {
		t.Error("patterns read from the named file must apply")
	}
	if cfg.ForFile("roles/a.yml") == nil {
		t.Error("a file no pattern matches must still be linted")
	}
}

// An unreadable ignore-from-file is a warning, not a failure: the repository
// chose the path, and refusing to lint over it would be the worse outcome.
func TestIgnoreFromFileMissingWarns(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "---\nignore-from-file: .absent\n")
	cfg, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("a missing ignore file must not fail the load: %v", err)
	}
	if cfg.ForFile("roles/a.yml") == nil {
		t.Error("nothing should be ignored when the pattern file could not be read")
	}
	if len(warnings) == 0 {
		t.Error("a pattern file that could not be read must be reported")
	}
}
