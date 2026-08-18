package rules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/discover"
	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// ruleCase is one fixture: content written to relPath, linted with opt, and
// judged on the tags rule produced. Restricting the assertion to a single rule
// keeps each fixture about one behaviour instead of every rule it happens to
// trip.
type ruleCase struct {
	name    string
	rule    string
	path    string
	kind    string
	content string
	opt     rules.Options
	want    []string
}

func (c ruleCase) run(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	abs := filepath.Join(dir, filepath.FromSlash(c.path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(c.content), 0o600); err != nil {
		t.Fatal(err)
	}
	kind := c.kind
	if kind == "" {
		kind = discover.KindOf(c.path)
	}
	found := rules.File(parse.Load(c.path, abs, kind), c.opt)
	rules.Sort(found)
	var got []string
	for _, f := range found {
		if f.RuleID() == c.rule {
			got = append(got, f.Tag)
		}
	}
	if strings.Join(got, ",") != strings.Join(c.want, ",") {
		t.Fatalf("%s tags: got %v, want %v", c.rule, got, c.want)
	}
}

func runCases(t *testing.T, cases []ruleCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}

// play wraps tasks in a minimal named playbook so a fixture only has to show
// the tasks the rule under test cares about.
func play(tasks string) string {
	return "---\n- name: Fixture\n  hosts: localhost\n  tasks:\n" + tasks
}

func TestTaskSafetyRules(t *testing.T) {
	runCases(t, []ruleCase{
		{
			name: "ignore-errors fires on a blanket ignore",
			rule: "ignore-errors", path: "playbooks/p.yml",
			content: play(`    - name: Reach out
      ansible.builtin.uri:
        url: https://example.invalid
      ignore_errors: true
`),
			want: []string{"ignore-errors"},
		},
		{
			name: "ignore-errors accepts a registered result",
			rule: "ignore-errors", path: "playbooks/p.yml",
			content: play(`    - name: Reach out
      ansible.builtin.uri:
        url: https://example.invalid
      ignore_errors: true
      register: reach_out
`),
		},
		{
			name: "ignore-errors accepts check mode",
			rule: "ignore-errors", path: "playbooks/p.yml",
			content: play(`    - name: Reach out
      ansible.builtin.uri:
        url: https://example.invalid
      ignore_errors: "{{ ansible_check_mode }}"
`),
		},
		{
			name: "ignore-errors ignores a disabled ignore",
			rule: "ignore-errors", path: "playbooks/p.yml",
			content: play(`    - name: Reach out
      ansible.builtin.uri:
        url: https://example.invalid
      ignore_errors: false
`),
		},
		{
			name: "no-tabs fires on a tab in a value and in a key",
			rule: "no-tabs", path: "playbooks/p.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        "ta\tg": "a\ttab"
`),
			want: []string{"no-tabs", "no-tabs"},
		},
		{
			name: "no-tabs allows a tab in a lineinfile line",
			rule: "no-tabs", path: "playbooks/p.yml",
			content: play(`    - name: Append
      ansible.builtin.lineinfile:
        path: /etc/example.conf
        line: "key\tvalue"
`),
		},
		{
			name: "no-tabs ignores a templated tab",
			rule: "no-tabs", path: "playbooks/p.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: "{{ 'a\tb' }}"
`),
		},
		{
			name: "no-relative-paths fires on a template escaping the role",
			rule: "no-relative-paths", path: "playbooks/p.yml",
			content: play(`    - name: Render
      ansible.builtin.template:
        src: ../templates/example.conf.j2
        dest: /etc/example.conf
        mode: "0644"
`),
			want: []string{"no-relative-paths"},
		},
		{
			name: "no-relative-paths accepts a plain src",
			rule: "no-relative-paths", path: "playbooks/p.yml",
			content: play(`    - name: Render
      ansible.builtin.template:
        src: example.conf.j2
        dest: /etc/example.conf
        mode: "0644"
`),
		},
		{
			name: "avoid-implicit fires on structured copy content",
			rule: "avoid-implicit", path: "playbooks/p.yml",
			content: play(`    - name: Write
      ansible.builtin.copy:
        content:
          key: value
        dest: /etc/example.json
        mode: "0644"
`),
			want: []string{"avoid-implicit"},
		},
		{
			name: "avoid-implicit accepts string copy content",
			rule: "avoid-implicit", path: "playbooks/p.yml",
			content: play(`    - name: Write
      ansible.builtin.copy:
        content: "key=value"
        dest: /etc/example.conf
        mode: "0644"
`),
		},
	})
}

