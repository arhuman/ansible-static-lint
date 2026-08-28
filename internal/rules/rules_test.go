package rules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/discover"
	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// lintInlineFindings writes content to a temporary file at relPath and
// returns the findings reported for it, in output order.
func lintInlineFindings(t *testing.T, relPath, content string) []rules.Finding {
	t.Helper()
	dir := t.TempDir()
	abs := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	found := rules.File(parse.Load(relPath, abs, discover.KindOf(relPath)), rules.Options{})
	rules.Sort(found)
	return rules.Dedupe(found)
}

// lintInline is lintInlineFindings reduced to the reported tags.
func lintInline(t *testing.T, relPath, content string) []string {
	t.Helper()
	found := lintInlineFindings(t, relPath, content)
	tags := make([]string, 0, len(found))
	for _, f := range found {
		tags = append(tags, f.Tag)
	}
	return tags
}

func assertTags(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestTaskRules(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		want    []string
	}{
		{
			name: "no-changed-when fires on a bare command",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Run something
      ansible.builtin.command: /bin/true
`,
			want: []string{"no-changed-when"},
		},
		{
			name: "no-changed-when is silenced by creates",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Run something
      ansible.builtin.command: /bin/true creates=/tmp/x
`,
			want: nil,
		},
		{
			name: "command-instead-of-module maps git to the git module",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Clone
      ansible.builtin.command: git clone blah
      changed_when: false
`,
			want: []string{"command-instead-of-module"},
		},
		{
			name: "command-instead-of-module skips read-only subcommands",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Inspect
      ansible.builtin.command: git log
      changed_when: false
`,
			want: nil,
		},
		{
			name: "command-instead-of-shell fires without shell metacharacters",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Echo
      ansible.builtin.shell: echo hello
      changed_when: false
`,
			want: []string{"command-instead-of-shell"},
		},
		{
			name: "command-instead-of-shell accepts a pipe",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Echo
      ansible.builtin.shell: echo hello | wc -l
      changed_when: false
`,
			// The pipe keeps command-instead-of-shell quiet, and is exactly
			// what risky-shell-pipe exists to flag.
			want: []string{"risky-shell-pipe"},
		},
		{
			name: "deprecated-local-action",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Local
      local_action: ansible.builtin.debug msg=hi
`,
			want: []string{"deprecated-local-action"},
		},
		{
			name: "deprecated-bare-vars on with_items",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Loop
      ansible.builtin.debug:
        msg: "{{ item }}"
      with_items: my_list
`,
			want: []string{"deprecated-bare-vars"},
		},
		{
			name: "deprecated-bare-vars ignores templated values",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Loop
      ansible.builtin.debug:
        msg: "{{ item }}"
      with_items: "{{ my_list }}"
`,
			want: nil,
		},
		{
			name: "partial-become at task and play level",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  become_user: root
  tasks:
    - name: Do
      ansible.builtin.debug:
        msg: hi
      become_user: root
`,
			want: []string{"partial-become[play]", "partial-become[task]"},
		},
		{
			name: "package-latest",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Install
      ansible.builtin.apt:
        name: apache2
        state: latest
`,
			want: []string{"package-latest"},
		},
		{
			name: "package-latest is silenced by a version",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Install
      ansible.builtin.apt:
        name: apache2
        state: latest
        version: "1.2"
`,
			want: nil,
		},
		{
			name: "key-order[task] wants name first",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - when: true
      name: Do
      ansible.builtin.debug:
        msg: hi
`,
			want: []string{"key-order[task]"},
		},
		{
			name: "noqa silences a rule",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Run something # noqa: no-changed-when
      ansible.builtin.command: /bin/true
`,
			want: nil,
		},
		{
			name: "skip_ansible_lint silences a task",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Run something
      ansible.builtin.command: /bin/true
      tags:
        - skip_ansible_lint
`,
			want: nil,
		},
		{
			// Issue 0001: no-handler anchors its finding on the when node,
			// not on the task's first line; the tag must still silence it.
			name: "skip_ansible_lint silences a sub-node-anchored rule",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Do something
      ansible.builtin.command: echo hi
      register: result
      changed_when: false
    - name: React to change
      ansible.builtin.service:
        name: foo
        state: restarted
      when: result.changed
      tags:
        - skip_ansible_lint
`,
			want: nil,
		},
		{
			name: "noqa on the when line silences no-handler",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Do something
      ansible.builtin.command: echo hi
      register: result
      changed_when: false
    - name: React to change
      ansible.builtin.service:
        name: foo
        state: restarted
      when: result.changed # noqa: no-handler
`,
			want: nil,
		},
		{
			// Upstream's _should_skip_play: a play-level tag silences the
			// play's own rules but is not inherited by its tasks.
			name: "skip_ansible_lint on a play silences play rules only",
			path: "playbooks/p.yml",
			content: `---
- hosts: localhost
  tags:
    - skip_ansible_lint
  tasks:
    - ansible.builtin.command: echo hi
      changed_when: false
`,
			want: []string{"name[missing]"},
		},
		{
			name: "a tagged task does not silence its untagged sibling",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Do something
      ansible.builtin.command: echo hi
      register: result
      changed_when: false
      tags:
        - skip_ansible_lint
    - name: React to change
      ansible.builtin.service:
        name: foo
        state: restarted
      when: result.changed
`,
			want: []string{"no-handler"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTags(t, lintInline(t, tc.path, tc.content), tc.want)
		})
	}
}

