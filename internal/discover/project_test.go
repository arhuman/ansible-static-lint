package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// link creates a symlink at name, relative to dir, pointing at target as
// written, and skips the test where symlinks are unavailable.
func link(t *testing.T, dir, target, name string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// mkdir creates dir/name and its parents.
func mkdir(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(name)), 0o755); err != nil {
		t.Fatal(err)
	}
}

// skipUnderAmbientMarker skips a test that needs no project marker above dir,
// which a temporary directory inside a checkout would have.
func skipUnderAmbientMarker(t *testing.T, dir string) {
	t.Helper()
	for d := filepath.Dir(dir); ; d = filepath.Dir(d) {
		if _, ok := projectMarker(d, nil); ok {
			t.Skipf("%s marks a project above the test directory", d)
		}
		if filepath.Dir(d) == d {
			return
		}
	}
}

// walkPaths walks roots within project and returns the display path of each
// item.
func walkPaths(t *testing.T, project string, roots ...string) map[string]bool {
	t.Helper()
	items, soft, err := WalkWithin(project, roots, nil)
	if err != nil || len(soft) != 0 {
		t.Fatalf("err=%v soft=%v", err, soft)
	}
	out := map[string]bool{}
	for _, it := range items {
		out[it.Path] = true
	}
	return out
}

// TestProjectRootMarkers pins upstream's marker search: `.git` of any kind,
// `.hg` only as a directory, a config file name only as a regular file, and the
// nearest marked ancestor of the common base wins.
func TestProjectRootMarkers(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		dirs  []string
		want  string
	}{
		{"git directory", nil, []string{".git"}, "."},
		{"git file of a worktree", map[string]string{".git": "gitdir: x\n"}, nil, "."},
		{"hg directory", nil, []string{".hg"}, "."},
		{"hg file is no marker", map[string]string{"top/.hg": "x"}, []string{".git"}, "."},
		{"config file", map[string]string{"top/.ansible-lint.yml": "{}\n"}, []string{".git"}, "top"},
		{"config under .config", map[string]string{"top/.config/ansible-lint.yml": "{}\n"}, []string{".git"}, "top"},
		{"config directory is no marker", nil, []string{".git", "top/.ansible-lint"}, "."},
		{"nearest marker wins", nil, []string{".git", "top/.git"}, "top"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := resolvePath(t.TempDir())
			mkdir(t, dir, "top/sub")
			for _, d := range tc.dirs {
				mkdir(t, dir, d)
			}
			for name, body := range tc.files {
				path := filepath.Join(dir, filepath.FromSlash(name))
				mkdir(t, filepath.Dir(path), ".")
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := ProjectRoot([]string{filepath.Join(dir, "top", "sub")}, []string{".ansible-lint.yml", ".config/ansible-lint.yml", ".ansible-lint"})
			if want := filepath.Join(dir, tc.want); got != want {
				t.Fatalf("ProjectRoot = %q, want %q", got, want)
			}
		})
	}
}

