// Package format renders findings as pep8 lines or SARIF 2.1.0 documents.
package format

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// bbKnown lists the tags ansible-lint's console renderer maps to a style
// (ansiblelint/output.py, _bbcode_to_ansi_mappings). Everything else is an
// unknown tag to that renderer, which is what leaks artifacts into its
// uncolored output.
var bbKnown = map[string]bool{
	"bold": true, "dim": true, "warning": true, "error": true, "info": true,
	"debug": true, "notset": true, "repr.path": true, "repr.number": true,
	"repr.link": true, "failed": true, "success": true,
}

// reBBTag is upstream's tag_pattern, `\[([\w.]+)(?:=(.*?))?\]|\[/\]`, with
// Python's unicode `\w` spelled out for RE2.
var reBBTag = regexp.MustCompile(`\[([\p{L}\p{N}_.]+)(?:=(.*?))?\]|\[/\]`)

// renderBB reproduces ansible-lint's plain-style BBCode rendering
// (ansiblelint/output.py, since 26.x rich is gone and this stack machine is
// the renderer). A known tag renders as nothing; an unknown tag is kept
// verbatim and pushed as "unknown" (except `link`, which is not pushed); a
// `[/]` prints literally exactly when it pops an unknown tag or an empty
// stack. Every stray `[/]` artifact in upstream's pep8 output is this
// machine's residue, so the emulation is the machine itself, not a heuristic
// over its outputs. Upstream's later `[link=url]title[/link]` substitution
// pass is not modeled: the pep8 template emits no link markup.
func renderBB(text string) string {
	var out strings.Builder
	var stack []bool // true = known tag
	idx := 0
	for _, m := range reBBTag.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(text[idx:m[0]])
		idx = m[1]
		if m[2] < 0 { // the `[/]` alternative
			if n := len(stack); n > 0 {
				known := stack[n-1]
				stack = stack[:n-1]
				if known {
					continue
				}
			}
			out.WriteString("[/]")
			continue
		}
		name := text[m[2]:m[3]]
		switch {
		case bbKnown[name]:
			stack = append(stack, true)
		case name == "link":
			out.WriteString(text[m[0]:m[1]])
		default:
			out.WriteString(text[m[0]:m[1]])
			stack = append(stack, false)
		}
	}
	out.WriteString(text[idx:])
	return out.String()
}

// PEP8 writes findings in `path:line[:column]: tag: message` form. The style
// selects both taxonomies at once: identifiers and message wording.
//
// Findings the ignore file marked print first, as a block, ahead of every other
// finding. That is ansible-lint's own ordering rather than a preference: it
// partitions its matches into ignored and fatal and prints the two in that
// order, each keeping the sort. Both blocks are already sorted here, so a
// stable pass over each reproduces it.
func PEP8(w io.Writer, findings []rules.Finding, style rules.IDStyle) error {
	for _, f := range findings {
		if !f.Ignored {
			continue
		}
		if err := writePEP8(w, f, style); err != nil {
			return err
		}
	}
	for _, f := range findings {
		if f.Ignored {
			continue
		}
		if err := writePEP8(w, f, style); err != nil {
			return err
		}
	}
	return nil
}

func writePEP8(w io.Writer, f rules.Finding, style rules.IDStyle) error {
	pos := fmt.Sprintf("%d", f.Line)
	if f.Column > 0 {
		pos = fmt.Sprintf("%d:%d", f.Line, f.Column)
	}
	path, tag, msg := sanitize(f.Path), rules.TagFor(f.Tag, style), sanitize(f.MessageFor(style))
	if style == rules.IDNative {
		level := ""
		if f.Warning {
			level = " (warning)"
		}
		_, err := fmt.Fprintf(w, "%s:%s: %s: %s%s\n", path, pos, tag, msg, level)
		return err
	}
	// Upstream's PEP8Formatter template, rendered through the same machine, so
	// its `[/]` artifacts land byte for byte wherever upstream leaves them.
	level := "error"
	if f.Warning {
		level = "warning"
	}
	line := fmt.Sprintf("[repr.path]%s[/][dim]:%s:[/] [%s][bold]%s[/]: %s[/]", path, pos, level, tag, msg)
	if f.Warning {
		line += fmt.Sprintf(" [dim][%s](%s)[/][/]", level, level)
	}
	_, err := fmt.Fprintln(w, renderBB(line))
	return err
}

