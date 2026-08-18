package parse

import (
	"os"
	"testing"
)

func TestParseKVFreeForm(t *testing.T) {
	got := parseKV("chdir=/tmp creates=/tmp/x /usr/bin/git clone blah", true)
	if got["chdir"] != "/tmp" || got["creates"] != "/tmp/x" {
		t.Fatalf("options not extracted: %v", got)
	}
	if got["_raw_params"] != "/usr/bin/git clone blah" {
		t.Fatalf("raw params = %q", got["_raw_params"])
	}
}

func TestParseKVKeepsUnknownOptionsAsRawParams(t *testing.T) {
	got := parseKV("echo a=b", true)
	if got["_raw_params"] != "echo a=b" {
		t.Fatalf("raw params = %q", got["_raw_params"])
	}
	if _, ok := got["a"]; ok {
		t.Fatal("a=b should not become an option for a free-form module")
	}
}

func TestParseKVMapForm(t *testing.T) {
	got := parseKV("state=latest name=httpd", false)
	if got["state"] != "latest" || got["name"] != "httpd" {
		t.Fatalf("unexpected options: %v", got)
	}
}

func TestSplitArgsKeepsQuotedRuns(t *testing.T) {
	got := splitArgs(`msg="hello world" x=1`)
	if len(got) != 2 || got[0] != `msg="hello world"` || got[1] != "x=1" {
		t.Fatalf("unexpected split: %q", got)
	}
}

func TestTasksRecurseIntoBlocks(t *testing.T) {
	f := loadString(t, "playbook", `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Outer
      block:
        - name: Inner
          ansible.builtin.debug:
            msg: hi
`)
	tasks := f.Tasks()
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if !tasks[0].IsBlock || tasks[0].Module != BlockModule {
		t.Fatalf("first task should be a block, got %q", tasks[0].Module)
	}
	if tasks[1].Module != "debug" {
		t.Fatalf("nested module = %q, want debug", tasks[1].Module)
	}
}

func TestActionShorthandStripsBuiltinPrefix(t *testing.T) {
	f := loadString(t, "playbook", `---
- name: Fixture
  hosts: localhost
  tasks:
    - name: Run
      action: ansible.builtin.command git clone blah
`)
	tasks := f.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	if tasks[0].Module != "command" {
		t.Fatalf("module = %q, want command", tasks[0].Module)
	}
	if tasks[0].CmdArgs() != "git clone blah" {
		t.Fatalf("cmd args = %q", tasks[0].CmdArgs())
	}
}

func loadString(t *testing.T, kind, content string) *File {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/f.yml"
	if err := writeFile(path, content); err != nil {
		t.Fatal(err)
	}
	f := Load("f.yml", path, kind)
	if f.Err != nil {
		t.Fatal(f.Err)
	}
	return f
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