func TestPermissionRules(t *testing.T) {
	runCases(t, []ruleCase{
		{
			name: "risky-file-permissions fires on a template without mode",
			rule: "risky-file-permissions", path: "playbooks/p.yml",
			content: play(`    - name: Render
      ansible.builtin.template:
        src: example.conf.j2
        dest: /etc/example.conf
`),
			want: []string{"risky-file-permissions"},
		},
		{
			name: "risky-file-permissions accepts an explicit mode",
			rule: "risky-file-permissions", path: "playbooks/p.yml",
			content: play(`    - name: Render
      ansible.builtin.template:
        src: example.conf.j2
        dest: /etc/example.conf
        mode: "0644"
`),
		},
		{
			name: "risky-file-permissions fires on lineinfile that creates",
			rule: "risky-file-permissions", path: "playbooks/p.yml",
			content: play(`    - name: Append
      ansible.builtin.lineinfile:
        path: /etc/example.conf
        line: key=value
        create: true
`),
			want: []string{"risky-file-permissions"},
		},
		{
			name: "risky-file-permissions ignores lineinfile that does not create",
			rule: "risky-file-permissions", path: "playbooks/p.yml",
			content: play(`    - name: Append
      ansible.builtin.lineinfile:
        path: /etc/example.conf
        line: key=value
`),
		},
		{
			name: "risky-file-permissions rejects preserve outside copy and template",
			rule: "risky-file-permissions", path: "playbooks/p.yml",
			content: play(`    - name: Own
      ansible.builtin.file:
        path: /etc/example.conf
        mode: preserve
`),
			want: []string{"risky-file-permissions"},
		},
		{
			name: "risky-file-permissions ignores a removal",
			rule: "risky-file-permissions", path: "playbooks/p.yml",
			content: play(`    - name: Drop
      ansible.builtin.file:
        path: /etc/example.conf
        state: absent
`),
		},
		{
			name: "risky-octal fires on a decimal mode",
			rule: "risky-octal", path: "playbooks/p.yml",
			content: play(`    - name: Own
      ansible.builtin.file:
        path: /etc/example.conf
        mode: 644
`),
			want: []string{"risky-octal"},
		},
		{
			name: "risky-octal accepts a quoted mode",
			rule: "risky-octal", path: "playbooks/p.yml",
			content: play(`    - name: Own
      ansible.builtin.file:
        path: /etc/example.conf
        mode: "0644"
`),
		},
		{
			name: "risky-octal accepts a leading-zero octal literal",
			rule: "risky-octal", path: "playbooks/p.yml",
			content: play(`    - name: Own
      ansible.builtin.file:
        path: /etc/example.conf
        mode: 0644
`),
		},
	})
}

func TestCommandRules(t *testing.T) {
	runCases(t, []ruleCase{
		{
			name: "risky-shell-pipe fires on an unguarded pipeline",
			rule: "risky-shell-pipe", path: "playbooks/p.yml",
			content: play(`    - name: Count
      ansible.builtin.shell: cat /etc/hosts | wc -l
      changed_when: false
`),
			want: []string{"risky-shell-pipe"},
		},
		{
			name: "risky-shell-pipe accepts pipefail on a later line",
			rule: "risky-shell-pipe", path: "playbooks/p.yml",
			content: play(`    - name: Count
      ansible.builtin.shell: |
        echo starting
        set -eo pipefail
        cat /etc/hosts | wc -l
      changed_when: false
`),
		},
		{
			name: "risky-shell-pipe ignores a boolean or",
			rule: "risky-shell-pipe", path: "playbooks/p.yml",
			content: play(`    - name: Try
      ansible.builtin.shell: /bin/false || /bin/true
      changed_when: false
`),
		},
		{
			name: "risky-shell-pipe ignores a powershell command",
			rule: "risky-shell-pipe", path: "playbooks/p.yml",
			content: play(`    - name: Count
      ansible.builtin.shell:
        executable: /bin/pwsh
        cmd: "Get-Item x | Measure-Object"
      changed_when: false
`),
		},
		{
			name: "inline-env-var fires on an inline assignment",
			rule: "inline-env-var", path: "playbooks/p.yml",
			content: play(`    - name: Build
      ansible.builtin.command: LANG=C /usr/bin/make
      changed_when: false
`),
			want: []string{"inline-env-var"},
		},
		{
			name: "inline-env-var accepts an option-looking argument",
			rule: "inline-env-var", path: "playbooks/p.yml",
			content: play(`    - name: Build
      ansible.builtin.command: /usr/bin/make --jobs=4
      changed_when: false
`),
		},
	})
}

