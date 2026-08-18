package discover

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/safeio"
)

// inclusionActions are the task keys that name another file of tasks or plays,
// spelled both bare and FQCN, matching upstream's INCLUSION_ACTION_NAMES.
var inclusionActions = map[string]bool{
	"include":                         true,
	"include_tasks":                   true,
	"import_tasks":                    true,
	"import_playbook":                 true,
	"ansible.builtin.include":         true,
	"ansible.builtin.include_tasks":   true,
	"ansible.builtin.import_tasks":    true,
	"ansible.builtin.import_playbook": true,
}

// playSections are the play keys holding task lists. The section's own name is
// the kind its included children inherit, which is upstream's
// `child_type = k if parent_type == "playbook" else parent_type`. It is why an
// include under `tasks:` gets the task rules and the same include under
// `pre_tasks:` does not: "pre_tasks" is not a kind any rule matches. Measured
// against ansible-lint 26.8.0 rather than inferred, see include_test.go.
var playSections = []string{"tasks", "pre_tasks", "post_tasks", "handlers"}

// nestedTaskKeys hold task lists inside a task.
var nestedTaskKeys = []string{"block", "rescue", "always"}

// scannedKinds are the kinds whose files can name children. Upstream's
// find_children returns nothing for anything else.
var scannedKinds = map[string]bool{
	KindPlaybook: true,
	KindTasks:    true,
	KindHandlers: true,
}

// mayBeScanned reports whether a file discovered under this kind is worth
// loading. KindYAML is in because it is the catch-all: a playbook whose path
// matches no playbook glob is discovered as yaml and only promoted once parsed,
// which is exactly the shape that carries includes in the repositories where
// this matters. The kind that decides is the one parse.Load returns.
func mayBeScanned(kind string) bool {
	return scannedKinds[kind] || kind == KindYAML
}

// ExpandIncludes returns the extra lintables reachable from items through
// `include_tasks`, `import_tasks`, `include` and `import_playbook`, following
// them to a fixpoint.
//
// This is what makes a task list linted as tasks when it does not live under a
// `tasks/` directory. ansible-lint decides a file's kind from its path, then
// overrides it for any file a playbook pulls in: the child inherits the section
// it was included from. astl classified by path alone, so on dell/omnia, whose
// task lists sit under `playbooks/`, 78 `key-order[task]` and 2
// `risky-shell-pipe` findings were missed (issue 0008).
//
// The returned items are additions, not replacements. A file reached both by
// the walk and through an include is linted under both kinds, as upstream does,
// and the duplicate findings that produces are removed by rules.Dedupe.
//
// Only literal targets are followed. A templated path (`{{ role_path }}/x.yml`)
// is not decidable without the ansible runtime, which ADR 0001 puts out of
// scope, so it is left alone rather than guessed at.
func ExpandIncludes(items []Item, excluded []string) []Item {
	wd, err := os.Getwd()
	if err != nil {
		wd = ""
	}
	wd = resolvePath(wd)

	e := &expander{wd: wd, excluded: excluded, seen: map[string]bool{}}
	for _, it := range items {
		e.seen[it.Abs+"\x00"+it.Kind] = true
	}

	frontier := items
	// A fixpoint, since an included tasks file may include another. It
	// terminates because every round only enqueues pairs not yet in seen, and
	// the number of (path, kind) pairs on disk is finite; the bound is a
	// backstop against a symlink loop making that set effectively unbounded.
	for round := 0; len(frontier) > 0 && round < maxIncludeRounds; round++ {
		frontier = e.expand(frontier)
	}
	sort.Slice(e.added, func(i, j int) bool { return e.added[i].Path < e.added[j].Path })
	return e.added
}

// maxIncludeRounds bounds the fixpoint. Real include chains are a handful deep;
// this only exists so a pathological repository cannot spin the loop.
const maxIncludeRounds = 32

type expander struct {
	wd       string
	excluded []string
	seen     map[string]bool
	added    []Item
}

// expand scans one round of parents and returns the children it found.
func (e *expander) expand(parents []Item) []Item {
	var next []Item
	for _, parent := range parents {
		if !mayBeScanned(parent.Kind) {
			continue
		}
		f, ok := loadForIncludes(parent)
		if !ok || !scannedKinds[f.Kind] {
			continue
		}
		for _, t := range targets(f) {
			child, ok := e.admit(parent, t)
			if ok {
				next = append(next, child)
			}
		}
	}
	return next
}

// admit resolves one target against the parent and records it, returning the
// new item and whether it is new.
func (e *expander) admit(parent Item, t target) (Item, bool) {
	abs, ok := resolveInclude(filepath.Dir(parent.Abs), t.file)
	if !ok {
		return Item{}, false
	}
	key := abs + "\x00" + t.kind
	if e.seen[key] {
		return Item{}, false
	}
	path := displayPath(e.wd, abs)
	if isExcluded(path, e.excluded) {
		return Item{}, false
	}
	e.seen[key] = true
	item := Item{Path: path, Abs: abs, Kind: t.kind, Size: fileSize(abs)}
	e.added = append(e.added, item)
	return item, true
}

// loadForIncludes parses a parent only when its text mentions an inclusion at
// all. The substring test is what keeps this pass off the critical path: most
// task files in a repository include nothing, and skipping their parse costs
// one read of bytes the linter was going to read anyway.
func loadForIncludes(it Item) (*parse.File, bool) {
	data, err := safeio.ReadFile(it.Abs, safeio.MaxLintableBytes)
	if err != nil || !mentionsInclusion(data) {
		return nil, false
	}
	f := parse.Load(it.Path, it.Abs, it.Kind)
	if f.Err != nil || f.Root == nil {
		return nil, false
	}
	return f, true
}

