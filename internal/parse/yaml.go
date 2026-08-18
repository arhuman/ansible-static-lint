// Package parse loads Ansible YAML files and normalizes their tasks so that
// lint rules can inspect them without depending on the YAML node API directly.
package parse

import "gopkg.in/yaml.v3"

// Pos is a 1-based source position. Column is 0 when unknown.
type Pos struct {
	Line   int
	Column int
}

// NodePos returns the position of a node, or the zero value for a nil node.
func NodePos(n *yaml.Node) Pos {
	if n == nil {
		return Pos{}
	}
	return Pos{Line: n.Line, Column: n.Column}
}

// pyYAMLBools are the boolean spellings PyYAML resolves, and they decide a
// class of question astl gets wrong by default: ansible parses with PyYAML,
// under YAML 1.1, while yaml.v3 implements YAML 1.2. So `no` reaches
// ansible-lint as False and astl as the string "no". Read as a string it is
// non-empty, and therefore truthy, which is the opposite answer.
//
// The list is PyYAML's exactly, which is narrower than the YAML 1.1 spec: three
// capitalizations of each word and nothing else, so `yEs` stays a string, and
// bare `y` and `n` are strings too despite the spec calling them booleans.
var pyYAMLBools = map[string]bool{
	"yes": true, "Yes": true, "YES": true,
	"on": true, "On": true, "ON": true,
	"no": false, "No": false, "NO": false,
	"off": false, "Off": false, "OFF": false,
}

// PyBool reports the boolean a scalar spells to ansible, and whether it spells
// one at all. It answers only for the values yaml.v3 hands back as strings:
// `true` and `false` are already !!bool and never reach here.
//
// Only a plain scalar counts, because quoting is what tells PyYAML the value is
// a string: `create: "no"` is a non-empty string and truthy, the opposite of
// bare `create: no`. Both were confirmed against ansible-lint. yaml.v3 records
// plain style as the zero Style and sets a flag for every other form, including
// an explicit `!!str` tag.
func PyBool(n *yaml.Node) (value, ok bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Style != 0 {
		return false, false
	}
	v, ok := pyYAMLBools[n.Value]
	return v, ok
}

// IsMap reports whether n is a mapping node.
func IsMap(n *yaml.Node) bool { return n != nil && n.Kind == yaml.MappingNode }

// IsSeq reports whether n is a sequence node.
func IsSeq(n *yaml.Node) bool { return n != nil && n.Kind == yaml.SequenceNode }

// IsScalar reports whether n is a scalar node.
func IsScalar(n *yaml.Node) bool { return n != nil && n.Kind == yaml.ScalarNode }

// MapGet returns the value node for key in a mapping node, or nil.
func MapGet(n *yaml.Node, key string) *yaml.Node {
	if !IsMap(n) {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// MapKeyNode returns the key node for key in a mapping node, or nil.
func MapKeyNode(n *yaml.Node, key string) *yaml.Node {
	if !IsMap(n) {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i]
		}
	}
	return nil
}

// MapHas reports whether a mapping node contains key.
func MapHas(n *yaml.Node, key string) bool { return MapKeyNode(n, key) != nil }

// MapKeys returns the mapping keys in document order.
func MapKeys(n *yaml.Node) []string {
	if !IsMap(n) {
		return nil
	}
	keys := make([]string, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

// Str returns the scalar string value of n, or "" when n is not a scalar.
func Str(n *yaml.Node) string {
	if !IsScalar(n) {
		return ""
	}
	return n.Value
}

// EachNested calls fn for every key/value pair reachable from n, descending
// into nested mappings and sequences, mirroring ansible-lint's
// nested_items_path. key is nil for sequence items, which have an index rather
// than a name.
func EachNested(n *yaml.Node, fn func(key, value *yaml.Node)) {
	switch {
	case IsMap(n):
		for i := 0; i+1 < len(n.Content); i += 2 {
			fn(n.Content[i], n.Content[i+1])
			EachNested(n.Content[i+1], fn)
		}
	case IsSeq(n):
		for _, item := range n.Content {
			fn(nil, item)
			EachNested(item, fn)
		}
	}
}

// StrList returns the scalar items of a sequence node.
func StrList(n *yaml.Node) []string {
	if !IsSeq(n) {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for _, item := range n.Content {
		out = append(out, item.Value)
	}
	return out
}
