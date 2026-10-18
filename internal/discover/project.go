package discover

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectRoot returns the directory a run is confined to, found the way
// ansible-lint's find_project_root finds its project_dir (ADR 0010).
//
// The search starts at the common base of srcs, resolved, where a directory
// counts as its own parent and a file stands for the directory holding it. That
// base and each of its ancestors are tried in turn, and the first one holding a
// `.git` (any kind of entry), a `.hg` directory, or one of cfgFiles as a regular
// file is the root. A relative entry of cfgFiles is looked for in each candidate
// directory. An absolute entry names the configuration file in use: once it
// exists the root is the directory holding it, or that directory's parent when
// it is named `.config`, whichever candidate is being tried.
//
// Where upstream falls back to the filesystem root and so confines nothing,
// ProjectRoot returns the common base itself. It returns "" when srcs is empty
// or share no common base, which confines a walk to its own roots.
func ProjectRoot(srcs, cfgFiles []string) string {
	base := commonBase(srcs)
	if base == "" {
		return ""
	}
	for dir := base; ; dir = filepath.Dir(dir) {
		if root, ok := projectMarker(dir, cfgFiles); ok {
			return root
		}
		if filepath.Dir(dir) == dir {
			return base
		}
	}
}

// commonBase returns the deepest directory every src lies in, or "" when there
// is none, which only happens across Windows volumes.
func commonBase(srcs []string) string {
	base := ""
	for _, src := range srcs {
		dir, err := srcDir(src)
		if err != nil {
			return ""
		}
		if base == "" {
			base = dir
			continue
		}
		for !insideDir(base, dir) {
			if filepath.Dir(base) == base {
				return ""
			}
			base = filepath.Dir(base)
		}
	}
	return base
}

// srcDir is the resolved directory src contributes to the common base: src
// itself when it is a directory, otherwise its parent.
func srcDir(src string) (string, error) {
	abs, err := filepath.Abs(src)
	if err != nil {
		return "", err
	}
	abs = resolvePath(abs)
	if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
		return abs, nil
	}
	return filepath.Dir(abs), nil
}

// projectMarker reports whether dir marks a project, and the root it marks.
func projectMarker(dir string, cfgFiles []string) (string, bool) {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return dir, true
	}
	if fi, err := os.Stat(filepath.Join(dir, ".hg")); err == nil && fi.IsDir() {
		return dir, true
	}
	for _, cfg := range cfgFiles {
		if !filepath.IsAbs(cfg) {
			if _, ok := existingFile(filepath.Join(dir, cfg)); ok {
				return dir, true
			}
			continue
		}
		if resolved, ok := existingFile(cfg); ok {
			return configDir(resolved), true
		}
	}
	return "", false
}

// configDir is the project directory an absolute configuration file implies.
func configDir(cfg string) string {
	dir := filepath.Dir(cfg)
	if filepath.Base(dir) == ".config" {
		return filepath.Dir(dir)
	}
	return dir
}

// insideDir reports whether abs is dir or lies under it, compared by path
// component so that a sibling sharing dir's prefix is outside. Upstream compares
// by string prefix, which admits that sibling (ADR 0010). An empty dir admits
// nothing.
//
// Both paths must be resolved: a lexical comparison is only a containment test
// once no symlink is left in either.
func insideDir(dir, abs string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
