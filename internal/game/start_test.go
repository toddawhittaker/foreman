package game

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 7.3: a start succeeds only once the game is in game and answers
// over RCON; every step is shown and the start is audited (SPEC 12.9).
func TestStartReachesRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Base")
	r.start(id)

	st := r.c.Status(id)
	if st.State != StateRunning {
		t.Fatalf("state %s, want running", st.State)
	}
	if st.Image != image21 || st.GameVersion != "2.1.24" || st.Save == "" {
		t.Errorf("status %+v lacks image, version or save", st)
	}
	if st.StartedAt.IsZero() || st.Uptime < 0 {
		t.Errorf("no uptime: %+v", st)
	}
	op := st.Operation
	if op == nil || !op.Done || op.Error != "" || op.Action != AuditStart {
		t.Fatalf("operation %+v", op)
	}
	for _, s := range op.Steps {
		want := StepDone
		if s.Name == StepSnapshot {
			want = StepSkipped
		}
		if s.State != want {
			t.Errorf("step %q is %s, want %s", s.Name, s.State, want)
		}
	}
	if cmds := r.commands(id); !slices.Contains(cmds, "/players online") {
		t.Errorf("start never checked RCON: %v", cmds)
	}
	entries := r.audit.list()
	if len(entries) != 1 || entries[0].Action != AuditStart || entries[0].WorldID != id ||
		entries[0].UserID != admin.UserID || entries[0].Username != admin.Username {
		t.Errorf("audit %+v", entries)
	}
}

// SPEC 4.3 to 4.6: the container's fixed
// shape, and the RCON password nowhere in its settings.
func TestContainerShape(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Shape")
	r.start(id)

	c, ok := r.game.Daemon().Container(ContainerName(id))
	if !ok {
		t.Fatal("no container named foreman-<id>")
	}
	s := c.Spec
	if s.Image != image21 {
		t.Errorf("image %q", s.Image)
	}
	if s.User != "845:845" {
		t.Errorf("user %q", s.User)
	}
	if !slices.Equal(s.Entrypoint, []string{"/bin/sh"}) || len(s.Cmd) != 2 || s.Cmd[0] != "-c" {
		t.Fatalf("entrypoint %v cmd %v", s.Entrypoint, s.Cmd)
	}
	script := s.Cmd[1]
	for _, want := range []string{
		"/opt/factorio/bin/x64/factorio", "--start-server-load-latest",
		"--mod-directory /factorio/mods", "--rcon-port 27015",
		"cat /factorio/" + world.RCONPasswordFile,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "--start-server ") {
		t.Error("the script names a save; it must load the latest (SPEC 4.3)")
	}
	if !s.OpenStdin {
		t.Error("stdin is not open (SPEC 4.3)")
	}
	h := s.HostConfig
	if h.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("restart policy %q", h.RestartPolicy.Name)
	}
	if !slices.Equal(h.CapDrop, []string{"ALL"}) || !slices.Equal(h.SecurityOpt, []string{"no-new-privileges"}) {
		t.Errorf("cap drop %v, security %v", h.CapDrop, h.SecurityOpt)
	}
	if h.LogConfig.Type != "json-file" || h.LogConfig.Config["max-size"] == "" || h.LogConfig.Config["max-file"] == "" {
		t.Errorf("log config %+v", h.LogConfig)
	}
	wantBind := filepath.Join(r.data, "worlds", id) + ":/factorio"
	if !slices.Equal(h.Binds, []string{wantBind}) {
		t.Errorf("binds %v, want only %s", h.Binds, wantBind)
	}
	if len(h.PortBindings) != 1 || h.PortBindings["34197/udp"][0].HostPort != "34197" {
		t.Errorf("ports %v, want only 34197/udp", h.PortBindings)
	}
	for p := range s.ExposedPorts {
		if strings.HasPrefix(p, "27015") {
			t.Error("the RCON port is exposed (SPEC 4.4)")
		}
	}
	if h.NetworkMode != "bridge" {
		t.Errorf("network %q, want bridge outside a container (SPEC 4.4)", h.NetworkMode)
	}
	if s.StopTimeout == nil || *s.StopTimeout != StopGraceSeconds {
		t.Errorf("stop timeout %v", s.StopTimeout)
	}
	if s.Labels[LabelWorld] != id || s.Labels[LabelInstall] != r.data {
		t.Errorf("labels %v", s.Labels)
	}
	pw, err := os.ReadFile(filepath.Join(r.worldDir(id), world.RCONPasswordFile))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), strings.TrimSpace(string(pw))) {
		t.Error("the RCON password is in the container's settings (SPEC 4.5)")
	}
}

