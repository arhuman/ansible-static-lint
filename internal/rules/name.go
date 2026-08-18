package rules

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

var reTemplatedInside = regexp.MustCompile(`^.*\{\{.*\}\}.*\w.*$`)

func nameTask(f *parse.File, t *parse.Task) []Finding {
	nameNode := t.RawGet("name")
	if nameNode == nil || !parse.IsScalar(nameNode) || nameNode.Value == "" {
		return []Finding{onLine(f, t.Pos.Line, "name[missing]", "All tasks should be named.",
			"This task has no name. Add one so logs can identify it.")}
	}
	return checkName(f, nameNode, nameNode.Value)
}

func namePlay(f *parse.File, play *yaml.Node) []Finding {
	nameNode := parse.MapGet(play, "name")
	if nameNode == nil {
		return []Finding{at(f, play, "name[play]", "All plays should be named.",
			"This play has no name. Add one so logs can identify it.")}
	}
	return checkName(f, nameNode, parse.Str(nameNode))
}

// checkName implements the casing and template checks shared by plays and
// tasks. The name[prefix] subtag is not emitted because upstream only raises it
// when explicitly enabled, but the prefix is still stripped before the casing
// check so that prefixed names are judged on their own first letter.
func checkName(f *parse.File, node *yaml.Node, name string) []Finding {
	var out []Finding
	effective := name
	if prefix := taskNamePrefix(f); prefix != "" && strings.HasPrefix(name, prefix) {
		effective = name[len(prefix):]
	}
	if r := firstRune(effective); r != 0 && unicode.IsLetter(r) && unicode.IsLower(r) {
		out = append(out, at(f, node, "name[casing]", "All names should start with an uppercase letter.",
			"This name does not start with a capital. Capitalize the first word."))
	}
	if reTemplatedInside.MatchString(name) {
		out = append(out, at(f, node, "name[template]", "Jinja templates should only be at the end of 'name'",
			"This name has a Jinja expression before its end. Move the template to the end."))
	}
	return out
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// taskNamePrefix reproduces ansible-lint's default `{stem} | ` prefix for
// non-main task files, including the parent directory when it is not the
// `tasks` directory itself.
func taskNamePrefix(f *parse.File) string {
	if f.Kind != "tasks" {
		return ""
	}
	stem := strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
	parts := []string{stem}
	if parent := filepath.Base(filepath.Dir(f.Path)); parent != "" && !strings.HasPrefix(parent, "tasks") {
		parts = append([]string{parent}, parts...)
	}
	if len(parts) == 1 && stem == "main" {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
		b.WriteString(" | ")
	}
	return b.String()
}
