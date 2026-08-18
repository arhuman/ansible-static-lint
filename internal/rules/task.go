package rules

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

var changedWhenModules = map[string]bool{
	"ansible.builtin.command": true, "ansible.builtin.shell": true, "ansible.builtin.raw": true,
	"ansible.legacy.command": true, "ansible.legacy.shell": true, "ansible.legacy.raw": true,
	"command": true, "shell": true, "raw": true,
}

// commandModules maps an executable to the Ansible module that replaces it.
// Embedded verbatim from ansible-lint's command_instead_of_module rule.
var commandModules = map[string]string{
	"apt-get":       "apt-get",
	"chkconfig":     "service",
	"curl":          "get_url or uri",
	"git":           "git",
	"hg":            "hg",
	"letsencrypt":   "acme_certificate",
	"mktemp":        "tempfile",
	"mount":         "mount",
	"patch":         "patch",
	"rpm":           "yum or rpm_key",
	"rsync":         "synchronize",
	"sed":           "template, replace or lineinfile",
	"service":       "service",
	"supervisorctl": "supervisorctl",
	"svn":           "subversion",
	"systemctl":     "systemd",
	"tar":           "unarchive",
	"unzip":         "unarchive",
	"wget":          "get_url or uri",
	"yum":           "yum",
}

// commandExecutableOptions lists subcommands that are read-only and therefore
// do not warrant a module replacement.
var commandExecutableOptions = map[string][]string{
	"git": {"branch", "log", "lfs", "rev-parse", "clean"},
	"systemctl": {
		"--version", "get-default", "kill", "set-default", "set-property",
		"set-environment", "unset-environment", "show-environment", "status",
		"reset-failed",
	},
	"yum": {"clean", "history", "info"},
	"rpm": {"--nodeps"},
}

var packageManagers = map[string]bool{
	"apk": true, "apt": true, "bower": true, "bundler": true, "dnf": true,
	"easy_install": true, "gem": true, "homebrew": true, "jenkins_plugin": true,
	"npm": true, "openbsd_package": true, "openbsd_pkg": true, "package": true,
	"pacman": true, "pear": true, "pip": true, "pkg5": true, "pkgutil": true,
	"portage": true, "slackpkg": true, "sorcery": true, "swdepot": true,
	"win_chocolatey": true, "yarn": true, "yum": true, "zypper": true,
}

// rolePathNativeMsg is shared by the three places a path-style role reference
// can appear: a task, a play's `roles:` list, and meta/main.yml dependencies.
// It omits the path upstream interpolates, which is unbounded; the finding's
// own position already points at it.
const rolePathNativeMsg = "This role is imported by path. Use the role name instead."

var roleImportActions = map[string]bool{
	"ansible.builtin.import_role": true, "ansible.builtin.include_role": true,
	"ansible.legacy.import_role": true, "ansible.legacy.include_role": true,
	"import_role": true, "include_role": true,
}

var (
	reJinjaExpression = regexp.MustCompile(`{{[^}]*}}`)
	reJinjaStatement  = regexp.MustCompile(`{%[^%]*%}`)
	reJinjaComment    = regexp.MustCompile(`{#[^#]*#}`)
	reHasJinja        = regexp.MustCompile(`(?s){[{%#].*[%#}]}`)
	reHasGlob         = regexp.MustCompile(`[\]\[*?]`)
	reFQCNOrName      = regexp.MustCompile(`^\w+(\.\w+){2,100}$|^\w+$`)
)

func unjinja(s string) string {
	s = reJinjaExpression.ReplaceAllString(s, "JINJA_EXPRESSION")
	s = reJinjaStatement.ReplaceAllString(s, "JINJA_STATEMENT")
	return reJinjaComment.ReplaceAllString(s, "JINJA_COMMENT")
}