// SPEC 4.4: the network the game joins, and the refusals that keep a game away
// from the Docker socket proxy and from posing as a trusted proxy.
func TestGameNetwork(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	// Both stand-ins for Foreman share "games"; each carries this install's
	// labels so that, while the other is self, it passes the members check.
	ours := map[string]string{LabelInstall: r.data, LabelWorld: "stand-in"}
	seed := func(name string, nets map[string]string) string {
		r.game.Daemon().Seed(dockertest.Container{Name: name, Status: "running", Networks: nets,
			Spec: docker.ContainerSpec{Labels: ours}})
		c, _ := r.game.Daemon().Container(name)
		return c.ID
	}
	one := seed("foreman-one", map[string]string{"games": "172.30.0.9/24"})
	two := seed("foreman-two", map[string]string{"games": "172.30.0.9/24", "proxy": "172.31.0.9/24"})
	proxies := []netip.Prefix{netip.MustParsePrefix("172.31.0.0/16")}
	wide := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}
	// What DOCKER_HOST's host resolves to; a host not listed does not resolve.
	hosts := map[string][]string{
		"proxy-apart":    {"172.31.0.5"},
		"proxy-on-games": {"172.31.0.5", "172.30.0.5"},
		"172.30.0.7":     {"172.30.0.7"},
		"mapped":         {"::ffff:172.30.0.5"},
		"garbled":        {"not-an-address"},
		"lan-proxy":      {"192.168.1.10"},
		"loopback":       {"127.0.0.1"},
		"half-apart":     {"172.31.0.5", "192.168.1.10"},
	}
	r.c.lookupHost = func(_ context.Context, host string) ([]string, error) {
		if a, ok := hosts[host]; ok {
			return a, nil
		}
		return nil, errors.New("no such host")
	}
	dockerenv := filepath.Join(t.TempDir(), "dockerenv")
	if err := os.WriteFile(dockerenv, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	noDockerenv := r.c.containerEnvs

	const lost = "cannot tell which networks it is on"
	cases := []struct {
		name, network, self string
		dockerenv           bool
		tcpHost             string // "" means DOCKER_HOST is a unix socket
		proxies             []netip.Prefix
		want, wantErr       string
	}{
		{"outside a container", "", "", false, "", nil, "bridge", ""},
		{"outside a container, set", "chosen", "", false, "", nil, "chosen", ""},
		{"hostname of no container", "", "my-laptop", false, "", nil, "bridge", ""},
		{"hostname of no container, but /.dockerenv", "games", "my-laptop", true, "", nil, "", lost},
		{"hostname of no container, but tcp", "games", "my-laptop", false, "proxy-apart", nil, "", lost},
		{"no ID at all, but /.dockerenv", "games", "", true, "", nil, "", lost},
		{"no ID at all, but tcp", "games", "", false, "proxy-apart", nil, "", lost},
		{"inside a container, unset", "", one, true, "", nil, "", "FOREMAN_GAME_NETWORK must be set"},
		{"inside, one network, socket", "games", one, true, "", nil, "games", ""},
		{"inside, not on it", "elsewhere", two, false, "", nil, "", "not on it"},
		{"tcp, proxy on the only network", "games", one, false, "proxy-on-games", nil, "", "172.30.0.5, which is on the game network"},
		{"tcp, proxy on the game network of two", "games", two, false, "proxy-on-games", nil, "", "172.30.0.5, which is on the game network"},
		{"tcp by address on the game network", "games", two, false, "172.30.0.7", nil, "", "172.30.0.7, which is on the game network"},
		{"tcp, IPv4 in IPv6 form", "games", two, false, "mapped", nil, "", "172.30.0.5, which is on the game network"},
		{"tcp, proxy elsewhere", "games", two, false, "proxy-apart", nil, "games", ""},
		{"tcp, proxy elsewhere, one network", "games", one, false, "proxy-apart", nil, "", "not on any of Foreman's networks other than"},
		{"tcp, proxy published on the LAN", "games", two, false, "lan-proxy", nil, "", "192.168.1.10, which is not on any"},
		{"tcp, proxy on loopback", "games", two, false, "loopback", nil, "", "127.0.0.1, which is not on any"},
		{"tcp, one address outside", "games", two, false, "half-apart", nil, "", "192.168.1.10, which is not on any"},
		{"tcp, host does not resolve", "games", two, false, "gone", nil, "", "cannot look up"},
		{"tcp, unreadable address", "games", two, false, "garbled", nil, "", "cannot read"},
		{"apart from trusted proxies", "games", two, false, "proxy-apart", proxies, "games", ""},
		{"overlaps trusted proxies", "games", two, false, "proxy-apart", wide, "", "overlaps FOREMAN_TRUSTED_PROXIES"},
		{"overlap on the proxy network", "proxy", two, false, "", proxies, "", "172.31.0.0/24"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.c.network, r.c.selfID, r.c.proxies = tc.network, tc.self, tc.proxies
			r.c.dockerTCP, r.c.dockerHost = tc.tcpHost != "", tc.tcpHost
			r.c.containerEnvs = noDockerenv
			if tc.dockerenv {
				r.c.containerEnvs = []string{filepath.Join(t.TempDir(), "absent"), dockerenv}
			}
			got, err := r.c.gameNetwork(context.Background())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %q, %v; want an error with %q", got, err, tc.wantErr)
				}
				if !errors.Is(err, ErrNetwork) {
					t.Errorf("%q is not an ErrNetwork, so the page cannot show it", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	c, err := New(Config{Docker: r.docker, Worlds: r.worlds, Audit: r.audit, HostDataDir: r.data,
		Images: testImages, DockerHost: "tcp://socket-proxy:2375", TrustedProxies: proxies})
	if err != nil || !c.dockerTCP || c.dockerHost != "socket-proxy" || len(c.proxies) != 1 {
		t.Errorf("Config's DockerHost and TrustedProxies were not taken: %v", err)
	}
	if _, err := New(Config{Docker: r.docker, Worlds: r.worlds, Audit: r.audit, HostDataDir: r.data,
		Images: testImages, Network: "host"}); err == nil {
		t.Error("FOREMAN_GAME_NETWORK=host was accepted (SPEC 14.4)")
	}
}

// SPEC 4.4: only Foreman and this install's games may be on the game
// network.
func TestGameNetworkMembers(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	d := r.game.Daemon()
	d.Seed(dockertest.Container{Name: "foreman-self", Status: "running", Networks: map[string]string{"games": "172.30.0.9/24"}})
	self, _ := d.Container("foreman-self")
	r.c.network, r.c.selfID = "games", self.ID
	check := func(want string) {
		t.Helper()
		got, err := r.c.gameNetwork(context.Background())
		if want == "" {
			if err != nil || got != "games" {
				t.Fatalf("got %q, %v; want games", got, err)
			}
			return
		}
		if !errors.Is(err, ErrNetwork) || !strings.Contains(err.Error(), want) {
			t.Fatalf("got %q, %v; want an ErrNetwork with %q", got, err, want)
		}
	}
	check("")
	d.Seed(dockertest.Container{Name: "foreman-mine", Status: "exited", Networks: map[string]string{"games": ""},
		Spec: docker.ContainerSpec{Labels: map[string]string{LabelInstall: r.data, LabelWorld: "mine"}}})
	check("")
	d.Seed(dockertest.Container{Name: "foreman-theirs", Status: "running", Networks: map[string]string{"games": "172.30.0.10/24"},
		Spec: docker.ContainerSpec{Labels: map[string]string{LabelInstall: "/srv/other", LabelWorld: "theirs"}}})
	d.Seed(dockertest.Container{Name: "socket-proxy", Status: "running", Networks: map[string]string{"games": "172.30.0.11/24"}})
	check("also holds foreman-theirs, socket-proxy")
	// Outside a container the default bridge is shared with anything, and
	// nothing is checked.
	r.c.selfID, r.c.network = "", ""
	if got, err := r.c.gameNetwork(context.Background()); err != nil || got != "bridge" {
		t.Errorf("outside a container: %q, %v", got, err)
	}
}

// SPEC 4.4, 12.6, 14.4: the game network's IPv6 range is checked too.
func TestGameNetworkIPv6(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.lookupHost = func(_ context.Context, host string) ([]string, error) {
		return map[string][]string{
			"proxy-v6-on-games": {"fd00:30::5"},
			"proxy-v6-zoned":    {"fd00:30::5%eth0"},
			"proxy-v6-apart":    {"fd00:31::5"},
		}[host], nil
	}
	games := docker.Network{IPAddress: "172.30.0.9", IPPrefixLen: 24, GlobalIPv6Address: "fd00:30::9", GlobalIPv6PrefixLen: 64}
	proxyNet := docker.Network{IPAddress: "172.31.0.9", IPPrefixLen: 24, GlobalIPv6Address: "fd00:31::9", GlobalIPv6PrefixLen: 64}
	noLen := games
	noLen.GlobalIPv6PrefixLen = 0
	cases := []struct {
		name    string
		net     docker.Network
		tcpHost string
		proxies []netip.Prefix
		wantErr string
	}{
		{"apart", games, "proxy-v6-apart", []netip.Prefix{netip.MustParsePrefix("fd00:31::/64")}, ""},
		{"trusted proxies overlap the IPv6 range", games, "", []netip.Prefix{netip.MustParsePrefix("fd00:30::/48")}, "overlaps FOREMAN_TRUSTED_PROXIES"},
		{"proxy's IPv6 address on the game network", games, "proxy-v6-on-games", nil, "which is on the game network"},
		{"zoned IPv6 address on the game network", games, "proxy-v6-zoned", nil, "which is on the game network"},
		{"IPv6 range unreadable", noLen, "proxy-v6-apart", nil, "cannot read the address range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.c.network, r.c.proxies = "games", tc.proxies
			r.c.dockerTCP, r.c.dockerHost = tc.tcpHost != "", tc.tcpHost
			self := docker.Container{Networks: map[string]docker.Network{"games": tc.net, "proxy": proxyNet}}
			got, err := r.c.checkGameNetwork(context.Background(), self)
			if tc.wantErr == "" {
				if err != nil || got != "games" {
					t.Fatalf("got %q, %v; want games", got, err)
				}
				return
			}
			if !errors.Is(err, ErrNetwork) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %q, %v; want an ErrNetwork with %q", got, err, tc.wantErr)
			}
		})
	}
}

func TestNewRefusesBadHostPath(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	for _, p := range []string{"relative/data", "/srv/da:ta", "/srv/data/", "/srv/a,b"} {
		if _, err := New(Config{Docker: r.docker, Worlds: r.worlds, Audit: r.audit, HostDataDir: p, Images: testImages}); err == nil {
			t.Errorf("host data path %q was accepted", p)
		}
	}
}

func TestImageFor(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	for v, want := range map[string]string{"1.1.110": image11, "2.0.72": image20, "2.1.24": image21} {
		if got, err := r.c.imageFor(v); err != nil || got != want {
			t.Errorf("imageFor(%s) = %q, %v", v, got, err)
		}
	}
	if _, err := r.c.imageFor("3.0.1"); err == nil {
		t.Error("an unknown version got an image")
	}
}

// SPEC 7.4: the pre-start check refuses, and nothing is created.
func TestStartRefusedByPrecheck(t *testing.T) {
	r := newRig(t, rigOptions{})
	id, err := r.worlds.Create(context.Background(), "Empty", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	err = r.c.Start(context.Background(), id, admin, StartOptions{Anyway: true})
	var pe *PrecheckError
	if !errors.As(err, &pe) || pe.Check.CanStartAnyway() {
		t.Fatalf("got %v, want a plain refusal for no save", err)
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("a container was created")
	}
	if st := r.c.Status(id); st.Operation == nil || !st.Operation.Done || st.Operation.Steps[0].State != StepFailed {
		t.Errorf("the check step is not shown failed: %+v", st.Operation)
	}
	for _, st := range r.c.Status(id).Operation.Steps[1:] {
		if st.State != StepSkipped {
			t.Errorf("step %q reads %q after the refusal, want skipped", st.Name, st.State)
		}
	}
}

// SPEC 7.4: an unreadable save refuses, and "Start anyway" snapshots first.
func TestStartAnyway(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Odd")
	r.writeFile(id, filepath.Join(world.SavesDir, "broken.zip"), []byte("not a zip"))

	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	var pe *PrecheckError
	if !errors.As(err, &pe) || !pe.Check.CanStartAnyway() {
		t.Fatalf("got %v, want a refusal that allows start anyway", err)
	}
	if err := r.c.Start(context.Background(), id, admin, StartOptions{Anyway: true}); err != nil {
		t.Fatal(err)
	}
	snaps, err := r.worlds.Snapshots(id)
	if err != nil || len(snaps) != 1 || snaps[0].Reason != ReasonStartAnyway || snaps[0].TakenBy != admin.Username {
		t.Fatalf("snapshots %+v, %v", snaps, err)
	}
	entries := r.audit.list()
	if last := entries[len(entries)-1]; last.Detail != "start anyway" {
		t.Errorf("audit detail %q", last.Detail)
	}
	if !hasStep(r.c.Status(id).Operation, StepSnapshot) {
		t.Error("a start anyway does not show its snapshot step")
	}
}

func hasStep(op *Operation, name string) bool {
	return op != nil && slices.ContainsFunc(op.Steps, func(s Step) bool { return s.Name == name })
}

// DESIGN 7.6: the snapshot step belongs to a start anyway only.
func TestPlainStartHasNoSnapshotStep(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Plain")
	r.start(id)
	if hasStep(r.c.Status(id).Operation, StepSnapshot) {
		t.Error("a plain start lists the start-anyway snapshot step")
	}
	if err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if hasStep(r.c.Status(id).Operation, StepSnapshot) {
		t.Error("a plain restart lists the start-anyway snapshot step")
	}
}

// Fail safe (SPEC 14.8): server settings Foreman cannot read may hold a
// game password it could not hide, so the game is not started.
func TestStartRefusesUnreadableServerSettings(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Bad settings")
	r.writeFile(id, filepath.Join(world.ConfigDir, "server-settings.json"), []byte(`{"game_password": "hunter2",`))
	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	if err == nil || !strings.Contains(err.Error(), "server-settings.json") {
		t.Fatalf("got %v, want a refusal naming server-settings.json", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Error("the message shows the password")
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("a container was created")
	}
}

// SPEC 14.5, 14.8: the game can write server-settings.json, so a named pipe
// or an oversized file there is refused at once rather than waited on or
// read whole.
func TestStartRefusesServerSettingsThatAreNotASmallFile(t *testing.T) {
	r := newRig(t, rigOptions{})
	pipe := r.newWorld("Pipe")
	p := filepath.Join(r.worldDir(pipe), world.ConfigDir, "server-settings.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.c.Start(context.Background(), pipe, admin, StartOptions{}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("pipe: got %v, want a refusal", err)
	}
	r = newRig(t, rigOptions{})
	big := r.newWorld("Big")
	r.writeFile(big, filepath.Join(world.ConfigDir, "server-settings.json"), make([]byte, maxSettings+1))
	if err := r.c.Start(context.Background(), big, admin, StartOptions{}); err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("big: got %v, want a refusal", err)
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("a container was created")
	}
}

// SPEC 3.7: Start never downloads; the download is its own audited action.
func TestImageMissingAndDownload(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Pull")
	r.game.SetImage(image21, false)

	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); !errors.Is(err, ErrImageMissing) {
		t.Fatalf("got %v, want ErrImageMissing", err)
	}
	if p := r.game.Daemon().Pulls(); len(p) != 0 {
		t.Fatalf("start pulled %v", p)
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Fatal("a container was created without its image")
	}
	ref, present, err := r.c.ImageFor(context.Background(), id)
	if err != nil || present || ref != image21 {
		t.Fatalf("ImageFor = %q, %v, %v", ref, present, err)
	}
	if err := r.c.PullImage(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if p := r.game.Daemon().Pulls(); !slices.Equal(p, []string{image21}) {
		t.Errorf("pulled %v, want exactly %s", p, image21)
	}
	last := r.audit.list()[len(r.audit.list())-1]
	if last.Action != AuditImageDownload || last.Detail != image21 {
		t.Errorf("audit %+v", last)
	}
	if d := r.c.Status(id).Download; d == nil || !d.Done || d.Steps[0].State != StepDone {
		t.Errorf("download %+v", d)
	}
	r.start(id)
}

func TestPullFailureShown(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Pull fail")
	r.game.Daemon().FailPull(image21, "toomanyrequests")
	if err := r.c.PullImage(context.Background(), id, admin); err == nil {
		t.Fatal("a failed pull reported success")
	}
	d := r.c.Status(id).Download
	if d == nil || d.Steps[0].State != StepFailed || !strings.Contains(d.Error, "toomanyrequests") {
		t.Errorf("download %+v", d)
	}
}

// Fail safe: a save newer than the pinned image's game is a plain refusal,
// before anything is created, and a restart refuses before stopping.
func TestStartRefusesSaveNewerThanImage(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Too new") // world 2.1.24, save from 2.1.24
	r.start(id)
	older := "factoriotools/factorio:2.1.20@sha256:" + strings.Repeat("0", 64)
	r.c.images = map[string]string{"1.1": image11, "2.0": image20, "2.1": older}

	err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{Anyway: true})
	if !errors.Is(err, ErrSaveNewerThanImage) {
		t.Fatalf("got %v, want ErrSaveNewerThanImage", err)
	}
	for _, v := range []string{"2.1.24", "2.1.20"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("message %q does not name %s", err, v)
		}
	}
	var pe *PrecheckError
	if errors.As(err, &pe) {
		t.Error("offered start anyway")
	}
	if r.container(id).Status != "running" {
		t.Error("the restart stopped the game before refusing")
	}
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	creates := len(r.calls("POST /containers/create"))
	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); !errors.Is(err, ErrSaveNewerThanImage) {
		t.Fatalf("start got %v", err)
	}
	if len(r.calls("POST /containers/create")) != creates {
		t.Error("a container was created for a save the image cannot load")
	}

	// An image as new as the save starts.
	r.c.images = testImages
	r.start(id)
}

