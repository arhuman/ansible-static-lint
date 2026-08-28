// Package discover walks input paths and assigns each file an Ansible "kind"
// (playbook, tasks, meta, galaxy, ...) mirroring ansible-lint's ordered kind table.
package discover

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arhuman/ansible-static-lint/internal/yamllint"
)

// Kind names used by the rules. Kinds outside this set are still assigned but
// carry no MVP rule.
const (
	KindPlaybook    = "playbook"
	KindTasks       = "tasks"
	KindHandlers    = "handlers"
	KindMeta        = "meta"
	KindMetaRuntime = "meta-runtime"
	KindGalaxy      = "galaxy"
	KindRole        = "role"
	KindYAML        = "yaml"
)

type kindRule struct {
	kind    string
	pattern string
}

// kindTable mirrors ansible-lint's config.DEFAULT_KINDS. Order matters: the
// first matching pattern wins.
var kindTable = []kindRule{
	{"jinja2", "**/*.j2"},
	{"jinja2", "**/*.j2.*"},
	{"yaml", ".github/**/*.{yaml,yml}"},
	{"text", "**/templates/**/*.*"},
	{"execution-environment", "**/execution-environment.yml"},
	{"ansible-lint-config", "**/.ansible-lint"},
	{"ansible-lint-config", "**/.ansible-lint.{yaml,yml}"},
	{"ansible-lint-config", "**/.config/ansible-lint.{yaml,yml}"},
	{"ansible-navigator-config", "**/ansible-navigator.{yaml,yml}"},
	{"inventory", "**/inventory/**.{yaml,yml}"},
	{"requirements", "**/meta/requirements.{yaml,yml}"},
	{KindGalaxy, "**/galaxy.yml"},
	{"reno", "**/releasenotes/*/*.{yaml,yml}"},
	{"vars", "**/{host_vars,group_vars,vars,defaults}/**/*.{yaml,yml}"},
	{KindTasks, "**/tasks/**/*.{yaml,yml}"},
	{"rulebook", "**/rulebooks/*.{yml,yaml}"},
	{"play-argspec", "**/*.meta.{yaml,yml}"},
	{KindPlaybook, "**/playbooks/*.{yml,yaml}"},
	{KindPlaybook, "**/*playbook*.{yml,yaml}"},
	{KindPlaybook, "**/extensions/patterns/*/playbooks/*.{yml,yaml}"},
	{KindHandlers, "**/handlers/*.{yaml,yml}"},
	{"test-meta", "**/tests/integration/targets/*/meta/main.{yaml,yml}"},
	{KindMeta, "**/meta/main.{yaml,yml}"},
	{KindMetaRuntime, "**/meta/runtime.{yaml,yml}"},
	{"role-arg-spec", "**/meta/argument_specs.{yaml,yml}"},
	{"yaml", ".config/molecule/config.{yaml,yml}"},
	{"requirements", "**/molecule/*/{collections,requirements}.{yaml,yml}"},
	{"yaml", "**/molecule/*/{base,molecule}.{yaml,yml}"},
	{"requirements", "**/requirements.{yaml,yml}"},
	{KindPlaybook, "**/molecule/*/*.{yaml,yml}"},
	{"yaml", "**/{.ansible-lint,.yamllint}"},
	{"changelog", "**/changelogs/changelog.{yaml,yml}"},
	{"yaml", "**/*.{yaml,yml}"},
	{"yaml", "**/.*.{yaml,yml}"},
	{"sanity-ignore-file", "**/tests/sanity/ignore-*.txt"},
	{"python", "**/*.py"},
}

// nonYAMLKinds are the kinds in kindTable whose files are not YAML documents.
// They are discovered because astl has text-level rules for some of them (a
// sanity ignore list) and because upstream's kind map names them, so failing to
// parse one as YAML is the expected outcome rather than a file left unchecked.
var nonYAMLKinds = map[string]bool{
	"jinja2":             true,
	"text":               true,
	"python":             true,
	"sanity-ignore-file": true,
}

// IsYAMLKind reports whether kind names files astl reads as YAML documents.
// Callers use it to tell a parse failure that hid a file from the rules from
// one that was never going to parse in the first place. An unknown kind counts
// as YAML: kindTable is the only source of kinds, and every other entry in it
// is a YAML document.
func IsYAMLKind(kind string) bool {
	return !nonYAMLKinds[kind]
}

