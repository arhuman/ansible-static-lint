package parse

import (
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// taskKeywords are the task-level keys that are never module names. Anything
// else in a task mapping is treated as the action, mirroring what ansible's
// ModuleArgsParser derives from the module loader.
var taskKeywords = map[string]bool{
	"action": true, "always": true, "always_run": true, "any_errors_fatal": true,
	"args": true, "async": true, "become": true, "become_exe": true,
	"become_flags": true, "become_method": true, "become_user": true,
	"block": true, "changed_when": true, "check_mode": true, "collections": true,
	"connection": true, "debugger": true, "delay": true, "delegate_facts": true,
	"delegate_to": true, "diff": true, "environment": true, "failed_when": true,
	"first_available_file": true, "ignore_errors": true, "ignore_unreachable": true,
	"listen": true, "local_action": true, "loop": true, "loop_control": true,
	"module_defaults": true, "name": true, "no_log": true, "notify": true,
	"poll": true, "port": true, "register": true, "remote_user": true,
	"rescue": true, "retries": true, "run_once": true, "tags": true,
	"throttle": true, "timeout": true, "until": true, "vars": true, "when": true,
}

// nestedTaskKeys are the keys whose values hold nested task lists.
var nestedTaskKeys = []string{"block", "rescue", "always"}

// rawParamModules take a free-form command line rather than key=value args.
var rawParamModules = map[string]bool{
	"command": true, "shell": true, "raw": true, "script": true,
	"win_command": true, "win_shell": true,
}

// rawParamOptions are the only key=value pairs still parsed as options for
// free-form modules; everything else is part of the command line.
var rawParamOptions = map[string]bool{
	"creates": true, "removes": true, "chdir": true, "executable": true, "warn": true,
}

// IsTaskKeyword reports whether key is a task-level keyword that can never
// be a module name.
func IsTaskKeyword(key string) bool { return taskKeywords[key] }

// Arg is a single module argument. Node is set when the argument came from a
// YAML mapping; Text is always the string form.
type Arg struct {
	Node *yaml.Node
	Text string
}

// Task is a normalized Ansible task.
type Task struct {
	// Node is the task mapping node.
	Node *yaml.Node
	// Pos is the position of the task mapping.
	Pos Pos
	// Module is the action name with any `ansible.builtin.` prefix removed.
	Module string
	// ModuleOriginal is the action name as written.
	ModuleOriginal string
	// Args holds the parsed module arguments.
	Args map[string]Arg
	// Kind is "tasks" or "handlers".
	Kind string
	// IsBlock reports whether the task is a block/rescue/always container.
	IsBlock bool
	// BlockDepth is how many `block:` keys were traversed to reach the task.
	BlockDepth int
}

// BlockModule is the synthetic module name ansible-lint gives block containers.
const BlockModule = "block/always/rescue"

// RawHas reports whether the raw task mapping contains key.
func (t *Task) RawHas(key string) bool { return MapHas(t.Node, key) }

// RawGet returns the raw task value node for key, or nil.
func (t *Task) RawGet(key string) *yaml.Node { return MapGet(t.Node, key) }

// RawKeys returns the raw task keys in document order.
func (t *Task) RawKeys() []string { return MapKeys(t.Node) }

// ArgText returns the string form of a module argument, or "".
func (t *Task) ArgText(name string) string { return t.Args[name].Text }

// HasArg reports whether the module argument is present.
func (t *Task) HasArg(name string) bool { _, ok := t.Args[name]; return ok }

// ArgTruthy reports whether the module argument is present and not a Python
// falsy value (absent, empty, false, 0, null).
func (t *Task) ArgTruthy(name string) bool {
	a, ok := t.Args[name]
	if !ok {
		return false
	}
	if a.Node != nil && a.Node.Kind == yaml.ScalarNode {
		switch a.Node.Tag {
		case "!!null":
			return false
		case "!!bool":
			return strings.EqualFold(a.Node.Value, "true") || a.Node.Value == "yes" || a.Node.Value == "on"
		case "!!int", "!!float":
			return a.Node.Value != "0" && a.Node.Value != "0.0"
		case "!!str":
			if v, ok := PyBool(a.Node); ok {
				return v
			}
		}
	}
	return a.Text != ""
}

// CmdArgs returns the command line of a free-form module, mirroring
// ansiblelint.utils.get_cmd_args.
func (t *Task) CmdArgs() string {
	if v, ok := t.Args["cmd"]; ok {
		return v.Text
	}
	return t.Args["_raw_params"].Text
}

// Tasks returns every task in the file, recursing into block/rescue/always and
// into play sections for playbooks. The result is memoized; callers must not
// mutate it.
func (f *File) Tasks() []*Task {
	if f.tasksDone {
		return f.tasks
	}
	f.tasksDone = true
	f.tasks = f.buildTasks()
	return f.tasks
}

func (f *File) buildTasks() []*Task {
	if f.Root == nil {
		return nil
	}
	var out []*Task
	switch f.Kind {
	case "playbook":
		if !IsSeq(f.Root) {
			return nil
		}
		for _, play := range f.Root.Content {
			if !IsMap(play) {
				continue
			}
			for _, attr := range []string{"tasks", "pre_tasks", "post_tasks", "handlers"} {
				section := MapGet(play, attr)
				kind := "handlers"
				if strings.Contains(attr, "tasks") {
					kind = "tasks"
				}
				out = append(out, tasksInList(section, kind)...)
			}
		}
	case "tasks", "handlers":
		out = append(out, tasksInList(f.Root, f.Kind)...)
	}
	return out
}

// Plays returns the play mappings of a playbook. The result is memoized;
// callers must not mutate it.
func (f *File) Plays() []*yaml.Node {
	if f.playsDone {
		return f.plays
	}
	f.playsDone = true
	f.plays = f.buildPlays()
	return f.plays
}

func (f *File) buildPlays() []*yaml.Node {
	if f.Kind != "playbook" || !IsSeq(f.Root) {
		return nil
	}
	var out []*yaml.Node
	for _, play := range f.Root.Content {
		if IsMap(play) {
			out = append(out, play)
		}
	}
	return out
}

func tasksInList(seq *yaml.Node, kind string) []*Task {
	return tasksAtDepth(seq, kind, 0)
}

// tasksAtDepth walks a task list, carrying the number of `block:` keys already
// traversed. Only `block` deepens the nesting, matching how ansible-lint counts
// it in a task's position path; `rescue` and `always` do not.
func tasksAtDepth(seq *yaml.Node, kind string, depth int) []*Task {
	if !IsSeq(seq) {
		return nil
	}
	var out []*Task
	for _, entry := range seq.Content {
		if !IsMap(entry) || len(entry.Content) == 0 {
			continue
		}
		t := newTask(entry, kind)
		t.BlockDepth = depth
		out = append(out, t)
		for _, nested := range nestedTaskKeys {
			next := depth
			if nested == "block" {
				next++
			}
			out = append(out, tasksAtDepth(MapGet(entry, nested), kind, next)...)
		}
	}
	return out
}

func newTask(node *yaml.Node, kind string) *Task {
	t := &Task{Node: node, Pos: NodePos(node), Kind: kind, Args: map[string]Arg{}}

	for _, k := range nestedTaskKeys {
		if MapHas(node, k) {
			t.IsBlock = true
			t.Module = BlockModule
			t.ModuleOriginal = BlockModule
			return t
		}
	}

	name, argsNode, freeForm := resolveAction(node)
	t.ModuleOriginal = name
	t.Module = strings.TrimPrefix(name, "ansible.builtin.")

	if argsNode != nil {
		// Under `action:` or `local_action:`, `module:` names the module and is
		// consumed by the parse; it is not an argument to it.
		skipModule := argsNode == actionMapping(node)
		for i := 0; i+1 < len(argsNode.Content); i += 2 {
			key := argsNode.Content[i].Value
			if skipModule && key == "module" {
				continue
			}
			t.Args[key] = Arg{Node: argsNode.Content[i+1], Text: argsNode.Content[i+1].Value}
		}
	}
	if freeForm != "" {
		for k, v := range parseKV(freeForm, rawParamModules[t.Module]) {
			t.Args[k] = Arg{Text: v}
		}
	}
	// An explicit `args:` mapping is merged on top of the action arguments.
	if extra := MapGet(node, "args"); IsMap(extra) {
		for i := 0; i+1 < len(extra.Content); i += 2 {
			t.Args[extra.Content[i].Value] = Arg{Node: extra.Content[i+1], Text: extra.Content[i+1].Value}
		}
	}
	return t
}

// CountActionKeys returns how many keys in a task mapping could be module
// names. More than one means ansible would reject the task.
func CountActionKeys(node *yaml.Node) int {
	if MapHas(node, "action") || MapHas(node, "local_action") {
		return 1
	}
	n := 0
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if taskKeywords[key] || strings.HasPrefix(key, "with_") || strings.HasPrefix(key, "__") {
			continue
		}
		n++
	}
	return n
}

