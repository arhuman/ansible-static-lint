package rules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/discover"
	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// benchSizes are chosen to show shape, not just cost. Each step quadruples the
// task count, so a linear pass reports roughly 4x the previous ns/op and a
// quadratic one roughly 16x: reading three rows says which one this is, which a
// single size never could.
var benchSizes = []int{64, 256, 1024}

// playbookOfTasks renders a playbook of n tasks. Every task trips at least one
// rule, so the rule bodies do real work rather than returning at their first
// guard. With noqa set, each task also carries a suppression comment, which is
// what puts entries in the noqa map the skip resolution walks.
func playbookOfTasks(n int, noqa bool) string {
	var b strings.Builder
	b.WriteString("---\n- name: Bench play\n  hosts: localhost\n  tasks:\n")
	for i := range n {
		fmt.Fprintf(&b, "    - name: task %d\n", i)
		b.WriteString("      ansible.builtin.command: echo hi")
		if noqa {
			b.WriteString("  # noqa no-changed-when")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeFixture puts content at a stable path inside a temporary directory and
// returns both halves of what parse.Load wants.
func writeFixture(tb testing.TB, name, content string) (rel, abs string) {
	tb.Helper()
	abs = filepath.Join(tb.TempDir(), name)
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
	return name, abs
}

// BenchmarkLintPlaybook measures the unit the linter actually repeats: load one
// file, run every rule over it. Parsing stays inside the timer because that is
// how a run pays for it, once per file.
func BenchmarkLintPlaybook(b *testing.B) {
	for _, n := range benchSizes {
		content := playbookOfTasks(n, false)
		rel, abs := writeFixture(b, "site-playbook.yml", content)
		kind := discover.KindOf(rel)

		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				f := parse.Load(rel, abs, kind)
				if got := rules.File(f, rules.Options{}); len(got) == 0 {
					b.Fatal("fixture tripped no rule, so this measures the guards only")
				}
			}
		})
	}
}

// BenchmarkLintPlaybookWithNoqa is the same playbook with a suppression comment
// on every task. The difference between this and BenchmarkLintPlaybook is the
// cost of resolving skips, which is the part that has to stay linear in the
// number of tasks.
func BenchmarkLintPlaybookWithNoqa(b *testing.B) {
	for _, n := range benchSizes {
		content := playbookOfTasks(n, true)
		rel, abs := writeFixture(b, "site-playbook.yml", content)
		kind := discover.KindOf(rel)

		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				f := parse.Load(rel, abs, kind)
				rules.File(f, rules.Options{})
			}
		})
	}
}

// BenchmarkKindOf covers the classification every discovered path pays for,
// before any file is opened.
func BenchmarkKindOf(b *testing.B) {
	paths := []string{
		"playbooks/site.yml",
		"roles/common/tasks/main.yml",
		"roles/common/meta/main.yml",
		"group_vars/all.yml",
		"plugins/modules/thing.py",
		"templates/config.j2",
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range paths {
			discover.KindOf(p)
		}
	}
}