func TestConditionRules(t *testing.T) {
	runCases(t, []ruleCase{
		{
			name: "no-handler fires on a changed condition",
			rule: "no-handler", path: "playbooks/p.yml",
			content: play(`    - name: Restart
      ansible.builtin.service:
        name: example
        state: restarted
      when: rendered.changed
`),
			want: []string{"no-handler"},
		},
		{
			name: "no-handler fires on a single-item condition list",
			rule: "no-handler", path: "playbooks/p.yml",
			content: play(`    - name: Restart
      ansible.builtin.service:
        name: example
        state: restarted
      when:
        - rendered is changed
`),
			want: []string{"no-handler"},
		},
		{
			name: "no-handler ignores a compound condition",
			rule: "no-handler", path: "playbooks/p.yml",
			content: play(`    - name: Restart
      ansible.builtin.service:
        name: example
        state: restarted
      when: rendered.changed and enabled
`),
		},
		{
			name: "no-handler ignores a handler",
			rule: "no-handler", path: "handlers/main.yml",
			content: `---
- name: Restart
  ansible.builtin.service:
    name: example
    state: restarted
  when: rendered.changed
`,
		},
		{
			name: "no-jinja-when fires on a templated condition",
			rule: "no-jinja-when", path: "playbooks/p.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
      when: "{{ enabled }}"
`),
			want: []string{"no-jinja-when"},
		},
		{
			name: "no-jinja-when accepts a raw condition",
			rule: "no-jinja-when", path: "playbooks/p.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
      when: enabled
`),
		},
		{
			name: "no-jinja-when checks a role condition",
			rule: "no-jinja-when", path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  roles:
    - role: example
      when: "{{ enabled }}"
`,
			want: []string{"no-jinja-when"},
		},
		{
			name: "literal-compare fires on a boolean comparison",
			rule: "literal-compare", path: "playbooks/p.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
      when: enabled == True
`),
			want: []string{"literal-compare"},
		},
		{
			name: "literal-compare accepts a plain condition",
			rule: "literal-compare", path: "playbooks/p.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
      when: enabled
`),
		},
		{
			name: "empty-string-compare stays off by default",
			rule: "empty-string-compare", path: "playbooks/p.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
      when: label != ""
`),
		},
		{
			name: "empty-string-compare fires once enabled",
			rule: "empty-string-compare", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"empty-string-compare"}},
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
      when: label != ""
