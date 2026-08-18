// There is deliberately no messages.go: the two message wordings live side by
// side at each finding construction site (finding.go's constructors take
// msg, nativeMsg as adjacent arguments) so the pair cannot drift, and ADR 0003
// records why a central registry was rejected. This file is the package-wide
// invariant test over all those sites: it AST-scans every rule file to enforce
// completeness, originality, grammar and the length budget.
package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// notEmitted lists equivalence rows carrying a subtag that no rule constructs.
// Only name[prefix] qualifies: upstream raises it solely under a configured
// prefix policy, so the row exists as a reserved alias. Anything else appearing
// here means a rule was declared and never wired up.
var notEmitted = map[string]bool{"name[prefix]": true}

// findingSite is one call to a finding constructor, reduced to the three
// literals a reader compares: which rule fired and how it is worded in each
// taxonomy. Interpolated calls contribute their format string, which is what
// carries the wording.
type findingSite struct {
	file     string
	line     int
	tag      string
	upstream string
	native   string
}

// TestEveryFindingCarriesBothWordings is the completeness gate. Scanning the
// constructor calls rather than consulting a hand-written list means a new rule
// fails this test until it words its defect in both taxonomies.
func TestEveryFindingCarriesBothWordings(t *testing.T) {
	for _, s := range findingSites(t) {
		switch {
		case s.tag == "":
			t.Errorf("%s:%d: finding has no literal tag", s.file, s.line)
		case s.upstream == "":
			t.Errorf("%s:%d: %s has no upstream message", s.file, s.line, s.tag)
		case s.native == "":
			t.Errorf("%s:%d: %s has no native message", s.file, s.line, s.tag)
		}
	}
}

// TestNativeMessagesAreOriginal keeps the second registry worth having: a native
// message that merely echoes upstream's would give a reader nothing the default
// output does not already say.
func TestNativeMessagesAreOriginal(t *testing.T) {
	for _, s := range findingSites(t) {
		if s.native != "" && strings.EqualFold(s.native, s.upstream) {
			t.Errorf("%s:%d: %s words both taxonomies identically: %q", s.file, s.line, s.tag, s.native)
		}
	}
}

// TestNativeMessagesAreTwoSentences pins the grammar the vocabulary is written
// in: the defect as observed fact, then an imperative fix.
func TestNativeMessagesAreTwoSentences(t *testing.T) {
	for _, s := range findingSites(t) {
		if s.native == "" {
			continue
		}
		if strings.ContainsAny(s.native, "–—") {
			t.Errorf("%s:%d: %s uses a dash as punctuation: %q", s.file, s.line, s.tag, s.native)
		}
		if !strings.HasSuffix(s.native, ".") {
			t.Errorf("%s:%d: %s does not end in a period: %q", s.file, s.line, s.tag, s.native)
		}
		if strings.Count(s.native, ". ") < 1 {
			t.Errorf("%s:%d: %s is one sentence, want a defect and a fix: %q", s.file, s.line, s.tag, s.native)
		}
	}
}

// maxNativeMessage is the rendered length budget, in runes. The surfaces that
// consume a finding (GitHub code scanning annotations, editor problem panels,
// narrow CI terminals) truncate past roughly this width, and truncation would
// cut the second half of the message, which is where the fix lives.
const maxNativeMessage = 100

// sampleVerb is what an interpolated value stands in as when measuring. Values
// come from the linted content and are not ours to bound; the budget is on the
// wording around them, measured with a representative substitution.
var sampleVerb = strings.NewReplacer("%s", "example", "%d", "10", "%o", "644")

// TestNativeMessagesFitTheBudget keeps the wording readable where it is
// consumed. It is a ratchet for future rules, not a one-off audit.
func TestNativeMessagesFitTheBudget(t *testing.T) {
	for _, s := range findingSites(t) {
		if s.native == "" {
			continue
		}
		rendered := sampleVerb.Replace(s.native)
		if n := len([]rune(rendered)); n > maxNativeMessage {
			t.Errorf("%s:%d: %s is %d runes, over the %d budget: %q",
				s.file, s.line, s.tag, n, maxNativeMessage, rendered)
		}
	}
}

// TestEveryEmittableRowIsWorded is the converse of the scan: a subtag declared
// in the equivalence table must be emitted by some rule, or be named as
// deliberately unemitted. It is what stops a new row from silently going
// message-less.
func TestEveryEmittableRowIsWorded(t *testing.T) {
	worded := map[string]bool{}
	for _, s := range findingSites(t) {
		if s.native != "" {
			worded[s.tag] = true
		}
	}
	for _, p := range equivalence {
		if _, sub := splitTag(p.upstream); sub == "" {
			continue
		}
		if !worded[p.upstream] && !notEmitted[p.upstream] {
			t.Errorf("%q has an equivalence row but no rule words it, and it is not listed as unemitted", p.upstream)
		}
	}
	for tag := range worded {
		if notEmitted[tag] {
			t.Errorf("%q is listed as unemitted but a rule emits it", tag)
		}
	}
}

