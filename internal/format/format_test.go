package format

import (
	"strconv"
	"strings"
	"testing"

	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// TestTagMarkupLeak pins where upstream's renderer leaks `[/]` artifacts into
// pep8 lines. A `[\w.]+` subtag is an unknown BBCode tag, so the template's
// closer right after it prints literally; a subtag with a dash is not a tag at
// all and leaks nothing.
func TestTagMarkupLeak(t *testing.T) {
	tests := map[string]string{
		"no-changed-when":                   "no-changed-when",
		"name[casing]":                      "name[casing][/]",
		"key-order[task]":                   "key-order[task][/]",
		"galaxy[no-changelog]":              "galaxy[no-changelog]",
		"meta-runtime[unsupported-version]": "meta-runtime[unsupported-version]",
	}
	for in, want := range tests {
		var b strings.Builder
		if err := PEP8(&b, []rules.Finding{{Path: "a.yml", Line: 1, Tag: in, Message: "m"}}, rules.IDUpstream); err != nil {
			t.Fatal(err)
		}
		got := "a.yml:1: " + want + ": m\n"
		if b.String() != got {
			t.Errorf("PEP8(%q) = %q, want %q", in, b.String(), got)
		}
	}
}

// TestMessageMarkupLeak is the message-side half of the same machine (astl
// issue 0012, found on kubespray): a `[\w.]+` sequence inside the message is
// an unknown tag too, and the template's final closer then pops it and prints
// literally at end of line. A bracketed run holding a dash, as in var-naming's
// regex message, is not a tag and leaves no artifact.
func TestMessageMarkupLeak(t *testing.T) {
	tests := map[string]struct{ msg, want string }{
		"word tag leaks": {
			"Avoid paths. (a/{{ b[container_manager] }})",
			"a.yml:1: role-name[path][/]: Avoid paths. (a/{{ b[container_manager] }})[/]\n",
		},
		"dashed run does not": {
			"Variables names should match ^[a-z_][a-z0-9_]*$ regex.",
			"a.yml:1: role-name[path][/]: Variables names should match ^[a-z_][a-z0-9_]*$ regex.\n",
		},
		"one artifact for two tags": {
			"first [foo] then [bar_baz]",
			"a.yml:1: role-name[path][/]: first [foo] then [bar_baz][/]\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			if err := PEP8(&b, []rules.Finding{{Path: "a.yml", Line: 1, Tag: "role-name[path]", Message: tc.msg}}, rules.IDUpstream); err != nil {
				t.Fatal(err)
			}
			if b.String() != tc.want {
				t.Errorf("got %q, want %q", b.String(), tc.want)
			}
		})
	}
}

// TestMessageMarkupLeakBeforeWarningSuffix pins the interaction of the two
// suffixes: the message's leaked closer lands before the ` (warning)` block,
// whose own tags are all known and render silently.
func TestMessageMarkupLeakBeforeWarningSuffix(t *testing.T) {
	var b strings.Builder
	f := rules.Finding{Path: "a.yml", Line: 1, Tag: "run-once[task]", Message: "see [foo]", Warning: true}
	if err := PEP8(&b, []rules.Finding{f}, rules.IDUpstream); err != nil {
		t.Fatal(err)
	}
	want := "a.yml:1: run-once[task][/]: see [foo][/] (warning)\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

// TestTagNativeStyleHasNoMarkupLeak pins that the `[/]` artifact belongs to the
// upstream compatibility contract and never reaches native ids, even when the
// message carries a tag-like bracket.
func TestTagNativeStyleHasNoMarkupLeak(t *testing.T) {
	tests := map[string]string{
		"no-changed-when":                   "task.unguarded-change",
		"name[casing]":                      "name.casing",
		"key-order[task]":                   "task.key-order[task]",
		"galaxy[no-changelog]":              "galaxy.changelog-missing",
		"meta-runtime[unsupported-version]": "meta.runtime-version[unsupported-version]",
	}
	for in, want := range tests {
		var b strings.Builder
		f := rules.Finding{Path: "a.yml", Line: 1, Tag: in, Message: "see [foo]", NativeMessage: "see [foo]"}
		if err := PEP8(&b, []rules.Finding{f}, rules.IDNative); err != nil {
			t.Fatal(err)
		}
		got := "a.yml:1: " + want + ": see [foo]\n"
		if b.String() != got {
			t.Errorf("PEP8(%q, native) = %q, want %q", in, b.String(), got)
		}
	}
}

func TestPEP8(t *testing.T) {
	var b strings.Builder
	err := PEP8(&b, []rules.Finding{
		{Path: "a.yml", Line: 3, Tag: "no-changed-when", Message: "msg"},
		{Path: "b.yml", Line: 2, Column: 1, Tag: "name[play]", Message: "msg2"},
	}, rules.IDUpstream)
	if err != nil {
		t.Fatal(err)
	}
	want := "a.yml:3: no-changed-when: msg\nb.yml:2:1: name[play][/]: msg2\n"
	if b.String() != want {
		t.Fatalf("got %q, want %q", b.String(), want)
	}
}

// worded is one finding carrying both taxonomies, used by the toggle tests.
var worded = rules.Finding{
	Path: "a.yml", Line: 3, Tag: "no-changed-when",
	Message:       "Commands should not change things if nothing needs doing.",
	NativeMessage: "This command always reports changed. Add changed_when, or a creates guard.",
}

// TestPEP8SwitchesMessageWithTheTaxonomy pins that `--ids` selects the wording
// as well as the identifier, and that the default keeps upstream's text.
func TestPEP8SwitchesMessageWithTheTaxonomy(t *testing.T) {
	tests := map[rules.IDStyle]string{
		rules.IDUpstream: "a.yml:3: no-changed-when: Commands should not change things if nothing needs doing.\n",
		rules.IDNative:   "a.yml:3: task.unguarded-change: This command always reports changed. Add changed_when, or a creates guard.\n",
	}
	for style, want := range tests {
		var b strings.Builder
		if err := PEP8(&b, []rules.Finding{worded}, style); err != nil {
			t.Fatal(err)
		}
		if b.String() != want {
			t.Errorf("PEP8(%s) = %q, want %q", style, b.String(), want)
		}
	}
}

// TestSARIFSwitchesMessageWithTheTaxonomy is the same assertion for the SARIF
// result message, which is a separate rendering path.
func TestSARIFSwitchesMessageWithTheTaxonomy(t *testing.T) {
	tests := map[rules.IDStyle]string{
		rules.IDUpstream: worded.Message,
		rules.IDNative:   worded.NativeMessage,
	}
	for style, want := range tests {
		var b strings.Builder
		if err := SARIF(&b, []rules.Finding{worded}, "test", style); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), `"text": `+strconv.Quote(want)) {
			t.Errorf("SARIF(%s) does not carry %q: %s", style, want, b.String())
		}
	}
}

