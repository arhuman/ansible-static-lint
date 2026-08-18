package yamllint

import (
	"path/filepath"
	"strings"
	"testing"
)

// The markers below are planted in files the fuzzed config can reach through
// `extends`. Neither string is anything astl could produce on its own, so
// seeing one in a message means it was read out of a target file and copied
// into the output.
// They name the two places a target file's own text reaches an error message:
// an unrecognised rule id and an unrecognised option key, both reported by
// name. Values do not need a marker, because options are parsed by hand rather
// than unmarshalled into typed fields, so yaml.v3 never gets to quote a scalar
// back; verified by removing safeExtendsError and watching a value-typed
// failure stay clean while both of these leaked.
const (
	markerRule   = "ZZLEAKRULEZZ"
	markerOption = "ZZLEAKOPTIONZZ"
)

// FuzzLoadConfig fuzzes the yamllint configuration loader, which reads a
// `.yamllint` out of the repository being linted and follows its `extends`
// chain. That file is attacker-chosen under astl's threat model, and both
// defects found in it this session (a chain that never terminates, and error
// messages that quoted the target's content back into the CI log) were found
// by hand.
//
// Beyond not crashing or hanging, it asserts the disclosure boundary that fix
// established: a failure reported for a file reached through `extends` must
// name only the category of the failure. Two files are planted for the fuzzer
// to find, one that fails to parse and one naming an unknown rule, which are
// the two channels that leaked. If either marker reaches an error, the
// boundary is open again.
func FuzzLoadConfig(f *testing.F) {
	dir := f.TempDir()
	write(f, filepath.Join(dir, "leak-option.yml"),
		"rules:\n  line-length:\n    "+markerOption+": 1\n")
	write(f, filepath.Join(dir, "leak-rule.yml"), "rules:\n  "+markerRule+": enable\n")
	write(f, filepath.Join(dir, "ok.yml"), "rules:\n  line-length:\n    max: 200\n")

	for _, seed := range []string{
		"",
		"---\nrules:\n  line-length:\n    max: 120\n",
		"extends: ok.yml\n",
		"extends: leak-option.yml\n",
		"extends: leak-rule.yml\n",
		"extends: .yamllint\n",
		"extends: ../.yamllint\n",
		"extends: /etc/hostname\n",
		"extends: default\nrules:\n  trailing-spaces: disable\n",
		"rules:\n  line-length: enable\n  comments:\n    require-starting-space: false\n",
		"rules:\n  indentation:\n    spaces: consistent\n    indent-sequences: whatever\n",
		"rules: [",
		"ignore: |\n  *.yml\n",
		"rules:\n  line-length:\n    max: -1\n",
		"rules:\n  line-length:\n    max: 99999999999999999999\n",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, cfgText string) {
		write(t, filepath.Join(dir, ".yamllint"), cfgText)

		cfg, warnings, err := Load(dir)
		if err != nil {
			if cfg != nil {
				t.Fatal("Load returned both a config and an error")
			}
		} else if cfg == nil {
			t.Fatal("Load returned neither a config nor an error")
		}

		reported := strings.Join(warnings, "\n")
		if err != nil {
			reported += "\n" + err.Error()
		}
		for _, marker := range []string{markerRule, markerOption} {
			if strings.Contains(reported, marker) {
				t.Fatalf("a file reached through extends leaked into the report: %q", reported)
			}
		}
	})
}
