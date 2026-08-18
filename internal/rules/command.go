package rules

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// rePipefail matches a `set` invocation that turns the pipefail option on,
// anywhere in a multi-line script.
var rePipefail = regexp.MustCompile(`(?m)^\s*set.*[+-][A-Za-z]*o\s*pipefail`)

// commandOptionArgs are the options the command module itself accepts. Any
// other argument means the author wrote something the module will not read,
// which for `command` is almost always an inline environment variable.
var commandOptionArgs = map[string]bool{
	"argv": true, "chdir": true, "cmd": true, "creates": true,
	"executable": true, "expand_argument_vars": true, "removes": true,
	"stdin": true, "stdin_add_newline": true, "strip_empty_ends": true,
	"_raw_params": true,
}

func riskyShellPipe(f *parse.File, t *parse.Task) []Finding {
	if t.Module != "shell" || truthy(t.RawGet("ignore_errors")) {
		return nil
	}
	if strings.Contains(t.ArgText("executable"), "pwsh") {
		return nil
	}
	cmd := unjinja(t.CmdArgs())
	if !hasBarePipe(cmd) || rePipefail.MatchString(cmd) || t.ArgTruthy("ignore_errors") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "risky-shell-pipe",
		"Shells that use pipes should set the pipefail option.",
		"This shell pipe lacks pipefail, hiding upstream failures. Add set -o pipefail.")}
}

// hasBarePipe reports a `|` that is not part of a `||`, that is, a real
// pipeline rather than a boolean or.
func hasBarePipe(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '|' {
			continue
		}
		if i > 0 && s[i-1] == '|' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '|' {
			continue
		}
		return true
	}
	return false
}

func inlineEnvVar(f *parse.File, t *parse.Task) []Finding {
	if t.Module != "command" {
		return nil
	}
	fields := strings.Fields(t.CmdArgs())
	if len(fields) == 0 {
		return nil
	}
	match := strings.Contains(fields[0], "=")
	for arg := range t.Args {
		if !commandOptionArgs[arg] {
			match = true
			break
		}
	}
	if !match {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "inline-env-var",
		"Command module does not accept setting environment variables inline.",
		"This command sets an env var inline. Use the environment: keyword instead.")}
}

// truthy evaluates a YAML node the way Python evaluates the value ansible's
// loader produced for it, including the YAML 1.1 boolean spellings that
// ansible honours and gopkg.in/yaml.v3 leaves as plain strings.
func truthy(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			return false
		case "!!int", "!!float":
			return n.Value != "0" && n.Value != "0.0"
		}
		switch strings.ToLower(n.Value) {
		case "true", "yes", "on", "y":
			return true
		case "false", "no", "off", "n":
			return false
		}
		return n.Value != ""
	case yaml.SequenceNode, yaml.MappingNode:
		return len(n.Content) > 0
	}
	return false
}
