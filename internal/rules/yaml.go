package rules

import (
	"fmt"
	"path"
	"strings"

	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/yamllint"
)

// yamlRules adapts the yamllint port's problems into yaml[*] findings, the
// way ansible-lint's YamllintRule embeds yamllint: tag `yaml[<rule>]`, the
// description capitalized Python-style, line but no column.
func yamlRules(f *parse.File, opt Options) []Finding {
	if !isYAMLBaseKind(f.Path, f.Kind) {
		return nil
	}
	// yamllint's `ignore` patterns skip the yaml family for a file while every
	// other rule still lints it, so this is scoped to yamlRules rather than to
	// discovery. A nil config is an ignored path.
	cfg := opt.yamllintConfig().ForFile(f.Path)
	if cfg == nil {
		return nil
	}
	workflow := inGithubWorkflows(f.Path)
	var out []Finding
	for _, p := range yamllint.Lint(f.Text, cfg) {
		// Ignore truthy violations in github workflows ("on:" keys).
		if workflow && p.Rule == "truthy" {
			continue
		}
		out = append(out, yamlFinding(f, p))
	}
	return out
}

// isYAMLBaseKind mirrors upstream's BASE_KINDS resolution to text/yaml:
// jinja2 templates and files under a templates/ directory are not YAML, all
// *.yml/*.yaml plus the extensionless lint configs are.
func isYAMLBaseKind(p, kind string) bool {
	switch kind {
	case "jinja2", "text", "python", "sanity-ignore-file":
		return false
	}
	base := path.Base(p)
	if base == ".ansible-lint" || base == ".yamllint" {
		return true
	}
	return strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")
}

// inGithubWorkflows reports whether the file sits directly in a
// .github/workflows directory, upstream's carve-out for workflow files whose
// `on:` key would otherwise trip truthy.
func inGithubWorkflows(p string) bool {
	dir := path.Dir(strings.ReplaceAll(p, "\\", "/"))
	parent := path.Base(dir)
	grand := path.Base(path.Dir(dir))
	return grand == ".github" && parent == "workflows"
}

