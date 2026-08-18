package rules_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/discover"
	"github.com/arhuman/ansible-static-lint/internal/format"
	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// fuzzKinds are the kinds a lintable can carry, indexed by the fuzzer's first
// argument. Kind decides which rule families run, so leaving it fixed would
// fuzz one branch of the dispatch in run.go and none of the others.
var fuzzKinds = []string{
	discover.KindPlaybook,
	discover.KindTasks,
	discover.KindHandlers,
	discover.KindMeta,
	discover.KindMetaRuntime,
	discover.KindGalaxy,
	discover.KindYAML,
	"requirements",
	"sanity-ignore-file",
	"inventory",
}

// fuzzSeeds are shapes worth starting from: valid content of each family, the
// suppression syntax, and the malformed and hostile inputs that a repository
// astl does not control can hand it. The fuzzer mutates from here, so a shape
// absent from this list is one it has to rediscover from scratch.
var fuzzSeeds = []string{
	"---\n- name: Play\n  hosts: all\n  tasks:\n    - name: Task\n      ansible.builtin.command: echo hi\n",
	"---\n- hosts: all\n  roles:\n    - role: a\n    - b\n",
	"---\n- name: P\n  hosts: all\n  tasks:\n    - ansible.builtin.command: x  # noqa no-changed-when\n",
	"---\n# noqa\n- hosts: all\n  tasks: []\n",
	"---\ngalaxy_info:\n  author: a\ndependencies: []\n",
	"---\nrequires_ansible: '>=2.9'\n",
	"---\nfoo: bar\n---\nbaz: qux\n",
	"---\n\tnot: [valid",
	"---\n- hosts: all\n  vars:\n    Bad_Name: 1\n    _x: 2\n",
	"---\n- hosts: all\n  tasks:\n    - block:\n      - block:\n        - block: []\n",
	"plugins/modules/x.py validate-modules:missing-gplv3-license\n",
	"---\n- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.shell: |\n        echo \"a\"\n",
	"---\n\x00\x00\x00\n",
	"---\n- hosts: all   \n  tasks: []\n\n\n",
	"- hosts: all\r\n  tasks: []\r\n",
	"---\n- hosts: ééé\n  tasks: []\n",
}

// lineCount counts the lines the linter sees, which is not the same as
// counting "\n". ansible-lint reads content with Path.read_text, so Python's
// universal-newline translation turns a lone "\r" into a line break before any
// rule looks at it, and internal/yamllint reproduces that on purpose. Counting
// raw "\n" here made this oracle reject a correct finding on the first
// fuzz-discovered input, "\r0".
func lineCount(content string) int {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.Count(content, "\n") + 1
}

// FuzzLintFile drives the whole per-file pipeline the way the CLI does: load,
// run every rule, sort, and render. SECURITY.md states astl runs in CI over
// repositories the operator does not control, which makes every byte here
// attacker-chosen, and this session found two denial-of-service defects in that
// surface by hand. This is the systematic version of that search.
//
// It asserts three properties beyond not crashing. Positions must point into
// the file, because pep8 output is consumed by editors and CI annotations that
// resolve them. Rendering must not fail. And the rendered bytes must be
// identical across two runs of the same input: the parity contract is
// byte-for-byte, so a finding order that depends on map iteration would break
// it intermittently and only ever in someone else's CI.
func FuzzLintFile(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add(uint8(0), seed)
	}
	// Kind and content are mutated independently, so seed one non-zero kind
	// explicitly rather than relying on the fuzzer to reach it from all-zero.
	f.Add(uint8(4), "---\ngalaxy_info:\n  author: a\n")

	dir := f.TempDir()

	f.Fuzz(func(t *testing.T, kindIdx uint8, content string) {
		kind := fuzzKinds[int(kindIdx)%len(fuzzKinds)]
		abs := filepath.Join(dir, "fuzz.yml")
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		render := func() string {
			found := rules.File(parse.Load("fuzz.yml", abs, kind), rules.Options{})
			rules.Sort(found)
			found = rules.Dedupe(found)

			lines := lineCount(content)
			for _, fd := range found {
				if fd.Line < 1 || fd.Line > lines {
					t.Fatalf("%s: finding at line %d, file has %d lines: %s",
						kind, fd.Line, lines, fd.Tag)
				}
				if fd.Column < 0 {
					t.Fatalf("%s: finding at negative column %d: %s", kind, fd.Column, fd.Tag)
				}
			}

			var buf bytes.Buffer
			for _, style := range []rules.IDStyle{rules.IDUpstream, rules.IDNative} {
				if err := format.PEP8(&buf, found, style); err != nil {
					t.Fatalf("render %s: %v", style, err)
				}
			}
			return buf.String()
		}

		if first, second := render(), render(); first != second {
			t.Fatalf("%s: output is not deterministic:\nfirst:\n%s\nsecond:\n%s",
				kind, first, second)
		}
	})
}
