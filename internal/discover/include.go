package discover

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/arhuman/ansible-static-lint/internal/yamllint"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
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
	parse.KindPlaybook: true,
	parse.KindTasks:    true,
	parse.KindHandlers: true,
}

// mayBeScanned reports whether a file discovered under this kind is worth
// loading. parse.KindYAML is in because it is the catch-all: a playbook whose path
// matches no playbook glob is discovered as yaml and only promoted once parsed,
// which is exactly the shape that carries includes in the repositories where
// this matters. The kind that decides is the one parse.Load returns.
func mayBeScanned(kind string) bool {
	return scannedKinds[kind] || kind == parse.KindYAML
}

// Includes follows `include_tasks`, `import_tasks`, `include` and
// `import_playbook` from the files a run lints to the files they name, to a
// fixpoint.
//
// This is what makes a task list linted as tasks when it does not live under a
// `tasks/` directory. ansible-lint decides a file's kind from its path, then
// overrides it for any file a playbook pulls in: the child inherits the section
// it was included from.
//
// It is driven round by round so that each file is read and parsed once, by the
// worker that lints it: the caller lints a round, hands every parsed file to
// Found, then lints what Next returns. A round is the files one more include
// away from the walk, which is what the round bound counts.
//
// The children are additions, not replacements. A file reached both by the
// walk and through an include is linted under both kinds, as upstream does, and
// the duplicate findings that produces are removed by rules.Dedupe.
//
// Only literal targets are followed. A templated path (`{{ role_path }}/x.yml`)
// is not decidable without the ansible runtime, which ADR 0001 puts out of
// scope, so it is left alone rather than guessed at.
//
// Children are confined to project, the directory ProjectRoot returns for the
// run. A child resolving outside it is not followed, as upstream's
// Runner.is_excluded skips it: the input is untrusted, and a playbook naming
// `../../etc/hosts` must not have astl read and report on it.
type Includes struct {
	wd       string
	project  string
	excluded *yamllint.PathSpec

	mu    sync.Mutex
	seen  map[string]bool
	next  []Item
	round int
}

// NewIncludes starts the fixpoint from items, the walk's lintables, which are
// round zero and are never returned as children under the kind they hold.
func NewIncludes(project string, items []Item, excluded []string) *Includes {
	in := &Includes{
		wd: WorkingDir(), project: project,
		excluded: yamllint.ParsePathSpec(excluded), seen: map[string]bool{},
	}
	for _, it := range items {
		in.seen[it.Abs+"\x00"+it.Kind] = true
	}
	return in
}

// Found records the children f names, f being parent loaded under its own
// kind. It is safe for concurrent use within a round, and the children it
// records are the same whatever order the round's files arrive in.
func (in *Includes) Found(parent Item, f *parse.File) {
	if !mayBeScanned(parent.Kind) || f.Err != nil || f.Root == nil || !scannedKinds[f.Kind] {
		return
	}
	for _, t := range targets(f) {
		in.admit(parent, t)
	}
}

// Next ends the current round and returns the children Found recorded during
// it, sorted by path. An empty result is the fixpoint.
func (in *Includes) Next() []Item {
	in.mu.Lock()
	defer in.mu.Unlock()
	next := in.next
	in.next = nil
	in.round++
	slices.SortFunc(next, func(a, b Item) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Kind, b.Kind))
	})
	return next
}

// maxIncludeRounds bounds the fixpoint. Real include chains are a handful deep;
// this only exists so a pathological repository cannot spin the loop.
const maxIncludeRounds = 32

// admit resolves one target against the parent and records it when it is new.
// The filesystem lookups run outside the lock, so workers only serialize on the
// seen set.
func (in *Includes) admit(parent Item, t target) {
	abs, ok := resolveInclude(filepath.Dir(parent.Abs), t.file)
	if !ok || !insideDir(in.project, abs) {
		return
	}
	path := displayPath(in.wd, abs)
	if in.excluded.Match(path) {
		return
	}
	item := Item{Path: path, Abs: abs, Kind: t.kind, Size: fileSize(abs)}
	key := abs + "\x00" + t.kind
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.seen[key] || in.round >= maxIncludeRounds {
		return
	}
	in.seen[key] = true
	in.next = append(in.next, item)
}

// target is one child named by an inclusion action: the path as written, and
// the kind the child inherits.
type target struct {
	file string
	kind string
}

// targets returns the children named in f.
func targets(f *parse.File) []target {
	if f.Kind == parse.KindPlaybook {
		return playbookTargets(f.Root)
	}
	return taskListTargets(f.Root, f.Kind)
}

// playbookTargets collects the children of a playbook: those a play names
// directly, which is `- import_playbook: other.yml` and inherits the playbook
// kind, and those its task sections name.
func playbookTargets(root *yaml.Node) []target {
	return seqTargets(root, parse.KindPlaybook)
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
