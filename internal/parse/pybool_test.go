package parse

import "testing"

// TestArgTruthyFollowsPyYAMLBooleans covers the schema mismatch between the two
// parsers. ansible reads YAML 1.1 through PyYAML, where `no` and `off` are
// booleans; yaml.v3 implements YAML 1.2, where they are ordinary strings. Read
// as strings they are non-empty and so truthy, which is the opposite answer and
// made astl report a risky-file-permissions ansible-lint does not.
//
// Every row was confirmed against ansible-lint 26.8.0 before being written
// here, including the quoted ones: quoting is what makes PyYAML keep a string,
// so `"no"` is truthy while bare `no` is not.
func TestArgTruthyFollowsPyYAMLBooleans(t *testing.T) {
	tests := map[string]bool{
		"true": true, "True": true, "TRUE": true,
		"false": false, "False": false, "FALSE": false,
		"yes": true, "Yes": true, "YES": true,
		"no": false, "No": false, "NO": false,
		"on": true, "On": true, "ON": true,
		"off": false, "Off": false, "OFF": false,
		`"no"`:  true, // quoted: a string to PyYAML, and non-empty
		`'off'`: true,
		"yEs":   true, // not a spelling PyYAML resolves, so a plain string
		"y":     true, // PyYAML does not resolve bare y/n either
		"n":     true,
		"0":     false,
		"1":     true,
		"0.0":   false,
		"~":     false,
		`""`:    false,
		"x":     true,
	}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			f := loadString(t, "tasks",
				"---\n- name: T\n  ansible.builtin.debug:\n    flag: "+value+"\n")
			tasks := f.Tasks()
			if len(tasks) != 1 {
				t.Fatalf("got %d tasks, want 1", len(tasks))
			}
			if got := tasks[0].ArgTruthy("flag"); got != want {
				t.Fatalf("ArgTruthy(%s) = %v, want %v", value, got, want)
			}
		})
	}
}

func TestArgTruthyOfAnAbsentArgument(t *testing.T) {
	f := loadString(t, "tasks", "---\n- name: T\n  ansible.builtin.debug:\n    msg: hi\n")
	if f.Tasks()[0].ArgTruthy("missing") {
		t.Fatal("an absent argument must not be truthy")
	}
}
