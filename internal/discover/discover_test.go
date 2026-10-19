package discover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

func TestKindOf(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"playbooks/example.yml", "playbook"},
		{"site-playbook.yml", "playbook"},
		{"roles/foo/tasks/main.yml", "tasks"},
		{"roles/foo/tasks/sub/other.yaml", "tasks"},
		{"roles/foo/handlers/main.yml", "handlers"},
		{"roles/foo/meta/main.yml", "meta"},
		{"meta/runtime.yml", "meta-runtime"},
		{"galaxy.yml", "galaxy"},
		{"group_vars/all.yml", "vars"},
		{"tests/integration/targets/x/meta/main.yml", "test-meta"},
		{"whatever.yml", "yaml"},
		{"templates/foo.yml", "text"},
		{"README.md", ""},
	}
	for _, tc := range tests {
		if got := KindOf(tc.path); got != tc.want {
			t.Errorf("KindOf(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// walkUnconfigured walks roots confined to the root a run without a
// configuration file finds for them.
func walkUnconfigured(roots, excluded []string) ([]Item, []error, error) {
	return WalkWithin(ProjectRoot(roots, nil), roots, excluded)
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.{yml,yaml}", "a/b/c.yaml", true},
		{"**/*.{yml,yaml}", "a/b/c.txt", false},
		{"**/tasks/**/*.yml", "tasks/main.yml", true},
		{"**/tasks/**/*.yml", "roles/r/tasks/deep/x.yml", true},
		{"**/*playbook*.yml", "my-playbook-2.yml", true},
		{"**/meta/main.yml", "meta/main.yml", true},
		{"**/meta/main.yml", "meta/other.yml", false},
	}
	for _, tc := range tests {
		if got := compileGlob(tc.pattern).match(tc.path); got != tc.want {
			t.Errorf("compileGlob(%q).match(%q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestWalkSingleFileKeepsItsKind(t *testing.T) {
	dir := t.TempDir()
	playbooks := filepath.Join(dir, "playbooks")
	if err := os.MkdirAll(playbooks, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(playbooks, "p.yml")
	if err := os.WriteFile(file, []byte("---\n[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, soft, err := walkUnconfigured([]string{file}, nil)
	if err != nil || len(soft) != 0 {
		t.Fatalf("err=%v soft=%v", err, soft)
	}
	if len(items) != 1 || items[0].Kind != parse.KindPlaybook {
		t.Fatalf("got %+v, want one playbook", items)
	}
}

func TestWalkRejectsMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	items, _, err := walkUnconfigured([]string{missing}, nil)
	if err == nil {
		t.Fatalf("got %+v and no error, want an error", items)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
}

func TestWalkReportsUnreadableDirAndContinues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod cannot make a directory unreadable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.yml"), []byte("---\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	items, soft, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatalf("unreadable subdirectory must not fail the walk: %v", err)
	}
	if len(soft) == 0 {
		t.Fatal("want the unreadable directory reported as a soft error")
	}
	if len(items) != 1 {
		t.Fatalf("got %+v, want the readable file", items)
	}
}

func TestWalkHonoursExcludePaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x.yml"), []byte("---\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, _, err := walkUnconfigured([]string{dir}, []string{"vendor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("got %+v, want nothing", items)
	}
}

// TestWalkSizesSymlinksByTheirTarget pins the property that makes the size
// safe to budget with: the workers admit a file by the bytes it declares
// before paying to read them, so the entry has to describe what will actually
// be read. WalkDir does not follow symlinks, so the unresolved size was the
// length of the target's path, not of the file.
func TestWalkSizesSymlinksByTheirTarget(t *testing.T) {
	dir := t.TempDir()
	body := []byte("---\n" + strings.Repeat("# padding\n", 64))
	target := filepath.Join(dir, "target.yml")
	if err := os.WriteFile(target, body, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	items, _, err := walkUnconfigured([]string{link}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %+v, want the symlink collected: a link to an ordinary "+
			"file is lintable for ansible-lint and must stay so here", items)
	}
	if got, want := items[0].Size, int64(len(body)); got != want {
		t.Errorf("Size = %d, want %d: the budget is sized from the link, not its target", got, want)
	}
}

// TestWalkSkipsIrregularFiles is the discovery half of the unbounded-read
// defect. git stores symlinks natively, so a checkout can carry
// `evil-playbook.yml -> /dev/zero`; collecting it admitted a file whose read
// does not end. Upstream collects a path only when Path.is_file() holds, which
// follows the link too, so dropping it is also what parity requires.
func TestWalkSkipsIrregularFiles(t *testing.T) {
	const dev = "/dev/zero"
	if fi, err := os.Stat(dev); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device on this platform", dev)
	}
	dir := t.TempDir()
	if err := os.Symlink(dev, filepath.Join(dir, "evil-playbook.yml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.yml"), []byte("---\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, _, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %+v, want only ok.yml", items)
	}
	if filepath.Base(items[0].Abs) != "ok.yml" {
		t.Errorf("collected %s, want ok.yml", items[0].Abs)
	}
}

// TestWalkSkipsDanglingSymlink covers the ordinary case that shares the branch:
// a link to nothing is not a file, and reporting it as unchecked would be noise
// about a path upstream does not collect either.
func TestWalkSkipsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling.yml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	items, _, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("got %+v, want nothing", items)
	}
}

// TestWalkReportsSymlinksUnderTheirTargetPath is a regression test.
// ansible-lint resolves every path in Lintable.__init__ before anything reads
// it, so the resolved path is what its findings print, what its kind globs
// match, and what a rule sees when it looks at sibling files. debops makes all
// three matter at once: `ansible/galaxy.yml` is a link into the collection
// tree, where the changelog and meta/runtime.yml the galaxy rules look for do
// exist, while nothing of the sort sits next to the link.
func TestWalkReportsSymlinksUnderTheirTargetPath(t *testing.T) {
	dir := t.TempDir()
	collDir := filepath.Join(dir, "collection")
	if err := os.MkdirAll(collDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(collDir, "galaxy.yml")
	if err := os.WriteFile(target, []byte("---\nname: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "galaxy.yml")
	if err := os.Symlink("collection/galaxy.yml", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	items, _, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The link and its target are one file, so they collapse to one item, as
	// they do for ansible-lint.
	if len(items) != 1 {
		t.Fatalf("got %d items %+v, want the link and its target deduplicated to one", len(items), items)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Abs != resolved {
		t.Errorf("Abs = %q, want the resolved target %q: a rule looking at sibling "+
			"files must see the target's directory", items[0].Abs, resolved)
	}
	if !strings.HasSuffix(items[0].Path, "collection/galaxy.yml") {
		t.Errorf("Path = %q, want it to print under the target's path", items[0].Path)
	}
}

// The kind follows the target too, not the link's own name: ansible-lint guesses
// it from the already-resolved path, so `tasks/main.yml -> ../vars/data.yml` is
// a vars file to it, not a tasks file.
func TestWalkKindFollowsTheSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"vars", "tasks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(dir, "vars", "data.yml")
	if err := os.WriteFile(target, []byte("---\nsome_var: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../vars/data.yml", filepath.Join(dir, "tasks", "main.yml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	items, _, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items %+v, want one", len(items), items)
	}
	if items[0].Kind != "vars" {
		t.Errorf("Kind = %q, want %q: the glob matches the resolved path, not the link name",
			items[0].Kind, "vars")
	}
}

// TestWalkRoleNeedsARoleSubdir pins upstream's _has_role_subdirs guard (their
// #5079): a directory under roles/ that carries none of the
// five canonical role subdirectories is not a role, even when it holds nested
// sub-roles, and those sub-roles are the role items instead. Found on
// kubespray's roles/remove-node, a pure container for three sub-roles.
func TestWalkRoleNeedsARoleSubdir(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"roles/real/tasks",
		"roles/container/sub-role/tasks",
		"roles/only-files/files",
	} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]bool{}
	for _, it := range items {
		if it.Kind == parse.KindRole {
			roles[filepath.Base(it.Abs)] = true
		}
	}
	for _, want := range []string{"real", "sub-role"} {
		if !roles[want] {
			t.Errorf("role %q not discovered: %v", want, roles)
		}
	}
	for _, not := range []string{"container", "only-files"} {
		if roles[not] {
			t.Errorf("%q discovered as a role, want skipped", not)
		}
	}
}

// TestWalkExcludeGlobs pins gitignore semantics for exclude_paths (issue
// 0013, found on k3s-ansible): a `**` glob excludes at depth, and a plain
// entry is a path name, not a substring, so it no longer over-matches files
// that merely contain it.
func TestWalkExcludeGlobs(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"molecule/ipv6", "molecule/default"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"molecule/ipv6/prepare.yml", "molecule/default/prepare.yml", "molecule/ipv6/verify.yml", "venv.yml"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("---\n{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	items, _, err := walkUnconfigured([]string{dir}, []string{"molecule/**/prepare.yml", "venv"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[filepath.ToSlash(it.Path)] = true
	}
	for _, want := range []string{"molecule/ipv6/verify.yml", "venv.yml"} {
		if !got[want] {
			t.Errorf("%s missing: %v", want, got)
		}
	}
	for _, not := range []string{"molecule/ipv6/prepare.yml", "molecule/default/prepare.yml"} {
		if got[not] {
			t.Errorf("%s not excluded", not)
		}
	}
}

// writeTree creates every named file under dir, with its parent directories,
// so an exclusion test reads as the tree it is about.
func writeTree(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\n{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// collected returns the discovered display paths as a set.
func collected(t *testing.T, roots []string, excluded []string) map[string]bool {
	t.Helper()
	items, _, err := walkUnconfigured(roots, excluded)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[filepath.ToSlash(it.Path)] = true
	}
	return got
}

func wantCollected(t *testing.T, got map[string]bool, want, notWant []string) {
	t.Helper()
	for _, p := range want {
		if !got[p] {
			t.Errorf("%s missing from %v", p, got)
		}
	}
	for _, p := range notWant {
		if got[p] {
			t.Errorf("%s collected, want it excluded", p)
		}
	}
}

// TestWalkBuiltinExcludes pins ansible-lint's own exclude list, which astl
// used to cover with two hardcoded directory names. It also pins issue #5's
// decision: upstream has no `.venv` entry, so a virtualenv inside a linted
// repository is still linted and astl must not invent a special case.
func TestWalkBuiltinExcludes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir,
		".tox/x.yml", ".git/x.yml", ".ansible/x.yml", "__pycache__/x.yml",
		".pytest_cache/x.yml", ".venv/x.yml", "ok.yml")

	got := collected(t, []string{dir}, nil)
	wantCollected(t, got,
		[]string{"ok.yml", ".venv/x.yml"},
		[]string{".tox/x.yml", ".git/x.yml", ".ansible/x.yml", "__pycache__/x.yml", ".pytest_cache/x.yml"})
}

// A `.git` worktree or submodule pointer is a file, not a directory, and the
// builtin patterns cover it either way now that the check is a pathspec rather
// than a directory-name comparison.
func TestWalkBuiltinExcludesAGitFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, "ok.yml")
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ../elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := collected(t, []string{dir}, nil); !got["ok.yml"] || len(got) != 1 {
		t.Errorf("got %v, want only ok.yml", got)
	}
}

// A user exclude_paths entry and the builtins compile to one merged spec, as
// they do upstream, so a user negation re-includes what a builtin excluded.
func TestWalkUserExcludeNegatesABuiltin(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, ".tox/x.yml", "ok.yml")

	got := collected(t, []string{dir}, []string{"!.tox"})
	wantCollected(t, got, []string{"ok.yml", ".tox/x.yml"}, nil)
}

// TestWalkRootGitignore covers the three shapes a real `.gitignore` uses: a
// plain name, a dir-only pattern that has to match the directory entry itself,
// and a `!` re-inclusion. `generated/x.yml` stays collected, verified against
// upstream's get_all_files on the same tree: an anchored pattern is tested
// against the root's immediate children, and `generated/` is not one of the
// paths it spells, so the recursion enters the directory with a fresh spec.
func TestWalkRootGitignore(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, "venv/x.yml", "build/x.yml", "generated/x.yml", "keep.yml", "drop.yml", "ok.yml")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"),
		[]byte("venv\nbuild/\n*.yml\n!keep.yml\n!ok.yml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := collected(t, []string{dir}, nil)
	wantCollected(t, got,
		[]string{"ok.yml", "keep.yml", "generated/x.yml"},
		[]string{"venv/x.yml", "build/x.yml", "drop.yml"})
}

// A read error on an existing `.gitignore` is a warning, not a failure: the
// repository chose the file, and refusing to lint over it would be worse.
func TestWalkUnreadableGitignoreWarns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod cannot make a file unreadable on Windows, only read-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, "ok.yml")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ok.yml\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	items, soft, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatalf("an unreadable .gitignore must not fail the walk: %v", err)
	}
	if len(soft) == 0 {
		t.Error("want the unreadable .gitignore reported as a soft error")
	}
	if len(items) != 1 {
		t.Errorf("got %+v, want ok.yml linted when its ignore file could not be read", items)
	}
}