func TestImageVersion(t *testing.T) {
	if v, ok := imageVersion(image20); !ok || v != [3]uint16{2, 0, 77} {
		t.Errorf("got %v, %v", v, ok)
	}
	if v, ok := imageVersion("registry:5000/factorio:1.1.110"); !ok || v != [3]uint16{1, 1, 110} {
		t.Errorf("got %v, %v", v, ok)
	}
	for _, ref := range []string{"registry:5000/factorio", "factorio:latest", "factorio:2.0", "factorio@sha256:ab"} {
		if _, ok := imageVersion(ref); ok {
			t.Errorf("%q read as a version", ref)
		}
	}
}

// SPEC 4.3: a container with the name that Foreman did not make is refused.
func TestStartRefusesForeignContainer(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Taken")
	r.game.Daemon().Seed(dockertest.Container{Name: ContainerName(id), Status: "exited"})
	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); !errors.Is(err, ErrForeign) {
		t.Fatalf("got %v, want ErrForeign", err)
	}
	if len(r.calls("DELETE /containers/")) != 0 {
		t.Error("Foreman removed a container it did not create")
	}
}

func TestStartRefusesWhenRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Twice")
	r.start(id)
	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("got %v, want ErrAlreadyRunning", err)
	}
	if r.container(id).Status != "running" {
		t.Error("the running game was disturbed")
	}
}