// taskRules runs every task-scoped rule over one file.
func taskRules(f *parse.File, opt Options) []Finding {
	prefix := newLoopVarPrefix(f, opt)
	var out []Finding
	for _, t := range f.Tasks() {
		out = append(out, noChangedWhen(f, t)...)
		out = append(out, commandInsteadOfModule(f, t)...)
		out = append(out, commandInsteadOfShell(f, t)...)
		out = append(out, deprecatedLocalAction(f, t)...)
		out = append(out, deprecatedBareVars(f, t)...)
		out = append(out, partialBecomeTask(f, t)...)
		out = append(out, packageLatest(f, t)...)
		out = append(out, keyOrderTask(f, t)...)
		out = append(out, roleNamePathTask(f, t)...)
		out = append(out, nameTask(f, t)...)
		out = append(out, ignoreErrors(f, t)...)
		out = append(out, noTabs(f, t)...)
		out = append(out, riskyFilePermissions(f, t)...)
		out = append(out, riskyOctal(f, t)...)
		out = append(out, riskyShellPipe(f, t)...)
		out = append(out, noHandler(f, t)...)
		out = append(out, noJinjaWhenTask(f, t)...)
		out = append(out, noRelativePaths(f, t)...)
		out = append(out, literalCompare(f, t)...)
		out = append(out, inlineEnvVar(f, t)...)
		out = append(out, avoidImplicit(f, t)...)
		out = append(out, complexityNesting(f, t, opt)...)
		out = append(out, runOnceTask(f, t)...)
		out = append(out, prefix.check(f, t)...)
		if opt.enabled("no-log-password") {
			out = append(out, noLogPassword(f, t)...)
		}
		if opt.enabled("no-prompting") {
			out = append(out, noPromptingTask(f, t)...)
		}
		if opt.enabled("empty-string-compare") {
			out = append(out, emptyStringCompare(f, t)...)
		}
		if opt.enabled("jinja-template-extension") {
			out = append(out, jinjaTemplateExtension(f, t)...)
		}
	}
	return out
}

func noChangedWhen(f *parse.File, t *parse.Task) []Finding {
	if !changedWhenModules[t.Module] {
		return nil
	}
	if t.RawHas("changed_when") || t.HasArg("creates") || t.HasArg("removes") {
		return nil
	}
	if t.RawHas("async") && parse.Str(t.RawGet("poll")) == "0" {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "no-changed-when",
		"Commands should not change things if nothing needs doing.",
		"This command always reports changed. Add changed_when, or a creates guard.")}
}

func commandInsteadOfModule(f *parse.File, t *parse.Task) []Finding {
	if t.Module != "command" && t.Module != "shell" {
		return nil
	}
	fields := strings.Fields(t.CmdArgs())
	if len(fields) == 0 {
		return nil
	}
	executable := parse.Base(fields[0])
	if len(fields) > 1 {
		for _, opt := range commandExecutableOptions[executable] {
			if fields[1] == opt {
				return nil
			}
		}
	}
	module, ok := commandModules[executable]
	if !ok {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "command-instead-of-module",
		fmt.Sprintf("%s used in place of %s module", executable, module),
		fmt.Sprintf("This task runs %s as a command. Use the %s module for idempotency.", executable, module))}
}

func commandInsteadOfShell(f *parse.File, t *parse.Task) []Finding {
	if t.Module != "shell" || t.HasArg("executable") {
		return nil
	}
	if strings.ContainsAny(unjinja(t.CmdArgs()), "&|<>;$\n*[]{}?`!") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "command-instead-of-shell",
		"Use shell only when shell functionality is required.",
		"This task needs no shell features. Use command, it is safer and faster.")}
}

func deprecatedLocalAction(f *parse.File, t *parse.Task) []Finding {
	if !t.RawHas("local_action") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "deprecated-local-action",
		"Do not use 'local_action', use 'delegate_to: localhost'.",
		"local_action is deprecated. Use delegate_to: localhost instead.")}
}

// bareVarSkippedLoops never carry a bare variable worth reporting.
var bareVarSkippedLoops = map[string]bool{
	"with_sequence": true, "with_ini": true, "with_inventory_hostnames": true,
}

