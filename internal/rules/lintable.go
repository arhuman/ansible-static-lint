package rules

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// allowedSanityIgnores is the list of sanity test results a collection may
// ignore, defined by the Partner Engineering team.
var allowedSanityIgnores = map[string]bool{
	"validate-modules:missing-gplv3-license": true,
	"action-plugin-docs":                     true,
	"import-2.6":                             true, "import-2.6!skip": true,
	"import-2.7": true, "import-2.7!skip": true,
	"import-3.5": true, "import-3.5!skip": true,
	"compile-2.6": true, "compile-2.6!skip": true,
	"compile-2.7": true, "compile-2.7!skip": true,
	"compile-3.5": true, "compile-3.5!skip": true,
	"shebang": true, "shellcheck": true,
	"pylint:used-before-assignment": true,
}

// uncheckedIgnoreFiles are the ignore files of ansible versions old enough that
// their entries are no longer judged.
var uncheckedIgnoreFiles = []string{"ignore-2.9", "ignore-2.10", "ignore-2.11", "ignore-2.12"}

// sanityCheckedDirs are the directories whose ignore entries are policed; an
// entry pointing anywhere else is none of this rule's business.
var sanityCheckedDirs = map[string]bool{"plugins": true, "roles": true}

// lintableRules holds the rules that judge a file as a whole rather than any
// task or play inside it.
func lintableRules(f *parse.File, opt Options) []Finding {
	var out []Finding
	out = append(out, playbookExtension(f)...)
	out = append(out, complexityTasks(f, opt)...)
	if f.Kind == "galaxy" && opt.enabled("galaxy-version-incorrect") {
		out = append(out, galaxyVersionIncorrect(f)...)
	}
	return out
}

func playbookExtension(f *parse.File) []Finding {
	if f.Kind != "playbook" {
		return nil
	}
	switch filepath.Ext(f.Path) {
	case ".yml", ".yaml":
		return nil
	}
	return []Finding{whole(f, "playbook-extension", `Use ".yml" or ".yaml" playbook extension.`,
		"This playbook does not end in .yml or .yaml. Rename it so tooling finds it.")}
}

func sanityRules(f *parse.File) []Finding {
	for _, name := range uncheckedIgnoreFiles {
		if strings.Contains(f.Abs, name) {
			return nil
		}
	}
	var out []Finding
	for i, entry := range strings.Split(f.Text, "\n") {
		line := i + 1
		if entry == "" {
			continue
		}
		dir, _, _ := strings.Cut(entry, "/")
		if !sanityCheckedDirs[dir] {
			continue
		}
		if comment := strings.IndexByte(entry, '#'); comment >= 0 {
			entry = entry[:comment]
		}
		fields := strings.Fields(entry)
		if len(fields) != 2 {
			out = append(out, onLine(f, line, "sanity[bad-ignore]",
				fmt.Sprintf("Ignore file entry at %d is formatted incorrectly. Please review.", line),
				"This ignore entry is malformed. Give it a file path and a test name."))
			continue
		}
		if !allowedSanityIgnores[fields[1]] {
			out = append(out, onLine(f, line, "sanity[cannot-ignore]",
				fmt.Sprintf("Ignore file contains %s at line %d, which is not a permitted ignore.", fields[1], line),
				"This entry ignores a check that cannot be ignored. Fix the underlying issue."))
		}
	}
	return out
}

// galaxyVersionIncorrect rejects a pre-1.0.0 collection version. The comparison
// is upstream's: dot-separated components compared as strings, not numbers.
func galaxyVersionIncorrect(f *parse.File) []Finding {
	version := parse.MapGet(f.Root, "version")
	v := parse.Str(version)
	if v != "" && !versionBelowOne(v) {
		return nil
	}
	const msg = "collection version should be greater than or equal to 1.0.0"
	const nativeMsg = "This collection's version is below 1.0.0. Set 1.0.0 or greater."
	if version == nil {
		return []Finding{whole(f, "galaxy-version-incorrect", msg, nativeMsg)}
	}
	return []Finding{at(f, version, "galaxy-version-incorrect", msg, nativeMsg)}
}

func versionBelowOne(v string) bool {
	got := strings.Split(v, ".")
	want := []string{"1", "0", "0"}
	for i := range got {
		if i >= len(want) {
			return false
		}
		if got[i] != want[i] {
			return got[i] < want[i]
		}
	}
	return len(got) < len(want)
}
