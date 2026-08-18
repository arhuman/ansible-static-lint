package rules

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// checkModeCondition is the one `ignore_errors` value that is deliberate: it
// only ignores errors during a check-mode dry run.
const checkModeCondition = "{{ ansible_check_mode }}"

// tabAllowedKeys are the module arguments whose value is a regexp or a literal
// line, where a tab is the author's intent rather than an accident.
var tabAllowedKeys = map[string]bool{
	"insertafter": true, "insertbefore": true, "regexp": true, "line": true,
}

// relativeSrcFolders maps a module to the role subdirectory ansible already
// searches for its `src`, which is what makes a `../` prefix redundant.
var relativeSrcFolders = map[string]string{
	"copy": "files", "win_copy": "files",
	"template": "templates", "win_template": "win_templates",
}

var templateModules = map[string]bool{
	"template": true, "ansible.legacy.template": true,
}

var pauseModules = map[string]bool{
	"pause": true, "ansible.builtin.pause": true,
}

func ignoreErrors(f *parse.File, t *parse.Task) []Finding {
	ignore := t.RawGet("ignore_errors")
	if !truthy(ignore) || parse.Str(ignore) == checkModeCondition {
		return nil
	}
	if truthy(t.RawGet("register")) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "ignore-errors",
		"Use failed_when and specify error conditions instead of using ignore_errors.",
		"ignore_errors hides every failure, not just the expected one. Use failed_when.")}
}

func noTabs(f *parse.File, t *parse.Task) []Finding {
	const msg = "Most files should not contain tabs."
	const nativeMsg = "This line contains a tab character. Use spaces only."
	var out []Finding
	parse.EachNested(t.Node, func(key, value *yaml.Node) {
		if key != nil && strings.Contains(key.Value, "\t") && !reHasJinja.MatchString(key.Value) {
			out = append(out, at(f, key, "no-tabs", msg, nativeMsg))
		}
		if !isString(value) || !strings.Contains(value.Value, "\t") || reHasJinja.MatchString(value.Value) {
			return
		}
		if key != nil && tabAllowedKeys[key.Value] {
			return
		}
		out = append(out, at(f, value, "no-tabs", msg, nativeMsg))
	})
	return out
}

func noRelativePaths(f *parse.File, t *parse.Task) []Finding {
	folder, ok := relativeSrcFolders[t.Module]
	if !ok || !t.HasArg("src") {
		return nil
	}
	if !strings.Contains(t.ArgText("src"), "../"+folder) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "no-relative-paths",
		"The src argument should not use a relative path.",
		fmt.Sprintf("This src climbs out of the role with ../%s. Name the file alone.", folder))}
}

func avoidImplicit(f *parse.File, t *parse.Task) []Finding {
	if t.Module != "copy" {
		return nil
	}
	content, ok := t.Args["content"]
	// An absent content defaults to the empty string, which is a string, and a
	// free-form `content=` always yields one too.
	if !ok || content.Node == nil {
		return nil
	}
	if isString(content.Node) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "avoid-implicit", "Avoid implicit behaviors",
		"This copy content is not a string. Convert it with a filter such as to_nice_json.")}
}

func jinjaTemplateExtension(f *parse.File, t *parse.Task) []Finding {
	if !templateModules[t.Module] {
		return nil
	}
	src := t.ArgText("src")
	if src == "" || strings.Contains(src, "{{") || strings.HasSuffix(src, ".j2") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "jinja-template-extension",
		"Template source file should have a .j2 extension",
		"This template source does not end in .j2. Rename it with a .j2 extension.")}
}

func noLogPassword(f *parse.File, t *parse.Task) []Finding {
	if !passesSecret(t) || !hasLoop(t) || logsNothing(t.RawGet("no_log")) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "no-log-password", "Password should not be logged.",
		"This looping task logs a password. Set no_log: true.")}
}

// passesSecret reports whether any module argument names a password. Locking an
// account without setting one passes no secret, even though `password_lock`
// contains the word.
func passesSecret(t *parse.Task) bool {
	if t.ModuleOriginal == "ansible.builtin.user" &&
		t.ArgTruthy("password_lock") && !t.HasArg("password") {
		return false
	}
	for param := range t.Args {
		if strings.Contains(param, "password") {
			return true
		}
	}
	return false
}

// logsNothing reports a no_log that keeps the secret out of the log. A
// templated value cannot be evaluated statically, so it is taken on trust.
func logsNothing(noLog *yaml.Node) bool {
	if isString(noLog) && strings.HasPrefix(noLog.Value, "{{") && strings.HasSuffix(noLog.Value, "}}") {
		return true
	}
	return truthy(noLog)
}

func hasLoop(t *parse.Task) bool {
	for _, k := range t.RawKeys() {
		if k == "loop" || strings.HasPrefix(k, "with_") {
			return true
		}
	}
	return false
}

func noPromptingTask(f *parse.File, t *parse.Task) []Finding {
	if !pauseModules[t.ModuleOriginal] {
		return nil
	}
	// A pause with a duration does not wait for a human.
	if t.ArgTruthy("minutes") || t.ArgTruthy("seconds") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "no-prompting", "Disallow prompting",
		"This pause waits for a human, blocking unattended runs. Give it a duration.")}
}

func noPromptingPlay(f *parse.File, play *yaml.Node) []Finding {
	prompts := parse.MapGet(play, "vars_prompt")
	if !parse.IsSeq(prompts) || len(prompts.Content) == 0 {
		return nil
	}
	return []Finding{at(f, prompts.Content[0], "no-prompting", "Play uses vars_prompt",
		"vars_prompt blocks unattended runs. Pass the value with extra-vars or a vault.")}
}