// ADR 0003: every start makes a fresh container.
func TestStartRecreatesContainer(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Fresh")
	r.start(id)
	first := r.container(id).ID
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	r.start(id)
	if second := r.container(id).ID; second == first {
		t.Error("the old container was reused")
	}
	if n := len(r.game.State().Containers); n != 1 {
		t.Errorf("%d containers, want 1", n)
	}
}

// SPEC 7.11: a chosen save loads once, then the newest loads again.
func TestChosenSaveAppliesOnce(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Choose")
	r.putSave(id, "2.1.24-no-space-age.zip", "newer.zip", time.Now().Add(-time.Minute))
	if err := r.worlds.SetLoadSave(id, "base.zip"); err != nil {
		t.Fatal(err)
	}
	r.start(id)
	newest, _, err := r.worlds.NewestSave(id)
	if err != nil || newest.Name != "base.zip" {
		t.Fatalf("newest save %q, %v; the chosen save was not made newest", newest.Name, err)
	}
	if !newest.ModTime.Before(time.Now().Add(time.Second)) {
		t.Error("the chosen save's time is in the future")
	}
	root, err := r.worlds.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil || meta.LoadSave != "" {
		t.Errorf("load_save is %q after the start reached the game (SPEC 7.11), %v", meta.LoadSave, err)
	}
}

