package main

import "runtime/debug"

// Build metadata, overridden at link time via -ldflags -X (see the Makefile's
// VERSION_PKG). They stay exported because the linker can only set package-level
// variables by their qualified name, and goreleaser stamps the same three.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// The values the three variables above hold when nothing stamped them.
const (
	devVersion  = "dev"
	unknownMeta = "unknown"
)

// build is what this binary reports about itself, resolved once at startup.
var build = buildFrom(buildMetadata{Version, GitCommit, BuildDate}, readBuildInfo())

func readBuildInfo() *debug.BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info
}

// buildMetadata is the version, commit and build date shown by --version and
// recorded as the tool version in SARIF output. commit and date are empty when
// the build carries nothing to fill them, and the rendered line then omits them
// rather than printing a placeholder.
type buildMetadata struct {
	version string
	commit  string
	date    string
}

// buildFrom derives the reported metadata from the linker-stamped values,
// falling back to the build info the toolchain embeds.
//
// The fallback is what makes `go install <module>/cmd/astl@latest` report a
// version: that build applies no -ldflags, so a tagged release would otherwise
// describe itself as "dev (unknown, unknown)". The module version the toolchain
// records is the honest answer there. VCS revision and time are embedded only
// for builds from a working tree, so a module install leaves them empty, and
// nothing is invented to fill the gap.
//
// info may be nil, which happens only for a binary built without module
// information at all.
func buildFrom(stamped buildMetadata, info *debug.BuildInfo) buildMetadata {
	b := stamped
	if b.commit == unknownMeta {
		b.commit = ""
	}
	if b.date == unknownMeta {
		b.date = ""
	}
	if info == nil {
		return b
	}
	if b.version == devVersion {
		if v := moduleVersion(info.Main.Version); v != "" {
			b.version = v
		}
	}
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			if b.date == "" {
				b.date = s.Value
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	// Read after the loop, not inside it: the settings carry no ordering
	// guarantee, and the dirty marker would be lost if it came second.
	if b.commit == "" && revision != "" {
		b.commit = shortRevision(revision)
		if modified {
			b.commit += "-dirty"
		}
	}
	return b
}

// moduleVersion returns the module version to report, or "" for a build that
// has none. The toolchain writes "(devel)" for a binary built outside the
// module proxy path, which says no more than "dev" already does.
func moduleVersion(v string) string {
	if v == "(devel)" {
		return ""
	}
	return v
}

// shortRevision abbreviates a commit hash to the width the Makefile and
// goreleaser stamp, so the three build paths print the same shape.
func shortRevision(rev string) string {
	const short = 7
	if len(rev) <= short {
		return rev
	}
	return rev[:short]
}

// String renders the metadata as it appears after "astl " on the version line.
func (b buildMetadata) String() string {
	switch {
	case b.commit == "" && b.date == "":
		return b.version
	case b.date == "":
		return b.version + " (" + b.commit + ")"
	case b.commit == "":
		return b.version + " (" + b.date + ")"
	default:
		return b.version + " (" + b.commit + ", " + b.date + ")"
	}
}