// yamlFinding words one yamllint problem in both taxonomies. The upstream
// wording is the yamllint description through Python's str.capitalize(),
// which also lowercases any interpolated value; a test checks each branch
// against pyCapitalize so the literals cannot drift from the rule's Desc.
//
//nolint:funlen,gocyclo // one flat wording table, a case per yamllint message form; splitting it would only scatter the pairs.
func yamlFinding(f *parse.File, p yamllint.Problem) Finding {
	switch p.Rule {
	case "anchors":
		return yamlAt(f, p, "yaml[anchors]",
			fmt.Sprintf(`Found undeclared alias "%s"`, pyLower(p.Args[0])),
			fmt.Sprintf(`Alias "%s" has no anchor above it. Declare the anchor before using the alias.`, p.Args[0]))
	case "braces":
		if strings.Contains(p.Desc, "empty") {
			return yamlAt(f, p, "yaml[braces]",
				"Too many spaces inside empty braces",
				"An empty flow mapping carries inner spaces. Write it as {}.")
		}
		return yamlAt(f, p, "yaml[braces]",
			"Too many spaces inside braces",
			"More than one space pads a brace. Keep at most one space inside { }.")
	case "brackets":
		if strings.Contains(p.Desc, "empty") {
			return yamlAt(f, p, "yaml[brackets]",
				"Too many spaces inside empty brackets",
				"An empty flow sequence carries inner spaces. Write it as [].")
		}
		return yamlAt(f, p, "yaml[brackets]",
			"Too many spaces inside brackets",
			"Spaces pad a bracket. Write flow sequences tight, like [1, 2].")
	case "colons":
		switch p.Desc {
		case "too many spaces before colon":
			return yamlAt(f, p, "yaml[colons]",
				"Too many spaces before colon",
				"Whitespace separates this key from its colon. Attach the colon to the key.")
		case "too many spaces after question mark":
			return yamlAt(f, p, "yaml[colons]",
				"Too many spaces after question mark",
				"More than one space follows the question mark. Keep exactly one.")
		default:
			return yamlAt(f, p, "yaml[colons]",
				"Too many spaces after colon",
				"More than one space follows this colon. Keep exactly one.")
		}
	case "commas":
		switch p.Desc {
		case "too many spaces before comma":
			return yamlAt(f, p, "yaml[commas]",
				"Too many spaces before comma",
				"Whitespace precedes this comma. Attach the comma to the item before it.")
		case "too few spaces after comma":
			return yamlAt(f, p, "yaml[commas]",
				"Too few spaces after comma",
				"No space follows this comma. Put one space after it.")
		default:
			return yamlAt(f, p, "yaml[commas]",
				"Too many spaces after comma",
				"More than one space follows this comma. Keep exactly one.")
		}
	case "comments":
		if strings.HasPrefix(p.Desc, "too few spaces before comment") {
			return yamlAt(f, p, "yaml[comments]",
				fmt.Sprintf("Too few spaces before comment: expected %d", p.Args...),
				fmt.Sprintf("The inline comment sits too close to its content. Leave %d space(s) before the #.", p.Args...))
		}
		return yamlAt(f, p, "yaml[comments]",
			"Missing starting space in comment",
			"No space follows the # of this comment. Start the comment text with a space.")
	case "empty-lines":
		return yamlAt(f, p, "yaml[empty-lines]",
			fmt.Sprintf("Too many blank lines (%d > %d)", p.Args...),
			fmt.Sprintf("Found %d consecutive blank lines, the limit is %d. Remove the extra ones.", p.Args...))
	case "hyphens":
		return yamlAt(f, p, "yaml[hyphens]",
			"Too many spaces after hyphen",
			"More than one space follows the list hyphen. Keep exactly one.")
	case "indentation":
		switch len(p.Args) {
		case 2:
			return yamlAt(f, p, "yaml[indentation]",
				fmt.Sprintf("Wrong indentation: expected %d but found %d", p.Args...),
				fmt.Sprintf("This element sits at column %[2]d, its structure wants %[1]d. Re-indent it.", p.Args...))
		case 1:
			return yamlAt(f, p, "yaml[indentation]",
				fmt.Sprintf("Wrong indentation: expected at least %d", p.Args...),
				fmt.Sprintf("This element is not indented past its parent. Move it to column %d or beyond.", p.Args...))
		default:
			return yamlAt(f, p, "yaml[indentation]",
				"Cannot infer indentation: unexpected token",
				"The token layout defeats indentation inference. Check the structure around this line.")
		}
	case "key-duplicates":
		return yamlAt(f, p, "yaml[key-duplicates]",
			fmt.Sprintf(`Duplication of key "%s" in mapping`, pyLower(p.Args[0])),
			fmt.Sprintf(`Key "%s" appears twice in this mapping and the last one silently wins. Remove one.`, p.Args[0]))
	case "line-length":
		return yamlAt(f, p, "yaml[line-length]",
			fmt.Sprintf("Line too long (%d > %d characters)", p.Args...),
			fmt.Sprintf("This line runs %d characters, the budget is %d. Split or shorten it.", p.Args...))
	case "new-line-at-end-of-file":
		return yamlAt(f, p, "yaml[new-line-at-end-of-file]",
			"No new line character at the end of file",
			"The last line has no final newline. End the file with one.")
	case "octal-values":
		if strings.Contains(p.Desc, "implicit") {
			return yamlAt(f, p, "yaml[octal-values]",
				fmt.Sprintf(`Forbidden implicit octal value "%s"`, p.Args...),
				fmt.Sprintf(`"%s" silently reads as octal under YAML 1.1. Quote it or drop the leading zero.`, p.Args...))
		}
		return yamlAt(f, p, "yaml[octal-values]",
			fmt.Sprintf(`Forbidden explicit octal value "%s"`, p.Args...),
			fmt.Sprintf(`"%s" is an octal literal. Quote it if it is meant as text.`, p.Args...))
	case "trailing-spaces":
		return yamlAt(f, p, "yaml[trailing-spaces]",
			"Trailing spaces",
			"Whitespace hangs past the last character. Delete it.")
	case "comments-indentation":
		return yamlAt(f, p, "yaml[comments-indentation]",
			"Comment not indented like content",
			"This comment does not line up with the content around it. Match their indentation.")
	case "document-start":
		if strings.Contains(p.Desc, "missing") {
			return yamlAt(f, p, "yaml[document-start]",
				`Missing document start "---"`,
				"The document does not open with ---. Add it on the first line.")
		}
		return yamlAt(f, p, "yaml[document-start]",
			`Found forbidden document start "---"`,
			"This document opens with ---, which the configuration forbids. Remove it.")
	default: // truthy is the only rule id left.
		return yamlAt(f, p, "yaml[truthy]",
			fmt.Sprintf("Truthy value should be one of [%s]", p.Args...),
			fmt.Sprintf("A bare YAML 1.1 boolean is ambiguous. Write one of %s instead.", p.Args...))
	}
}

// pyLower matches Python's str.lower() as applied by str.capitalize() to the
// interpolated values of upstream's yaml messages.
func pyLower(v any) string {
	s, _ := v.(string)
	return strings.ToLower(s)
}