func TestMessageForSelectsTheTaxonomy(t *testing.T) {
	fd := Finding{Message: "upstream", NativeMessage: "native"}
	if got := fd.MessageFor(IDUpstream); got != "upstream" {
		t.Errorf("MessageFor(upstream) = %q, want the upstream wording", got)
	}
	if got := fd.MessageFor(IDNative); got != "native" {
		t.Errorf("MessageFor(native) = %q, want the native wording", got)
	}
	bare := Finding{Message: "upstream"}
	if got := bare.MessageFor(IDNative); got != "upstream" {
		t.Errorf("MessageFor(native) on an unworded finding = %q, want the upstream fallback", got)
	}
}

// constructorArgs maps each finding constructor to the argument positions of
// its tag, upstream message and native message.
var constructorArgs = map[string][3]int{
	"at":         {2, 3, 4},
	"onLine":     {2, 3, 4},
	"warnAt":     {2, 3, 4},
	"warnOnLine": {2, 3, 4},
	"whole":      {1, 2, 3},
	"yamlAt":     {2, 3, 4},
}

// findingSites scans the package's own rule sources for every finding
// construction, whether through a constructor or a composite literal.
func findingSites(t *testing.T) []findingSite {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, name := range ruleSourceFiles(t) {
		// finding.go holds the constructors themselves, whose Finding literals
		// carry parameters rather than the wording of any one rule.
		if name == "finding.go" {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files[name] = file
	}

	// Package-level constants resolve across files; locals only shadow within
	// the file that declares them.
	shared := map[string]string{}
	for _, file := range files {
		packageStrings(file, shared)
	}

	var out []findingSite
	for name, file := range files {
		strs := map[string]string{}
		for k, v := range shared {
			strs[k] = v
		}
		localStrings(file, strs)
		ast.Inspect(file, func(n ast.Node) bool {
			site, ok := siteOf(n, strs)
			if !ok {
				return true
			}
			site.file = name
			site.line = fset.Position(n.Pos()).Line
			out = append(out, site)
			return true
		})
	}
	if len(out) < len(IDs) {
		t.Fatalf("found only %d finding constructions, expected at least the %d rule ids", len(out), len(IDs))
	}
	return out
}

// siteOf reduces a constructor call or a Finding literal to its three strings.
func siteOf(n ast.Node, strs map[string]string) (findingSite, bool) {
	switch node := n.(type) {
	case *ast.CallExpr:
		fn, ok := node.Fun.(*ast.Ident)
		if !ok {
			return findingSite{}, false
		}
		pos, ok := constructorArgs[fn.Name]
		if !ok || len(node.Args) <= pos[2] {
			return findingSite{}, false
		}
		return findingSite{
			tag:      stringOf(node.Args[pos[0]], strs),
			upstream: stringOf(node.Args[pos[1]], strs),
			native:   stringOf(node.Args[pos[2]], strs),
		}, true
	case *ast.CompositeLit:
		// A literal inside a []Finding elides its type, so an absent type is
		// accepted too; the Tag key below is what identifies a real finding.
		if node.Type != nil {
			if id, ok := node.Type.(*ast.Ident); !ok || id.Name != "Finding" {
				return findingSite{}, false
			}
		}
		var site findingSite
		for _, elt := range node.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Tag":
				site.tag = stringOf(kv.Value, strs)
			case "Message":
				site.upstream = stringOf(kv.Value, strs)
			case "NativeMessage":
				site.native = stringOf(kv.Value, strs)
			}
		}
		return site, site.tag != ""
	}
	return findingSite{}, false
}

// stringOf renders the constant part of an expression: a literal as itself, a
// named string as its declaration, an fmt.Sprintf as its format string, and a
// concatenation as its resolvable pieces joined. That is exactly the wording a
// reviewer judges, with the interpolated values left out.
func stringOf(e ast.Expr, strs map[string]string) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.Ident:
		return strs[v.Name]
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return ""
		}
		return stringOf(v.X, strs) + stringOf(v.Y, strs)
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" || len(v.Args) == 0 {
			return ""
		}
		return stringOf(v.Args[0], strs)
	}
	return ""
}

// record resolves a single-name, single-value declaration into out.
func record(out map[string]string, names []*ast.Ident, values []ast.Expr) {
	if len(names) != 1 || len(values) != 1 {
		return
	}
	if s := stringOf(values[0], out); s != "" {
		out[names[0].Name] = s
	}
}

// packageStrings collects the file's package-level string constants, the ones a
// rule in another file may name.
func packageStrings(file *ast.File, out map[string]string) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				record(out, vs.Names, vs.Values)
			}
		}
	}
}

// localStrings adds the file's function-scoped constants and single assignments,
// so a message hoisted out of a call still resolves.
func localStrings(file *ast.File, out map[string]string) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ValueSpec:
			record(out, v.Names, v.Values)
		case *ast.AssignStmt:
			if len(v.Lhs) != 1 {
				return true
			}
			if id, ok := v.Lhs[0].(*ast.Ident); ok {
				record(out, []*ast.Ident{id}, v.Rhs)
			}
		}
		return true
	})
}
