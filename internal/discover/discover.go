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

	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/safeio"
	"github.com/arhuman/ansible-static-lint/internal/yamllint"
)

type kindRule struct {
	kind    string
	pattern string
}

// kindTable mirrors ansible-lint's config.DEFAULT_KINDS. Order matters: the
// first matching pattern wins.
var kindTable = []kindRule{
	{parse.KindJinja2, "**/*.j2"},
	{parse.KindJinja2, "**/*.j2.*"},
	{parse.KindYAML, ".github/**/*.{yaml,yml}"},
	{parse.KindText, "**/templates/**/*.*"},
	{"execution-environment", "**/execution-environment.yml"},
	{"ansible-lint-config", "**/.ansible-lint"},
	{"ansible-lint-config", "**/.ansible-lint.{yaml,yml}"},
	{"ansible-lint-config", "**/.config/ansible-lint.{yaml,yml}"},
	{"ansible-navigator-config", "**/ansible-navigator.{yaml,yml}"},
	{"inventory", "**/inventory/**.{yaml,yml}"},
	{parse.KindRequirements, "**/meta/requirements.{yaml,yml}"},
	{parse.KindGalaxy, "**/galaxy.yml"},
	{parse.KindReno, "**/releasenotes/*/*.{yaml,yml}"},
	{parse.KindVars, "**/{host_vars,group_vars,vars,defaults}/**/*.{yaml,yml}"},
	{parse.KindTasks, "**/tasks/**/*.{yaml,yml}"},
	{"rulebook", "**/rulebooks/*.{yml,yaml}"},
	{"play-argspec", "**/*.meta.{yaml,yml}"},
	{parse.KindPlaybook, "**/playbooks/*.{yml,yaml}"},
	{parse.KindPlaybook, "**/*playbook*.{yml,yaml}"},
	{parse.KindPlaybook, "**/extensions/patterns/*/playbooks/*.{yml,yaml}"},
	{parse.KindHandlers, "**/handlers/*.{yaml,yml}"},
	{parse.KindTestMeta, "**/tests/integration/targets/*/meta/main.{yaml,yml}"},
	{parse.KindMeta, "**/meta/main.{yaml,yml}"},
	{parse.KindMetaRuntime, "**/meta/runtime.{yaml,yml}"},
	{"role-arg-spec", "**/meta/argument_specs.{yaml,yml}"},
	{parse.KindYAML, ".config/molecule/config.{yaml,yml}"},
	{parse.KindRequirements, "**/molecule/*/{collections,requirements}.{yaml,yml}"},
	{parse.KindYAML, "**/molecule/*/{base,molecule}.{yaml,yml}"},
	{parse.KindRequirements, "**/requirements.{yaml,yml}"},
	{parse.KindPlaybook, "**/molecule/*/*.{yaml,yml}"},
	{parse.KindYAML, "**/{.ansible-lint,.yamllint}"},
	{"changelog", "**/changelogs/changelog.{yaml,yml}"},
	{parse.KindYAML, "**/*.{yaml,yml}"},
	{parse.KindYAML, "**/.*.{yaml,yml}"},
	{parse.KindSanityIgnoreFile, "**/tests/sanity/ignore-*.txt"},
	{parse.KindPython, "**/*.py"},
}

// nonYAMLKinds are the kinds in kindTable whose files are not YAML documents.
// They are discovered because astl has text-level rules for some of them (a
// sanity ignore list) and because upstream's kind map names them, so failing to
// parse one as YAML is the expected outcome rather than a file left unchecked.
var nonYAMLKinds = map[string]bool{
	parse.KindJinja2:           true,
	parse.KindText:             true,
	parse.KindPython:           true,
	parse.KindSanityIgnoreFile: true,
}

// IsYAMLKind reports whether kind names files astl reads as YAML documents.
// Callers use it to tell a parse failure that hid a file from the rules from
// one that was never going to parse in the first place. An unknown kind counts
// as YAML: kindTable is the only source of kinds, and every other entry in it
// is a YAML document.
func IsYAMLKind(kind string) bool {
	return !nonYAMLKinds[kind]
}