// SPEC 8.7, 7.3: a save that fails to load fails the start with the game's
// reason, and the crash loop then leaves it stopped (SPEC 8.9).
func TestStartLoadFailure(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Corrupt")
	r.game.SetCrashLoop(ContainerName(id), true)
	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	if !errors.Is(err, ErrStartFailed) || !strings.Contains(err.Error(), "could not load the save") {
		t.Fatalf("got %v", err)
	}
	st := r.waitState(id, StateCrashLooping)
	if len(st.CrashLines) == 0 || len(st.CrashLines) > CrashLines {
		t.Errorf("%d crash lines", len(st.CrashLines))
	}
	eventually(t, func() bool { return r.container(id).Status == "exited" }, "the container to stop")
}

func TestStartTimeout(t *testing.T) {
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 10 * time.Second}})
	r.c.startTimeout = 200 * time.Millisecond
	id := r.newWorld("Slow")
	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	if !errors.Is(err, ErrStartFailed) || !strings.Contains(err.Error(), "did not finish loading") {
		t.Fatalf("got %v", err)
	}
}

// SPEC 7.3: in game alone is not enough; RCON must answer.
func TestStartNeedsRCON(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("No RCON")
	r.game.SetRCONRefuse(ContainerName(id), true)
	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	if !errors.Is(err, ErrStartFailed) || !strings.Contains(err.Error(), "RCON") {
		t.Fatalf("got %v", err)
	}
	op := r.c.Status(id).Operation
	if op.Steps[len(op.Steps)-1].State != StepFailed {
		t.Errorf("the RCON step is not failed: %+v", op.Steps)
	}
}

// The audit comes first: an action that cannot be recorded is not done.
func TestStartNotDoneWithoutAudit(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Unaudited")
	r.audit.setFail(true)
	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); err == nil {
		t.Fatal("started without an audit entry")
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("a container was created")
	}
}

// SPEC 5.3: nothing assumes one world.
func TestTwoWorldsAreSeparate(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.newWorld("Alpha"), r.newWorld("Beta")
	r.start(a)
	r.startAsActive(b)
	if err := r.c.Stop(context.Background(), a, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	r.waitState(a, StateStopped)
	if st := r.c.Status(b); st.State != StateRunning {
		t.Errorf("world b is %s after stopping a", st.State)
	}
}
