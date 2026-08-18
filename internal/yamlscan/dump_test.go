package yamlscan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// jsonStr matches python json.dumps(value, ensure_ascii=False): no HTML escaping.
func jsonStr(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

var kindName = map[Kind]string{
	StreamStart:        "StreamStart",
	StreamEnd:          "StreamEnd",
	VersionDirective:   "VersionDirective",
	TagDirective:       "TagDirective",
	DocumentStart:      "DocumentStart",
	DocumentEnd:        "DocumentEnd",
	BlockSequenceStart: "BlockSequenceStart",
	BlockMappingStart:  "BlockMappingStart",
	BlockEnd:           "BlockEnd",
	FlowSequenceStart:  "FlowSequenceStart",
	FlowSequenceEnd:    "FlowSequenceEnd",
	FlowMappingStart:   "FlowMappingStart",
	FlowMappingEnd:     "FlowMappingEnd",
	BlockEntry:         "BlockEntry",
	FlowEntry:          "FlowEntry",
	Key:                "Key",
	Value:              "Value",
	Alias:              "Alias",
	Anchor:             "Anchor",
	Tag:                "Tag",
	Scalar:             "Scalar",
}

var styleName = map[Style]string{
	StylePlain:        "plain",
	StyleSingleQuoted: "single",
	StyleDoubleQuoted: "double",
	StyleLiteral:      "literal",
	StyleFolded:       "folded",
}

// dumpTokens renders a token stream in the exact format of the pyyaml
// reference dumper used to generate the testdata goldens
// (scratchpad pytokdump.py in the design notes).
func dumpTokens(src string) string {
	var b strings.Builder
	for _, t := range Tokens(src) {
		value, style := "", ""
		switch t.Kind {
		case VersionDirective:
			value = fmt.Sprintf("%d.%d", t.Major, t.Minor)
		case Scalar:
			value = t.Value
			style = styleName[t.Style]
		case Alias, Anchor:
			value = t.Value
		}
		fmt.Fprintf(&b, "%s s=%d:%d:%d e=%d:%d:%d style=%s value=%s\n",
			kindName[t.Kind],
			t.Start.Line, t.Start.Column, t.Start.Pointer,
			t.End.Line, t.End.Column, t.End.Pointer,
			style, jsonStr(value))
	}
	return b.String()
}