// TestWalkNestedGitignoreIsShallow reproduces an upstream quirk rather than
// git's behavior: get_all_files rebuilds its pathspec list at every level of
// the recursion, so a directory's `.gitignore` governs that directory's
// immediate children and nothing deeper.
func TestWalkNestedGitignoreIsShallow(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, "sub/drop.yml", "sub/deeper/drop.yml")
	if err := os.WriteFile(filepath.Join(dir, "sub", ".gitignore"), []byte("drop.yml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := collected(t, []string{dir}, nil)
	wantCollected(t, got, []string{"sub/deeper/drop.yml"}, []string{"sub/drop.yml"})
}

// TestWalkAnchoredGitignoreQuirk pins the second half of the same quirk:
// candidates are matched against the path accumulated from the walk root, so an
// anchored pattern in a nested `.gitignore` is anchored to the root and can
// never match. git would exclude sub/build here; ansible-lint does not.
func TestWalkAnchoredGitignoreQuirk(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, "sub/build/x.yml")
	if err := os.WriteFile(filepath.Join(dir, "sub", ".gitignore"), []byte("/build\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := collected(t, []string{dir}, nil)
	wantCollected(t, got, []string{"sub/build/x.yml"}, nil)
}

// A path passed on the command line is exempt from every exclusion: upstream
// checks the items it iterates over, never the path it was handed.
func TestWalkExplicitFileRootBypassesExcludes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, ".tox/x.yml")

	got := collected(t, []string{filepath.Join(dir, ".tox", "x.yml")}, nil)
	wantCollected(t, got, []string{".tox/x.yml"}, nil)
}