// actionMapping returns the mapping form of an `action:` or `local_action:`
// key, or nil when the task uses neither or wrote it as a string.
func actionMapping(node *yaml.Node) *yaml.Node {
	for _, key := range []string{"action", "local_action"} {
		if v := MapGet(node, key); IsMap(v) {
			return v
		}
	}
	return nil
}

// resolveAction determines the module name and where its arguments live. It
// returns the module name, a mapping node of arguments (may be nil) and a
// free-form argument string (may be empty).
func resolveAction(node *yaml.Node) (name string, args *yaml.Node, freeForm string) {
	for _, key := range []string{"action", "local_action"} {
		v := MapGet(node, key)
		if v == nil {
			continue
		}
		if IsScalar(v) {
			mod, rest, _ := strings.Cut(strings.TrimSpace(v.Value), " ")
			return mod, nil, strings.TrimSpace(rest)
		}
		if IsMap(v) {
			mod := Str(MapGet(v, "module"))
			return mod, v, ""
		}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if taskKeywords[key] || strings.HasPrefix(key, "with_") || strings.HasPrefix(key, "__") {
			continue
		}
		v := node.Content[i+1]
		if IsMap(v) {
			return key, v, ""
		}
		if IsScalar(v) {
			return key, nil, v.Value
		}
		return key, nil, ""
	}
	return "", nil, ""
}

