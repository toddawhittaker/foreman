package version

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func info(main string, settings map[string]string) *debug.BuildInfo {
	bi := &debug.BuildInfo{Main: debug.Module{Version: main}}
	for k, v := range settings {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: k, Value: v})
	}
	return bi
}

const full = "0123456789abcdef0123456789abcdef01234567"

// The build variables win over Go's build info, field by field.
func TestResolveOrder(t *testing.T) {
	bi := info("v9.9.9", map[string]string{"vcs.revision": "ffffffffffffffffffff", "vcs.modified": "true"})
	tests := []struct {
		name            string
		ver, commit     string
		bi              *debug.BuildInfo
		wantV, wantC    string
		wantDevelopment bool
	}{
		{"build variables", "v1.2.3-4-gabc", full, bi, "v1.2.3-4-gabc", "0123456789ab", false},
		{"build variables, version only", "v1.2.3", "", nil, "v1.2.3", "", false},
		{"build info, clean tree", "", "", info("(devel)", map[string]string{"vcs.revision": full, "vcs.modified": "false"}), "", "0123456789ab", false},
		{"build info, dirty tree", "", "", info("(devel)", map[string]string{"vcs.revision": full, "vcs.modified": "true"}), "", "0123456789ab-dirty", false},
		{"build info module version", "", "", info("v0.4.0", nil), "v0.4.0", "", false},
		{"variable version, build info commit", "v1.0.0", "", bi, "v1.0.0", "ffffffffffff-dirty", false},
		{"build info without version control", "", "", info("(devel)", nil), "", "", true},
		{"no build info at all", "", "", nil, "", "", true},
		{"short revision kept whole", "", "", info("", map[string]string{"vcs.revision": "abc123"}), "", "abc123", false},
	}
	for _, tc := range tests {
		got := resolve(tc.ver, tc.commit, tc.bi)
		if got.Version != tc.wantV || got.Commit != tc.wantC || got.Development() != tc.wantDevelopment {
			t.Errorf("%s: got %+v (development %v), want version %q commit %q development %v",
				tc.name, got, got.Development(), tc.wantV, tc.wantC, tc.wantDevelopment)
		}
	}
}

// Get reads the variables the linker sets, and the Go version from the
// runtime; nothing is spelled out in the code.
func TestGetUsesBuildVariables(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() { Version, Commit = oldV, oldC })
	Version, Commit = "v7.7.7", full

	got := Get()
	if got.Version != "v7.7.7" || got.Commit != full[:12] {
		t.Errorf("got %q %q", got.Version, got.Commit)
	}
	if got.Go != runtime.Version() {
		t.Errorf("go version %q, want %q", got.Go, runtime.Version())
	}
	if got.Licence != "MIT" || got.Repository != "https://github.com/toddawhittaker/foreman" {
		t.Errorf("licence %q repository %q", got.Licence, got.Repository)
	}
	if got.Development() {
		t.Error("a build with variables is not a development build")
	}

	Version, Commit = "", ""
	got = Get()
	if got.Go != runtime.Version() {
		t.Errorf("go version %q", got.Go)
	}
}
