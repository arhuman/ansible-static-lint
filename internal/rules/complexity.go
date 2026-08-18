package rules

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// reToIdentifier matches the runs ansible-lint replaces to turn a role
// directory name into a variable name.
var reToIdentifier = regexp.MustCompile(`[\s-]+`)

func complexityPlay(f *parse.File, play *yaml.Node, opt Options) []Finding {
	tasks := parse.MapGet(play, "tasks")
	if !parse.IsSeq(tasks) || len(tasks.Content) <= opt.maxTasks() {
		return nil
	}
	return []Finding{warnAt(f, play, "complexity[play]",
		fmt.Sprintf("Maximum tasks allowed in a play is %d.", opt.maxTasks()),
		fmt.Sprintf("This play exceeds %d tasks, which hurts readability. Split it with include_tasks.", opt.maxTasks()))}
}

func complexityNesting(f *parse.File, t *parse.Task, opt Options) []Finding {
	if !t.IsBlock || t.BlockDepth <= opt.maxBlockDepth() {
		return nil
	}
	return []Finding{warnOnLine(f, t.Pos.Line, "complexity[nesting]",
		fmt.Sprintf("Replace nested block with an include_tasks to make code easier to maintain. Maximum block depth allowed is %d.", opt.maxBlockDepth()),
		fmt.Sprintf("This block nests past %d levels, which hurts readability. Use include_tasks.", opt.maxBlockDepth()))}
}

// complexityTasks caps the tasks a task or handler file may hold. A playbook is
// judged per play instead, by complexityPlay.
func complexityTasks(f *parse.File, opt Options) []Finding {
	if f.Kind != "tasks" && f.Kind != "handlers" {
		return nil
	}
	count := len(f.Tasks())
	if count <= opt.maxTasks() {
		return nil
	}
	fd := whole(f, "complexity[tasks]",
		fmt.Sprintf("File contains %d tasks, exceeding the maximum of %d. Consider using `ansible.builtin.include_tasks` to split the tasks into smaller files.", count, opt.maxTasks()),
		fmt.Sprintf("This file holds %d tasks, above the %d allowed. Split it with include_tasks.", count, opt.maxTasks()))
	fd.Warning = true
	return []Finding{fd}
}

// runOncePlay flags the `free` strategy, under which `run_once` no longer means
// what its name says. Upstream reports it at line 1 rather than at the keyword.
func runOncePlay(f *parse.File, play *yaml.Node) []Finding {
	if parse.Str(parse.MapGet(play, "strategy")) != "free" {
		return nil
	}
	return []Finding{whole(f, "run-once[play]", "Play uses strategy: free",
		"strategy: free breaks the run_once guarantee. Drop it, or stop using run_once.")}
}

func runOnceTask(f *parse.File, t *parse.Task) []Finding {
	if f.Kind != "playbook" || !truthy(t.RawGet("run_once")) {
		return nil
	}
	return []Finding{onLine(f, t.Pos.Line, "run-once[task]",
		"Using run_once may behave differently if strategy is set to free.",
		"run_once behaves differently under the free strategy. Check the play strategy.")}
}

// loopVarPrefix is the compiled form of the `loop_var_prefix` option for one
// file, or nil when the option is unset or the file is not part of a role.
type loopVarPrefix struct {
	re      *regexp.Regexp
	pattern string
	role    string
}

// newLoopVarPrefix binds the option to a file's role. It returns nil whenever
// the rule is inert, which is the default: upstream ships `loop_var_prefix`
// unset.
func newLoopVarPrefix(f *parse.File, opt Options) *loopVarPrefix {
	if opt.LoopVarPrefix == "" || f.Role == "" {
		return nil
	}
	role := reToIdentifier.ReplaceAllString(f.Role, "_")
	// Anchoring reproduces Python's re.match, which only ever matches at the
	// start of the variable name.
	re, err := regexp.Compile(`\A(?:` + strings.ReplaceAll(opt.LoopVarPrefix, "{role}", role) + `)`)
	if err != nil {
		return nil
	}
	return &loopVarPrefix{re: re, pattern: opt.LoopVarPrefix, role: role}
}

func (p *loopVarPrefix) check(f *parse.File, t *parse.Task) []Finding {
	if p == nil {
		return nil
	}
	anchor := loopAnchor(t)
	if anchor == nil {
		return nil
	}
	loopVar := parse.MapGet(t.RawGet("loop_control"), "loop_var")
	if name := parse.Str(loopVar); name != "" {
		if p.re.MatchString(name) {
			return nil
		}
		return []Finding{at(f, loopVar, "loop-var-prefix[wrong]",
			fmt.Sprintf("Loop variable name does not match /%s/ regex, where role=%s.", p.pattern, p.role),
			fmt.Sprintf("This loop_var does not match /%s/. Rename it to avoid shadowing.", p.pattern))}
	}
	return []Finding{at(f, anchor, "loop-var-prefix[missing]",
		fmt.Sprintf("Replace unsafe implicit `item` loop variable by adding a `loop_var` that is matching /%s/ regex.", p.pattern),
		fmt.Sprintf("This loop has no loop_var, so item can shadow. Add one matching /%s/.", p.pattern))}
}

// loopAnchor returns the node a loop-var-prefix finding is reported on: the
// `loop` value, or the `with_*` key itself.
func loopAnchor(t *parse.Task) *yaml.Node {
	if v := t.RawGet("loop"); v != nil {
		return v
	}
	for i := 0; i+1 < len(t.Node.Content); i += 2 {
		if strings.HasPrefix(t.Node.Content[i].Value, "with_") {
			return t.Node.Content[i]
		}
	}
	return nil
}
