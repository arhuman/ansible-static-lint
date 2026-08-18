package rules

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// varnaming.go implements var-naming, transcribed from upstream
// var_naming.py. One defect can be reported through two upstream paths at
// once: a play-shaped pass over top-level entries (no suffix on play vars)
// and a task pass (suffixed), and upstream keeps both, so astl emits both.

// VarNamingPatternDefault is upstream's default naming pattern.
const VarNamingPatternDefault = `^[a-z_][a-z0-9_]*$`

// varPrefix mirrors upstream's Prefix named tuple, plus active standing in
// for Python's None-vs-Prefix() distinction (an inactive prefix disables the
// role-prefix check and never relaxes the pattern check).
type varPrefix struct {
	value    string
	fromFQCN bool
	active   bool
}

// parseVarPrefix is upstream's _parse_prefix: a dotted reference yields no
// usable prefix value, a path keeps its last segment.
func parseVarPrefix(ref string) varPrefix {
	value := ""
	if !strings.Contains(ref, ".") {
		parts := strings.Split(ref, "/")
		value = parts[len(parts)-1]
	}
	return varPrefix{value: value, fromFQCN: isFQCN(ref), active: true}
}

// reFQCN is the dotted-only alternative of reFQCNOrName (task.go), which is
// what upstream's is_fqcn accepts: at least two dot-separated segments after
// the first.
var reFQCN = regexp.MustCompile(`^\w+(\.\w+){2,100}$`)

func isFQCN(s string) bool       { return reFQCN.MatchString(s) }
func isFQCNOrName(s string) bool { return reFQCNOrName.MatchString(s) }

// varPattern is the compiled naming pattern with the source text the message
// interpolates.
type varPattern struct {
	re  *regexp.Regexp
	src string
}

func (o Options) varNamingPattern() varPattern {
	src := o.VarNamingPattern
	if src == "" {
		src = VarNamingPatternDefault
	}
	return varPattern{re: regexp.MustCompile(src), src: src}
}

// varNameFinding classifies one variable name, positioned on node. The check
// order and every message transcribe upstream's get_var_naming_matcherror.
func varNameFinding(f *parse.File, node *yaml.Node, ident string, prefix varPrefix, pat varPattern) (Finding, bool) {
	if annotationKeys[ident] || allowedSpecialVarNames[ident] {
		return Finding{}, false
	}
	if !isASCII(ident) {
		return at(f, node, "var-naming[non-ascii]",
			fmt.Sprintf("Variables names must be ASCII. (%s)", ident),
			fmt.Sprintf("Variable name %s carries non-ASCII characters. Rename it using plain ASCII.", ident)), true
	}
	if pythonKeywords[ident] {
		return at(f, node, "var-naming[no-keyword]",
			fmt.Sprintf("Variables names must not be Python keywords. (%s)", ident),
			fmt.Sprintf("Variable name %s is a Python keyword. Choose a non-reserved name.", ident)), true
	}
	if ansibleReservedNames[ident] {
		return at(f, node, "var-naming[no-reserved]",
			fmt.Sprintf("Variables names must not be Ansible reserved names. (%s)", ident),
			fmt.Sprintf("Variable name %s is reserved by Ansible. Choose another name.", ident)), true
	}
	if readOnlyVarNames[ident] {
		return at(f, node, "var-naming[read-only]",
			fmt.Sprintf("This special variable is read-only. (%s)", ident),
			fmt.Sprintf("Variable %s is read-only in Ansible. Assign your value to a different name.", ident)), true
	}
	// Jinja templating in a variable name is allowed.
	if strings.Contains(ident, "{{") {
		return Finding{}, false
	}
	if !pat.re.MatchString(ident) && (!prefix.active || !prefix.fromFQCN) {
		return at(f, node, "var-naming[pattern]",
			fmt.Sprintf("Variables names should match %s regex. (%s)", pat.src, ident),
			fmt.Sprintf("Variable name %s does not match %s. Use lowercase letters, digits and underscores.", ident, pat.src)), true
	}
	// The ansible_ namespace is owned by Ansible and cannot carry a role prefix.
	bare := strings.TrimLeft(ident, "_")
	if prefix.active &&
		!strings.HasPrefix(bare, "ansible_") &&
		!strings.HasPrefix(bare, prefix.value+"_") &&
		!reHasJinja.MatchString(prefix.value) &&
		isFQCNOrName(prefix.value) {
		return at(f, node, "var-naming[no-role-prefix]",
			fmt.Sprintf("Variables names from within roles should use %s_ as a prefix.", prefix.value),
			fmt.Sprintf("Role variables should carry the %s_ prefix. Rename the variable to start with it.", prefix.value)), true
	}
	return Finding{}, false
}