// sanitize drops the control characters that would let text taken from the
// linted repository drive the terminal it is printed to: the C0 range except
// tab, DEL, and the C1 range, which includes ESC and so covers ANSI and OSC
// sequences. SARIF needs no such pass, its JSON encoder escapes them.
//
// It runs on the path as well as the message, and the path is the sharper of
// the two. A message is interpolated from a document astl parsed, but a path is
// a filename, and a repository may hold one containing a newline: unsanitized,
// a single finding then renders as two pep8 lines, which is enough to forge a
// finding against a file the repository does not own. pep8 output is parsed by
// editors and CI annotators, so one finding must stay one line.
//
// This is a deliberate divergence from ansible-lint, which prints such a path
// verbatim. It costs nothing on the parity corpus, whose paths are all
// ordinary, and refusing it would mean reproducing a defect for its own sake.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return r
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		}
		return r
	}, s)
}

// upstreamRuleDoc prefixes ansible-lint's own page for a rule. Subtags have no
// page, so a descriptor links its base rule. Every page the table can produce
// was checked to resolve.
const upstreamRuleDoc = "https://docs.ansible.com/projects/lint/rules/"

// SARIF writes a SARIF 2.1.0 document, naming rules and wording their messages
// in the requested taxonomy. Beyond the findings it declares what astl is: the
// driver lists every rule it can report, with both taxonomies and a link to
// upstream's page, and a run-level `astl.scope` property names the rules it
// deliberately does not implement. A consumer can therefore tell a rule that
// found nothing from a rule that never ran, which is the difference between
// astl's report and a full ansible-lint run.
//
// workDir is the directory a result's relative artifact URI is relative to,
// declared as the run's invocation working directory so a report that is saved
// or moved can still resolve its paths. An empty workDir emits no invocations
// array rather than an unresolvable one. sel is the run's configuration, from
// which the scope block names the rules this run could actually report: a
// subset of the supported list, which says only what astl implements.
func SARIF(w io.Writer, findings []rules.Finding, version string, style rules.IDStyle, workDir string, sel rules.Selection) error {
	descriptors, supported := sarifRules(style)
	out := sarifDoc{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "astl",
				Version:        version,
				InformationURI: "https://github.com/arhuman/ansible-static-lint",
				Rules:          descriptors,
			}},
			ColumnKind:  "unicodeCodePoints",
			Invocations: sarifInvocations(workDir),
			Results:     sarifResults(findings, style),
			Properties: sarifRunProperties{Scope: sarifScope{
				Note:       scopeNote,
				Taxonomy:   string(rules.IDUpstream),
				Supported:  supported,
				Enabled:    rules.EnabledRules(sel),
				OutOfScope: sarifOutOfScope(),
			}},
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// scopeNote states, inside the report, what the report is not. A consumer that
// reads only the results cannot infer it, and the whole point of the block is
// that silence about a rule is not a verdict on it.
const scopeNote = "astl reports only the ansible-lint rules decidable from YAML source alone. " +
	"A rule listed under outOfScope is never evaluated, so the absence of a finding " +
	"for it is not a pass. Run ansible-lint for those."

type (
	sarifRegion struct {
		StartLine int `json:"startLine"`
		// StartColumn is omitted rather than sent as 0 for the rules that
		// report no column: upstream reports a line for them, and a fabricated
		// column would place a marker the finding does not claim.
		StartColumn int `json:"startColumn,omitempty"`
	}
	sarifArtifact struct {
		URI string `json:"uri"`
	}
	sarifPhysical struct {
		ArtifactLocation sarifArtifact `json:"artifactLocation"`
		Region           sarifRegion   `json:"region"`
	}
	sarifLocation struct {
		PhysicalLocation sarifPhysical `json:"physicalLocation"`
	}
	sarifMessage struct {
		Text string `json:"text"`
	}
	sarifResult struct {
		RuleID    string          `json:"ruleId"`
		Level     string          `json:"level"`
		Message   sarifMessage    `json:"message"`
		Locations []sarifLocation `json:"locations"`
	}
	sarifDescriptor struct {
		ID               string            `json:"id"`
		Name             string            `json:"name"`
		ShortDescription sarifMessage      `json:"shortDescription"`
		HelpURI          string            `json:"helpUri"`
		Properties       map[string]string `json:"properties"`
	}
	sarifDriver struct {
		Name           string            `json:"name"`
		Version        string            `json:"version"`
		InformationURI string            `json:"informationUri"`
		Rules          []sarifDescriptor `json:"rules"`
	}
	sarifTool struct {
		Driver sarifDriver `json:"driver"`
	}
	sarifUnsupported struct {
		ID       string `json:"id"`
		Requires string `json:"requires"`
	}
	sarifScope struct {
		Note      string   `json:"note"`
		Taxonomy  string   `json:"taxonomy"`
		Supported []string `json:"supported"`
		// Enabled is what this run could report; Supported is what astl can
		// report at all. A rule in Supported but not Enabled was configured off,
		// so its silence is no more a pass than an out-of-scope rule's.
		Enabled    []string           `json:"enabled"`
		OutOfScope []sarifUnsupported `json:"outOfScope"`
	}
	sarifInvocation struct {
		ExecutionSuccessful bool           `json:"executionSuccessful"`
		WorkingDirectory    *sarifArtifact `json:"workingDirectory,omitempty"`
	}
	sarifRunProperties struct {
		Scope sarifScope `json:"astl.scope"`
	}
	sarifRun struct {
		Tool sarifTool `json:"tool"`
		// ColumnKind states what a column number counts. astl counts code
		// points, which is what yaml.v3's scanner tracks; ansible-lint
		// declares utf16CodeUnits while counting Python string indices, so
		// its declaration is wrong outside the BMP and is not copied here
		// (ADR 0007).
		ColumnKind  string             `json:"columnKind"`
		Invocations []sarifInvocation  `json:"invocations,omitempty"`
		Results     []sarifResult      `json:"results"`
		Properties  sarifRunProperties `json:"properties"`
	}
	sarifDoc struct {
		Schema  string     `json:"$schema"`
		Version string     `json:"version"`
		Runs    []sarifRun `json:"runs"`
	}
)

