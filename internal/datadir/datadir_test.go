package datadir

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

const ownID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeInspector struct {
	containers map[string]docker.Container
	asked      []string
}

func (f *fakeInspector) InspectContainer(_ context.Context, id string) (docker.Container, error) {
	f.asked = append(f.asked, id)
	c, ok := f.containers[id]
	if !ok {
		return docker.Container{}, errors.New("no such container")
	}
	return c, nil
}

func writeMountinfo(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

var self = docker.Container{Mounts: []docker.Mount{
	{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
	{Source: "/srv/foreman", Destination: "/data"},
}}

func TestLayout(t *testing.T) {
	if Database("/data") != "/data/foreman.db" {
		t.Error("layout does not match SPEC 5.2")
	}
}

func TestHostDirConfiguredWins(t *testing.T) {
	f := &fakeInspector{}
	got, err := HostDir(context.Background(), "/data", "/opt/x", f, "/nonexistent", "host")
	if err != nil || got != "/opt/x" || len(f.asked) != 0 {
		t.Fatalf("got %q, %v, asked %v", got, err, f.asked)
	}
}

func TestHostDirFromMountinfo(t *testing.T) {
	mi := writeMountinfo(t, "100 90 0:50 / / rw - overlay overlay rw\n"+
		"200 100 8:1 /var/lib/docker/containers/"+ownID+"/hostname /etc/hostname rw - ext4 /dev/sda1 rw\n")
	f := &fakeInspector{containers: map[string]docker.Container{ownID: self}}
	got, err := HostDir(context.Background(), "/data/", "", f, mi, "somehost")
	if err != nil || got != "/srv/foreman" {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.asked[0] != ownID {
		t.Errorf("inspected %v", f.asked)
	}
}

func TestHostDirFallsBackToHostname(t *testing.T) {
	mi := writeMountinfo(t, "100 90 0:50 / / rw - overlay overlay rw\n")
	f := &fakeInspector{containers: map[string]docker.Container{"abc123": self}}
	got, err := HostDir(context.Background(), "/data", "", f, mi, "abc123")
	if err != nil || got != "/srv/foreman" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestHostDirRefusesInPlainWords(t *testing.T) {
	cases := map[string]*fakeInspector{
		"inspect fails": {},
		"no mount":      {containers: map[string]docker.Container{"h": {Mounts: []docker.Mount{{Source: "/a", Destination: "/other"}}}}},
	}
	for name, f := range cases {
		_, err := HostDir(context.Background(), "/data", "", f, "/nonexistent", "h")
		if err == nil || !strings.Contains(err.Error(), "Set FOREMAN_HOST_DATA_DIR") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	_, err := HostDir(context.Background(), "/data", "", &fakeInspector{}, "/nonexistent", "")
	if err == nil || !strings.Contains(err.Error(), "FOREMAN_HOST_DATA_DIR") {
		t.Errorf("no id: got %v", err)
	}
}

func TestProbeOwnership(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		uid, gid int
		ok       bool
	}{
		{845, 845, true},
		{1000, 845, true},
		{845, 1000, true},
		{1000, 1000, false},
	}
	for _, tc := range cases {
		err := probe(dir, tc.uid, tc.gid, 845, 845)
		if (err == nil) != tc.ok {
			t.Errorf("uid %d gid %d: %v", tc.uid, tc.gid, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("probe left files behind: %v", entries)
	}
}

func TestProbeDirectoryProblems(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := probe(filepath.Join(dir, "missing"), 845, 845, 845, 845); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing: %v", err)
	}
	if err := probe(file, 845, 845, 845, 845); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file: %v", err)
	}
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := probe(ro, 845, 845, 845, 845); err == nil || !strings.Contains(err.Error(), "cannot write") {
		t.Errorf("read-only: %v", err)
	}
}
