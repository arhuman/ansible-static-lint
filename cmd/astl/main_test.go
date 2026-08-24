package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/discover"
)

// dirtyPlaybook has an unnamed play and an unnamed task, so it yields two
// name findings. cleanPlaybook is the same content with both names supplied.
const (
	dirtyPlaybook = "---\n" +
		"- hosts: localhost\n" +
		"  tasks:\n" +
		"    - ansible.builtin.debug:\n" +
		"        msg: hi\n"

	cleanPlaybook = "---\n" +
		"- name: Example play\n" +
		"  hosts: localhost\n" +
		"  tasks:\n" +
		"    - name: Say hi\n" +
		"      ansible.builtin.debug:\n" +
		"        msg: hi\n"

	// runOncePlaybook is otherwise clean, so run-once is the only rule that can
	// fire on it and the profile tests read unambiguously.
	runOncePlaybook = "---\n" +
		"- name: Example play\n" +
		"  hosts: localhost\n" +
		"  tasks:\n" +
		"    - name: Say hi\n" +
		"      ansible.builtin.debug:\n" +
		"        msg: hi\n" +
		"      run_once: true\n"
)

func fixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site-playbook.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// reported is the path a finding on p prints under. astl resolves symlinks the
// way ansible-lint does, and on macOS `t.TempDir()` hands back a path under
// /var, which is itself a link to /private/var. A test that expected the
// unresolved spelling would be pinning a divergence: given the same /var path,
// ansible-lint prints the /private/var one too.
func reported(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// runCLI runs the CLI and returns its exit code with both streams.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut strings.Builder
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunWithoutArgsPrintsUsage(t *testing.T) {
	code, stdout, stderr := runCLI(t)
	if code != exitError {
		t.Errorf("got exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "usage: astl") {
		t.Errorf("stderr = %q, want usage", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunVersion(t *testing.T) {
	code, stdout, _ := runCLI(t, "--version")
	if code != exitClean {
		t.Errorf("got exit %d, want %d", code, exitClean)
	}
	if !strings.HasPrefix(stdout, "astl ") {
		t.Errorf("stdout = %q, want a version line", stdout)
	}
}

func TestRunCleanFileExitsClean(t *testing.T) {
	code, stdout, stderr := runCLI(t, fixture(t, cleanPlaybook))
	if code != exitClean {
		t.Errorf("got exit %d (stderr %q), want %d", code, stderr, exitClean)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunViolationsExitTwo(t *testing.T) {
	path := fixture(t, dirtyPlaybook)
	for _, args := range [][]string{{path}, {"-f", "pep8", path}} {
		code, stdout, _ := runCLI(t, args...)
		if code != exitViolations {
			t.Errorf("%v: got exit %d, want %d", args, code, exitViolations)
		}
		if !strings.Contains(stdout, "name[play][/]: All plays should be named.") {
			t.Errorf("%v: stdout = %q, want a pep8 name finding", args, stdout)
		}
		if strings.Count(stdout, "\n") != 2 {
			t.Errorf("%v: stdout = %q, want two lines", args, stdout)
		}
	}
}

// TestRunDefaultPEP8OutputIsUnchanged pins the default output byte for byte.
// Adding `--ids` must not move a single character of it: pep8 compatibility with
// ansible-lint is the compatibility contract.
func TestRunDefaultPEP8OutputIsUnchanged(t *testing.T) {
	path := fixture(t, dirtyPlaybook)
	shown := reported(t, path)
	want := shown + ":2:3: name[play][/]: All plays should be named.\n" +
		shown + ":4: name[missing][/]: All tasks should be named.\n"
	for _, args := range [][]string{{path}, {"--ids", "upstream", path}} {
		code, stdout, stderr := runCLI(t, args...)
		if code != exitViolations {
			t.Errorf("%v: got exit %d (stderr %q), want %d", args, code, stderr, exitViolations)
		}
		if stdout != want {
			t.Errorf("%v: stdout = %q, want %q", args, stdout, want)
		}
	}
}

// TestRunNativeIDs pins that `--ids native` switches identifier and wording
// together, in both output formats.
func TestRunNativeIDs(t *testing.T) {
	path := fixture(t, dirtyPlaybook)
	shown := reported(t, path)
	want := shown + ":2:3: name.play-missing: This play has no name. Add one so logs can identify it.\n" +
		shown + ":4: name.task-missing: This task has no name. Add one so logs can identify it.\n"
	code, stdout, stderr := runCLI(t, "--ids", "native", path)
	if code != exitViolations {
		t.Fatalf("got exit %d (stderr %q), want %d", code, stderr, exitViolations)
	}
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	_, stdout, _ = runCLI(t, "--ids", "native", "-f", "sarif", path)
	if !strings.Contains(stdout, `"ruleId": "name.play-missing"`) {
		t.Errorf("sarif ruleId not in the native taxonomy: %s", stdout)
	}
	if !strings.Contains(stdout, `"text": "This play has no name. Add one so logs can identify it."`) {
		t.Errorf("sarif message not in the native vocabulary: %s", stdout)
	}
	if strings.Contains(stdout, "All plays should be named.") {
		t.Errorf("sarif still carries upstream wording: %s", stdout)
	}
}

// TestRunDefaultSARIFKeepsUpstreamMessages is the SARIF half of the default
// output contract: the native vocabulary must not leak into it.
func TestRunDefaultSARIFKeepsUpstreamMessages(t *testing.T) {
	path := fixture(t, dirtyPlaybook)
	for _, args := range [][]string{{"-f", "sarif", path}, {"--ids", "upstream", "-f", "sarif", path}} {
		_, stdout, _ := runCLI(t, args...)
		if !strings.Contains(stdout, `"text": "All plays should be named."`) {
			t.Errorf("%v: sarif lost the upstream wording: %s", args, stdout)
		}
		if strings.Contains(stdout, "Add one so logs can identify it.") {
			t.Errorf("%v: native wording leaked into the default output: %s", args, stdout)
		}
	}
}

func TestRunUnknownIDs(t *testing.T) {
	code, stdout, stderr := runCLI(t, "--ids", "cheese", fixture(t, cleanPlaybook))
	if code != exitError {
		t.Errorf("got exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, `unknown ids taxonomy "cheese"`) {
		t.Errorf("stderr = %q, want an unknown taxonomy message", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunSARIFFormat(t *testing.T) {
	path := fixture(t, dirtyPlaybook)
	for _, args := range [][]string{{"-f", "sarif", path}, {"--format", "sarif", path}} {
		code, stdout, _ := runCLI(t, args...)
		if code != exitViolations {
			t.Errorf("%v: got exit %d, want %d", args, code, exitViolations)
		}
		var doc struct {
			Version string `json:"version"`
			Runs    []struct {
				Results []struct {
					RuleID string `json:"ruleId"`
				} `json:"results"`
			} `json:"runs"`
		}
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
			t.Fatalf("%v: sarif output is not JSON: %v\n%s", args, err, stdout)
		}
		if doc.Version != "2.1.0" {
			t.Errorf("%v: version = %q, want 2.1.0", args, doc.Version)
		}
		if len(doc.Runs) != 1 || len(doc.Runs[0].Results) != 2 {
			t.Fatalf("%v: want one run with two results, got %+v", args, doc.Runs)
		}
		if doc.Runs[0].Results[0].RuleID != "name[play]" {
			t.Errorf("%v: ruleId = %q, want name[play]", args, doc.Runs[0].Results[0].RuleID)
		}
	}
}

func TestRunUnknownFormat(t *testing.T) {
	code, stdout, stderr := runCLI(t, "-f", "junit", fixture(t, cleanPlaybook))
	if code != exitError {
		t.Errorf("got exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, `unknown format "junit"`) {
		t.Errorf("stderr = %q, want an unknown format message", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunMissingPathIsFatal(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	code, stdout, stderr := runCLI(t, missing)
	if code != exitError {
		t.Errorf("got exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, missing) {
		t.Errorf("stderr = %q, want the offending path", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunUnreadableFileWarnsAndContinues(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site-playbook.yml"), []byte(dirtyPlaybook), 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "other-playbook.yml")
	if err := os.WriteFile(locked, []byte(cleanPlaybook), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })

	code, stdout, stderr := runCLI(t, dir)
	// Continuing past the unreadable file is the point of this test, and the
	// two assertions below are what prove it. The code is exitIncomplete
	// rather than exitViolations because one file went unchecked, which the
	// run reports in preference to the violations it did find.
	if code != exitIncomplete {
		t.Errorf("got exit %d, want %d", code, exitIncomplete)
	}
	if !strings.Contains(stderr, locked) {
		t.Errorf("stderr = %q, want the unreadable file reported", stderr)
	}
	if !strings.Contains(stdout, "name[play][/]") {
		t.Errorf("stdout = %q, want the readable file still linted", stdout)
	}
}

// A file astl cannot parse is a file astl did not check. Reporting nothing and
// exiting clean claimed the opposite, which let a malformed playbook pass a CI
// gate that a well-formed one with the same violation would have failed.
func TestUnparsableFileIsReportedAndNotClean(t *testing.T) {
	path := fixture(t, dirtyPlaybook+"  [[[ not yaml\n")

	code, stdout, stderr := runCLI(t, path)
	if code != exitIncomplete {
		t.Errorf("got exit %d (stderr %q), want %d", code, stderr, exitIncomplete)
	}
	if !strings.Contains(stderr, "not checked") {
		t.Errorf("stderr does not say the file went unchecked: %q", stderr)
	}
	// The report belongs on stderr: pep8 stdout is compared byte for byte
	// against ansible-lint, which reports this through a rule astl lacks.
	if strings.Contains(stdout, "not checked") {
		t.Errorf("the unchecked report leaked into pep8 stdout: %q", stdout)
	}
}

// An unreadable file is the same failure arriving by a different route.
func TestUnreadableFileIsNotClean(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0o000 file regardless of its mode")
	}
	path := fixture(t, cleanPlaybook)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	code, _, stderr := runCLI(t, path)
	if code != exitIncomplete {
		t.Errorf("got exit %d (stderr %q), want %d", code, stderr, exitIncomplete)
	}
}

// Incompleteness outranks violations: both are true, but the findings are on
// stdout to be read while only the exit code can carry "and there is more I
// could not look at".
func TestUnparsableFileOutranksViolations(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"site-playbook.yml":   dirtyPlaybook,
		"broken-playbook.yml": "- hosts: all\n  [[[ not yaml\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code, stdout, stderr := runCLI(t, dir)
	if code != exitIncomplete {
		t.Errorf("got exit %d (stderr %q), want %d", code, stderr, exitIncomplete)
	}
	if !strings.Contains(stdout, "name[play]") {
		t.Errorf("the parsable file's findings were dropped: %q", stdout)
	}
}

// Files that were never YAML are discovered on purpose and read as text where
// astl has rules for them, so failing to parse one as YAML says nothing about
// whether it was checked and must not make the run incomplete.
func TestNonYAMLKindsDoNotMakeTheRunIncomplete(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"some_filter.py": "def filters():\n    return {}\n",
		"tpl.j2":         "{{ not_yaml }}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code, _, stderr := runCLI(t, dir)
	if code != exitClean {
		t.Errorf("got exit %d (stderr %q), want %d", code, stderr, exitClean)
	}
	if strings.Contains(stderr, "not checked") {
		t.Errorf("a non-YAML file was reported as unchecked: %q", stderr)
	}
}

// The ceiling has to clear the live heap a run actually holds. Below it a soft
// limit cannot shrink anything and only makes the collector run continuously,
// which is what a fixed 128 MiB did on repositories of large files.
func TestMemoryLimitFor(t *testing.T) {
	items := func(n int, size int64) []discover.Item {
		out := make([]discover.Item, n)
		for i := range out {
			out[i] = discover.Item{Size: size}
		}
		return out
	}

	cases := []struct {
		name    string
		items   []discover.Item
		workers int
		want    int64
	}{
		{"no input keeps the floor", nil, 12, minMemoryLimit},
		{"ordinary repository keeps the floor", items(3000, 8<<10), 12, minMemoryLimit},
		{"one worker cannot raise it alone", items(10, 1<<20), 1, minMemoryLimit},
		{
			// 12 workers x 1 MiB is over the 4 MiB budget, so the budget caps
			// what is resident and sets the ceiling.
			"large files clear the floor",
			items(24, 1<<20), 12,
			int64(bytesInFlight) * memoryAmplification,
		},
		{
			// Under the budget, the ceiling follows the input instead.
			"medium files scale with the input",
			items(24, 256<<10), 12,
			12 * (256 << 10) * memoryAmplification,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := memoryLimitFor(c.items, c.workers); got != c.want {
				t.Errorf("got %d MiB, want %d MiB", got>>20, c.want>>20)
			}
		})
	}
}

// The ceiling must never drop below what an ordinary repository already ran
// under, or this becomes a memory regression for every normal user.
func TestMemoryLimitNeverBelowTheFloor(t *testing.T) {
	for _, size := range []int64{0, 1, 512, 4 << 10, 64 << 10, 8 << 20} {
		items := []discover.Item{{Size: size}, {Size: size}}
		if got := memoryLimitFor(items, 12); got < minMemoryLimit {
			t.Errorf("size %d: got %d, below the floor %d", size, got, int64(minMemoryLimit))
		}
	}
}

// configuredRepo writes a playbook and an .ansible-lint into a temp directory
// and makes it the working directory, because run() resolves configuration
// from ".". Returns the playbook's name relative to that directory.
func configuredRepo(t *testing.T, playbook, config string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site-playbook.yml"), []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ansible-lint"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return "site-playbook.yml"
}

// TestConfigIsFoundUnderConfigDir is issue 0007 end to end, on the shape that
// found it: dell/omnia keeps its policy at `.config/ansible-lint.yml` and no
// `.ansible-lint`, so astl applied none of it.
func TestConfigIsFoundUnderConfigDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site-playbook.yml"), []byte(dirtyPlaybook), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".config", "ansible-lint.yml"),
		[]byte("skip_list:\n  - name\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	code, stdout, stderr := runCLI(t, "site-playbook.yml")
	if stdout != "" {
		t.Errorf("stdout = %q, want the skip_list under .config/ to apply", stdout)
	}
	if code != exitClean {
		t.Errorf("got exit %d (stderr %q), want %d", code, stderr, exitClean)
	}
}

// TestConfigFlagOverridesTheSearch covers `-c`, which omnia's CI uses. The
// config sits outside the linted directory precisely so that finding it can
// only be the flag's doing.
func TestConfigFlagOverridesTheSearch(t *testing.T) {
	path := configuredRepo(t, dirtyPlaybook, "skip_list:\n  - name[task]\n")
	elsewhere := filepath.Join(t.TempDir(), "policy.yml")
	if err := os.WriteFile(elsewhere, []byte("skip_list:\n  - name\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "-c", elsewhere, path)
	if stdout != "" {
		t.Errorf("stdout = %q, want -c to replace the repository's own config, not merge with it", stdout)
	}
	if code != exitClean {
		t.Errorf("got exit %d, want %d", code, exitClean)
	}
}

// TestConfigFlagRejectsAMissingFile is the other half: a named policy that is
// not there must stop the run, not silently fall back to defaults.
func TestConfigFlagRejectsAMissingFile(t *testing.T) {
	path := fixture(t, dirtyPlaybook)

	code, _, stderr := runCLI(t, "-c", filepath.Join(t.TempDir(), "nowhere.yml"), path)
	if code != exitError {
		t.Errorf("got exit %d, want %d for a config file that does not exist", code, exitError)
	}
	if !strings.Contains(stderr, "nowhere.yml") {
		t.Errorf("stderr = %q, want the missing path named", stderr)
	}
}

// TestWarnListPrintsAndPasses is issue 0004 end to end, and the exit code is
// the half that matters: astl used to fail a build whose own CI was green.
// Upstream reports `Passed: 0 failure(s), 1 warning(s)` on this shape.
func TestWarnListPrintsAndPasses(t *testing.T) {
	path := configuredRepo(t, dirtyPlaybook, "warn_list:\n  - name\n")

	code, stdout, stderr := runCLI(t, path)
	if code != exitClean {
		t.Errorf("got exit %d (stderr %q), want %d: a warning must not fail the run", code, stderr, exitClean)
	}
	if !strings.Contains(stdout, "name[play][/]: All plays should be named. (warning)") {
		t.Errorf("stdout = %q, want the finding printed with pep8's warning suffix", stdout)
	}
}

// TestWarnListLeavesOtherRulesFailing pins that the demotion is per rule: one
// warned rule must not turn the whole run green.
func TestWarnListLeavesOtherRulesFailing(t *testing.T) {
	path := configuredRepo(t, dirtyPlaybook, "warn_list:\n  - name[play]\n")

	code, stdout, _ := runCLI(t, path)
	if code != exitViolations {
		t.Errorf("got exit %d, want %d: the un-warned finding still fails the run", code, exitViolations)
	}
	if strings.Count(stdout, "(warning)") != 1 {
		t.Errorf("stdout = %q, want exactly one demoted line", stdout)
	}
}

// TestProfileDropsRulesOutsideIt is issue 0005 end to end.
func TestProfileDropsRulesOutsideIt(t *testing.T) {
	path := configuredRepo(t, runOncePlaybook, "profile: production\n")

	code, stdout, stderr := runCLI(t, path)
	if strings.Contains(stdout, "run-once") {
		t.Errorf("stdout = %q, want run-once dropped: it belongs to no upstream profile", stdout)
	}
	if code != exitClean {
		t.Errorf("got exit %d (stderr %q), want %d", code, stderr, exitClean)
	}
}

func TestWithoutProfileRunOnceStillFires(t *testing.T) {
	path := configuredRepo(t, runOncePlaybook, "")

	code, stdout, _ := runCLI(t, path)
	if !strings.Contains(stdout, "run-once[task][/]") {
		t.Errorf("stdout = %q, want run-once with no profile set", stdout)
	}
	if code != exitViolations {
		t.Errorf("got exit %d, want %d", code, exitViolations)
	}
}

// TestUnknownProfileWarnsAndRunsEverything pins the safe failure direction: a
// profile name astl's table predates must not silently mute the linter.
func TestUnknownProfileWarnsAndRunsEverything(t *testing.T) {
	path := configuredRepo(t, runOncePlaybook, "profile: hardened\n")

	code, stdout, stderr := runCLI(t, path)
	if !strings.Contains(stdout, "run-once[task][/]") {
		t.Errorf("stdout = %q, want every rule to run under an unknown profile", stdout)
	}
	if !strings.Contains(stderr, "unknown profile") {
		t.Errorf("stderr = %q, want a warning naming the unknown profile", stderr)
	}
	if code != exitViolations {
		t.Errorf("got exit %d, want %d", code, exitViolations)
	}
}

// ignoreRepo writes a playbook and an `.ansible-lint-ignore` into a directory
// the test owns, then makes it the working directory: the ignore file is
// resolved from there and nowhere else, so a stray one on the developer's
// machine cannot reach the run.
func ignoreRepo(t *testing.T, playbook, ignore string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site-playbook.yml"), []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ansible-lint-ignore"), []byte(ignore), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return "site-playbook.yml"
}

// TestIgnoreFileSkipRemovesTheFinding is the reported case: an entry qualified
// `skip` must silence the finding completely, not merely demote it.
func TestIgnoreFileSkipRemovesTheFinding(t *testing.T) {
	path := ignoreRepo(t, dirtyPlaybook, "site-playbook.yml name[missing] skip\n")

	code, stdout, _ := runCLI(t, path)
	if strings.Contains(stdout, "name[missing]") {
		t.Errorf("stdout = %q, want no trace of the skipped rule", stdout)
	}
	if !strings.Contains(stdout, "name[play][/]") {
		t.Errorf("stdout = %q, want the rule that was not skipped", stdout)
	}
	if code != exitViolations {
		t.Errorf("got exit %d, want %d", code, exitViolations)
	}
}

// TestIgnoreFileBareEntryReportsAsAnIgnoredWarning pins the half that surprises:
// an entry without `skip` does not hide anything. The finding still prints, at
// warning level and ahead of the rest, and only stops failing the run.
//
// These bytes are not derived from reading upstream's source. They were taken
// from ansible-lint 26.8.0 run on the same fixture from the same directory: the
// ordering, the ` (warning)` suffix and the exit code all matched exactly. The
// corpus cannot cover this, because the harness lints from the repository root
// where one ignore file would apply to every case at once.
func TestIgnoreFileBareEntryReportsAsAnIgnoredWarning(t *testing.T) {
	path := ignoreRepo(t, dirtyPlaybook, "site-playbook.yml name[missing]\n")

	code, stdout, _ := runCLI(t, path)
	want := "site-playbook.yml:4: name[missing][/]: All tasks should be named. (warning)\n" +
		"site-playbook.yml:2:3: name[play][/]: All plays should be named.\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if code != exitViolations {
		t.Errorf("got exit %d, want %d", code, exitViolations)
	}
}

// TestIgnoreFileOnlyIgnoredFindingsExitsClean: an ignored finding is not a
// failure, which is what lets a repository adopt the linter before fixing
// everything it reports.
func TestIgnoreFileOnlyIgnoredFindingsExitsClean(t *testing.T) {
	path := ignoreRepo(t, dirtyPlaybook, "site-playbook.yml name[missing]\nsite-playbook.yml name[play]\n")

	code, stdout, _ := runCLI(t, path)
	if !strings.Contains(stdout, "(warning)") {
		t.Errorf("stdout = %q, want the findings still reported", stdout)
	}
	if code != exitClean {
		t.Errorf("got exit %d, want %d", code, exitClean)
	}
}

func TestIgnoreFileFromConfigKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site-playbook.yml"), []byte(dirtyPlaybook), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "named.txt"), []byte("site-playbook.yml name[missing] skip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ansible-lint"), []byte("ignore_file: named.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	code, stdout, _ := runCLI(t, "site-playbook.yml")
	if strings.Contains(stdout, "name[missing]") {
		t.Errorf("stdout = %q, want the config key to select the ignore file", stdout)
	}
	if code != exitViolations {
		t.Errorf("got exit %d, want %d", code, exitViolations)
	}
}

// TestIgnoreFileFlagWinsOverConfigKey: `-i` is the operator's decision made on
// the spot, so it outranks the file's own.
func TestIgnoreFileFlagWinsOverConfigKey(t *testing.T) {
	path := ignoreRepo(t, dirtyPlaybook, "site-playbook.yml name[missing] skip\n")
	if err := os.WriteFile(".ansible-lint", []byte("ignore_file: .ansible-lint-ignore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("flagged.txt", []byte("site-playbook.yml name[play] skip\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"-i", "flagged.txt", path}, {"--ignore-file", "flagged.txt", path}} {
		_, stdout, _ := runCLI(t, args...)
		if strings.Contains(stdout, "name[play]") {
			t.Errorf("%v: stdout = %q, want the flag's file to apply", args, stdout)
		}
		if !strings.Contains(stdout, "name[missing]") {
			t.Errorf("%v: stdout = %q, want the config key's file ignored", args, stdout)
		}
	}
}

// TestIgnoreFileMissingNamedFileWarnsAndContinues: upstream reports it and
// carries on. Unlike a missing config, this cannot lint under the wrong policy;
// it can only report findings the repository already knew about.
func TestIgnoreFileMissingNamedFileWarnsAndContinues(t *testing.T) {
	path := fixture(t, dirtyPlaybook)

	code, stdout, stderr := runCLI(t, "-i", "no-such-file.txt", path)
	if !strings.Contains(stderr, "ignore file not found") {
		t.Errorf("stderr = %q, want a warning naming the missing file", stderr)
	}
	if !strings.Contains(stdout, "name[missing][/]") {
		t.Errorf("stdout = %q, want the run to have continued", stdout)
	}
	if code != exitViolations {
		t.Errorf("got exit %d, want %d", code, exitViolations)
	}
}

func TestIgnoreFileUnreadableLineExitsError(t *testing.T) {
	path := ignoreRepo(t, dirtyPlaybook, "site-playbook.yml\n")

	code, _, stderr := runCLI(t, path)
	if !strings.Contains(stderr, "no rule id after the path") {
		t.Errorf("stderr = %q, want the parse failure explained", stderr)
	}
	if code != exitError {
		t.Errorf("got exit %d, want %d", code, exitError)
	}
}
