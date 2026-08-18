package rules_test

import (
	"strings"
	"testing"
)

// Expectations below were verified against ansible-lint 26.8.0 output; the
// probe trees are recorded in docs/design/static-yaml-and-var-naming.md.

func TestVarNamingVarsFileSubtags(t *testing.T) {
	content := "---\nCamelCase: 1\nimport: 2\nrésumé: 3\nlipsum: 4\nplaybook_dir: 5\nansible_facts: 6\n\"{{ jinja }}\": 7\n123: 8\n"
	got := lintInline(t, "group_vars/all.yml", content)
	assertTags(t, got, []string{
		"var-naming[non-string]",
		"var-naming[pattern]",
		"var-naming[no-keyword]",
		"var-naming[non-ascii]",
		"var-naming[no-reserved]",
		"var-naming[read-only]",
	})
}

// A tasks file reports a task's vars twice, once through the play-shaped pass
// (no suffix) and once through the task pass (suffixed), exactly as upstream
// does; a play in a playbook reports its vars once.
func TestVarNamingTaskVarsAreDoubled(t *testing.T) {
	content := "---\n- name: T\n  ansible.builtin.debug:\n    msg: hi\n  vars:\n    BadVar: 1\n"
	got := lintInlineMessages(t, "tasks/main.yml", content)
	want := []string{
		"Variables names should match ^[a-z_][a-z0-9_]*$ regex. (BadVar)",
		"Variables names should match ^[a-z_][a-z0-9_]*$ regex. (BadVar) (vars: BadVar)",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A noqa on the task's own lines silences only the task-pass finding: the
// play-shaped pass filters on the finding's line alone, as upstream does.
func TestVarNamingNoqaScopeAsymmetry(t *testing.T) {
	content := "---\n- name: T # noqa: var-naming[pattern]\n  ansible.builtin.debug:\n    msg: hi\n  vars:\n    BadVar: 1\n"
	got := lintInlineMessages(t, "tasks/main.yml", content)
	if len(got) != 1 || strings.Contains(got[0], "(vars:") {
		t.Errorf("want only the unsuffixed play-pass finding, got %q", got)
	}
	// On the key's own line, both paths go quiet.
	content = "---\n- name: T\n  ansible.builtin.debug:\n    msg: hi\n  vars:\n    BadVar: 1 # noqa: var-naming[pattern]\n"
	if got := lintInlineMessages(t, "tasks/main.yml", content); len(got) != 0 {
		t.Errorf("want no findings, got %q", got)
	}
}

func TestVarNamingRolePrefixInRoleTasks(t *testing.T) {
	content := "---\n- name: R\n  ansible.builtin.command: echo hi\n  register: plain_result\n  changed_when: false\n\n- name: S\n  ansible.builtin.set_fact:\n    plain_fact: 1\n    cacheable: true\n\n- name: OK\n  ansible.builtin.command: echo hi\n  register: myrole_result\n  changed_when: false\n"
	got := lintInlineMessages(t, "roles/myrole/tasks/main.yml", content)
	want := []string{
		"Variables names from within roles should use myrole_ as a prefix. (register: plain_result)",
		"Variables names from within roles should use myrole_ as a prefix. (set_fact: plain_fact)",
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVarNamingIncludeRolePrefix(t *testing.T) {
	content := "---\n- name: P\n  hosts: all\n  roles:\n    - role: myrole\n      vars:\n        unprefixed: 1\n      badkey: 2\n  tasks:\n    - name: I\n      ansible.builtin.include_role:\n        name: myrole\n      vars:\n        other_bad: 1\n"
	got := lintInlineMessages(t, "playbook.yml", content)
	want := []string{
		"Variables names from within roles should use myrole_ as a prefix. (vars: unprefixed)",
		"Variables names from within roles should use myrole_ as a prefix. (vars: badkey)",
		"Variables names from within roles should use myrole_ as a prefix. (vars: other_bad)",
	}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("got %q, want %q", got, want)
	}
}

// lintInlineMessages is lintInline returning upstream messages instead of tags.
func lintInlineMessages(t *testing.T, relPath, content string) []string {
	t.Helper()
	found := lintInlineFindings(t, relPath, content)
	msgs := make([]string, 0, len(found))
	for _, f := range found {
		msgs = append(msgs, f.Message)
	}
	return msgs
}
