package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildFrom(t *testing.T) {
	unstamped := buildMetadata{devVersion, unknownMeta, unknownMeta}
	stamped := buildMetadata{"v0.1.0", "4270e51", "2026-08-23T10:00:00+02:00"}

	tests := []struct {
		name    string
		in      buildMetadata
		info    *debug.BuildInfo
		want    string
		wantVer string
	}{
		{
			name:    "ldflags win over build info",
			in:      stamped,
			info:    info("v0.9.9", "ffffffffffffffffffffffffffffffffffffffff", "2020-01-01T00:00:00Z", "false"),
			want:    "v0.1.0 (4270e51, 2026-08-23T10:00:00+02:00)",
			wantVer: "v0.1.0",
		},
		{
			name:    "go install reports the module version alone",
			in:      unstamped,
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}},
			want:    "v0.1.0",
			wantVer: "v0.1.0",
		},
		{
			name:    "working tree build takes revision and time",
			in:      unstamped,
			info:    info("(devel)", "4270e51abcdef0123456789", "2026-08-23T10:00:00+02:00", "false"),
			want:    "dev (4270e51, 2026-08-23T10:00:00+02:00)",
			wantVer: "dev",
		},
		{
			name:    "a modified working tree says so",
			in:      unstamped,
			info:    info("(devel)", "4270e51abcdef0123456789", "2026-08-23T10:00:00+02:00", "true"),
			want:    "dev (4270e51-dirty, 2026-08-23T10:00:00+02:00)",
			wantVer: "dev",
		},
		{
			name:    "no build info at all",
			in:      unstamped,
			info:    nil,
			want:    "dev",
			wantVer: "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFrom(tt.in, tt.info)
			if got.String() != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got.version != tt.wantVer {
				t.Errorf("version = %q, want %q", got.version, tt.wantVer)
			}
		})
	}
}

// info builds the embedded build info a working-tree build carries, with the
// vcs.modified setting placed before vcs.revision because the settings carry no
// ordering guarantee and the dirty marker has to survive either order.
func info(version, revision, when, modified string) *debug.BuildInfo {
	return &debug.BuildInfo{
		Main: debug.Module{Version: version},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.modified", Value: modified},
			{Key: "vcs.revision", Value: revision},
			{Key: "vcs.time", Value: when},
		},
	}
}