`),
			want: []string{"empty-string-compare"},
		},
	})
}

func TestPlayScopedRules(t *testing.T) {
	runCases(t, []ruleCase{
		{
			name: "run-once flags the free strategy and the task using it",
			rule: "run-once", path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  strategy: free
  tasks:
    - name: Announce
      ansible.builtin.debug:
        msg: hello
      run_once: true
`,
			want: []string{"run-once[play]", "run-once[task]"},
		},
		{
			name: "run-once stays quiet without run_once or a free strategy",
			rule: "run-once", path: "playbooks/p.yml",
			content: play(`    - name: Announce
      ansible.builtin.debug:
        msg: hello
`),
		},
		{
			name: "complexity caps the tasks of a play",
			rule: "complexity", path: "playbooks/p.yml",
			opt: rules.Options{MaxTasks: 2},
			content: play(`    - name: One
      ansible.builtin.debug:
        msg: one
    - name: Two
      ansible.builtin.debug:
        msg: two
    - name: Three
      ansible.builtin.debug:
        msg: three
`),
			want: []string{"complexity[play]"},
		},
		{
			name: "complexity caps block nesting",
			rule: "complexity", path: "playbooks/p.yml",
			opt: rules.Options{MaxBlockDepth: 1},
			content: play(`    - name: Outer
      block:
        - name: Middle
          block:
            - name: Inner
              block:
                - name: Leaf
                  ansible.builtin.debug:
                    msg: leaf
`),
			want: []string{"complexity[nesting]"},
		},
		{
			name: "complexity caps the tasks of a task file",
			rule: "complexity", path: "tasks/main.yml",
			opt: rules.Options{MaxTasks: 1},
			content: `---
- name: One
  ansible.builtin.debug:
    msg: one
- name: Two
  ansible.builtin.debug:
    msg: two
`,
			want: []string{"complexity[tasks]"},
		},
		{
			name: "complexity accepts a play within the defaults",
			rule: "complexity", path: "playbooks/p.yml",
			content: play(`    - name: One
      ansible.builtin.debug:
        msg: one
`),
		},
		{
			name: "no-prompting stays off by default",
			rule: "no-prompting", path: "playbooks/p.yml",
			content: `---
- name: Fixture
  hosts: localhost
  vars_prompt:
    - name: token
      prompt: Token?
  tasks:
    - name: Wait
      ansible.builtin.pause: {}
`,
		},
		{
			name: "no-prompting flags vars_prompt and an unbounded pause once enabled",
			rule: "no-prompting", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"no-prompting"}},
			content: `---
- name: Fixture
  hosts: localhost
  vars_prompt:
    - name: token
      prompt: Token?
  tasks:
    - name: Wait
      ansible.builtin.pause: {}
`,
			want: []string{"no-prompting", "no-prompting"},
		},
		{
			name: "no-prompting accepts a timed pause",
			rule: "no-prompting", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"no-prompting"}},
			content: play(`    - name: Wait
      ansible.builtin.pause:
        seconds: 5
`),
		},
	})
}

func TestOptInTaskRules(t *testing.T) {
	runCases(t, []ruleCase{
		{
			name: "no-log-password stays off by default",
			rule: "no-log-password", path: "playbooks/p.yml",
			content: play(`    - name: Create users
      ansible.builtin.user:
        name: "{{ item.name }}"
        password: "{{ item.secret }}"
      loop: "{{ accounts }}"
`),
		},
		{
			name: "no-log-password fires on a looped password once enabled",
			rule: "no-log-password", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"no-log-password"}},
			content: play(`    - name: Create users
      ansible.builtin.user:
        name: "{{ item.name }}"
        password: "{{ item.secret }}"
      loop: "{{ accounts }}"
`),
			want: []string{"no-log-password"},
		},
		{
			name: "no-log-password accepts no_log",
			rule: "no-log-password", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"no-log-password"}},
			content: play(`    - name: Create users
      ansible.builtin.user:
        name: "{{ item.name }}"
        password: "{{ item.secret }}"
      loop: "{{ accounts }}"
      no_log: true
`),
		},
		{
			name: "no-log-password accepts a task without a loop",
			rule: "no-log-password", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"no-log-password"}},
			content: play(`    - name: Create user
      ansible.builtin.user:
        name: example
        password: "{{ secret }}"
`),
		},
		{
			name: "jinja-template-extension fires on a src without .j2 once enabled",
			rule: "jinja-template-extension", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"jinja-template-extension"}},
			content: play(`    - name: Render
      ansible.builtin.template:
        src: example.conf
        dest: /etc/example.conf
        mode: "0644"
`),
			want: []string{"jinja-template-extension"},
		},
		{
			name: "jinja-template-extension accepts a .j2 src",
			rule: "jinja-template-extension", path: "playbooks/p.yml",
			opt: rules.Options{EnableList: []string{"jinja-template-extension"}},
			content: play(`    - name: Render
      ansible.builtin.template:
        src: example.conf.j2
        dest: /etc/example.conf
        mode: "0644"
`),
		},
	})
}

