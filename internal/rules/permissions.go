package rules

import (
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// permissionModules take a `mode` and create the file they act on, so leaving
// mode unset makes the resulting permissions depend on the ansible version.
var permissionModules = map[string]bool{
	"archive": true, "community.general.archive": true, "assemble": true,
	"copy": true, "file": true, "get_url": true, "replace": true, "template": true,
}

// createModules only touch a file when their `create` option is on; the value
// is that option's default for the module.
var createModules = map[string]bool{
	"blockinfile": false, "lineinfile": false,
	"htpasswd": true, "community.general.htpasswd": true,
	"ini_file": true, "community.general.ini_file": true,
}

// preserveModules are the only modules accepting `mode: preserve`.
var preserveModules = map[string]bool{"copy": true, "template": true}

// octalModules interpret `mode` as a permission bitmask.
var octalModules = map[string]bool{
	"assemble": true, "copy": true, "file": true, "ini_file": true,
	"lineinfile": true, "replace": true, "synchronize": true, "template": true,
	"unarchive": true,
}

func riskyFilePermissions(f *parse.File, t *parse.Task) []Finding {
	if !riskyMode(t) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "risky-file-permissions",
		"File permissions unset or incorrect.",
		"This task creates a file without a mode. Set mode explicitly.")}
}

// riskyMode reports a task whose resulting file permissions depend on the
// ansible version: no mode where one is needed, or the special `preserve` value
// on a module that does not accept it.
func riskyMode(t *parse.Task) bool {
	defaultCreate, isCreate := createModules[t.Module]
	if !permissionModules[t.Module] && !isCreate {
		return false
	}
	// An `args:` that is not a mapping is a jinja template; its keys are
	// unknowable, so upstream declines to judge the task.
	if args := t.RawGet("args"); args != nil && !parse.IsMap(args) {
		return false
	}
	// With a mode set, the only remaining defect is an unsupported `preserve`.
	if mode, ok := modeArg(t); ok {
		return mode.Text == "preserve" && !preserveModules[t.Module]
	}
	if isCreate {
		if t.HasArg("create") {
			return t.ArgTruthy("create")
		}
		return defaultCreate
	}
	return createsSomething(t)
}

// createsSomething reports whether a task can bring a file into existence, and
// so owes it a mode. A removal, a symlink, a recursive chmod and a `file` task
// left at its default state all either cannot carry one mode or never create
// anything; `replace` edits a file whose permissions it correctly preserves.
func createsSomething(t *parse.Task) bool {
	switch t.ArgText("state") {
	case "absent", "link":
		return false
	}
	if t.ArgTruthy("recurse") {
		return false
	}
	if t.Module == "file" && (!t.HasArg("state") || t.ArgText("state") == "file") {
		return false
	}
	return t.Module != "replace"
}

// modeArg returns the `mode` module argument. An absent or null mode reports
// false, matching Python's `get("mode", None)` yielding None for both.
func modeArg(t *parse.Task) (parse.Arg, bool) {
	a, ok := t.Args["mode"]
	if !ok {
		return a, false
	}
	if a.Node != nil && a.Node.Tag == "!!null" {
		return a, false
	}
	return a, true
}

func riskyOctal(f *parse.File, t *parse.Task) []Finding {
	if !octalModules[t.Module] {
		return nil
	}
	mode, ok := t.Args["mode"]
	// A free-form `mode=0644` parses to a string, which upstream never judges.
	if !ok || mode.Node == nil || mode.Node.Kind != yaml.ScalarNode || mode.Node.Tag != "!!int" {
		return nil
	}
	// Base 0 reproduces the YAML integer resolution that turns a leading zero
	// into an octal literal, so `0644` is 420 rather than 644.
	n, err := strconv.ParseInt(mode.Node.Value, 0, 64)
	if err != nil || !invalidPermission(n) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "risky-octal",
		fmt.Sprintf("`mode: %d` should have a string value with leading zero `mode: \"0%o\"` or use symbolic mode.", n, n),
		fmt.Sprintf("This mode is decimal %d, not octal. Quote it as \"0%o\", or use symbolic mode.", n, n))}
}

// invalidPermission reports a mode whose bits are implausible as permissions:
// a write bit without the matching read bit, or a class more generous than the
// one above it. Such a mode is almost always a decimal number the author meant
// to write in octal.
func invalidPermission(mode int64) bool {
	other, group, user := mode%8, (mode>>3)%8, (mode>>6)%8
	// An execute-only class is legitimate: always for the user, and for group
	// and other when the user can execute too. That is how a traversable but
	// unlistable directory is set.
	userExecutes := (mode>>6)%2 == 1
	return writeWithoutRead(other, userExecutes) ||
		writeWithoutRead(group, userExecutes) ||
		writeWithoutRead(user, true) ||
		other > group || other > user || group > user
}

// writeWithoutRead reports a permission class carrying a write or execute bit
// with no read bit.
func writeWithoutRead(class int64, executeAllowed bool) bool {
	if class == 0 || class >= 4 {
		return false
	}
	return class != 1 || !executeAllowed
}