// TestProjectRootAbsoluteConfigNamesItsDirectory pins the absolute entry, which
// is how upstream passes the config file it loaded: wherever the search stands,
// the root is the file's directory, or the parent of a `.config` directory.
func TestProjectRootAbsoluteConfigNamesItsDirectory(t *testing.T) {
	dir := resolvePath(t.TempDir())
	mkdir(t, dir, "proj/sub")
	mkdir(t, dir, "else/.config")
	for _, name := range []string{"else/policy.yml", "else/.config/ansible-lint.yml"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := []string{filepath.Join(dir, "proj", "sub")}
	if got, want := ProjectRoot(src, []string{filepath.Join(dir, "else", "policy.yml")}), filepath.Join(dir, "else"); got != want {
		t.Errorf("plain config: ProjectRoot = %q, want %q", got, want)
	}
	cfg := filepath.Join(dir, "else", ".config", "ansible-lint.yml")
	if got, want := ProjectRoot(src, []string{cfg}), filepath.Join(dir, "else"); got != want {
		t.Errorf(".config config: ProjectRoot = %q, want %q", got, want)
	}
}

// TestProjectRootFallsBackToTheCommonBase pins astl's divergence when nothing
// marks a project: upstream falls back to the filesystem root, which confines
// nothing, and astl to the deepest directory holding every path it was given,
// where a file stands for its directory.
func TestProjectRootFallsBackToTheCommonBase(t *testing.T) {
	dir := resolvePath(t.TempDir())
	skipUnderAmbientMarker(t, dir)
	mkdir(t, dir, "a")
	mkdir(t, dir, "b/c")
	file := filepath.Join(dir, "a", "x.yml")
	if err := os.WriteFile(file, []byte("---\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ProjectRoot([]string{file, filepath.Join(dir, "b", "c")}, nil); got != dir {
		t.Errorf("two sources: ProjectRoot = %q, want their common base %q", got, dir)
	}
	if got, want := ProjectRoot([]string{file}, nil), filepath.Join(dir, "a"); got != want {
		t.Errorf("one file: ProjectRoot = %q, want its directory %q", got, want)
	}
	if got := ProjectRoot(nil, nil); got != "" {
		t.Errorf("no source: ProjectRoot = %q, want empty", got)
	}
}

// TestWalkSkipsSymlinkOutsideProjectRoot is review reproduction R1: a link in
// the linted tree to a file outside the project is not read, while a link to a
// file inside it is linted under the target's path, as before.
func TestWalkSkipsSymlinkOutsideProjectRoot(t *testing.T) {
	dir := resolvePath(t.TempDir())
	mkdir(t, dir, "repo/.git")
	mkdir(t, dir, "repo/sub")
	mkdir(t, dir, "outside")
	for _, name := range []string{"repo/site.yml", "repo/sub/real.yml", "outside/private.yml"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte("---\n{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	repoDir := filepath.Join(dir, "repo")
	link(t, repoDir, "../outside/private.yml", "playbook.yml")
	link(t, repoDir, "sub/real.yml", "inner.yml")
	t.Chdir(repoDir)

	got := walkPaths(t, ProjectRoot([]string{"."}, nil), ".")
	if len(got) != 2 || !got["site.yml"] || !got["sub/real.yml"] {
		t.Fatalf("walked %v, want site.yml and sub/real.yml only", got)
	}
}

// TestWalkSkipsSymlinkToASiblingSharingThePrefix covers astl's component-wise
// containment: `proj-evil` starts with `proj` but is outside it, where
// upstream's string prefix test would admit it.
func TestWalkSkipsSymlinkToASiblingSharingThePrefix(t *testing.T) {
	dir := resolvePath(t.TempDir())
	mkdir(t, dir, "proj/.git")
	mkdir(t, dir, "proj-evil")
	if err := os.WriteFile(filepath.Join(dir, "proj-evil", "x.yml"), []byte("---\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(dir, "proj")
	link(t, proj, "../proj-evil/x.yml", "x.yml")

	if got := walkPaths(t, ProjectRoot([]string{proj}, nil), proj); len(got) != 0 {
		t.Fatalf("walked %v, want nothing", got)
	}
}

// TestWalkLintsAnExplicitFileOutsideProjectRoot pins upstream's exemption for
// explicit lintables: a file named as a root is linted wherever it lies.
func TestWalkLintsAnExplicitFileOutsideProjectRoot(t *testing.T) {
	dir := resolvePath(t.TempDir())
	mkdir(t, dir, "proj")
	mkdir(t, dir, "outside")
	file := filepath.Join(dir, "outside", "site.yml")
	if err := os.WriteFile(file, []byte("---\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := walkPaths(t, filepath.Join(dir, "proj"), file); len(got) != 1 {
		t.Fatalf("walked %v, want the explicit file", got)
	}
}

// TestExpandIncludesFollowsParentIncludeInsideProjectRoot is review
// reproduction R2: run from a subdirectory of a repository, an include of
// `../common.yml` stays inside the project and is followed, printed under its
// absolute path as upstream prints it.
func TestExpandIncludesFollowsParentIncludeInsideProjectRoot(t *testing.T) {
	root := repo(t, map[string]string{
		"sub/site.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Inc\n" +
			"      ansible.builtin.include_tasks: ../common.yml\n",
		"common.yml": childTasks,
	})
	mkdir(t, root, ".git")
	t.Chdir(filepath.Join(root, "sub"))

	project := ProjectRoot([]string{"."}, nil)
	items, _, err := WalkWithin(project, []string{"."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := expandIncludes(project, items, nil)
	want := filepath.Join(root, "common.yml")
	if len(got) != 1 || got[0].Abs != want || got[0].Path != filepath.ToSlash(want) || got[0].Kind != parse.KindTasks {
		t.Fatalf("expanded %+v, want %s as tasks under its absolute path", got, want)
	}
}
