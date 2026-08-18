package discover

import (
	"os"
	"path/filepath"
	"testing"
)

// childTasks is a task list carrying a key-order violation. Its content does
// not matter to these tests, only that the file exists and holds tasks.
const childTasks = "---\n" +
	"- ansible.builtin.command: /bin/true\n" +
	"  changed_when: false\n" +
	"  name: Child task\n"

// repo writes files into a fresh directory, makes it the working directory so
// that item paths come out relative, and returns its resolved root.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return resolvePath(dir)
}

// expanded runs a walk over the repository and returns the kind of each file
// the include pass added, keyed by path.
func expanded(t *testing.T, excluded ...string) map[string]string {
	t.Helper()
	items, soft, err := Walk([]string{"."}, excluded)
	if err != nil {
		t.Fatal(err)
	}
	if len(soft) != 0 {
		t.Fatalf("unexpected soft errors: %v", soft)
	}
	out := map[string]string{}
	for _, it := range ExpandIncludes(items, excluded) {
		out[it.Path] = it.Kind
	}
	return out
}

// TestIncludedFileInheritsItsSection is the core of issue 0008, and the table
// is measured against ansible-lint 26.8.0 rather than reasoned about: an
// include under `tasks:` or `handlers:` yields a child the task rules match,
// and the same include under `pre_tasks:` or `post_tasks:` does not, because
// upstream names the child after the section it came from.
func TestIncludedFileInheritsItsSection(t *testing.T) {
	for _, section := range []string{"tasks", "pre_tasks", "post_tasks", "handlers"} {
		t.Run(section, func(t *testing.T) {
			repo(t, map[string]string{
				"pb.yml": "---\n" +
					"- name: P\n" +
					"  hosts: localhost\n" +
					"  " + section + ":\n" +
					"    - name: Inc\n" +
					"      ansible.builtin.include_tasks: child.yml\n",
				"child.yml": childTasks,
			})

			got := expanded(t)
			if got["child.yml"] != section {
				t.Fatalf("child.yml kind = %q, want %q", got["child.yml"], section)
			}
		})
	}
}

// TestIncludeValueForms covers the spellings of the target, and the two that
// must not be followed. Each row was checked against ansible-lint 26.8.0 on the
// same fixture.
//
// The free-form row is the one worth reading twice: `include_tasks: child.yml
// tags=setup` looks like a target astl could resolve, but ansible-core rejects
// the including file for it (`Invalid options for ansible.builtin.include_tasks:
// tags`), upstream abandons that file at syntax-check and reports nothing from
// it, and following the include would put astl-only findings on the child.
func TestIncludeValueForms(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"bare", "child.yml", true},
		{"file kwarg", "file=child.yml", true},
		{"free-form args", "child.yml tags=setup", false},
		{"lone foreign kwarg", "tags=setup", false},
		{"templated", "{{ which }}.yml", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo(t, map[string]string{
				"pb.yml": "---\n" +
					"- name: P\n" +
					"  hosts: localhost\n" +
					"  tasks:\n" +
					"    - name: Inc\n" +
					"      ansible.builtin.include_tasks: " + tc.value + "\n",
				"child.yml": childTasks,
			})

			_, found := expanded(t)["child.yml"]
			if found != tc.want {
				t.Fatalf("child.yml expanded = %v, want %v for %q", found, tc.want, tc.value)
			}
		})
	}
}

// TestIncludeMappingForm covers `include_tasks: {file: x.yml}`, the form that
// carries options beside the path.
func TestIncludeMappingForm(t *testing.T) {
	repo(t, map[string]string{
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Inc\n" +
			"      ansible.builtin.include_tasks:\n" +
			"        file: child.yml\n" +
			"        apply:\n" +
			"          tags: [setup]\n",
		"child.yml": childTasks,
	})

	if got := expanded(t)["child.yml"]; got != "tasks" {
		t.Fatalf("child.yml kind = %q, want tasks", got)
	}
}

// TestIncludeInsideBlockIsFollowed pins the descent into block/rescue/always.
func TestIncludeInsideBlockIsFollowed(t *testing.T) {
	repo(t, map[string]string{
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Wrapper\n" +
			"      block:\n" +
			"        - name: Inc\n" +
			"          ansible.builtin.include_tasks: child.yml\n" +
			"      rescue:\n" +
			"        - name: Inc rescue\n" +
			"          ansible.builtin.include_tasks: rescued.yml\n",
		"child.yml":   childTasks,
		"rescued.yml": childTasks,
	})

	got := expanded(t)
	if got["child.yml"] != "tasks" || got["rescued.yml"] != "tasks" {
		t.Fatalf("block children = %v, want both under tasks", got)
	}
}

// TestIncludeChainIsFollowed covers the fixpoint: an included tasks file that
// includes another, resolved relative to its own directory.
func TestIncludeChainIsFollowed(t *testing.T) {
	repo(t, map[string]string{
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Inc\n" +
			"      ansible.builtin.include_tasks: mid/middle.yml\n",
		"mid/middle.yml": "---\n" +
			"- name: Chain\n" +
			"  ansible.builtin.include_tasks: leaf.yml\n",
		"mid/leaf.yml": childTasks,
	})

	got := expanded(t)
	if got["mid/leaf.yml"] != "tasks" {
		t.Fatalf("mid/leaf.yml kind = %q, want tasks: the chain was not followed", got["mid/leaf.yml"])
	}
}