// KindOf returns the kind for a slash-separated path, or "" when the path
// matches no known kind. Patterns are `**/`-anchored, so absolute and
// relative paths both work.
func KindOf(relPath string) string {
	for _, r := range kindTable {
		if matchGlob(r.pattern, relPath) {
			return r.kind
		}
	}
	return ""
}

// Item is one discovered lintable: a file, or a role directory.
type Item struct {
	// Path is the path as it should appear in output, relative to the working
	// directory when possible.
	Path string
	// Abs is the absolute path on disk.
	Abs string
	// Kind is the ansible-lint kind.
	Kind string
	// Size is the file's size in bytes, or 0 when it could not be determined
	// or the item is a role directory. Callers use it to budget how much they
	// read at once; peak memory tracks bytes in flight, not files in flight.
	Size int64
}

// roleMarkers are the subdirectories that make a directory look like a role.
var roleMarkers = []string{"tasks", "meta", "defaults", "handlers", "vars", "templates", "files"}

func looksLikeRole(dir string) bool {
	for _, m := range roleMarkers {
		if fi, err := os.Stat(filepath.Join(dir, m)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// Walk collects lintables under the given roots. excluded is a list of
// gitignore-style patterns (ansible-lint hands exclude_paths to pathspec's
// GitIgnoreSpec, issue 0013): an unanchored name matches at any depth, a
// slash anchors to the working directory, `**` spans segments.
//
// A root that cannot be stat'ed is fatal and comes back as the error return.
// Failures on individual entries below a root are collected in soft and the
// traversal continues, so one unreadable directory does not lose the rest of
// the run; callers are expected to report them as warnings.
func Walk(roots []string, excluded []string) (items []Item, soft []error, err error) {
	w := &walk{wd: WorkingDir(), excluded: yamllint.ParsePathSpec(excluded), seen: map[string]bool{}}

	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, w.soft, fmt.Errorf("discover: %s: %w", root, err)
		}
		absRoot = resolvePath(absRoot)
		if _, err := os.Stat(absRoot); err != nil {
			return nil, w.soft, fmt.Errorf("discover: %w", err)
		}
		if err := filepath.WalkDir(absRoot, w.visit); err != nil {
			return nil, w.soft, fmt.Errorf("discover: %w", err)
		}
	}
	sort.Slice(w.items, func(i, j int) bool { return w.items[i].Path < w.items[j].Path })
	return w.items, w.soft, nil
}

// walk holds the state the traversal callback accumulates, so that visiting an
// entry stays a named method instead of a closure over six locals. wd is
// resolved once per Walk because rendering a display path needs it for every
// entry.
type walk struct {
	wd       string
	excluded *yamllint.PathSpec
	seen     map[string]bool
	items    []Item
	soft     []error
}

// visit is the filepath.WalkDir callback.
func (w *walk) visit(p string, d fs.DirEntry, err error) error {
	if err != nil {
		w.soft = append(w.soft, err)
		return nil
	}
	if w.excluded.Match(displayPath(w.wd, p)) {
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	}
	if d.IsDir() {
		return w.visitDir(p, d.Name())
	}
	target, ok := resolveLink(p, d)
	if !ok {
		return nil
	}
	// Kind patterns are `**/`-anchored, so they match the full path regardless
	// of which root the walk started from. They are matched against the
	// resolved path because that is the one upstream classifies: a symlink
	// `tasks/main.yml -> ../vars/data.yml` is a vars file to ansible-lint, not
	// a tasks file.
	kind := KindOf(filepath.ToSlash(target))
	if kind == "" {
		return nil
	}
	size, ok := regularSize(target, d)
	if !ok {
		return nil
	}
	w.add(target, kind, size)
	return nil
}

// resolveLink returns the path a lintable entry really refers to, following
// symlinks, and false when there is nothing to lint at the other end.
//
// ansible-lint resolves every path it takes in (`Lintable.__init__` calls
// `Path.resolve()` before anything else reads it), so the resolved path is what
// its findings print, what its kind globs match, and what a rule sees when it
// looks at sibling files. Keeping the link's own path instead makes astl
// disagree in all three places at once. On debops that showed up as two false
// positives: `ansible/galaxy.yml` is a link into the collection tree, where the
// changelog and `meta/runtime.yml` the galaxy rules look for do exist, while
// nothing of the sort sits next to the link.
//
// Resolving also collapses a link and its target to one entry, since w.add
// deduplicates on the path, which is again what upstream does.
func resolveLink(p string, d fs.DirEntry) (string, bool) {
	if d.Type()&fs.ModeSymlink == 0 {
		return p, true
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		// A broken link, or one whose chain does not terminate. There is no
		// content to lint and no path to report it against.
		return "", false
	}
	return resolved, true
}

// regularSize returns the size of the file p will be read as, and false when it
// is not a regular file and so is not lintable at all.
//
// The size is read here rather than in the workers because the walk already
// holds the directory entry, and it is what sizes the memory budget: the
// workers admit a file by the bytes it declares before paying to read them.
//
// Which means the entry has to describe the thing that will actually be read.
// WalkDir does not follow symlinks, so d.Info() lstats the link and reports the
// length of its target's path, while the read follows it. A repository could
// ship `playbook.yml -> /dev/zero` and be admitted as a 9 byte file. Resolving
// the link fixes both halves: the budget sees the real size, and a link to
// something that is not a file is dropped here rather than read.
//
// Dropping it also matches upstream, which collects a path only when
// Path.is_file() holds, and that follows the link too. A symlink to an ordinary
// file stays lintable, as it is for ansible-lint.
func regularSize(p string, d fs.DirEntry) (int64, bool) {
	if d.Type().IsRegular() {
		info, err := d.Info()
		if err != nil {
			// Not worth failing a run over: it only costs a less precise
			// budget, and the read is bounded independently.
			return 0, true
		}
		return info.Size(), true
	}
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	return info.Size(), true
}

func (w *walk) visitDir(p, name string) error {
	if name == ".git" || name == "__pycache__" {
		return fs.SkipDir
	}
	if isRoleDir(p) {
		// A role is a directory, so it contributes no source bytes of its own:
		// the files inside it are discovered and sized separately.
		w.add(p, KindRole, 0)
	}
	return nil
}

func (w *walk) add(abs, kind string, size int64) {
	if kind == "" || w.seen[abs] {
		return
	}
	w.seen[abs] = true
	w.items = append(w.items, Item{
		Path: displayPath(w.wd, abs), Abs: abs, Kind: kind, Size: size,
	})
}

// roleSubdirsStrict is upstream's ROLE_SUBDIRS. It is narrower than
// roleMarkers on purpose: templates/ and files/ make a directory look like a
// role for the namespace test, but do not qualify it as a role root.
var roleSubdirsStrict = []string{"tasks", "meta", "vars", "defaults", "handlers"}

func hasRoleSubdirs(dir string) bool {
	for _, sub := range roleSubdirsStrict {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// isRoleDir mirrors ansible-lint's role discovery: a direct child of a
// `roles/` directory, or a grandchild when the intermediate directory is a
// namespace-style folder rather than a role itself. Either way the candidate
// must carry a canonical role subdirectory (upstream's _has_role_subdirs
// guard, their #5079): a container directory holding only nested sub-roles is
// not a role (astl issue 0011).
func isRoleDir(dir string) bool {
	if !hasRoleSubdirs(dir) {
		return false
	}
	parent := filepath.Base(filepath.Dir(dir))
	if parent == "roles" {
		return true
	}
	grand := filepath.Base(filepath.Dir(filepath.Dir(dir)))
	return grand == "roles" && !looksLikeRole(filepath.Dir(dir))
}

// WorkingDir returns the base Walk renders its relative display paths against:
// the process working directory with every symlink component resolved, or ""
// when it cannot be read, in which case Walk prints absolute paths.
//
// It is exported so a report that carries relative paths can also declare the
// directory they are relative to, and be guaranteed to declare the same one
// Walk used.
//
// The working directory is resolved for the same reason the entries are: a
// resolved symlink target has to be comparable to it, or a file inside the tree
// prints as an absolute path. It is not hypothetical. macOS makes /tmp and /var
// symlinks, so any run under a temporary directory has an unresolved working
// directory unless this happens.
func WorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return resolvePath(wd)
}

// resolvePath returns path with every symlink component resolved, or path
// itself when it cannot be resolved. A path that does not exist yet is not an
// error here: the caller's own Stat reports that, with a better message.
func resolvePath(path string) string {
	if path == "" {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// displayPath renders an absolute path relative to wd when it sits underneath
// it, matching ansible-lint's relative output paths. An empty wd yields the
// absolute path.
func displayPath(wd, abs string) string {
	if wd == "" {
		return filepath.ToSlash(abs)
	}
	rel, err := filepath.Rel(wd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}
