package parse

import "testing"

// TestYAMLKindIsPromotedToPlaybook covers the dispatch decision Load makes for
// a file discovery could only classify as generic `yaml`: whether it is really
// a playbook. Getting it wrong is silent and total. Promote a file that is not
// a playbook and every play rule runs against something that has no plays;
// fail to promote one that is and the whole play and task families skip it,
// reporting nothing and looking clean.
//
// The `rules:` exclusion is the subtle case. An ansible-rulebook is also a
// sequence of mappings with `hosts:`, so the key is what separates the two.
func TestYAMLKindIsPromotedToPlaybook(t *testing.T) {
	tests := map[string]struct {
		content string
		want    string
	}{
		"sequence of plays": {
			"---\n- hosts: all\n  tasks: []\n", "playbook",
		},
		"rulebook is not a playbook": {
			"---\n- name: R\n  hosts: all\n  rules:\n    - name: r\n", "yaml",
		},
		"mapping at the root": {
			"---\nhosts: all\n", "yaml",
		},
		"sequence without hosts": {
			"---\n- name: not a play\n", "yaml",
		},
		// A first play holding only an import_playbook is still a playbook;
		// missing it silences the whole file, later plays included (issue
		// 0010, found on kubespray's tests/testcases/tests.yml).
		"first play is an import_playbook": {
			"---\n- name: Import\n  import_playbook: other.yml\n\n- hosts: all\n  tasks: []\n", "playbook",
		},
		"first play is a fqcn import_playbook": {
			"---\n- ansible.builtin.import_playbook: other.yml\n", "playbook",
		},
		"sequence of scalars": {
			"---\n- one\n- two\n", "yaml",
		},
		"empty sequence": {
			"---\n[]\n", "yaml",
		},
		"empty document": {
			"---\n", "yaml",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := loadString(t, "yaml", tc.content).Kind; got != tc.want {
				t.Fatalf("kind = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDeclaredKindIsNeverOverridden pins the other half: promotion applies only
// to the generic `yaml` kind. A file discovery already classified keeps that
// classification even when its contents look like something else, because the
// path it was found at is the stronger signal.
func TestDeclaredKindIsNeverOverridden(t *testing.T) {
	f := loadString(t, "tasks", "---\n- hosts: all\n  tasks: []\n")
	if f.Kind != "tasks" {
		t.Fatalf("kind = %q, want tasks", f.Kind)
	}
}

// TestActionFormsResolveToTheSameModule covers the ways a task can name its
// module. They are not stylistic variants: `action:` and `local_action:` put
// the module name in a different place from the ordinary form, and a rule that
// reads Module gets the wrong answer for every task written the other way.
func TestActionFormsResolveToTheSameModule(t *testing.T) {
	tests := map[string]struct {
		task     string
		module   string
		freeForm string
	}{
		"ordinary mapping form": {
			"ansible.builtin.command:\n        cmd: echo hi\n", "command", "",
		},
		"ordinary free-form": {
			"ansible.builtin.command: echo hi\n", "command", "echo hi",
		},
		"action as a string": {
			"action: command echo hi\n", "command", "echo hi",
		},
		"action as a string with no arguments": {
			"action: setup\n", "setup", "",
		},
		"action as a mapping": {
			"action:\n        module: command\n        cmd: echo hi\n", "command", "",
		},
		"local_action as a string": {
			"local_action: command echo hi\n", "command", "echo hi",
		},
		"local_action as a mapping": {
			"local_action:\n        module: command\n        cmd: echo hi\n", "command", "",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := loadString(t, "playbook", "---\n- name: P\n  hosts: all\n  tasks:\n    - name: T\n      "+tc.task)
			tasks := f.Tasks()
			if len(tasks) != 1 {
				t.Fatalf("got %d tasks, want 1", len(tasks))
			}
			if tasks[0].Module != tc.module {
				t.Fatalf("module = %q, want %q", tasks[0].Module, tc.module)
			}
			if got := tasks[0].CmdArgs(); tc.freeForm != "" && got != tc.freeForm {
				t.Fatalf("free-form args = %q, want %q", got, tc.freeForm)
			}
		})
	}
}

// TestLoopKeysAreNotMistakenForTheModule covers the skip list in action
// resolution. A task's module is found by taking its first key that is not a
// task keyword, so `with_items` and the `__`-prefixed keys ansible-lint's own
// fixtures use would otherwise be read as the module name.
func TestLoopKeysAreNotMistakenForTheModule(t *testing.T) {
	f := loadString(t, "playbook", `---
- name: P
  hosts: all
  tasks:
    - name: T
      with_items: [1, 2]
      __line__: 7
      ansible.builtin.debug:
        msg: hi
`)
	tasks := f.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].Module != "debug" {
		t.Fatalf("module = %q, want debug", tasks[0].Module)
	}
}

// TestTaskWithNoActionResolvesToNothing covers the exhausted case. A mapping
// carrying only task keywords names no module, and the rules that read Module
// have to see an empty string rather than a keyword promoted into one.
func TestTaskWithNoActionResolvesToNothing(t *testing.T) {
	f := loadString(t, "tasks", "---\n- name: T\n  when: true\n")
	tasks := f.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].Module != "" {
		t.Fatalf("module = %q, want empty", tasks[0].Module)
	}
}

// TestModuleWithASequenceValueIsStillNamed covers the last branch of action
// resolution: a key whose value is neither a mapping nor a scalar still names
// the module, it simply carries no arguments this parser can read.
func TestModuleWithASequenceValueIsStillNamed(t *testing.T) {
	f := loadString(t, "tasks", "---\n- name: T\n  ansible.builtin.debug:\n    - one\n")
	tasks := f.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].Module != "debug" {
		t.Fatalf("module = %q, want debug", tasks[0].Module)
	}
}

func TestNodePosOfNilIsZero(t *testing.T) {
	if got := NodePos(nil); got.Line != 0 || got.Column != 0 {
		t.Fatalf("NodePos(nil) = %+v, want the zero position", got)
	}
}

func TestUnquote(t *testing.T) {
	tests := map[string]string{
		`"quoted"`:  "quoted",
		`'quoted'`:  "quoted",
		`bare`:      "bare",
		`"`:         `"`,
		`"mixed'`:   `"mixed'`,
		`""`:        "",
		`"unclosed`: `"unclosed`,
	}
	for in, want := range tests {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapHelpersAreNilSafe(t *testing.T) {
	if got := MapKeyNode(nil, "k"); got != nil {
		t.Errorf("MapKeyNode(nil) = %v, want nil", got)
	}
	if got := MapKeys(nil); got != nil {
		t.Errorf("MapKeys(nil) = %v, want nil", got)
	}
	// A sequence is a node, but not a mapping: the helpers answer for it too.
	seq := loadString(t, "yaml", "---\n- one\n").Root
	if got := MapKeys(seq); got != nil {
		t.Errorf("MapKeys(sequence) = %v, want nil", got)
	}
	if got := MapKeyNode(seq, "one"); got != nil {
		t.Errorf("MapKeyNode(sequence) = %v, want nil", got)
	}
}