// varNonString reports a mapping key that is not a string, which upstream
// pins to line 1 with no column.
func varNonString(f *parse.File) Finding {
	return onLine(f, 1, "var-naming[non-string]",
		"Variables names must be strings.",
		"A mapping key here is not a string. Use a plain string as the variable name.")
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// suffix appends upstream's context marker to both wordings.
func (f *Finding) suffix(kind, key string) {
	f.Message += fmt.Sprintf(" (%s: %s)", kind, key)
	f.NativeMessage += fmt.Sprintf(" (%s: %s)", kind, key)
}

// checkVarKey classifies one mapping key node, non-string keys included.
func checkVarKey(f *parse.File, key *yaml.Node, prefix varPrefix, pat varPattern) (Finding, bool) {
	if key.Tag != "!!str" {
		return varNonString(f), true
	}
	return varNameFinding(f, key, key.Value, prefix, pat)
}

var noVarPrefix = varPrefix{}

// varNamingRules runs every var-naming path that applies to the file's kind.
func varNamingRules(f *parse.File, opt Options) []Finding {
	pat := opt.varNamingPattern()
	switch f.Kind {
	case "vars":
		return varNamingVarsFile(f, pat)
	case "playbook", "tasks", "handlers":
		out := varNamingPlays(f, pat)
		return append(out, varNamingTasks(f, pat)...)
	}
	return nil
}

// varNamingVarsFile is upstream's matchyaml vars path: top-level keys only,
// role prefix from the role directory, ` (vars: k)` suffix, line-scoped
// suppression.
func varNamingVarsFile(f *parse.File, pat varPattern) []Finding {
	if !parse.IsMap(f.Root) {
		return nil
	}
	prefix := varPrefix{value: f.Role, active: true}
	var out []Finding
	for i := 0; i+1 < len(f.Root.Content); i += 2 {
		key := f.Root.Content[i]
		fd, ok := checkVarKey(f, key, prefix, pat)
		if !ok {
			continue
		}
		fd.suffix("vars", pyStr(key))
		fd.lineScoped = true
		out = append(out, fd)
	}
	return out
}

// varNamingPlays is upstream's matchplay path over every top-level entry:
// play (or bare task) vars without suffix, `roles:` entries with one. An
// entry is skipped whole when a noqa inside it names the bare rule id or its
// tags carry skip_ansible_lint, and each finding is line-scoped after that.
func varNamingPlays(f *parse.File, pat varPattern) []Finding {
	if !parse.IsSeq(f.Root) {
		return nil
	}
	var out []Finding
	for _, entry := range f.Root.Content {
		if !parse.IsMap(entry) {
			continue
		}
		if skipTagIn(parse.MapGet(entry, "tags")) {
			continue
		}
		// Upstream attaches aggregated noqa skips to tasks, never to plays, so
		// only a task-shaped entry of a tasks or handlers file is skipped
		// whole, and only by its bare rule id.
		if f.Kind != "playbook" {
			entrySkips := canonicalSkips(f.SkipsInRange(parse.NodePos(entry).Line, parse.EndLine(entry)))
			if entrySkips["var-naming"] {
				continue
			}
		}
		if vars := parse.MapGet(entry, "vars"); parse.IsMap(vars) {
			for i := 0; i+1 < len(vars.Content); i += 2 {
				if fd, ok := checkVarKey(f, vars.Content[i], noVarPrefix, pat); ok {
					fd.lineScoped = true
					out = append(out, fd)
				}
			}
		}
		out = append(out, varNamingRoleEntries(f, parse.MapGet(entry, "roles"), pat)...)
	}
	return out
}

// varNamingRoleEntries checks the mapping entries of a play's `roles:` list:
// every non-keyword key and every key under the entry's `vars:`, against the
// prefix the entry's `role` (or `name`) reference implies.
func varNamingRoleEntries(f *parse.File, roles *yaml.Node, pat varPattern) []Finding {
	if !parse.IsSeq(roles) {
		return nil
	}
	var out []Finding
	for _, role := range roles.Content {
		if !parse.IsMap(role) {
			continue
		}
		ref := parse.MapGet(role, "role")
		if ref == nil {
			ref = parse.MapGet(role, "name")
		}
		prefix := parseVarPrefix(parse.Str(ref))
		for i := 0; i+1 < len(role.Content); i += 2 {
			key := role.Content[i]
			if playbookRoleKeywords[key.Value] && key.Tag == "!!str" {
				continue
			}
			if fd, ok := checkVarKey(f, key, prefix, pat); ok {
				fd.suffix("vars", pyStr(key))
				fd.lineScoped = true
				out = append(out, fd)
			}
		}
		if vars := parse.MapGet(role, "vars"); parse.IsMap(vars) {
			for i := 0; i+1 < len(vars.Content); i += 2 {
				if fd, ok := checkVarKey(f, vars.Content[i], prefix, pat); ok {
					fd.suffix("vars", pyStr(vars.Content[i]))
					fd.lineScoped = true
					out = append(out, fd)
				}
			}
		}
	}
	return out
}

// varNamingTasks is upstream's matchtask path: task vars (role prefix only
// for include_role/import_role), set_fact keys and registered names, all
// under the normal task suppression scopes.
func varNamingTasks(f *parse.File, pat varPattern) []Finding {
	rolePrefix := varPrefix{value: f.Role, active: true}
	var out []Finding
	for _, t := range f.Tasks() {
		out = append(out, varNamingTaskVars(f, t, pat)...)
		if t.Module == "set_fact" {
			out = append(out, varNamingSetFact(f, t, rolePrefix, pat)...)
		}
		out = append(out, varNamingRegister(f, t, rolePrefix, pat)...)
	}
	return out
}

// varNamingTaskVars checks a task's `vars:` keys; only include_role and
// import_role lend the referenced role's prefix to them.
func varNamingTaskVars(f *parse.File, t *parse.Task, pat varPattern) []Finding {
	vars := t.RawGet("vars")
	if !parse.IsMap(vars) {
		return nil
	}
	taskPrefix := varPrefix{active: true}
	if t.Module == "include_role" || t.Module == "import_role" {
		taskPrefix = parseVarPrefix(t.ArgText("name"))
	}
	var out []Finding
	for i := 0; i+1 < len(vars.Content); i += 2 {
		fd, ok := checkVarKey(f, vars.Content[i], taskPrefix, pat)
		if !ok {
			continue
		}
		fd.suffix("vars", pyStr(vars.Content[i]))
		if fd.Line < t.Pos.Line {
			fd.Line = t.Pos.Line
		}
		out = append(out, fd)
	}
	return out
}

// varNamingSetFact checks the fact names a set_fact task assigns, on the
// task's line with each name's own column.
func varNamingSetFact(f *parse.File, t *parse.Task, rolePrefix varPrefix, pat varPattern) []Finding {
	var out []Finding
	for _, key := range moduleArgKeyNodes(t) {
		if key.Value == "cacheable" || strings.HasPrefix(key.Value, "__") || key.Tag != "!!str" {
			continue
		}
		fd, ok := varNameFinding(f, key, key.Value, rolePrefix, pat)
		if !ok {
			continue
		}
		fd.suffix("set_fact", key.Value)
		fd.Line = t.Pos.Line
		out = append(out, fd)
	}
	return out
}

// varNamingRegister checks a task's registered names: the scalar form, or
// each key of the rare mapping form. Findings sit on the task's line with the
// name's own column, which is exactly how upstream mixes the two.
func varNamingRegister(f *parse.File, t *parse.Task, rolePrefix varPrefix, pat varPattern) []Finding {
	reg := t.RawGet("register")
	if reg == nil {
		return nil
	}
	var names []*yaml.Node
	switch {
	case parse.IsScalar(reg):
		names = []*yaml.Node{reg}
	case parse.IsMap(reg):
		for i := 0; i+1 < len(reg.Content); i += 2 {
			names = append(names, reg.Content[i])
		}
	}
	var out []Finding
	for _, node := range names {
		if node.Tag != "!!str" || strings.HasPrefix(node.Value, "__") {
			continue
		}
		fd, ok := varNameFinding(f, node, node.Value, rolePrefix, pat)
		if !ok {
			continue
		}
		fd.suffix("register", node.Value)
		fd.Line = t.Pos.Line
		out = append(out, fd)
	}
	return out
}

// moduleArgKeyNodes returns the key nodes of a task's module arguments as
// written: the module's own mapping (or its action:/local_action: form,
// whose `module` key names the module rather than an argument) plus an
// explicit args: mapping. Free-form arguments carry no nodes and, like
// upstream's position-less keys, produce no finding position.
func moduleArgKeyNodes(t *parse.Task) []*yaml.Node {
	var out []*yaml.Node
	appendKeys := func(m *yaml.Node, skipModule bool) {
		if !parse.IsMap(m) {
			return
		}
		for i := 0; i+1 < len(m.Content); i += 2 {
			if skipModule && m.Content[i].Value == "module" {
				continue
			}
			out = append(out, m.Content[i])
		}
	}
	for _, key := range []string{"action", "local_action"} {
		if v := t.RawGet(key); parse.IsMap(v) {
			appendKeys(v, true)
		}
	}
	for _, key := range t.RawKeys() {
		if taskKeywordForArgs(key) {
			continue
		}
		appendKeys(t.RawGet(key), false)
	}
	appendKeys(t.RawGet("args"), false)
	return out
}

// taskKeywordForArgs reports whether a task mapping key can never be the
// module name, mirroring the action resolution in parse.
func taskKeywordForArgs(key string) bool {
	return parse.IsTaskKeyword(key) || strings.HasPrefix(key, "with_") || strings.HasPrefix(key, "__")
}