func mentionsInclusion(data []byte) bool {
	return bytes.Contains(data, []byte("include")) || bytes.Contains(data, []byte("import"))
}

// target is one child named by an inclusion action: the path as written, and
// the kind the child inherits.
type target struct {
	file string
	kind string
}

// targets returns the children named in f.
func targets(f *parse.File) []target {
	if f.Kind == KindPlaybook {
		return playbookTargets(f.Root)
	}
	return taskListTargets(f.Root, f.Kind)
}

// playbookTargets collects the children of a playbook: those a play names
// directly, which is `- import_playbook: other.yml` and inherits the playbook
// kind, and those its task sections name.
func playbookTargets(root *yaml.Node) []target {
	return seqTargets(root, KindPlaybook)
}

// taskListTargets walks a task list, descending into block/rescue/always the
// way upstream's taskshandlers_children does.
func taskListTargets(list *yaml.Node, kind string) []target {
	return seqTargets(list, kind)
}

// seqTargets is the one walk both levels share: for every mapping in seq, take
// the children it names directly under kind own, then recurse into its nested
// lists.
func seqTargets(seq *yaml.Node, own string) []target {
	if !parse.IsSeq(seq) {
		return nil
	}
	var out []target
	for _, m := range seq.Content {
		if !parse.IsMap(m) {
			continue
		}
		out = inclusionTargets(m, own, out)
		for _, key := range parse.MapKeys(m) {
			if kind, ok := nestedKind(key, own); ok {
				out = append(out, seqTargets(parse.MapGet(m, key), kind)...)
			}
		}
	}
	return out
}

// nestedKind reports whether key holds a nested list of tasks, and the kind an
// inclusion inside it gives its child.
//
// One function covers both levels because the two key sets cannot collide: a
// play has no `block`, and a task has no `tasks`. A play section names the
// child's kind, which is why `tasks:` and `pre_tasks:` behave differently; a
// block keeps its parent's, because wrapping tasks in one does not change what
// they are.
func nestedKind(key, parent string) (string, bool) {
	if slices.Contains(playSections, key) {
		return key, true
	}
	if slices.Contains(nestedTaskKeys, key) {
		return parent, true
	}
	return "", false
}

// inclusionTargets appends the children named by the inclusion keys of one
// mapping, which is either a play or a task depending on the caller.
func inclusionTargets(m *yaml.Node, kind string, out []target) []target {
	for _, key := range parse.MapKeys(m) {
		if !inclusionActions[key] {
			continue
		}
		if t, ok := fileOf(parse.MapGet(m, key), kind); ok {
			out = append(out, t)
		}
	}
	return out
}

// fileOf extracts the included path from an action's value: a bare path, a
// lone `file=path` argument, or a mapping with a `file:` key.
func fileOf(v *yaml.Node, kind string) (target, bool) {
	var raw string
	switch {
	case parse.IsScalar(v):
		raw = v.Value
	case parse.IsMap(v):
		raw = parse.Str(parse.MapGet(v, "file"))
	}
	name := includedFile(raw)
	if name == "" {
		return target{}, false
	}
	return target{file: name, kind: kind}, true
}

// includedFile reads the target out of an action value, and refuses the two
// kinds of value that are not a target astl may follow.
//
// Jinja is refused because its result is not knowable statically, which ADR
// 0001 puts out of scope.
//
// Several tokens is refused for a different reason, measured rather than
// assumed: `include_tasks: child.yml tags=setup` is free-form for a module that
// takes none, so ansible-core rejects the *including* file outright. Upstream
// then abandons it at syntax-check and emits nothing from it at all, while astl
// has no syntax-check phase and would happily report on the child. Following
// such a value is therefore a false-positive generator, not extra coverage. A
// lone `file=child.yml` is a real module option and is followed.
func includedFile(raw string) string {
	if raw == "" || strings.Contains(raw, "{{") {
		return ""
	}
	fields := strings.Fields(raw)
	if len(fields) != 1 {
		return ""
	}
	key, value, isArg := strings.Cut(fields[0], "=")
	if !isArg {
		return fields[0]
	}
	if key == "file" {
		return value
	}
	return ""
}

// resolveInclude mirrors ansible's path_dwim as upstream drives it: relative to
// the including file's directory, then that directory's `tasks/`, then the same
// two questions one directory up, until the filesystem root.
//
// Walking up is what makes `include_tasks: common.yml` work from a role's
// `tasks/main.yml` when the file sits at the role root, and it is upstream's
// behaviour rather than a convenience: matching it is the point.
func resolveInclude(baseDir, file string) (string, bool) {
	base := baseDir
	for {
		if p, ok := existingFile(filepath.Join(base, file)); ok {
			return p, true
		}
		if p, ok := existingFile(filepath.Join(base, "tasks", file)); ok {
			return p, true
		}
		parent := filepath.Dir(base)
		if parent == base {
			return "", false
		}
		base = parent
	}
}

// existingFile reports whether p is a regular file, and returns it with
// symlinks resolved, which is the spelling every other path in a run carries.
func existingFile(p string) (string, bool) {
	resolved := resolvePath(p)
	fi, err := os.Stat(resolved)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return resolved, true
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}
