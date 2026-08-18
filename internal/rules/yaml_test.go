package rules_test

import "testing"

// The yaml[*] pass mirrors upstream's yamllint embedding: it runs on any
// text/yaml lintable however broken its ansible semantics, and only the
// same-line tag-exact noqa silences it.

func TestYamlRuleFindsTruthy(t *testing.T) {
	got := lintInline(t, "x.yml", "---\nkey: yes\n")
	assertTags(t, got, []string{"yaml[truthy]"})
}

func TestYamlRuleSkipsTruthyInWorkflows(t *testing.T) {
	got := lintInline(t, ".github/workflows/ci.yml", "---\nkey: yes\n")
	assertTags(t, got, nil)
}

func TestYamlRuleIgnoresSkipAnsibleLintTag(t *testing.T) {
	content := "---\n- name: P\n  hosts: all\n  tasks:\n    - name: T\n      ansible.builtin.debug:\n        msg: yes\n      tags:\n        - skip_ansible_lint\n"
	got := lintInline(t, "playbook.yml", content)
	assertTags(t, got, []string{"yaml[truthy]"})
}

func TestYamlRuleHonorsSameLineNoqa(t *testing.T) {
	got := lintInline(t, "x.yml", "---\nkey: yes  # noqa: yaml[truthy]\n")
	assertTags(t, got, nil)
	// The generic per-line filter is tag-exact, as upstream's is: naming the
	// bare rule id does not silence a tagged finding.
	got = lintInline(t, "x.yml", "---\nkey: yes  # noqa: yaml\n")
	assertTags(t, got, []string{"yaml[truthy]"})
}

func TestYamlRuleLintsMultiDocumentFiles(t *testing.T) {
	got := lintInline(t, "x.yml", "---\na: 1\n---\nb: yes\n")
	assertTags(t, got, []string{"yaml[truthy]"})
}

func TestYamlRuleLintsCommentOnlyFiles(t *testing.T) {
	got := lintInline(t, "x.yml", "#bad\n")
	assertTags(t, got, []string{"yaml[comments]"})
}

func TestYamlRuleSkipsNonYAMLBaseKinds(t *testing.T) {
	got := lintInline(t, "templates/sub/x.yml", "---\nkey: yes   \n")
	assertTags(t, got, nil)
}