// sarifInvocations declares the directory the results' relative artifact URIs
// resolve against, as the absolute `file:` URI the spec asks for, with the
// trailing slash that marks a directory. executionSuccessful is true because
// the document is only written once the run completed: a run that failed
// outright exits before any format is rendered.
//
// An empty workDir means the working directory could not be read, and the
// results carry absolute paths instead. Nothing then needs a base, so the whole
// array is omitted rather than filled with a URI that resolves nowhere.
func sarifInvocations(workDir string) []sarifInvocation {
	if workDir == "" {
		return nil
	}
	uri := (&url.URL{Scheme: "file", Path: workDir + "/"}).String()
	return []sarifInvocation{{
		ExecutionSuccessful: true,
		WorkingDirectory:    &sarifArtifact{URI: uri},
	}}
}

func sarifResults(findings []rules.Finding, style rules.IDStyle) []sarifResult {
	out := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		level := "error"
		if f.Warning {
			level = "warning"
		}
		out = append(out, sarifResult{
			RuleID:  rules.TagFor(f.Tag, style),
			Level:   level,
			Message: sarifMessage{Text: f.MessageFor(style)},
			Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
				ArtifactLocation: sarifArtifact{URI: f.Path},
				Region:           sarifRegion{StartLine: f.Line, StartColumn: f.Column},
			}}},
		})
	}
	return out
}

// sarifRules renders one descriptor per reportable tag, and alongside it the
// bare rule ids the scope block declares as supported. The two are built from
// one walk so a rule cannot be described and left out of the scope, or the
// reverse.
func sarifRules(style rules.IDStyle) ([]sarifDescriptor, []string) {
	all := rules.Descriptors(style)
	descriptors := make([]sarifDescriptor, 0, len(all))
	supported := make([]string, 0, len(rules.IDs))
	for _, d := range all {
		other := d.Native
		if style == rules.IDNative {
			other = d.Upstream
		}
		descriptors = append(descriptors, sarifDescriptor{
			ID:               d.ID,
			Name:             other,
			ShortDescription: sarifMessage{Text: plainText(d.Description)},
			HelpURI:          upstreamRuleDoc + d.Base + "/",
			Properties:       map[string]string{"upstreamId": d.Upstream, "nativeId": d.Native},
		})
		if d.Upstream == d.Base {
			supported = append(supported, d.Upstream)
		}
	}
	return descriptors, supported
}

// plainText renders a rule description for a SARIF `text` field, which the
// spec defines as plain text a viewer may show verbatim. The descriptions are
// markdown source, shared with docs/rules.md, so their code spans would show
// as literal backticks in a tooltip. Only the delimiters go: the content of a
// span is the identifier being named and has to survive.
func plainText(s string) string {
	return strings.ReplaceAll(s, "`", "")
}

// sarifOutOfScope keeps upstream ids whatever the run's taxonomy: astl has no
// native name for a rule it does not implement, and naming one list natively
// and the other upstream would make the two incomparable.
func sarifOutOfScope() []sarifUnsupported {
	out := make([]sarifUnsupported, 0, len(rules.OutOfScope))
	for _, r := range rules.OutOfScope {
		out = append(out, sarifUnsupported{ID: r.ID, Requires: r.Requires})
	}
	return out
}
