package yamllint

import (
	"fmt"
	"path/filepath"
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
