package rules

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

var (
	reLiteralBoolCompare = regexp.MustCompile(`[=!]= ?(True|true|False|false)`)
	reEmptyStringCompare = regexp.MustCompile(`[=!]= ?("{2}|'{2})`)
)

// changedMarkers are the ways a condition can read a previous task's changed
// status.
var changedMarkers = []string{".changed", "|changed", `["changed"]`, `['changed']`, "is changed"}

// changedInWhen reports a condition that tests only for a change. A condition
// combining several terms is left alone: moving it to a handler would drop the
// other terms.
func changedInWhen(item string) bool {
	for _, word := range strings.Fields(item) {
		if word == "and" || word == "or" || word == "not" {
			return false
		}
	}
	for _, marker := range changedMarkers {
		if strings.Contains(item, marker) {
			return true
		}
	}
	return false
}

func noHandler(f *parse.File, t *parse.Task) []Finding {
	if t.Kind == "handlers" {
		return nil
	}
	when := t.RawGet("when")
	if when == nil {
		return nil
	}
	match := false
	switch {
	case isString(when):
		match = changedInWhen(when.Value)
	case parse.IsSeq(when) && len(when.Content) == 1:
		match = isString(when.Content[0]) && changedInWhen(when.Content[0].Value)
	}
	if !match {
		return nil
	}
	return []Finding{at(f, when, "no-handler",
		"Tasks that run when changed should likely be handlers.",
		"This task runs only when something changed. Move it to a handler.")}
}

// jinjaWhenNativeMsg is shared by the task and the play-level role check, which
// report the same defect at two positions.
const jinjaWhenNativeMsg = "This when already evaluates as Jinja. Remove the redundant {{ }}."

// jinjaFreeCondition reports whether a `when` value is already a raw Jinja
// expression, which is what ansible evaluates it as, so wrapping it in `{{ }}`
// is redundant.
func jinjaFreeCondition(when *yaml.Node) bool {
	if parse.IsSeq(when) {
		for _, item := range when.Content {
			if isString(item) && strings.Contains(item.Value, "{{") && strings.Contains(item.Value, "}}") {
				return false
			}
		}
		return true
	}
	if !isString(when) {
		return true
	}
	return !strings.Contains(when.Value, "{{") && !strings.Contains(when.Value, "}}")
}

func noJinjaWhenTask(f *parse.File, t *parse.Task) []Finding {
	when := t.RawGet("when")
	if when == nil || jinjaFreeCondition(when) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "no-jinja-when", "No Jinja2 in when.", jinjaWhenNativeMsg)}
}

// noJinjaWhenPlay checks the `when` of each role entry in a play, which is not
// reached by the task walk.
func noJinjaWhenPlay(f *parse.File, play *yaml.Node) []Finding {
	roles := parse.MapGet(play, "roles")
	if !parse.IsSeq(roles) {
		return nil
	}
	var out []Finding
	for _, role := range roles.Content {
		when := parse.MapGet(role, "when")
		if when == nil || jinjaFreeCondition(when) {
			continue
		}
		out = append(out, at(f, role, "no-jinja-when", "No Jinja2 in when.", jinjaWhenNativeMsg))
	}
	return out
}

func literalCompare(f *parse.File, t *parse.Task) []Finding {
	if !conditionMatches(t, reLiteralBoolCompare) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "literal-compare",
		"Don't compare to literal True/False.",
		"This condition compares to literal True or False. Use the value directly.")}
}

func emptyStringCompare(f *parse.File, t *parse.Task) []Finding {
	if !conditionMatches(t, reEmptyStringCompare) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "empty-string-compare",
		"Don't compare to empty string.",
		"This condition compares to an empty string. Use its truthiness instead.")}
}

// conditionMatches reports whether any `when` anywhere inside the task, including
// the ones its nested blocks carry, matches re.
func conditionMatches(t *parse.Task, re *regexp.Regexp) bool {
	found := false
	parse.EachNested(t.Node, func(key, value *yaml.Node) {
		if found || key == nil || key.Value != "when" {
			return
		}
		switch {
		case isString(value):
			found = re.MatchString(value.Value)
		case parse.IsSeq(value):
			for _, item := range value.Content {
				if isString(item) && re.MatchString(item.Value) {
					found = true
					return
				}
			}
		}
	})
	return found
}

// isString reports whether a node is a YAML string, as opposed to a boolean or
// a number that Python would not run a string check against.
func isString(n *yaml.Node) bool { return parse.IsScalar(n) && n.Tag == "!!str" }
