package discover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		if got := matchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
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
	items, soft, err := Walk([]string{file}, nil)
	if err != nil || len(soft) != 0 {
		t.Fatalf("err=%v soft=%v", err, soft)
	}
	if len(items) != 1 || items[0].Kind != KindPlaybook {
		t.Fatalf("got %+v, want one playbook", items)
	}
}

func TestWalkRejectsMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	items, _, err := Walk([]string{missing}, nil)
	if err == nil {
		t.Fatalf("got %+v and no error, want an error", items)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
}

func TestWalkReportsUnreadableDirAndContinues(t *testing.T) {
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

	items, soft, err := Walk([]string{dir}, nil)
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
	items, _, err := Walk([]string{dir}, []string{"vendor"})
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

	items, _, err := Walk([]string{link}, nil)
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

	items, _, err := Walk([]string{dir}, nil)
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
	items, _, err := Walk([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("got %+v, want nothing", items)
	}
}

// TestWalkReportsSymlinksUnderTheirTargetPath is issue 0002's regression test.
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

	items, _, err := Walk([]string{dir}, nil)
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

	items, _, err := Walk([]string{dir}, nil)
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
// #5079, astl issue 0011): a directory under roles/ that carries none of the
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
	items, _, err := Walk([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]bool{}
	for _, it := range items {
		if it.Kind == KindRole {
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
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	items, _, err := Walk([]string{dir}, []string{"molecule/**/prepare.yml", "venv"})
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
