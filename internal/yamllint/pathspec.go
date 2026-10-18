package yamllint

// PathSpec is a compiled gitignore-style pattern list, exported for callers
// outside the yamllint pass. ansible-lint feeds `exclude_paths` to pathspec's
// GitIgnoreSpec during discovery, the same class yamllint uses for `ignore:`,
// so both features share one pattern language and astl shares one compiler.
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

// MatchEntry reports whether a directory entry is matched. Pass isDir for a
// directory, which is then tested with a trailing slash so that a dir-only
// pattern (`build/`) matches the directory itself and not only its contents.
// ansible-lint tests every discovery candidate this way. A nil receiver
// matches nothing.
func (s *PathSpec) MatchEntry(path string, isDir bool) bool {
	if s == nil {
		return false
	}
	return s.spec.matchEntry(path, isDir)
}