func TestNameRule(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		want    []string
	}{
		{
			name: "name[play] and name[missing]",
			path: "playbooks/p.yml",
			content: `---
- hosts: localhost
  tasks:
    - ansible.builtin.debug:
        msg: hi
`,
			want: []string{"name[play]", "name[missing]"},
		},
		{
			name: "name[casing]",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: lowercase start
      ansible.builtin.debug:
        msg: hi
`,
			want: []string{"name[casing]"},
		},
		{
			name: "name[template] for a jinja expression in the middle",
			path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Do {{ thing }} now
      ansible.builtin.debug:
        msg: hi
`,
			want: []string{"name[template]"},
		},
		{
			name: "prefixed task names are judged after the prefix",
			path: "tasks/other.yml",
			content: `---
- name: other | Correct
  ansible.builtin.debug:
    msg: hi
`,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTags(t, lintInline(t, tc.path, tc.content), tc.want)
		})
	}
}

func TestMetadataRules(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		want    []string
	}{
		{
			name: "meta-no-tags rejects uppercase tags",
			path: "meta/main.yml",
			content: `---
galaxy_info:
  galaxy_tags: [MYTAG]
`,
			want: []string{"meta-no-tags"},
		},
		{
			name: "meta-no-tags rejects categories",
			path: "meta/main.yml",
			content: `---
galaxy_info:
  categories: networking
`,
			want: []string{"meta-no-tags", "meta-no-tags"},
		},
		{
			name: "meta-incorrect on scaffold defaults",
			path: "meta/main.yml",
			content: `---
galaxy_info:
  author: your name
`,
			want: []string{"meta-incorrect"},
		},
		{
			name: "meta-video-links rejects unknown providers",
			path: "meta/main.yml",
			content: `---
galaxy_info:
  video_links:
    - url: https://example.com/video
      title: Demo
`,
			want: []string{"meta-video-links"},
		},
		{
			name: "role-name[path] on a meta dependency",
			path: "meta/main.yml",
			content: `---
dependencies:
  - role: subfolder/other
`,
			want: []string{"role-name[path]"},
		},
		{
			name: "meta-runtime rejects an unsupported version",
			path: "meta/runtime.yml",
			content: `---
requires_ansible: ">=2.9"
`,
			want: []string{"meta-runtime[unsupported-version]"},
		},
		{
			name: "meta-runtime rejects an invalid specifier",
			path: "meta/runtime.yml",
			content: `---
requires_ansible: "2.15.0,<2.16"
`,
			want: []string{"meta-runtime[invalid-version]"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTags(t, lintInline(t, tc.path, tc.content), tc.want)
		})
	}
}

func TestGalaxyRule(t *testing.T) {
	got := lintInline(t, "galaxy.yml", `---
namespace: foo
name: bar
version: 1.0.0
license: MIT
repository: https://example.com
tags:
  - not_a_required_tag
`)
	// Upstream orders same-line matches by message, which puts galaxy[tags]
	// ("galaxy.yaml must have...") before galaxy[no-runtime] ("meta/runtime.yml
	// file not found."); the frozen golden shows exactly this order.
	assertTags(t, got, []string{"galaxy[no-changelog]", "galaxy[tags]", "galaxy[no-runtime]"})
}