// parseKV mirrors ansible.parsing.splitter.parse_kv. When checkRaw is set,
// only a small allowlist of key=value pairs is treated as options; everything
// else accumulates into _raw_params.
func parseKV(s string, checkRaw bool) map[string]string {
	options := map[string]string{}
	var raw []string
	for _, tok := range splitArgs(s) {
		k, v, found := strings.Cut(tok, "=")
		if found && k != "" && !strings.ContainsAny(k, " \t") {
			if checkRaw && !rawParamOptions[k] {
				raw = append(raw, tok)
				continue
			}
			options[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
			continue
		}
		raw = append(raw, tok)
	}
	if len(raw) > 0 {
		options["_raw_params"] = joinArgs(raw)
	}
	return options
}

// splitArgs splits on whitespace while keeping quoted runs together. A line
// break becomes a token of its own so that joinArgs can put it back: rules
// reading a multi-line script, risky-shell-pipe among them, match per line.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '\n':
			flush()
			out = append(out, "\n")
		case c == ' ' || c == '\t' || c == '\r':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// joinArgs reassembles the tokens splitArgs produced, mirroring ansible's
// join_args: a space between tokens, none around a line break.
func joinArgs(parts []string) string {
	var b strings.Builder
	last := ""
	for _, p := range parts {
		if b.Len() > 0 && last != "\n" && p != "\n" {
			b.WriteByte(' ')
		}
		b.WriteString(p)
		last = p
	}
	// No trim: ansible's join_args keeps the trailing newline a clip-chomped
	// block scalar carries, and command-instead-of-shell's shell-character
	// check depends on seeing it (issue 0014).
	return b.String()
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// Base returns the final path element, used to reduce a command to its
// executable name.
func Base(p string) string { return path.Base(p) }
