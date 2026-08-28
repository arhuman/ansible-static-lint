package yamllint

// PathSpec is a compiled gitignore-style pattern list, exported for callers
// outside the yamllint pass. ansible-lint feeds `exclude_paths` to pathspec's
// GitIgnoreSpec during discovery, the same class yamllint uses for `ignore:`,
// so both features share one pattern language and astl shares one compiler
// (issue 0013).
type PathSpec struct {
	spec *ignoreSpec
}

// ParsePathSpec compiles a pattern list. Lines that do not compile are
// dropped, as pathspec drops them; a nil receiver matches nothing.
func ParsePathSpec(lines []string) *PathSpec {
	spec := parseIgnore(lines)
	if spec == nil {
		return nil
	}
	return &PathSpec{spec: spec}
}

// Match reports whether the slash-separated relative path is matched.
func (s *PathSpec) Match(path string) bool {
	if s == nil {
		return false
	}
	return s.spec.match(path)
}