func TestPEP8StripsControlCharacters(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"ansi color":      {"\x1b[31mred\x1b[0m", "[31mred[0m"},
		"osc title":       {"\x1b]0;pwned\x07x", "]0;pwnedx"},
		"newline":         {"a\nb", "ab"},
		"carriage return": {"a\rb", "ab"},
		"c1 csi":          {"a\u009bb", "ab"},
		"del":             {"a\x7fb", "ab"},
		"tab kept":        {"a\tb", "a\tb"},
		"utf8 kept":       {"ä ok", "ä ok"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			if err := PEP8(&b, []rules.Finding{{Path: "a.yml", Line: 1, Tag: "name", Message: tc.in}}, rules.IDUpstream); err != nil {
				t.Fatal(err)
			}
			want := "a.yml:1: name: " + tc.want + "\n"
			if b.String() != want {
				t.Fatalf("got %q, want %q", b.String(), want)
			}
		})
	}
}

// TestPEP8StripsControlCharactersFromThePath covers the other half of the
// sanitized surface. The path is a filename out of the linted repository, so it
// is chosen by whoever wrote that repository, not by astl.
func TestPEP8StripsControlCharactersFromThePath(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"ansi color":      {"\x1b[31mred\x1b[0m.yml", "[31mred[0m.yml"},
		"newline":         {"a\nb.yml", "ab.yml"},
		"carriage return": {"a\rb.yml", "ab.yml"},
		"utf8 kept":       {"rôle/main.yml", "rôle/main.yml"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			if err := PEP8(&b, []rules.Finding{{Path: tc.in, Line: 1, Tag: "name", Message: "m"}}, rules.IDUpstream); err != nil {
				t.Fatal(err)
			}
			want := tc.want + ":1: name: m\n"
			if b.String() != want {
				t.Fatalf("got %q, want %q", b.String(), want)
			}
		})
	}
}

// TestPEP8KeepsOneFindingOnOneLine states the property the sanitizing exists
// for, rather than the mechanism. A filename holding a newline used to split
// one finding across two lines, each of which parses as a valid pep8 record, so
// a repository could forge a finding against a file it does not own by naming a
// file after the line it wanted to inject. Editors and CI annotators read this
// output a line at a time, which is what made it worth diverging from upstream.
func TestPEP8KeepsOneFindingOnOneLine(t *testing.T) {
	forged := "ok.yml\nvictim.yml:9:1: syntax-check[/]: Injected"
	var b strings.Builder
	findings := []rules.Finding{{Path: forged, Line: 1, Tag: "name", Message: "m"}}
	if err := PEP8(&b, findings, rules.IDUpstream); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(b.String(), "\n"); got != 1 {
		t.Fatalf("one finding rendered as %d lines: %q", got, b.String())
	}
	// The injected text still appears, glued onto the real path, which is the
	// point: it is no longer a record of its own. A reader splitting on
	// newlines sees one finding with an odd path, not two findings.
	for _, line := range strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n") {
		if strings.HasPrefix(line, "victim.yml:") {
			t.Fatalf("a forged record stands alone in the output: %q", line)
		}
	}
}

func TestSARIFIsValidJSON(t *testing.T) {
	var b strings.Builder
	if err := SARIF(&b, []rules.Finding{{Path: "a.yml", Line: 1, Tag: "name[play]", Message: "m"}}, "test", rules.IDUpstream); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"version": "2.1.0"`) {
		t.Fatalf("missing sarif version: %s", b.String())
	}
	if !strings.Contains(b.String(), `"ruleId": "name[play]"`) {
		t.Fatalf("missing ruleId: %s", b.String())
	}
}
