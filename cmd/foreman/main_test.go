package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The controller gets the host data directory, and accepts what serve gives it.
func TestGameConfig(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	worlds, err := world.NewManager(world.Dirs{
		Worlds:    filepath.Join(dir, "worlds"),
		Snapshots: filepath.Join(dir, "snapshots"),
		Imports:   filepath.Join(dir, "import"),
	}, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := docker.New("unix:///nonexistent.sock")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{GameUID: 845, GameGID: 845, GameNetwork: "games"}
	gc := gameConfig(cfg, d, worlds, st, "/srv/foreman", "self")
	if gc.HostDataDir != "/srv/foreman" || gc.Network != "games" || gc.SelfID != "self" {
		t.Errorf("game config = %+v", gc)
	}
	if _, err := game.New(gc); err != nil {
		t.Errorf("game.New: %v", err)
	}
}

// Port 0 is bound before the line is logged, and the line carries the port
// the system chose, which the e2e harness reads.
func TestListenLogsRealPort(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	ln, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var line struct{ Msg, Addr string }
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line %q: %v", buf.String(), err)
	}
	if line.Msg != "listening" {
		t.Errorf("msg = %q", line.Msg)
	}
	_, port, err := net.SplitHostPort(line.Addr)
	if err != nil || port == "0" || port == "" {
		t.Errorf("addr = %q, want a real port", line.Addr)
	}
	if line.Addr != ln.Addr().String() {
		t.Errorf("addr = %q, listener is on %q", line.Addr, ln.Addr())
	}
}

// A taken port is an error from listen, so serve exits instead of logging "listening".
func TestListenTakenPort(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	if ln, err := listen(held.Addr().String()); err == nil {
		_ = ln.Close()
		t.Error("listen on a taken port succeeded")
	}
}