func TestFileScopedRules(t *testing.T) {
	runCases(t, []ruleCase{
		{
			name: "playbook-extension fires on a playbook without a yaml suffix",
			rule: "playbook-extension", path: "playbooks/site", kind: "playbook",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
`),
			want: []string{"playbook-extension"},
		},
		{
			name: "playbook-extension accepts a .yml playbook",
			rule: "playbook-extension", path: "playbooks/site.yml",
			content: play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
`),
		},
		{
			name: "sanity rejects an ignore that is not permitted",
			rule: "sanity", path: "tests/sanity/ignore-2.16.txt",
			content: "plugins/modules/example.py validate-modules:deprecation-mismatch\n",
			want:    []string{"sanity[cannot-ignore]"},
		},
		{
			name: "sanity rejects a malformed entry",
			rule: "sanity", path: "tests/sanity/ignore-2.16.txt",
			content: "plugins/modules/example.py\n",
			want:    []string{"sanity[bad-ignore]"},
		},
		{
			name: "sanity accepts a permitted ignore and an unpoliced directory",
			rule: "sanity", path: "tests/sanity/ignore-2.16.txt",
			content: "plugins/modules/example.py shellcheck  # noisy\ntests/unit/example.py anything at all\n",
		},
		{
			name: "sanity skips the retired ignore files",
			rule: "sanity", path: "tests/sanity/ignore-2.11.txt",
			content: "plugins/modules/example.py validate-modules:deprecation-mismatch\n",
		},
		{
			name: "galaxy-version-incorrect stays off by default",
			rule: "galaxy-version-incorrect", path: "galaxy.yml",
			content: "---\nname: example\nnamespace: acme\nversion: 0.4.0\n",
		},
		{
			name: "galaxy-version-incorrect rejects a pre-1.0.0 version once enabled",
			rule: "galaxy-version-incorrect", path: "galaxy.yml",
			opt:     rules.Options{EnableList: []string{"galaxy-version-incorrect"}},
			content: "---\nname: example\nnamespace: acme\nversion: 0.4.0\n",
			want:    []string{"galaxy-version-incorrect"},
		},
		{
			name: "galaxy-version-incorrect accepts a released version",
			rule: "galaxy-version-incorrect", path: "galaxy.yml",
			opt:     rules.Options{EnableList: []string{"galaxy-version-incorrect"}},
			content: "---\nname: example\nnamespace: acme\nversion: 1.4.0\n",
		},
	})
}

// TestLoopVarPrefix needs a role on disk, because the rule only applies inside
// one and names the role in its message.
func TestLoopVarPrefix(t *testing.T) {
	const rel = "roles/example/tasks/main.yml"
	opt := rules.Options{LoopVarPrefix: rules.LoopVarPrefixDefault}
	runCases(t, []ruleCase{
		{
			name: "an implicit item loop variable is flagged",
			rule: "loop-var-prefix", path: rel, opt: opt,
			content: `---
- name: Install
  ansible.builtin.package:
    name: "{{ item }}"
  loop: "{{ packages }}"
`,
			want: []string{"loop-var-prefix[missing]"},
		},
		{
			name: "an unprefixed loop variable is flagged",
			rule: "loop-var-prefix", path: rel, opt: opt,
			content: `---
- name: Install
  ansible.builtin.package:
    name: "{{ pkg }}"
  loop: "{{ packages }}"
  loop_control:
    loop_var: pkg
`,
			want: []string{"loop-var-prefix[wrong]"},
		},
		{
			name: "a role-prefixed loop variable is accepted",
			rule: "loop-var-prefix", path: rel, opt: opt,
			content: `---
- name: Install
  ansible.builtin.package:
    name: "{{ example_pkg }}"
  loop: "{{ packages }}"
  loop_control:
    loop_var: example_pkg
`,
		},
		{
			name: "loop-var-prefix stays inert without the option",
			rule: "loop-var-prefix", path: rel,
			content: `---
- name: Install
  ansible.builtin.package:
    name: "{{ item }}"
  loop: "{{ packages }}"
`,
		},
	})
}

