// Package yamlscan exposes the YAML token stream of the libyaml scanner that
// gopkg.in/yaml.v3 embeds (see NOTICE). The token kinds, marks and scan order
// mirror pyyaml's scanner, which is what yamllint's rules are written against;
// internal/yamllint is the only intended consumer.
package yamlscan

import "sync"

// Kind identifies a token type, one per pyyaml token class.
type Kind int

// Token kinds, in the order yamlh.go declares them.
const (
	KindNone Kind = iota
	StreamStart
	StreamEnd
	VersionDirective
	TagDirective
	DocumentStart
	DocumentEnd
	BlockSequenceStart
	BlockMappingStart
	BlockEnd
	FlowSequenceStart
	FlowSequenceEnd
	FlowMappingStart
	FlowMappingEnd
	BlockEntry
	FlowEntry
	Key
	Value
	Alias
	Anchor
	Tag
	Scalar
)

// Style is a scalar's presentation style.
type Style int

// Scalar styles. StylePlain is what pyyaml reports as style None.
const (
	StylePlain Style = iota
	StyleSingleQuoted
	StyleDoubleQuoted
	StyleLiteral
	StyleFolded
)

// Mark is a position in the scanned buffer. Line and Column are 0-based and
// Pointer indexes the buffer, all counted in runes, matching pyyaml marks.
type Mark struct {
	Line, Column, Pointer int
}

// Token is one scanner token with pyyaml-equivalent marks.
type Token struct {
	Kind       Kind
	Start, End Mark
	// Value is the alias, anchor or scalar text.
	Value string
	// Style is meaningful for Scalar tokens only.
	Style Style
	// Major and Minor are meaningful for VersionDirective tokens only.
	Major, Minor int
}

var kindOf = map[yaml_token_type_t]Kind{
	yaml_STREAM_START_TOKEN:         StreamStart,
	yaml_STREAM_END_TOKEN:           StreamEnd,
	yaml_VERSION_DIRECTIVE_TOKEN:    VersionDirective,
	yaml_TAG_DIRECTIVE_TOKEN:        TagDirective,
	yaml_DOCUMENT_START_TOKEN:       DocumentStart,
	yaml_DOCUMENT_END_TOKEN:         DocumentEnd,
	yaml_BLOCK_SEQUENCE_START_TOKEN: BlockSequenceStart,
	yaml_BLOCK_MAPPING_START_TOKEN:  BlockMappingStart,
	yaml_BLOCK_END_TOKEN:            BlockEnd,
	yaml_FLOW_SEQUENCE_START_TOKEN:  FlowSequenceStart,
	yaml_FLOW_SEQUENCE_END_TOKEN:    FlowSequenceEnd,
	yaml_FLOW_MAPPING_START_TOKEN:   FlowMappingStart,
	yaml_FLOW_MAPPING_END_TOKEN:     FlowMappingEnd,
	yaml_BLOCK_ENTRY_TOKEN:          BlockEntry,
	yaml_FLOW_ENTRY_TOKEN:           FlowEntry,
	yaml_KEY_TOKEN:                  Key,
	yaml_VALUE_TOKEN:                Value,
	yaml_ALIAS_TOKEN:                Alias,
	yaml_ANCHOR_TOKEN:               Anchor,
	yaml_TAG_TOKEN:                  Tag,
	yaml_SCALAR_TOKEN:               Scalar,
}

var styleOf = map[yaml_scalar_style_t]Style{
	yaml_PLAIN_SCALAR_STYLE:         StylePlain,
	yaml_SINGLE_QUOTED_SCALAR_STYLE: StyleSingleQuoted,
	yaml_DOUBLE_QUOTED_SCALAR_STYLE: StyleDoubleQuoted,
	yaml_LITERAL_SCALAR_STYLE:       StyleLiteral,
	yaml_FOLDED_SCALAR_STYLE:        StyleFolded,
}

// Scanner yields one token at a time, so a caller that only ever looks at a
// small window (yamllint holds four) never materializes the stream. On a
// scanner error the stream simply ends, which is yamllint's own behavior:
// its line rules still run past the broken point.
type Scanner struct {
	parser yaml_parser_t
	done   bool
}

// scannerPool recycles the parser's working buffers (read buffers, token
// queue, indent and simple-key stacks) between files.
var scannerPool = sync.Pool{New: func() any { return &Scanner{} }}

// NewScanner starts scanning src. Call Close when done with it.
func NewScanner(src string) *Scanner {
	s := scannerPool.Get().(*Scanner)
	s.done = false
	p := &s.parser
	// Reinitialize in place, keeping every slice's backing array. The zeroed
	// struct matches yaml_parser_initialize; the retained buffers are exactly
	// what it would otherwise allocate.
	rawBuffer := p.raw_buffer[:0]
	buffer := p.buffer[:0]
	tokens := p.tokens[:0]
	indents := p.indents[:0]
	simpleKeys := p.simple_keys[:0]
	comments := p.comments[:0]
	byTok := p.simple_keys_by_tok
	*p = yaml_parser_t{
		raw_buffer:  rawBuffer,
		buffer:      buffer,
		tokens:      tokens,
		indents:     indents,
		simple_keys: simpleKeys,
		comments:    comments,
	}
	if p.raw_buffer == nil {
		p.raw_buffer = make([]byte, 0, input_raw_buffer_size)
	}
	if p.buffer == nil {
		p.buffer = make([]byte, 0, input_buffer_size)
	}
	if byTok != nil {
		clear(byTok)
		p.simple_keys_by_tok = byTok
	}
	yaml_parser_set_input_string(p, []byte(src))
	return s
}

// Next fills tok with the next token and reports whether one was produced.
// Writing into a caller-owned Token keeps the per-token cost at one string
// copy for the value, nothing else.
func (s *Scanner) Next(tok *Token) bool {
	if s.done {
		return false
	}
	var raw yaml_token_t
	if !yaml_parser_scan(&s.parser, &raw) || raw.typ == yaml_NO_TOKEN {
		s.done = true
		return false
	}
	if raw.typ == yaml_STREAM_END_TOKEN {
		s.done = true
	}
	tok.Kind = kindOf[raw.typ]
	tok.Start = Mark{Line: raw.start_mark.line, Column: raw.start_mark.column, Pointer: raw.start_mark.index}
	tok.End = Mark{Line: raw.end_mark.line, Column: raw.end_mark.column, Pointer: raw.end_mark.index}
	tok.Value = string(raw.value)
	tok.Style = styleOf[raw.style]
	tok.Major = int(raw.major)
	tok.Minor = int(raw.minor)
	return true
}

// Close returns the scanner to the pool; the parser's buffers are recycled
// by the next NewScanner. The input reference is dropped so a pooled scanner
// never pins a file's text.
func (s *Scanner) Close() {
	s.parser.input = nil
	s.done = true
	scannerPool.Put(s)
}

// Tokens scans src and returns its token stream whole; tests and one-shot
// callers use it, the linting loop streams through Scanner instead.
func Tokens(src string) []Token {
	s := NewScanner(src)
	defer s.Close()
	var out []Token
	var tok Token
	for s.Next(&tok) {
		out = append(out, tok)
	}
	return out
}
