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

// The build variables win over Go's build info, field by field; the
// version is a release or nothing; the commit carries a modified mark the
// same way from either source.
func TestResolve(t *testing.T) {
	bi := info("v9.9.9", map[string]string{"vcs.revision": "ffffffffffffffffffff", "vcs.modified": "true"})
	tests := []struct {
		name         string
		ver, commit  string
		bi           *debug.BuildInfo
		wantV, wantC string
		wantModified bool
	}{
		{"build variables", "v1.2.3-4-gabc", full, bi, "v1.2.3-4-gabc", "0123456789ab", false},
		{"build variables, modified tree", "v1.2.3", full + "-dirty", bi, "v1.2.3", "0123456789ab", true},
		{"build variables, version only", "v1.2.3", "", nil, "v1.2.3", "", false},
		{"build variables, commit only", "", full, nil, "", "0123456789ab", false},
		{"build variables, commit only, modified", "", full + "-dirty", info("(devel)", nil), "", "0123456789ab", true},
		{"build info, clean tree", "", "", info("(devel)", map[string]string{"vcs.revision": full, "vcs.modified": "false"}), "", "0123456789ab", false},
		{"build info, modified tree", "", "", info("(devel)", map[string]string{"vcs.revision": full, "vcs.modified": "true"}), "", "0123456789ab", true},
		{"build info module version", "", "", info("v0.4.0", nil), "v0.4.0", "", false},
		{"build info release version with a commit", "", "", info("v0.4.0", map[string]string{"vcs.revision": full}), "v0.4.0", "0123456789ab", false},
		{"variable version, build info commit", "v1.0.0", "", bi, "v1.0.0", "ffffffffffff", true},
		{"pseudo-version is no release", "", "", info("v0.0.0-20261112173800-0123456789ab", map[string]string{"vcs.revision": full}), "", "0123456789ab", false},
		{"pseudo-version after a release is no release", "", "", info("v1.2.4-0.20261112173800-0123456789ab+dirty", nil), "", "", false},
		{"build info without version control", "", "", info("(devel)", nil), "", "", false},
		{"no build info at all", "", "", nil, "", "", false},
		{"short revision kept whole", "", "", info("", map[string]string{"vcs.revision": "abc123"}), "", "abc123", false},
	}
	for _, tc := range tests {
		got := resolve(tc.ver, tc.commit, tc.bi)
		if got.Version != tc.wantV || got.Commit != tc.wantC || got.Modified != tc.wantModified {
			t.Errorf("%s: got version %q commit %q modified %v, want %q %q %v",
				tc.name, got.Version, got.Commit, got.Modified, tc.wantV, tc.wantC, tc.wantModified)
		}
	}
}

// Get reads the variables the linker sets, and the Go version from the
// runtime; nothing is spelled out in the code.
func TestGetUsesBuildVariables(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() { Version, Commit = oldV, oldC })
	Version, Commit = "v7.7.7", full+"-dirty"

	got := Get()
	if got.Version != "v7.7.7" || got.Commit != full[:12] || !got.Modified {
		t.Errorf("got %q %q modified %v", got.Version, got.Commit, got.Modified)
	}
	if got.Go != runtime.Version() {
		t.Errorf("go version %q, want %q", got.Go, runtime.Version())
	}
	if got.Licence != "MIT" || got.Repository != "https://github.com/toddawhittaker/foreman" {
		t.Errorf("licence %q repository %q", got.Licence, got.Repository)
	}

	// With no variables the values come from build info (a test binary has no
	// version control data), never from anything spelled out here.
	Version, Commit = "", ""
	got = Get()
	want := resolve("", "", buildInfo())
	if got.Version != want.Version || got.Commit != want.Commit || got.Modified != want.Modified {
		t.Errorf("without variables: got %q %q %v, build info says %q %q %v",
			got.Version, got.Commit, got.Modified, want.Version, want.Commit, want.Modified)
	}
	if got.Version == "v7.7.7" || got.Commit == full[:12] {
		t.Errorf("the earlier values stuck: %q %q", got.Version, got.Commit)
	}
	if got.Go != runtime.Version() {
		t.Errorf("go version %q", got.Go)
	}
}
