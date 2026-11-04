package docker

import (
	"os"
	"path/filepath"
	"testing"
)

const selfID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func writeMountinfo(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSelfID(t *testing.T) {
	withID := writeMountinfo(t, "100 90 0:50 / / rw - overlay overlay rw\n"+
		"200 100 8:1 /var/lib/docker/containers/"+selfID+"/hostname /etc/hostname rw - ext4 /dev/sda1 rw\n")
	without := writeMountinfo(t, "100 90 0:50 / / rw - overlay overlay rw\n")
	cases := []struct {
		name, path, hostname, want string
	}{
		{"mountinfo wins over hostname", withID, "abc123", selfID},
		{"hostname when mountinfo has no ID", without, "abc123", "abc123"},
		{"hostname when mountinfo is missing", "/nonexistent", "abc123", "abc123"},
		{"nothing known", without, "", ""},
	}
	for _, tc := range cases {
		if got := SelfID(tc.path, tc.hostname); got != tc.want {
			t.Errorf("%s: SelfID = %q, want %q", tc.name, got, tc.want)
		}
	}
}