// TestIncludeCycleTerminates is the guard, not a behaviour: two files including
// each other must not spin the fixpoint.
func TestIncludeCycleTerminates(t *testing.T) {
	repo(t, map[string]string{
		"a.yml": "---\n" +
			"- name: A\n" +
			"  ansible.builtin.include_tasks: b.yml\n",
		"b.yml": "---\n" +
			"- name: B\n" +
			"  ansible.builtin.include_tasks: a.yml\n",
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Inc\n" +
			"      ansible.builtin.include_tasks: a.yml\n",
	})

	got := expanded(t)
	if got["a.yml"] != "tasks" || got["b.yml"] != "tasks" {
		t.Fatalf("cycle = %v, want both files reached once under tasks", got)
	}
}

// TestIncludeResolvesThroughTasksDir covers ansible's path_dwim fallbacks: the
// target is looked for beside the including file, then under its `tasks/`, then
// the same two questions one directory up.
func TestIncludeResolvesThroughTasksDir(t *testing.T) {
	repo(t, map[string]string{
		"roles/r/tasks/main.yml": "---\n" +
			"- name: Inc\n" +
			"  ansible.builtin.include_tasks: extra.yml\n",
		"roles/r/tasks/extra.yml": childTasks,
		"roles/r/other.yml":       childTasks,
	})

	got := expanded(t)
	// extra.yml is already discovered as tasks by its path, so the pass has
	// nothing to add: what matters is that it did not invent a second entry.
	if _, added := got["roles/r/tasks/extra.yml"]; added {
		t.Fatalf("expanded = %v, want nothing added for a file already discovered under the same kind", got)
	}
}

// TestIncludeWalksUpToTheRoleRoot is the other half of path_dwim: a target that
// exists neither beside the including file nor under its `tasks/` is looked for
// in the parent directory.
func TestIncludeWalksUpToTheRoleRoot(t *testing.T) {
	repo(t, map[string]string{
		"roles/r/tasks/main.yml": "---\n" +
			"- name: Inc\n" +
			"  ansible.builtin.include_tasks: shared.yml\n",
		"roles/r/shared.yml": childTasks,
	})

	if got := expanded(t)["roles/r/shared.yml"]; got != "tasks" {
		t.Fatalf("roles/r/shared.yml kind = %q, want tasks", got)
	}
}

// TestExcludedIncludeIsSkipped pins that exclude_paths applies to a file
// reached through an include, as it does to one reached by the walk. Upstream
// checks the same thing on every child it adds.
func TestExcludedIncludeIsSkipped(t *testing.T) {
	repo(t, map[string]string{
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Inc\n" +
			"      ansible.builtin.include_tasks: vendor/child.yml\n",
		"vendor/child.yml": childTasks,
	})

	if got := expanded(t, "vendor/"); len(got) != 0 {
		t.Fatalf("expanded = %v, want the excluded child left out", got)
	}
}

// TestMissingIncludeIsSilent covers a target that does not exist. Upstream
// creates the lintable and then skips it because the path is not there, so
// astl reports nothing rather than treating it as an unchecked file.
func TestMissingIncludeIsSilent(t *testing.T) {
	repo(t, map[string]string{
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Inc\n" +
			"      ansible.builtin.include_tasks: nowhere.yml\n",
	})

	if got := expanded(t); len(got) != 0 {
		t.Fatalf("expanded = %v, want nothing for a target that does not exist", got)
	}
}

// TestImportPlaybookInheritsPlaybookKind covers the play-level inclusion, where
// the child is a playbook rather than a task list.
func TestImportPlaybookInheritsPlaybookKind(t *testing.T) {
	repo(t, map[string]string{
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Noop\n" +
			"      ansible.builtin.debug:\n" +
			"        msg: hi\n" +
			"- ansible.builtin.import_playbook: other/second.yml\n",
		"other/second.yml": "---\n" +
			"- name: Q\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Noop\n" +
			"      ansible.builtin.debug:\n" +
			"        msg: hi\n",
	})

	if got := expanded(t)["other/second.yml"]; got != KindPlaybook {
		t.Fatalf("other/second.yml kind = %q, want %q", got, KindPlaybook)
	}
}

// TestIncludedFileIsAddedUnderBothKinds pins the choice not to replace the
// walk's own classification. A file discovered as yaml and included as tasks is
// linted under both, which is upstream's behaviour: its yaml findings come from
// one entry and its task findings from the other. rules.Dedupe removes the
// lines the two agree on.
func TestIncludedFileIsAddedUnderBothKinds(t *testing.T) {
	repo(t, map[string]string{
		"pb.yml": "---\n" +
			"- name: P\n" +
			"  hosts: localhost\n" +
			"  tasks:\n" +
			"    - name: Inc\n" +
			"      ansible.builtin.include_tasks: child.yml\n",
		"child.yml": childTasks,
	})

	items, _, err := Walk([]string{"."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var walked string
	for _, it := range items {
		if it.Path == "child.yml" {
			walked = it.Kind
		}
	}
	if walked != KindYAML {
		t.Fatalf("the walk classified child.yml as %q, want %q; the fixture no longer tests what it means to",
			walked, KindYAML)
	}
	if got := ExpandIncludes(items, nil); len(got) != 1 || got[0].Kind != "tasks" {
		t.Fatalf("ExpandIncludes = %v, want one added entry under tasks beside the walk's yaml one", got)
	}
}