// bareVarListLoops accept either an inline list or a single variable.
var bareVarListLoops = map[string]bool{
	"with_nested": true, "with_together": true, "with_flattened": true,
	"with_filetree": true, "with_community.general.filetree": true,
}

func deprecatedBareVars(f *parse.File, t *parse.Task) []Finding {
	var loopType string
	var loopValue *yaml.Node
	for i := 0; i+1 < len(t.Node.Content); i += 2 {
		if strings.HasPrefix(t.Node.Content[i].Value, "with_") {
			loopType = t.Node.Content[i].Value
			loopValue = t.Node.Content[i+1]
			break
		}
	}
	if loopType == "" || bareVarSkippedLoops[loopType] {
		return nil
	}

	match := func(v *yaml.Node) []Finding {
		return matchBareVar(f, t, loopType, loopValue, v)
	}
	switch {
	case bareVarListLoops[loopType]:
		if parse.IsSeq(loopValue) {
			if len(loopValue.Content) == 0 {
				return nil
			}
			// Upstream returns on the first item of the list.
			return match(loopValue.Content[0])
		}
		return match(loopValue)
	case loopType == "with_subelements":
		if parse.IsSeq(loopValue) && len(loopValue.Content) > 0 {
			return match(loopValue.Content[0])
		}
		return nil
	default:
		return match(loopValue)
	}
}

func matchBareVar(f *parse.File, t *parse.Task, loopType string, loopValue, v *yaml.Node) []Finding {
	if !parse.IsScalar(v) || v.Tag != "!!str" {
		return nil
	}
	s := v.Value
	if reHasJinja.MatchString(s) || !reFQCNOrName.MatchString(s) {
		return nil
	}
	valid := loopType == "with_fileglob" && (reHasJinja.MatchString(s) || reHasGlob.MatchString(s))
	valid = valid || (loopType == "with_filetree" && (reHasJinja.MatchString(s) || strings.HasSuffix(s, "/")))
	if valid {
		return nil
	}
	shown := pyReprInner(loopValue)
	msg := fmt.Sprintf(
		"Possible bare variable '%s' used in a '%s' loop. You should use the full variable syntax ('{{ %s }}') or convert it to a list if that is not really a variable.",
		shown, loopType, shown)
	nativeMsg := fmt.Sprintf("This loop uses the bare variable '%s'. Wrap it in {{ }}.", shown)
	return []Finding{onLine(f, t.Pos.Line, "deprecated-bare-vars", msg, nativeMsg)}
}

// pyReprInner renders a value the way Python's str.format does: plain for
// strings, repr-like for containers.
func pyReprInner(n *yaml.Node) string {
	if parse.IsScalar(n) {
		return n.Value
	}
	return pyRepr(n)
}

func partialBecomeTask(f *parse.File, t *parse.Task) []Finding {
	if !t.RawHas("become_user") || t.RawHas("become") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "partial-become[task]",
		"``become_user`` should have a corresponding ``become`` at the same level as itself.",
		"This task sets become_user without become. Set both, or neither.")}
}

func packageLatest(f *parse.File, t *parse.Task) []Finding {
	if !packageManagers[t.Module] {
		return nil
	}
	if t.ArgTruthy("version") || t.ArgTruthy("update_only") ||
		t.ArgTruthy("only_upgrade") || t.ArgTruthy("download_only") {
		return nil
	}
	if t.ArgText("state") != "latest" {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "package-latest",
		"Package installs should not use latest.",
		"This package has no pinned version. Pin one so installs stay reproducible.")}
}

func roleNamePathTask(f *parse.File, t *parse.Task) []Finding {
	if !roleImportActions[t.ModuleOriginal] {
		return nil
	}
	name := t.ArgText("name")
	if !strings.Contains(name, "/") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "role-name[path]",
		fmt.Sprintf("Avoid using paths when importing roles. (%s)", name),
		rolePathNativeMsg)}
}
