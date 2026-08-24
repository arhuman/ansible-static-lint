// Package format renders findings as pep8 lines or SARIF 2.1.0 documents.
package format

import (
	"encoding/json"
	"fmt"
	"io"
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

// SARIF writes a minimal SARIF 2.1.0 document, naming rules and wording their
// messages in the requested taxonomy.
func SARIF(w io.Writer, findings []rules.Finding, version string, style rules.IDStyle) error {
	type region struct {
		StartLine   int `json:"startLine"`
		StartColumn int `json:"startColumn,omitempty"`
	}
	type artifact struct {
		URI string `json:"uri"`
	}
	type physical struct {
		ArtifactLocation artifact `json:"artifactLocation"`
		Region           region   `json:"region"`
	}
	type location struct {
		PhysicalLocation physical `json:"physicalLocation"`
	}
	type message struct {
		Text string `json:"text"`
	}
	type result struct {
		RuleID    string     `json:"ruleId"`
		Level     string     `json:"level"`
		Message   message    `json:"message"`
		Locations []location `json:"locations"`
	}
	type driver struct {
		Name           string `json:"name"`
		Version        string `json:"version"`
		InformationURI string `json:"informationUri"`
	}
	type tool struct {
		Driver driver `json:"driver"`
	}
	type run struct {
		Tool    tool     `json:"tool"`
		Results []result `json:"results"`
	}
	type doc struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []run  `json:"runs"`
	}

	results := make([]result, 0, len(findings))
	for _, f := range findings {
		level := "error"
		if f.Warning {
			level = "warning"
		}
		results = append(results, result{
			RuleID:  rules.TagFor(f.Tag, style),
			Level:   level,
			Message: message{Text: f.MessageFor(style)},
			Locations: []location{{PhysicalLocation: physical{
				ArtifactLocation: artifact{URI: f.Path},
				Region:           region{StartLine: f.Line, StartColumn: f.Column},
			}}},
		})
	}
	out := doc{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []run{{
			Tool:    tool{Driver: driver{Name: "astl", Version: version, InformationURI: "https://github.com/arhuman/ansible-static-lint"}},
			Results: results,
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
