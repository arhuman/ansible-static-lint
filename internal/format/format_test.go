package format

import (
	"encoding/json"
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
		if err := SARIF(&b, []rules.Finding{worded}, "test", style, "", rules.Selection{}); err != nil {
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
	if err := SARIF(&b, []rules.Finding{{Path: "a.yml", Line: 1, Tag: "name[play]", Message: "m"}}, "test", rules.IDUpstream, "", rules.Selection{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"version": "2.1.0"`) {
		t.Fatalf("missing sarif version: %s", b.String())
	}
	if !strings.Contains(b.String(), `"ruleId": "name[play]"`) {
		t.Fatalf("missing ruleId: %s", b.String())
	}
}

// parsedSARIF is the shape the assertions below read. It restates the fields
// rather than decoding into the emitter's own types, so that a renamed JSON tag
// fails a test instead of round-tripping through the struct that renamed it.
type parsedSARIF struct {
	Runs []struct {
		Tool struct {
			Driver struct {
				Rules []struct {
					ID               string `json:"id"`
					Name             string `json:"name"`
					ShortDescription struct {
						Text string `json:"text"`
					} `json:"shortDescription"`
					HelpURI    string            `json:"helpUri"`
					Properties map[string]string `json:"properties"`
				} `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		ColumnKind  string `json:"columnKind"`
		Invocations []struct {
			ExecutionSuccessful bool `json:"executionSuccessful"`
			WorkingDirectory    *struct {
				URI string `json:"uri"`
			} `json:"workingDirectory"`
		} `json:"invocations"`
		Results []struct {
			RuleID string `json:"ruleId"`
			Level  string `json:"level"`
		} `json:"results"`
		Properties struct {
			Scope struct {
				Note       string   `json:"note"`
				Taxonomy   string   `json:"taxonomy"`
				Supported  []string `json:"supported"`
				Enabled    []string `json:"enabled"`
				OutOfScope []struct {
					ID       string `json:"id"`
					Requires string `json:"requires"`
				} `json:"outOfScope"`
			} `json:"astl.scope"`
		} `json:"properties"`
	} `json:"runs"`
}

func decodeSARIF(t *testing.T, findings []rules.Finding, style rules.IDStyle) parsedSARIF {
	t.Helper()
	return decodeRun(t, findings, style, "", rules.Selection{})
}

// decodeRun is decodeSARIF for the two fields that describe the run rather than
// its findings: the invocation's working directory and the selection the scope
// block reports as enabled.
func decodeRun(t *testing.T, findings []rules.Finding, style rules.IDStyle, workDir string, sel rules.Selection) parsedSARIF {
	t.Helper()
	var b strings.Builder
	if err := SARIF(&b, findings, "test", style, workDir, sel); err != nil {
		t.Fatal(err)
	}
	var doc parsedSARIF
	if err := json.Unmarshal([]byte(b.String()), &doc); err != nil {
		t.Fatalf("SARIF output is not valid JSON: %v", err)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(doc.Runs))
	}
	return doc
}

// TestSARIFResultsResolveToADescriptor is the assertion an editor depends on:
// a result whose ruleId names no entry in tool.driver.rules renders with no
// name and no help link. Every tag astl can emit must therefore be declared,
// in whichever taxonomy the run was asked for.
func TestSARIFResultsResolveToADescriptor(t *testing.T) {
	for _, style := range []rules.IDStyle{rules.IDUpstream, rules.IDNative} {
		findings := make([]rules.Finding, 0, len(rules.Descriptors(style)))
		for _, d := range rules.Descriptors(style) {
			findings = append(findings, rules.Finding{Path: "a.yml", Line: 1, Tag: d.Upstream, Message: "m"})
		}
		doc := decodeSARIF(t, findings, style)
		declared := make(map[string]bool, len(doc.Runs[0].Tool.Driver.Rules))
		for _, r := range doc.Runs[0].Tool.Driver.Rules {
			if declared[r.ID] {
				t.Errorf("%s: descriptor %q declared twice", style, r.ID)
			}
			declared[r.ID] = true
		}
		for _, r := range doc.Runs[0].Results {
			if !declared[r.RuleID] {
				t.Errorf("%s: result ruleId %q has no descriptor", style, r.RuleID)
			}
		}
	}
}

// TestSARIFDescriptorsCarryBothTaxonomies keeps the descriptor useful to a
// consumer that reads one taxonomy and suppresses in the other, which is the
// case for an editor sharing a repository's `.ansible-lint`.
func TestSARIFDescriptorsCarryBothTaxonomies(t *testing.T) {
	doc := decodeSARIF(t, nil, rules.IDNative)
	for _, r := range doc.Runs[0].Tool.Driver.Rules {
		if r.Properties["upstreamId"] == "" || r.Properties["nativeId"] == "" {
			t.Errorf("descriptor %q is missing a taxonomy: %v", r.ID, r.Properties)
		}
		if r.ID != r.Properties["nativeId"] {
			t.Errorf("descriptor id %q should be the native id under --ids native, got %q", r.ID, r.Properties["nativeId"])
		}
		if r.Name != r.Properties["upstreamId"] {
			t.Errorf("descriptor %q should name its upstream counterpart, got %q", r.ID, r.Name)
		}
		if !strings.HasPrefix(r.HelpURI, "https://docs.ansible.com/projects/lint/rules/") {
			t.Errorf("descriptor %q has no upstream help link: %q", r.ID, r.HelpURI)
		}
	}
}

// TestSARIFCarriesTheSeverity covers the one signal a consumer keys its own
// severity mapping on. pep8 says it with a trailing ` (warning)` and that
// suffix is tested end to end; SARIF says it with `level`, and nothing tested
// that until now. Everything the ignore file and `warn_list` demote arrives
// here, so a run of accepted debt would otherwise report as errors.
func TestSARIFCarriesTheSeverity(t *testing.T) {
	tests := map[string]struct {
		finding rules.Finding
		want    string
	}{
		"plain":             {rules.Finding{Path: "a.yml", Line: 3, Tag: "no-changed-when", Message: "m"}, "error"},
		"warn_list":         {rules.Finding{Path: "a.yml", Line: 3, Tag: "no-changed-when", Message: "m", Warning: true}, "warning"},
		"ignore file entry": {rules.Finding{Path: "a.yml", Line: 3, Tag: "no-changed-when", Message: "m", Warning: true, Ignored: true}, "warning"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			doc := decodeSARIF(t, []rules.Finding{tc.finding}, rules.IDUpstream)
			if len(doc.Runs[0].Results) != 1 {
				t.Fatalf("got %d results, want 1", len(doc.Runs[0].Results))
			}
			if got := doc.Runs[0].Results[0].Level; got != tc.want {
				t.Errorf("level %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSARIFRegionOmitsAnAbsentColumn pins what docs/sarif.md tells an
// integrator: most rules report a line and no column, and the region says so by
// leaving the field out rather than sending 0. A zero would place a marker the
// finding does not claim, and SARIF numbers columns from 1.
func TestSARIFRegionOmitsAnAbsentColumn(t *testing.T) {
	findings := []rules.Finding{
		{Path: "a.yml", Line: 3, Tag: "no-changed-when", Message: "m"},
		{Path: "a.yml", Line: 4, Column: 7, Tag: "name[play]", Message: "m"},
	}
	var b strings.Builder
	if err := SARIF(&b, findings, "test", rules.IDUpstream, "", rules.Selection{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `"startColumn": 0`) {
		t.Errorf("a column-less finding sent startColumn 0: %s", b.String())
	}
	if !strings.Contains(b.String(), `"startColumn": 7`) {
		t.Errorf("a finding with a column lost it: %s", b.String())
	}
}

// TestSARIFCleanRunIsStillAWellFormedReport covers the common CI case. A run
// with nothing to report must send an empty results array, not null: the
// schema requires an array, and a consumer iterating it would fault. The rest
// of the document still has to describe the tool, which is the point of
// declaring the scope in the first place.
func TestSARIFCleanRunIsStillAWellFormedReport(t *testing.T) {
	for name, findings := range map[string][]rules.Finding{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			if err := SARIF(&b, findings, "test", rules.IDUpstream, "", rules.Selection{}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(b.String(), `"results": []`) {
				t.Errorf("a clean run did not emit an empty results array: %s", b.String())
			}
			doc := decodeSARIF(t, findings, rules.IDUpstream)
			if len(doc.Runs[0].Tool.Driver.Rules) == 0 {
				t.Error("a clean run declares no rules, so a consumer cannot tell it ran")
			}
			if len(doc.Runs[0].Properties.Scope.OutOfScope) == 0 {
				t.Error("a clean run does not say what it skipped, which is when it matters most")
			}
		})
	}
}

// TestSARIFEscapesTextTakenFromTheRepository is the SARIF half of the property
// TestPEP8KeepsOneFindingOnOneLine states for pep8. A path is chosen by the
// linted repository, so it may hold a newline or an escape sequence. pep8 needs
// a sanitizing pass; SARIF is asserted to need none, because the JSON encoder
// escapes them, and that claim deserves a test rather than a comment.
func TestSARIFEscapesTextTakenFromTheRepository(t *testing.T) {
	forged := "ok.yml\nvictim.yml:9:1: syntax-check: Injected"
	var b strings.Builder
	findings := []rules.Finding{{Path: forged, Line: 1, Tag: "name", Message: "a\x1b[31mred"}}
	if err := SARIF(&b, findings, "test", rules.IDUpstream, "", rules.Selection{}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"\n" + "victim.yml", "\x1b["} {
		if strings.Contains(b.String(), raw) {
			t.Errorf("raw control sequence %q reached the output", raw)
		}
	}
	// Escaped, not dropped: SARIF carries the path a consumer must resolve, so
	// unlike pep8 nothing may be removed from it.
	doc := decodeSARIF(t, findings, rules.IDUpstream)
	if got := doc.Runs[0].Results[0].RuleID; got != "name" {
		t.Fatalf("ruleId %q", got)
	}
	if !strings.Contains(b.String(), `\n`) {
		t.Errorf("the newline was dropped rather than escaped: %s", b.String())
	}
}

func TestPlainText(t *testing.T) {
	tests := map[string]string{
		"a command can change the host with no `changed_when` guard": "a command can change the host with no changed_when guard",
		"no code span here":        "no code span here",
		"`leading` and `trailing`": "leading and trailing",
		"":                         "",
	}
	for in, want := range tests {
		if got := plainText(in); got != want {
			t.Errorf("plainText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSARIFDeclaresHowItCountsColumns pins a claim astl has to be able to
// defend. yaml.v3's scanner advances a column per code point, so that is what
// a column number counts here. ansible-lint declares utf16CodeUnits over
// Python string indices, which is the same count inside the BMP and wrong
// outside it; copying the declaration would make astl assert something untrue
// (ADR 0007).
func TestSARIFDeclaresHowItCountsColumns(t *testing.T) {
	if got := decodeSARIF(t, nil, rules.IDUpstream).Runs[0].ColumnKind; got != "unicodeCodePoints" {
		t.Fatalf("columnKind is %q, want unicodeCodePoints", got)
	}
}

// TestSARIFDescriptionsAreNativeAndPlain covers both halves of the field: it
// carries astl's own wording rather than upstream's, and it is rendered for a
// SARIF `text` field, which a viewer shows verbatim and where the markdown
// code spans the description is written with would read as literal backticks.
func TestSARIFDescriptionsAreNativeAndPlain(t *testing.T) {
	described := 0
	for _, r := range decodeSARIF(t, nil, rules.IDUpstream).Runs[0].Tool.Driver.Rules {
		if r.ShortDescription.Text == "" {
			t.Errorf("descriptor %q has no description", r.ID)
			continue
		}
		if strings.Contains(r.ShortDescription.Text, "`") {
			t.Errorf("descriptor %q leaks a code span into plain text: %q", r.ID, r.ShortDescription.Text)
		}
		described++
	}
	if described != len(rules.Descriptors(rules.IDUpstream)) {
		t.Errorf("described %d rules, declared %d", described, len(rules.Descriptors(rules.IDUpstream)))
	}
	// The one upstream sentence astl does carry verbatim is a finding's
	// message, under --ids upstream. A rule description is not that, and must
	// not become a copy of it.
	for _, r := range decodeSARIF(t, nil, rules.IDUpstream).Runs[0].Tool.Driver.Rules {
		if r.ID == "no-changed-when" && r.ShortDescription.Text == worded.Message {
			t.Errorf("descriptor %q reproduces upstream's message rather than describing the rule", r.ID)
		}
	}
}

// TestSARIFDeclaresItsScope is the point of the property block: a report that
// carries no findings for `fqcn` must say that `fqcn` was never evaluated,
// rather than let a consumer read silence as a pass.
func TestSARIFDeclaresItsScope(t *testing.T) {
	scope := decodeSARIF(t, nil, rules.IDUpstream).Runs[0].Properties.Scope
	if scope.Note == "" || scope.Taxonomy != "upstream" {
		t.Errorf("scope block is not self-describing: %+v", scope)
	}
	if len(scope.Supported) != len(rules.IDs) {
		t.Errorf("got %d supported rules, want %d", len(scope.Supported), len(rules.IDs))
	}
	supported := make(map[string]bool, len(scope.Supported))
	for _, id := range scope.Supported {
		supported[id] = true
	}
	if len(scope.OutOfScope) != len(rules.OutOfScope) {
		t.Fatalf("got %d out-of-scope rules, want %d", len(scope.OutOfScope), len(rules.OutOfScope))
	}
	for _, r := range scope.OutOfScope {
		if supported[r.ID] {
			t.Errorf("%q is declared both supported and out of scope", r.ID)
		}
		if r.Requires == "" {
			t.Errorf("out-of-scope rule %q says nothing about what it requires", r.ID)
		}
	}
}

// TestSARIFScopeStaysUpstreamUnderNativeIDs pins the one deliberate mixing of
// taxonomies. Results and descriptors follow --ids, but the scope block cannot:
// an unimplemented rule has no native name, so naming half the block natively
// would make the two lists incomparable.
func TestSARIFScopeStaysUpstreamUnderNativeIDs(t *testing.T) {
	scope := decodeSARIF(t, nil, rules.IDNative).Runs[0].Properties.Scope
	if scope.Taxonomy != "upstream" {
		t.Fatalf("scope taxonomy is %q under --ids native, want upstream", scope.Taxonomy)
	}
	for _, id := range scope.Supported {
		if rules.Canonical(id) != id {
			t.Errorf("supported id %q is not an upstream id", id)
		}
	}
}

// TestSARIFRecordsWorkingDirectory covers what the invocation block is for: a
// result's artifact URI stays relative, which only resolves against a base, and
// a report read from anywhere other than where it was produced has no other way
// to learn that base.
func TestSARIFRecordsWorkingDirectory(t *testing.T) {
	inv := decodeRun(t, nil, rules.IDUpstream, "/repo/ansible", rules.Selection{}).Runs[0].Invocations
	if len(inv) != 1 {
		t.Fatalf("got %d invocations, want 1", len(inv))
	}
	if !inv[0].ExecutionSuccessful {
		t.Error("executionSuccessful is false on a completed run")
	}
	if inv[0].WorkingDirectory == nil {
		t.Fatal("invocation carries no workingDirectory")
	}
	// The spec asks for an absolute URI, and a directory URI ends with a slash
	// so a consumer resolving `roles/x.yml` against it does not lose the last
	// segment.
	if got := inv[0].WorkingDirectory.URI; got != "file:///repo/ansible/" {
		t.Errorf("workingDirectory uri = %q, want file:///repo/ansible/", got)
	}
}

// TestSARIFOmitsUnknownWorkingDirectory pins the other half: when the working
// directory could not be read the results carry absolute paths, so there is
// nothing to declare and an empty invocation would only assert something false.
func TestSARIFOmitsUnknownWorkingDirectory(t *testing.T) {
	var b strings.Builder
	if err := SARIF(&b, nil, "test", rules.IDUpstream, "", rules.Selection{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "invocations") {
		t.Errorf("empty workDir still emitted an invocations array: %s", b.String())
	}
}

// TestSARIFScopeListsEnabledRules is the distinction the block exists to draw.
// supported says what astl implements; enabled says what this run configured on.
// A rule in neither enabled nor outOfScope reported nothing because it was
// switched off, which is again not a pass.
func TestSARIFScopeListsEnabledRules(t *testing.T) {
	sel := rules.Selection{Profile: "basic", SkipList: []string{"name"}, EnableList: []string{"no-prompting"}}
	scope := decodeRun(t, nil, rules.IDUpstream, "", sel).Runs[0].Properties.Scope
	if len(scope.Enabled) == 0 {
		t.Fatal("scope declares no enabled rules")
	}

	supported := make(map[string]bool, len(scope.Supported))
	for _, id := range scope.Supported {
		supported[id] = true
	}
	outOfScope := make(map[string]bool, len(scope.OutOfScope))
	for _, r := range scope.OutOfScope {
		outOfScope[r.ID] = true
	}
	enabled := make(map[string]bool, len(scope.Enabled))
	for _, id := range scope.Enabled {
		if !supported[id] {
			t.Errorf("enabled rule %q is not declared supported", id)
		}
		if outOfScope[id] {
			t.Errorf("%q is declared both enabled and out of scope", id)
		}
		enabled[id] = true
	}
	if enabled["name"] {
		t.Error("a skip_list rule is still declared enabled")
	}
	if !enabled["no-prompting"] {
		t.Error("an enable_list rule is not declared enabled")
	}
	if len(scope.Enabled) >= len(scope.Supported) {
		t.Errorf("a profile with a skip_list enabled %d of %d rules, want fewer",
			len(scope.Enabled), len(scope.Supported))
	}
}

// TestSARIFEnabledStaysUpstreamUnderNativeIDs extends the rule that governs the
// rest of the scope block to its new list: an id there is comparable with
// outOfScope, which has no native spelling, so it stays upstream.
func TestSARIFEnabledStaysUpstreamUnderNativeIDs(t *testing.T) {
	sel := rules.Selection{EnableList: []string{"no-prompting"}}
	for _, id := range decodeRun(t, nil, rules.IDNative, "", sel).Runs[0].Properties.Scope.Enabled {
		if rules.Canonical(id) != id {
			t.Errorf("enabled id %q is not an upstream id", id)
		}
	}
}