func TestRoleDirName(t *testing.T) {
	dir := t.TempDir()
	role := filepath.Join(dir, "roles", "bad-name")
	if err := os.MkdirAll(role, 0o755); err != nil {
		t.Fatal(err)
	}
	found := rules.RoleDir("roles/bad-name", role)
	if len(found) != 1 || found[0].Tag != "role-name" {
		t.Fatalf("got %v, want one role-name finding", found)
	}
	if found[0].Message != "Role name bad-name does not match ``^[a-z][a-z0-9_]*$`` pattern." {
		t.Fatalf("unexpected message %q", found[0].Message)
	}
}

func TestFilterSkipList(t *testing.T) {
	in := []rules.Finding{
		{Tag: "name[casing]"},
		{Tag: "no-changed-when"},
	}
	if got := rules.Filter(append([]rules.Finding(nil), in...), []string{"name"}); len(got) != 1 {
		t.Fatalf("skip by rule id: got %d findings", len(got))
	}
	if got := rules.Filter(append([]rules.Finding(nil), in...), []string{"no-changed-when"}); len(got) != 1 {
		t.Fatalf("skip by tag: got %d findings", len(got))
	}
}

// TestSkipListAcceptsBothTaxonomies pins that a `.ansible-lint` skip_list
// silences the same findings whichever id spelling it uses.
func TestSkipListAcceptsBothTaxonomies(t *testing.T) {
	in := []rules.Finding{
		{Tag: "name[casing]"},
		{Tag: "no-changed-when"},
		{Tag: "role-name[path]"},
		{Tag: "galaxy[no-changelog]"},
	}
	tests := map[string][]string{
		"upstream tags":       {"name[casing]", "no-changed-when", "role-name[path]", "galaxy[no-changelog]"},
		"native tags":         {"name.casing", "task.unguarded-change", "role.name[path]", "galaxy.changelog-missing"},
		"mixed":               {"name.casing", "no-changed-when", "role.name[path]", "galaxy[no-changelog]"},
		"upstream rule ids":   {"name", "no-changed-when", "role-name", "galaxy"},
		"native rule ids":     {"name", "task.unguarded-change", "role.name", "galaxy"},
		"padded native entry": {"  name.casing  ", "task.unguarded-change", "role.name", "galaxy"},
	}
	for name, skipList := range tests {
		t.Run(name, func(t *testing.T) {
			got := rules.Filter(append([]rules.Finding(nil), in...), skipList)
			if len(got) != 0 {
				t.Fatalf("got %v, want everything skipped", got)
			}
		})
	}
}

// TestNoqaAcceptsBothTaxonomies pins the same for inline `# noqa` comments.
func TestNoqaAcceptsBothTaxonomies(t *testing.T) {
	const playbook = `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Run something # noqa: %s
      ansible.builtin.command: git clone blah
`
	for _, token := range []string{
		"no-changed-when", "task.unguarded-change",
		"command-instead-of-module", "task.use-module",
	} {
		t.Run(token, func(t *testing.T) {
			tags := lintInline(t, "playbooks/p.yml", fmt.Sprintf(playbook, token))
			for _, tag := range tags {
				if rules.Canonical(token) == tag {
					t.Fatalf("noqa %q left %q in %v", token, tag, tags)
				}
			}
			if len(tags) != 1 {
				t.Fatalf("got %v, want the one rule not silenced", tags)
			}
		})
	}
}

// TestCommandInsteadOfShellChomping pins the block-scalar distinction
// upstream's shell-character check makes (issue 0014, freeipa/ansible-freeipa):
// a clip-chomped scalar (`>`) keeps its trailing newline, `\n` counts as a
// shell feature, and the rule stays silent; the strip-chomped form (`>-`) and
// a plain scalar are flagged. Verified against ansible-lint 26.8.0.
func TestCommandInsteadOfShellChomping(t *testing.T) {
	src := "---\n" +
		"- name: P\n" +
		"  hosts: all\n" +
		"  tasks:\n" +
		"    - name: Folded clip\n" +
		"      ansible.builtin.shell: >\n" +
		"        echo hi\n" +
		"      changed_when: false\n" +
		"    - name: Plain\n" +
		"      ansible.builtin.shell: echo hi\n" +
		"      changed_when: false\n" +
		"    - name: Folded strip\n" +
		"      ansible.builtin.shell: >-\n" +
		"        echo hi\n" +
		"      changed_when: false\n"
	var lines []int
	for _, f := range lintInlineFindings(t, "playbook.yml", src) {
		if f.Tag == "command-instead-of-shell" {
			lines = append(lines, f.Line)
		}
	}
	want := []int{9, 12}
	if len(lines) != len(want) || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("flagged lines %v, want %v (clip-chomped scalar must stay silent)", lines, want)
	}
}