// The exemption is narrower for a directory root: only the directory itself
// escapes the check, and its descendants are still tested against the full
// path, so `astl .tox` collects nothing while `astl .tox/x.yml` collects the
// file. Verified against upstream's get_all_files on the same tree.
func TestWalkExplicitDirRootStillExcludesItsDescendants(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, ".tox/x.yml", "excluded-dir/ok.yml")

	if got := collected(t, []string{filepath.Join(dir, ".tox")}, nil); len(got) != 0 {
		t.Errorf("got %v, want nothing: a builtin pattern still matches the descendants", got)
	}
	if got := collected(t, []string{filepath.Join(dir, "excluded-dir")}, []string{"excluded-dir"}); len(got) != 0 {
		t.Errorf("got %v, want nothing: a user pattern reaches the descendants too", got)
	}
	got := collected(t, []string{filepath.Join(dir, "excluded-dir")}, []string{"nothing"})
	wantCollected(t, got, []string{"excluded-dir/ok.yml"}, nil)
}

// A repository chooses its own `.gitignore`, content included, and git stores
// symlinks natively, so a checkout can carry `.gitignore -> /dev/zero`. The
// read is bounded and regular-file only for the same reason every other
// configuration read in astl is.
func TestWalkGitignoreReadIsBounded(t *testing.T) {
	const dev = "/dev/zero"
	if fi, err := os.Stat(dev); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device on this platform", dev)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	writeTree(t, dir, "ok.yml")
	if err := os.Symlink(dev, filepath.Join(dir, ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	items, soft, err := walkUnconfigured([]string{dir}, nil)
	if err != nil {
		t.Fatalf("a non-regular .gitignore must not fail the walk: %v", err)
	}
	if len(soft) == 0 {
		t.Error("want the refused .gitignore reported as a soft error")
	}
	if len(items) != 1 {
		t.Errorf("got %+v, want ok.yml still linted", items)
	}
}