// load writes content under a fresh temporary directory and parses it, for the
// assertions below that look at a finding rather than only its tag.
func load(t *testing.T, rel, content string) *parse.File {
	t.Helper()
	abs := filepath.Join(t.TempDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return parse.Load(rel, abs, discover.KindOf(rel))
}

// one runs the rules and returns the single finding rule produced, failing the
// test when the count is anything else.
func one(t *testing.T, f *parse.File, opt rules.Options, rule string) rules.Finding {
	t.Helper()
	var got []rules.Finding
	for _, fd := range rules.File(f, opt) {
		if fd.RuleID() == rule {
			got = append(got, fd)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%s: got %d findings, want 1", rule, len(got))
	}
	return got[0]
}

func TestRiskyOctalMessage(t *testing.T) {
	f := load(t, "playbooks/octal.yml", play(`    - name: Own
      ansible.builtin.file:
        path: /etc/example.conf
        mode: 644
`))
	const want = "`mode: 644` should have a string value with leading zero `mode: \"01204\"` or use symbolic mode."
	if got := one(t, f, rules.Options{}, "risky-octal"); got.Message != want {
		t.Errorf("message = %q, want %q", got.Message, want)
	}
}

func TestNoHandlerPointsAtTheCondition(t *testing.T) {
	f := load(t, "playbooks/handler.yml", play(`    - name: Restart
      ansible.builtin.service:
        name: example
        state: restarted
      when: rendered.changed
`))
	got := one(t, f, rules.Options{}, "no-handler")
	if got.Line != 9 || got.Column != 13 {
		t.Errorf("position = %d:%d, want 9:13", got.Line, got.Column)
	}
}

// A bare string role reports at the play and a mapping role at the mapping.
// Regression: the position used to be computed once for the whole roles list,
// so a string entry following a mapping entry inherited the mapping's line
// instead of the play's.
func TestRoleNamePathPositionsDoNotLeakBetweenEntries(t *testing.T) {
	f := load(t, "playbooks/roles.yml", `---
- hosts: localhost
  roles:
    - first/path
    - role: second/path
    - third/path
`)
	var got []string
	for _, fd := range rules.File(f, rules.Options{}) {
		if fd.RuleID() == "role-name" {
			got = append(got, fmt.Sprintf("%d:%d %s", fd.Line, fd.Column, fd.Message))
		}
	}
	sort.Strings(got)
	want := []string{
		"2:3 Avoid using paths when importing roles. (first/path)",
		"2:3 Avoid using paths when importing roles. (third/path)",
		"5:7 Avoid using paths when importing roles. (second/path)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("positions:\ngot:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestComplexityIsAWarning(t *testing.T) {
	f := load(t, "playbooks/complex.yml", play(`    - name: One
      ansible.builtin.debug:
        msg: one
    - name: Two
      ansible.builtin.debug:
        msg: two
`))
	got := one(t, f, rules.Options{MaxTasks: 1}, "complexity")
	if !got.Warning {
		t.Error("complexity finding is not marked as a warning")
	}
	if got.Message != "Maximum tasks allowed in a play is 1." {
		t.Errorf("message = %q", got.Message)
	}
}

func TestSanityNumbersTheOffendingLine(t *testing.T) {
	f := load(t, "tests/sanity/ignore-2.16.txt",
		"# leading comment\nplugins/modules/example.py validate-modules:deprecation-mismatch\n")
	got := one(t, f, rules.Options{}, "sanity")
	const want = "Ignore file contains validate-modules:deprecation-mismatch at line 2, which is not a permitted ignore."
	if got.Line != 2 || got.Message != want {
		t.Errorf("got %d: %q, want 2: %q", got.Line, got.Message, want)
	}
}

func TestRetiredNoqaIDSuppressesTheCurrentRule(t *testing.T) {
	f := load(t, "playbooks/retired.yml", play(`    - name: Show
      ansible.builtin.debug:
        msg: hello
      when: "{{ enabled }}" # noqa: 102
`))
	for _, fd := range rules.File(f, rules.Options{}) {
		if fd.RuleID() == "no-jinja-when" {
			t.Errorf("got %s, want it suppressed", fd.Tag)
		}
	}
}
