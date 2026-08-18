package rules

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// keyOrderIndex ranks a key according to ansible-lint's SORTER_TASKS:
// `name` first, `block`/`rescue`/`always` last, everything else in between.
func keyOrderIndex(key string) int {
	switch key {
	case "name":
		return 0
	case "block":
		return 2
	case "rescue":
		return 3
	case "always":
		return 4
	default:
		return 1
	}
}

func sortedKeys(keys []string) []string {
	out := append([]string(nil), keys...)
	sort.SliceStable(out, func(i, j int) bool {
		return keyOrderIndex(out[i]) < keyOrderIndex(out[j])
	})
	return out
}

func keyOrderTask(f *parse.File, t *parse.Task) []Finding {
	var keys []string
	for _, k := range t.RawKeys() {
		if !strings.HasPrefix(k, "_") {
			keys = append(keys, k)
		}
	}
	want := sortedKeys(keys)
	if strings.Join(keys, ",") == strings.Join(want, ",") {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "key-order[task]",
		"You can improve the task key order to: "+strings.Join(want, ", "),
		"This task's keys are out of order. Put name first, block/rescue/always last.")}
}

func keyOrderPlay(f *parse.File, play *yaml.Node) []Finding {
	keys := parse.MapKeys(play)
	want := sortedKeys(keys)
	if strings.Join(keys, ",") == strings.Join(want, ",") {
		return nil
	}
	return []Finding{at(f, play, "key-order[play]",
		"You can improve the play key order to: "+strings.Join(want, ", "),
		"This play's keys are out of order. Put name first, tasks after the settings.")}
}

func partialBecomePlay(f *parse.File, play *yaml.Node) []Finding {
	if !parse.MapHas(play, "become_user") || parse.MapHas(play, "become") {
		return nil
	}
	return []Finding{at(f, play, "partial-become[play]",
		"``become_user`` should have a corresponding ``become`` at the same level as itself.",
		"This play sets become_user without become. Set both, or neither.")}
}

// roleNamePathPlay flags path-style role references in a play's `roles:` list.
// Upstream reports a string entry at the play's position and a mapping entry at
// its own position.
func roleNamePathPlay(f *parse.File, play *yaml.Node) []Finding {
	roles := parse.MapGet(play, "roles")
	if !parse.IsSeq(roles) {
		return nil
	}
	var out []Finding
	for _, role := range roles.Content {
		name := ""
		// pos is per-entry: a bare string role reports at the play, a mapping
		// role at the mapping. Hoisting this out of the loop leaks one entry's
		// position into the next and misplaces a string entry that follows a
		// mapping one.
		pos := parse.NodePos(play)
		if parse.IsMap(role) {
			pos = parse.NodePos(role)
			name = parse.Str(parse.MapGet(role, "role"))
		} else if parse.IsScalar(role) {
			name = role.Value
		}
		if !strings.Contains(name, "/") {
			continue
		}
		out = append(out, Finding{
			Path: f.Path, Line: pos.Line, Column: pos.Column, Tag: "role-name[path]",
			Message:       fmt.Sprintf("Avoid using paths when importing roles. (%s)", name),
			NativeMessage: rolePathNativeMsg,
		})
	}
	return out
}

func playRules(f *parse.File, opt Options) []Finding {
	var out []Finding
	for _, play := range f.Plays() {
		// Upstream's _should_skip_play: the tag silences the play's own
		// rules only; tasks keep their findings unless tagged themselves.
		if skipTagIn(parse.MapGet(play, "tags")) {
			continue
		}
		out = append(out, namePlay(f, play)...)
		out = append(out, keyOrderPlay(f, play)...)
		out = append(out, partialBecomePlay(f, play)...)
		out = append(out, roleNamePathPlay(f, play)...)
		out = append(out, noJinjaWhenPlay(f, play)...)
		out = append(out, runOncePlay(f, play)...)
		out = append(out, complexityPlay(f, play, opt)...)
		if opt.enabled("no-prompting") {
			out = append(out, noPromptingPlay(f, play)...)
		}
	}
	return out
}