// compiledKindTable is kindTable with every pattern's brace expansion and
// segment split done once. The patterns are compile-time constants, so this is
// pure per-call work removed from the discovery hot path: KindOf runs on every
// discovered file, and rebuilding these made up a measurable share of it.
// kindTable stays the readable source of truth and its order is preserved.
var compiledKindTable = func() []compiledGlob {
	out := make([]compiledGlob, len(kindTable))
	for i, r := range kindTable {
		out[i] = compileGlob(r.pattern)
	}
	return out
}()

// KindOf returns the kind for a slash-separated path, or "" when the path
// matches no known kind. Patterns are `**/`-anchored, so absolute and
// relative paths both work.
func KindOf(relPath string) string {
	for i, g := range compiledKindTable {
		if g.match(relPath) {
			return kindTable[i].kind
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

// builtinExcludes is ansible-lint's own exclude list, the one it merges with
// the user's exclude_paths before walking a path (`get_all_files`). It is one
// merged spec there and here, so a user pattern that negates a builtin
// (`!.tox`) re-includes it, later pattern winning.
var builtinExcludes = []string{
	".ansible",
	".git",
	".tox",
	".mypy_cache",
	"__pycache__",
	".DS_Store",
	".coverage",
	".pytest_cache",
	".ruff_cache",
}

// WalkWithin collects lintables under the given roots. excluded is a list of
// gitignore-style patterns (ansible-lint hands exclude_paths to pathspec's
// GitIgnoreSpec): an unanchored name matches at any depth, a
// slash anchors to the working directory, `**` spans segments.
//
// Discovery also applies builtinExcludes and, for every directory it enters,
// that directory's own `.gitignore`. Both reproduce ansible-lint's
// `get_all_files`, quirks included: a `.gitignore` governs only its
// directory's immediate children, and patterns are matched against the path
// accumulated from the walk root, so an anchored pattern in a nested
// `.gitignore` cannot match anything. A path passed in as a root is exempt
// from every exclusion, as it is upstream.
//
// A root that cannot be stat'ed is fatal and comes back as the error return.
// Failures on individual entries below a root are collected in soft and the
// traversal continues, so one unreadable directory does not lose the rest of
// the run; callers are expected to report them as warnings.
//
// Entries are confined to project, the directory ProjectRoot returns for the
// run. An entry below a root whose resolved path lies outside project is
// skipped, as upstream's Runner.is_excluded skips it, so a symlink in the
// linted tree cannot have astl read and report on a file elsewhere on the host. A root itself is always linted, wherever it is, as
// upstream never excludes a path it was handed.
func WalkWithin(project string, roots []string, excluded []string) (items []Item, soft []error, err error) {
	w := &walk{
		wd:         WorkingDir(),
		project:    project,
		excluded:   yamllint.ParsePathSpec(append(append([]string{}, builtinExcludes...), excluded...)),
		seen:       map[string]bool{},
		gitignores: map[string]*yamllint.PathSpec{},
	}

	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, w.soft, fmt.Errorf("discover: %s: %w", root, err)
		}
		absRoot = resolvePath(absRoot)
		if _, err := os.Stat(absRoot); err != nil {
			return nil, w.soft, fmt.Errorf("discover: %w", err)
		}
		w.root = absRoot
		if err := filepath.WalkDir(absRoot, w.visit); err != nil {
			return nil, w.soft, fmt.Errorf("discover: %w", err)
		}
	}
	sort.Slice(w.items, func(i, j int) bool { return w.items[i].Path < w.items[j].Path })
	return w.items, w.soft, nil
}

// walk holds the state the traversal callback accumulates, so that visiting an
// entry stays a named method instead of a closure over six locals. wd is
// resolved once per WalkWithin because rendering a display path needs it for every
// entry. root is the root currently being walked, the one path exclusions and
// project containment do not apply to. gitignores maps a directory to the spec its own `.gitignore`
// compiled to.
type walk struct {
	wd         string
	project    string
	root       string
	excluded   *yamllint.PathSpec
	gitignores map[string]*yamllint.PathSpec
	seen       map[string]bool
	items      []Item
	soft       []error
}

// visit is the filepath.WalkDir callback.
func (w *walk) visit(p string, d fs.DirEntry, err error) error {
	if err != nil {
		w.soft = append(w.soft, err)
		return nil
	}
	if w.isExcluded(p, d.IsDir()) {
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	}
	if d.IsDir() {
		return w.visitDir(p)
	}
	target, ok := resolveLink(p, d)
	if !ok {
		return nil
	}
	// Classify the resolved path, as upstream does: a symlink
	// `tasks/main.yml -> ../vars/data.yml` is a vars file to ansible-lint.
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
// The walk already holds the entry, and the workers admit a file to the memory
// budget by this size before reading it, so it must describe what will be read.
// d.Info() lstats a symlink, which would admit `playbook.yml -> /dev/zero` as a
// 9 byte file; resolving the link gives the real size and drops a link to a
// non-file, as upstream's Path.is_file() does. A symlink to an ordinary file
// stays lintable, as it is for ansible-lint.
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

// isExcluded reports whether an entry is dropped from discovery, and the
// directory it sits in skipped with it.
//
// A root is never excluded: ansible-lint checks the items it iterates over,
// never the path it was handed, so `astl .tox/x.yml` lints that file.
// Otherwise the entry is tested against the merged builtin and user spec, then
// against its own parent directory's `.gitignore` alone. That second lookup is
// deliberately shallow: upstream rebuilds its pathspec list at every level of
// the recursion, so a `.gitignore` never reaches past its directory's
// immediate children.
func (w *walk) isExcluded(p string, isDir bool) bool {
	if p == w.root {
		return false
	}
	// The wd-relative path is what both specs see, which is also how upstream
	// leaves an anchored pattern in a nested `.gitignore` matching nothing.
	rel := displayPath(w.wd, p)
	return w.excluded.MatchEntry(rel, isDir) || w.gitignores[filepath.Dir(p)].MatchEntry(rel, isDir)
}

func (w *walk) visitDir(p string) error {
	w.loadGitignore(p)
	if isRoleDir(p) {
		// A role is a directory, so it contributes no source bytes of its own:
		// the files inside it are discovered and sized separately.
		w.add(p, parse.KindRole, 0)
	}
	return nil
}

// loadGitignore compiles dir's own `.gitignore`, to be applied to that
// directory's immediate children and to nothing else. WalkDir is pre-order, so
// the file is read before the children it governs are visited.
//
// A `.gitignore` that exists but cannot be read is a warning: the repository
// chose the file, and refusing to lint over it would be the worse outcome. The
// read is bounded and regular-file only because the repository also chose the
// content, and `.gitignore -> /dev/zero` is a link a checkout can carry.
func (w *walk) loadGitignore(dir string) {
	data, err := safeio.ReadFile(filepath.Join(dir, ".gitignore"), safeio.MaxConfigBytes)
	if err != nil {
		if !os.IsNotExist(err) {
			w.soft = append(w.soft, err)
		}
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	if spec := yamllint.ParsePathSpec(lines); spec != nil {
		w.gitignores[dir] = spec
	}
}

func (w *walk) add(abs, kind string, size int64) {
	if kind == "" || w.seen[abs] || (abs != w.root && !insideDir(w.project, abs)) {
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
// not a role.
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

// WorkingDir returns the base WalkWithin renders its relative display paths against:
// the process working directory with every symlink component resolved, or ""
// when it cannot be read, in which case WalkWithin prints absolute paths.
//
// It is exported so a report that carries relative paths can also declare the
// directory they are relative to, and be guaranteed to declare the same one
// WalkWithin used.
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
