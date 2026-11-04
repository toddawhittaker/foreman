package fakegame_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	image21  = "factoriotools/factorio:2.1.24@sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28"
	image11  = "factoriotools/factorio:1.1.110@sha256:6fb9b7bfb12fb51bf668fce7e29cb1e98053f248201e64dfee870ef73b2f31cb"
	password = "pw-for-this-test"
)

type rig struct {
	t      *testing.T
	game   *fakegame.Game
	docker *docker.Client
	world  string
}

func newRig(t *testing.T, cfg fakegame.Config) *rig {
	t.Helper()
	if cfg.Images == nil {
		cfg.Images = []string{image21, image11}
	}
	g := fakegame.New(t, cfg)
	c, err := docker.New(g.Host())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, world.ConfigDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, world.RCONPasswordFile), []byte(password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, game: g, docker: c, world: dir}
}

func (r *rig) spec(img, policy string) docker.ContainerSpec {
	return docker.ContainerSpec{
		Image:      img,
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"-c", "exec factorio", "sh", "mysave.zip"},
		OpenStdin:  true,
		HostConfig: docker.HostConfig{
			Binds:         []string{r.world + ":/factorio"},
			RestartPolicy: docker.RestartPolicy{Name: policy},
		},
	}
}

// start creates and starts a container and returns its ID.
func (r *rig) start(name, img, policy string) string {
	r.t.Helper()
	ctx := context.Background()
	id, err := r.docker.CreateContainer(ctx, name, r.spec(img, policy))
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *rig) inspect(id string) docker.Container {
	r.t.Helper()
	c, err := r.docker.InspectContainer(context.Background(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

func (r *rig) log(id string) string {
	r.t.Helper()
	s, err := r.docker.Logs(context.Background(), id, docker.LogOptions{})
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var out strings.Builder
	for {
		l, err := s.Next()
		if err != nil {
			return out.String()
		}
		out.WriteString(l.Text + "\n")
	}
}

func (r *rig) rcon(id string) *rcon.Client {
	r.t.Helper()
	ip := r.inspect(id).Networks["bridge"].IPAddress
	if !strings.HasPrefix(ip, "127.") {
		r.t.Fatalf("container address %q is not a loopback address", ip)
	}
	c := rcon.New(ip+":27015", secret.New(password))
	r.t.Cleanup(func() { _ = c.Close() })
	return c
}

func (r *rig) exec(id, command string) string {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := r.rcon(id).Exec(ctx, command)
	if err != nil {
		r.t.Fatalf("%s: %v", command, err)
	}
	return out
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *rig) status(id string) string { return r.inspect(id).State.Status }

func TestStartReplaysTheTranscriptForTheImageVersion(t *testing.T) {
	for _, tc := range []struct{ image, banner, state string }{
		{image21, "Factorio 2.1.24", "ServerMultiplayerManager.cpp:809"},
		{image11, "Factorio 1.1.110", "ServerMultiplayerManager.cpp:814"},
		{"factoriotools/factorio:2.0.77", "Factorio 2.0.77", "ServerMultiplayerManager.cpp:808"},
		{"factoriotools/factorio:stable", "Factorio 2.1.24", "ServerMultiplayerManager.cpp:809"},
		{"localhost:5000/factorio", "Factorio 2.1.24", "ServerMultiplayerManager.cpp:809"},
	} {
		t.Run(tc.image, func(t *testing.T) {
			r := newRig(t, fakegame.Config{Images: []string{tc.image}})
			id := r.start("foreman-w1", tc.image, "unless-stopped")
			log := r.log(id)
			for _, want := range []string{
				tc.banner, tc.state, "to(InGame)", "Starting RCON interface",
				"Loading map /factorio/saves/mysave.zip: ",
			} {
				if !strings.Contains(log, want) {
					t.Errorf("log lacks %q:\n%s", want, log)
				}
			}
			if strings.Contains(log, "spike.zip") || strings.Contains(log, password) {
				t.Errorf("log has the transcript's save name or the password:\n%s", log)
			}
		})
	}
}

func TestRCONOnTheContainersOwnAddress(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	a := r.start("foreman-a", image21, "unless-stopped")
	b := r.start("foreman-b", image21, "unless-stopped")
	if ia, ib := r.inspect(a).Networks["bridge"].IPAddress, r.inspect(b).Networks["bridge"].IPAddress; ia == ib {
		t.Fatalf("both containers have address %s", ia)
	}
	// Reply format from internal/game/testdata/README.md, section 6.
	if got := r.exec(a, "/players online"); got != "Online players (0):\n" {
		t.Errorf("/players online = %q", got)
	}
	if !strings.Contains(r.log(a), "New RCON connection from") {
		t.Error("the game did not log the RCON connection")
	}
	if got := r.game.State().Containers[0].Commands; len(got) != 1 || got[0] != "/players online" {
		t.Errorf("commands = %q", got)
	}

	wrong := rcon.New(r.rconAddr(a), secret.New("wrong"))
	defer func() { _ = wrong.Close() }()
	if _, err := wrong.Exec(context.Background(), "/players online"); !errors.Is(err, rcon.ErrLogin) {
		t.Errorf("wrong password: %v, want ErrLogin", err)
	}
}

func (r *rig) rconAddr(id string) string {
	return r.inspect(id).Networks["bridge"].IPAddress + ":27015"
}

func TestRCONIsClosedBeforeLoadFinishes(t *testing.T) {
	r := newRig(t, fakegame.Config{LoadDelay: 300 * time.Millisecond})
	id := r.start("foreman-w1", image21, "unless-stopped")
	log := r.log(id)
	if !strings.Contains(log, "Loading map") || strings.Contains(log, "InGame") {
		t.Fatalf("a delayed start should stop after Loading map:\n%s", log)
	}
	c := rcon.New(r.rconAddr(id), secret.New(password))
	defer func() { _ = c.Close() }()
	if _, err := c.Exec(context.Background(), "/players online"); err == nil {
		t.Error("RCON answered before the game was loaded")
	}
	eventually(t, "InGame", func() bool { return strings.Contains(r.log(id), "to(InGame)") })
	r.exec(id, "/players online")
}

func TestPlayers(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	if err := r.game.Join("foreman-w1", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.game.Join("foreman-w1", "bob"); err != nil {
		t.Fatal(err)
	}
	// The reply format is not yet seen on a real server.
	if got, want := r.exec(id, "/players online"), "Online players (2):\n  alice (online)\n  bob (online)\n"; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
	if err := r.game.Leave("foreman-w1", "alice"); err != nil {
		t.Fatal(err)
	}
	if got := r.exec(id, "/players online"); got != "Online players (1):\n  bob (online)\n" {
		t.Errorf("after leave: %q", got)
	}
	log := r.log(id)
	for _, want := range []string{"[JOIN] alice joined the game", "[JOIN] bob joined the game", "[LEAVE] alice left the game"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q", want)
		}
	}
	for name, err := range map[string]error{
		"join twice":     r.game.Join("foreman-w1", "bob"),
		"leave when off": r.game.Leave("foreman-w1", "carol"),
		"no such game":   r.game.Join("foreman-nope", "carol"),
		"unnamed player": r.game.Join("foreman-w1", ""),
		"leave no game":  r.game.Leave("foreman-nope", "bob"),
		"crash no game":  r.game.Crash("foreman-nope", 1, false),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestJoinBeforeInGameIsRefused(t *testing.T) {
	r := newRig(t, fakegame.Config{LoadDelay: time.Second})
	r.start("foreman-w1", image21, "unless-stopped")
	if err := r.game.Join("foreman-w1", "alice"); err == nil {
		t.Error("a player joined a game that is still loading")
	}
}

func TestServerSaveFinishesInTheLog(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	if got := r.exec(id, "/server-save"); got != "Saving the map\n" {
		t.Errorf("reply = %q", got)
	}
	eventually(t, "Saving finished", func() bool {
		log := r.log(id)
		return strings.Contains(log, "Saving game as /factorio/saves/mysave.zip") && strings.Contains(log, "Saving finished")
	})
}

func TestServerSaveThatHangsNeverFinishes(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	r.game.SetSaveHangs("foreman-w1", true)
	id := r.start("foreman-w1", image21, "unless-stopped")
	if got := r.exec(id, "/server-save"); got != "Saving the map\n" {
		t.Errorf("reply = %q", got)
	}
	time.Sleep(300 * time.Millisecond)
	log := r.log(id)
	if !strings.Contains(log, "Saving game as") || strings.Contains(log, "Saving finished") {
		t.Errorf("a hanging save should start and not finish:\n%s", log)
	}
	if !r.game.State().Containers[0].SaveHangs {
		t.Error("State does not show the hanging save")
	}
}

func TestStopSavesAndStaysStopped(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	if err := r.docker.StopContainer(context.Background(), id, 120); err != nil {
		t.Fatal(err)
	}
	log := r.log(id)
	for _, want := range []string{"Received SIGTERM, shutting down", "Saving map as /factorio/saves/mysave.zip", "Goodbye"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q", want)
		}
	}
	c := r.inspect(id)
	if c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Errorf("state = %+v", c.State)
	}
	c2 := rcon.New(r.rconAddr(id), secret.New(password))
	defer func() { _ = c2.Close() }()
	if _, err := c2.Exec(context.Background(), "/players online"); err == nil {
		t.Error("RCON still answers after the stop")
	}
	time.Sleep(400 * time.Millisecond) // past several restart delays
	if c := r.inspect(id); c.State.Status != "exited" || c.RestartCount != 0 {
		t.Errorf("a stopped container came back: %+v", c)
	}
}

func TestKillStaysStopped(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	if err := r.docker.KillContainer(context.Background(), id, ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	c := r.inspect(id)
	if c.State.Status != "exited" || c.State.ExitCode != 137 || c.RestartCount != 0 {
		t.Errorf("after kill: %+v", c)
	}
	if strings.Contains(r.log(id), "SIGTERM") {
		t.Error("a kill wrote the SIGTERM shutdown")
	}
}

func TestCrashRestartsUnderUnlessStopped(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	if err := r.game.Join("foreman-w1", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.game.Crash("foreman-w1", 139, true); err != nil {
		t.Fatal(err)
	}
	// Docker shows "restarting" while the policy waits to start it again.
	if c := r.inspect(id); c.State.Status != "restarting" || c.State.ExitCode != 139 || !c.State.OOMKilled {
		t.Errorf("right after the crash: %+v", c.State)
	}
	eventually(t, "restart", func() bool { return r.inspect(id).RestartCount == 1 && r.status(id) == "running" })
	eventually(t, "InGame again", func() bool { return strings.Count(r.log(id), "to(InGame)") == 2 })
	if got := r.exec(id, "/players online"); got != "Online players (0):\n" {
		t.Errorf("a restart should clear the players: %q", got)
	}
}

func TestNoRestartWithoutThePolicy(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "")
	if err := r.game.Crash("foreman-w1", 1, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if c := r.inspect(id); c.State.Status != "exited" || c.RestartCount != 0 {
		t.Errorf("restarted without a policy: %+v", c)
	}
}

func TestCrashLoopRestartsUntilStopped(t *testing.T) {
	for _, tc := range []struct {
		image    string
		wantExit int // R27: 1.1 exits 0 after a failed load
	}{{image21, 1}, {image11, 0}} {
		t.Run(tc.image, func(t *testing.T) {
			r := newRig(t, fakegame.Config{})
			id := r.start("foreman-w1", tc.image, "unless-stopped")
			r.game.SetCrashLoop("foreman-w1", true)
			eventually(t, "three restarts", func() bool { return r.inspect(id).RestartCount >= 3 })
			log := r.log(id)
			for _, want := range []string{"MultiplayerManager failed", "InitializationFailed", "/factorio/saves/mysave.zip"} {
				if !strings.Contains(log, want) {
					t.Errorf("log lacks %q", want)
				}
			}
			if strings.Count(log, "to(InGame)") != 1 {
				t.Error("a looping start must not reach InGame again")
			}
			// Wait for a moment when it is between restarts, then stop it.
			eventually(t, "restarting", func() bool { return r.status(id) == "restarting" })
			if c := r.inspect(id); c.State.ExitCode != tc.wantExit {
				t.Errorf("exit code = %d, want %d", c.State.ExitCode, tc.wantExit)
			}
			_ = r.docker.StopContainer(context.Background(), id, 1)
			at := r.inspect(id).RestartCount
			time.Sleep(600 * time.Millisecond)
			if c := r.inspect(id); c.State.Status != "exited" || c.RestartCount != at {
				t.Errorf("the loop went on after a stop: %+v", c)
			}
		})
	}
}

func TestCrashLoopOffLetsTheNextRestartLoad(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	r.game.SetCrashLoop("foreman-w1", true)
	eventually(t, "a restart", func() bool { return r.inspect(id).RestartCount >= 1 })
	r.game.SetCrashLoop("foreman-w1", false)
	eventually(t, "InGame again", func() bool { return strings.Count(r.log(id), "to(InGame)") == 2 })
	r.exec(id, "/players online")
}

func TestRCONRefusesLoginsOnRequest(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	r.exec(id, "/players online")

	r.game.SetRCONRefuse("foreman-w1", true)
	c := rcon.New(r.rconAddr(id), secret.New(password))
	defer func() { _ = c.Close() }()
	if _, err := c.Exec(context.Background(), "/players online"); !errors.Is(err, rcon.ErrLogin) {
		t.Errorf("err = %v, want ErrLogin", err)
	}
	r.game.SetRCONRefuse("foreman-w1", false)
	if got := r.exec(id, "/players online"); got == "" {
		t.Error("RCON did not recover")
	}

	// The setting also applies to a container made later with that name.
	r.game.SetRCONRefuse("foreman-w2", true)
	id2 := r.start("foreman-w2", image21, "unless-stopped")
	c2 := rcon.New(r.rconAddr(id2), secret.New(password))
	defer func() { _ = c2.Close() }()
	if _, err := c2.Exec(context.Background(), "/players online"); !errors.Is(err, rcon.ErrLogin) {
		t.Errorf("new container: %v, want ErrLogin", err)
	}
}

func TestImageMissing(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	r.game.SetImage(image21, false)
	_, err := r.docker.CreateContainer(context.Background(), "foreman-w1", r.spec(image21, "unless-stopped"))
	if !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("create with a missing image: %v, want ErrNotFound", err)
	}
	if _, err := r.docker.InspectImage(context.Background(), image21); !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("inspect: %v", err)
	}
	if err := r.docker.PullImage(context.Background(), image21); err != nil {
		t.Fatal(err)
	}
	r.start("foreman-w1", image21, "unless-stopped")
}

func TestRemoveEndsTheGameAndFreesItsAddress(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	addr := r.rconAddr(id)
	if err := r.docker.RemoveContainer(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	c := rcon.New(addr, secret.New(password))
	defer func() { _ = c.Close() }()
	if _, err := c.Exec(context.Background(), "/players online"); err == nil {
		t.Error("RCON answers for a removed container")
	}
	id2 := r.start("foreman-w1", image21, "unless-stopped")
	if got := r.rconAddr(id2); got != addr {
		t.Errorf("the freed address %s was not reused (got %s)", addr, got)
	}
	r.exec(id2, "/players online")
}

func TestStartAfterAStopLoadsAgain(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	if err := r.docker.StopContainer(context.Background(), id, 120); err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.exec(id, "/players online")
}

func TestTwoGamesNeverShareAnAddress(t *testing.T) {
	a := newRig(t, fakegame.Config{})
	b := newRig(t, fakegame.Config{})
	ia := a.start("foreman-w1", image21, "unless-stopped")
	ib := b.start("foreman-w1", image21, "unless-stopped")
	if a.rconAddr(ia) == b.rconAddr(ib) {
		t.Fatalf("both games use %s", a.rconAddr(ia))
	}
	a.exec(ia, "/players online")
	b.exec(ib, "/players online")
}

func TestCloseRemovesTheSocketAndStopsGames(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	addr := r.rconAddr(id)
	r.game.Close()
	if _, err := os.Stat(r.game.Socket()); err == nil {
		t.Error("the socket file is still there")
	}
	c := rcon.New(addr, secret.New(password))
	defer func() { _ = c.Close() }()
	if _, err := c.Exec(context.Background(), "/players online"); err == nil {
		t.Error("RCON answers after Close")
	}
	r.game.Close() // a second Close is harmless
}
