package yamllint

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// astl reads configuration out of repositories the operator does not control,
// so a chain that loops has to be rejected rather than followed. Before this
// was enforced the two files below recursed until the process was killed.
func TestExtendsCycleIsRejected(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "extends: b.yml\nrules: {}\n")
	write(t, filepath.Join(dir, "b.yml"), "extends: .yamllint\nrules: {}\n")

	_, _, err := Load(dir)
	if err == nil {
		t.Fatal("a looping extends chain was accepted")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error does not name the cycle: %v", err)
	}
}

// A file reaching itself directly is the degenerate case of the same defect.
func TestExtendsSelfReferenceIsRejected(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "extends: .yamllint\nrules: {}\n")

	if _, _, err := Load(dir); err == nil {
		t.Fatal("a self-referential extends was accepted")
	}
}

// A chain can also fail to terminate without ever repeating a file, so depth is
// bounded separately from the cycle check.
func TestExtendsChainDepthIsBounded(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "extends: c0.yml\nrules: {}\n")
	for i := 0; i < maxExtendsDepth+5; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("c%d.yml", i)),
			fmt.Sprintf("extends: c%d.yml\nrules: {}\n", i+1))
	}

	_, _, err := Load(dir)
	if err == nil {
		t.Fatal("an over-long extends chain was accepted")
	}
	if !strings.Contains(err.Error(), "longer than") {
		t.Errorf("error does not name the depth limit: %v", err)
	}
}

// The bound must not cost legitimate configurations anything: a chain of
// ordinary length still resolves, and the values it layers still arrive.
func TestExtendsChainWithinTheBoundStillResolves(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "extends: mid.yml\nrules: {}\n")
	write(t, filepath.Join(dir, "mid.yml"), "extends: base.yml\nrules: {}\n")
	write(t, filepath.Join(dir, "base.yml"), "extends: default\nrules:\n  line-length: disable\n")

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.Describe(), "line-length: disabled") {
		t.Errorf("value from the far end of the chain did not survive:\n%s", cfg.Describe())
	}
}

// A repository astl lints chooses its own `extends` paths, and the resulting
// error travels into a CI log, so nothing read at that path may appear in it.
// Before this, yaml.v3 quoted a fragment of the target and an unrecognised rule
// was reported by name, which together made the loader a disclosure channel.
func TestExtendsErrorsDoNotEchoTheTargetsContent(t *testing.T) {
	const secret = "TOPSECRETVALUE"

	t.Run("file that is not YAML", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "outside.env"), secret+"\n")
		write(t, filepath.Join(dir, ".yamllint"), "extends: outside.env\nrules: {}\n")

		_, _, err := Load(dir)
		if err == nil {
			t.Fatal("want an error, got none")
		}
		if strings.Contains(err.Error(), secret[:7]) {
			t.Errorf("error echoes the target's content: %v", err)
		}
	})

	t.Run("rule key names", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "outside.yml"), "rules:\n  "+secret+": enable\n")
		write(t, filepath.Join(dir, ".yamllint"), "extends: outside.yml\nrules: {}\n")

		_, _, err := Load(dir)
		if err == nil {
			t.Fatal("want an error, got none")
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error echoes a key read from the target: %v", err)
		}
	})
}

// The reduction applies to what `extends` reached, not to the file the operator
// pointed astl at: that one is theirs, its detail tells them what to fix, and
// repeating it discloses nothing they did not already write.
func TestTopLevelConfigErrorsKeepTheirDetail(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "rules:\n  no-such-rule: enable\n")

	_, _, err := Load(dir)
	if err == nil {
		t.Fatal("want an error, got none")
	}
	if !strings.Contains(err.Error(), "no-such-rule") {
		t.Errorf("detail was dropped from the operator's own config: %v", err)
	}
}

// extendsFailure loads a config whose `extends` names target and returns the
// error with target and the config's directory masked, so failures on
// different targets compare by wording alone.
func extendsFailure(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "extends: '"+target+"'\nrules: {}\n")
	_, _, err := Load(dir)
	if err == nil {
		t.Fatalf("extends %s was accepted", target)
	}
	return redact(redact(err.Error(), target, "TARGET"), dir, "DIR")
}

// ignoreFromFileWarning is extendsFailure for `ignore-from-file`, which warns
// rather than fails.
func ignoreFromFileWarning(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "ignore-from-file: '"+target+"'\n")
	_, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want one for %s", warnings, target)
	}
	return redact(warnings[0], target, "TARGET")
}

// redact replaces path in s with label, both as printed raw and as printed by
// %q, which doubles a Windows path's backslashes.
func redact(s, path, label string) string {
	q := strconv.Quote(path)
	s = strings.ReplaceAll(s, q[1:len(q)-1], label)
	return strings.ReplaceAll(s, path, label)
}

// unreadableTargets are paths a config can name that astl cannot read, keyed by
// why. The CI log must not tell them apart, or a pull request could probe the
// runner for which files exist.
func unreadableTargets(t *testing.T) map[string]string {
	t.Helper()
	targets := map[string]string{
		"missing":   filepath.Join(t.TempDir(), "absent.yml"),
		"directory": t.TempDir(),
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		denied := filepath.Join(t.TempDir(), "denied.yml")
		write(t, denied, "rules: {}\n")
		if err := os.Chmod(denied, 0o000); err != nil {
			t.Fatal(err)
		}
		targets["permission denied"] = denied
	}
	return targets
}

func TestExtendsUnreadableTargetsShareOneWording(t *testing.T) {
	want := extendsFailure(t, filepath.Join(t.TempDir(), "absent.yml"))
	for why, target := range unreadableTargets(t) {
		if got := extendsFailure(t, target); got != want {
			t.Errorf("%s: error %q, want the same wording as a missing file: %q", why, got, want)
		}
	}
}

func TestIgnoreFromFileUnreadableTargetsShareOneWording(t *testing.T) {
	want := ignoreFromFileWarning(t, filepath.Join(t.TempDir(), "absent"))
	for why, target := range unreadableTargets(t) {
		if got := ignoreFromFileWarning(t, target); got != want {
			t.Errorf("%s: warning %q, want the same wording as a missing file: %q", why, got, want)
		}
	}
}

// Upstream yamllint follows an absolute `extends` anywhere on the host, and so
// does astl: the reach is kept, only what its failures reveal is narrowed.
func TestExtendsAbsolutePathOutsideTheRepositoryResolves(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "shared.yml")
	write(t, outside, "extends: default\nrules:\n  line-length: disable\n")
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".yamllint"), "extends: '"+outside+"'\nrules: {}\n")

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.Describe(), "line-length: disabled") {
		t.Errorf("the out-of-repository config was not applied:\n%s", cfg.Describe())
	}
}
