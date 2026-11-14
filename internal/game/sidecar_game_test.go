//go:build game

package game

// Real-container tests of a game in a VPN sidecar's network (SPEC 4.13,
// 12.6, 14.16). A container of the pinned game image running `sleep`
// stands in for the sidecar: it owns the network namespace the game joins,
// on a temporary game network and a second, shared network. No VPN runs, so
// these prove the Docker integration and RCON's binding, not live VPN play.
// Run by `make test-game`; temporary networks have foreman-e3test-* names,
// and helper containers carry the foreman-e3test label. All resources are
// removed by test cleanup; killing the test process can leave leftovers.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/store"
)

// sidecarLab is one test's temporary Docker networks and sidecar.
type sidecarLab struct {
	e        *gameEnv
	image    string // the pinned game image, already on the host
	gameNet  string // FOREMAN_GAME_NETWORK
	otherNet string // a second, shared network the sidecar is also on
	name     string // FOREMAN_GAME_NETWORK_CONTAINER
	// secret is in the sidecar's environment, as a VPN key would be; no
	// error or relayed line may show it (SPEC 4.13, 14.16).
	secret string
}

// forEachSidecarTag runs f for each pinned game line, honouring
// FOREMAN_TEST_GAME_TAG as the other real-game tests do.
func forEachSidecarTag(t *testing.T, f func(t *testing.T, e *gameEnv, tag, fixture, version string)) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			start := time.Now()
			e := newGameEnv(t)
			e.requireImage(t, ImageRef(g.version))
			f(t, e, g.tag, g.fixture, g.version)
			t.Logf("TIMING %s: %s", g.tag, time.Since(start).Round(time.Second))
		})
	}
}

// newSidecarLab makes the two networks, each removed at the end of the
// test. Docker picks their ranges.
func newSidecarLab(t *testing.T, e *gameEnv, tag, version string) *sidecarLab {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	prefix := fmt.Sprintf("foreman-e3test-sc-%s-%s-%s", e.run, strings.ReplaceAll(tag, ".", ""), hex.EncodeToString(b))
	l := &sidecarLab{
		e: e, image: ImageRef(version),
		gameNet: prefix + "-game", otherNet: prefix + "-other", name: prefix + "-vpn",
		secret: "tskey-e3test-" + hex.EncodeToString(b) + "-not-a-real-key",
	}
	for _, n := range []string{l.gameNet, l.otherNet} {
		mustDocker(t, "network", "create", "--label", "foreman-e3test=1", n)
		t.Cleanup(func() {
			if out, err := dockerCLI("network", "rm", n); err != nil && !strings.Contains(out, "not found") {
				t.Errorf("could not remove test network %s: %v: %s", n, err, out)
			}
		})
	}
	return l
}

// dockerCLI runs the docker command line. The controller's client has no
// call to make networks or exec in a container, by design (SPEC 14.3).
func dockerCLI(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() //nolint:gosec // fixed test commands
	return strings.TrimSpace(string(out)), err
}

func mustDocker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := dockerCLI(args...)
	if err != nil {
		t.Fatalf("docker %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return out
}

// newContainer creates a container that sleeps in the game image and
// removes it, by ID, at the end of the test. It is not started.
func (l *sidecarLab) newContainer(t *testing.T, args ...string) string {
	t.Helper()
	all := append([]string{"create", "--label", "foreman-e3test=1"}, args...)
	all = append(all, "--entrypoint", "sleep", l.image, "infinity")
	id := mustDocker(t, all...)
	t.Cleanup(func() {
		if out, err := dockerCLI("rm", "-f", id); err != nil && !strings.Contains(out, "No such container") {
			t.Errorf("could not remove test container %s: %v: %s", id, err, out)
		}
	})
	return id
}

// startSidecar makes and starts the sidecar on the game network and the
// shared network. With publish, it publishes UDP 34197 on a random port
// of the host's loopback, so no test needs the host's real game port.
func (l *sidecarLab) startSidecar(t *testing.T, publish bool) docker.Container {
	t.Helper()
	args := []string{"--name", l.name, "--network", l.gameNet, "-e", "TS_AUTHKEY=" + l.secret}
	if publish {
		args = append(args, "-p", "127.0.0.1::34197/udp")
	}
	id := l.newContainer(t, args...)
	mustDocker(t, "network", "connect", l.otherNet, id)
	mustDocker(t, "start", id)
	sc, err := l.e.docker.InspectContainer(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Networks) != 2 {
		t.Fatalf("the sidecar is on %d networks, want the game network and the shared one", len(sc.Networks))
	}
	return sc
}

// controller is gameEnv.controller with the sidecar configured.
func (l *sidecarLab) controller(t *testing.T) (*Controller, func()) {
	t.Helper()
	e := l.e
	c, err := New(Config{
		Docker: e.docker, Worlds: e.worlds, Audit: e.audit, HostDataDir: e.data,
		GameUID: e.uid, GameGID: e.gid, Network: l.gameNet, NetworkContainer: l.name,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.pollInterval = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return c, stop
}

// probe is what a TCP connection attempt found.
type probe string

const (
	probeOpen    probe = "open"    // something listens
	probeRefused probe = "refused" // the address is up and nothing listens
)

// hostDial connects from the test, on the host, to addr:27015. Anything but
// a connection or a refusal (a timeout, no route) proves nothing about a
// listener, so it fails the test.
func hostDial(t *testing.T, addr string) probe {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(addr, RCONPort), 5*time.Second)
	if err == nil {
		_ = conn.Close()
		return probeOpen
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return probeRefused
	}
	t.Fatalf("the probe of %s from the host was inconclusive: %v", addr, err)
	return ""
}

// nsDial connects from inside container id's network namespace to
// addr:27015, with bash's /dev/tcp (the game image has bash and timeout).
// addr is an IP literal, so no DNS is involved. A failed exec, a missing
// tool or any other error fails the test as inconclusive.
func nsDial(t *testing.T, id, addr string) probe {
	t.Helper()
	out, err := dockerCLI("exec", id, "timeout", "5", "bash", "-c", `exec 3<>"/dev/tcp/$0/$1"`, addr, RCONPort)
	switch {
	case err == nil:
		return probeOpen
	case strings.Contains(out, "Connection refused"):
		return probeRefused
	}
	t.Fatalf("the probe of %s inside %s was inconclusive: %v: %s", addr, id, err, out)
	return ""
}

// listeners are the TCP sockets listening on the RCON port in container
// id's network namespace, read from /proc/net/tcp and tcp6, which show the
// sockets of the reader's network namespace.
func listeners(t *testing.T, id string) []string {
	t.Helper()
	out, err := dockerCLI("exec", id, "cat", "/proc/net/tcp", "/proc/net/tcp6")
	if err != nil {
		t.Fatalf("cannot read the sockets in %s: %v: %s", id, err, out)
	}
	port, _ := strconv.Atoi(RCONPort)
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != "0A" { // 0A is LISTEN
			continue
		}
		host, hexPort, ok := strings.Cut(f[1], ":")
		if p, err := strconv.ParseUint(hexPort, 16, 16); !ok || err != nil || int(p) != port {
			continue
		}
		addr, err := procAddr(host)
		if err != nil {
			t.Fatalf("cannot read the address %q in /proc/net: %v", host, err)
		}
		got = append(got, netip.AddrPortFrom(addr, uint16(port)).String()) //nolint:gosec // a fixed port
	}
	slices.Sort(got)
	return got
}

// procAddr decodes an address as /proc/net/tcp{,6} writes it: 32-bit words
// of hex in the host's byte order (little endian here).
func procAddr(s string) (netip.Addr, error) {
	b, err := hex.DecodeString(s)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return netip.Addr{}, fmt.Errorf("not an address: %q", s)
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	a, _ := netip.AddrFromSlice(b)
	return a.Unmap(), nil
}

// interfaces lists the network interfaces container id sees.
func interfaces(t *testing.T, id string) string {
	t.Helper()
	out, err := dockerCLI("exec", id, "ls", "/sys/class/net")
	if err != nil {
		t.Fatalf("cannot list the interfaces in %s: %v: %s", id, err, out)
	}
	return strings.Join(slices.DeleteFunc(strings.Fields(out), func(s string) bool { return s == "bonding_masters" }), " ")
}

// checkSharedSpec checks the game container Docker accepted: it joins the
// sidecar by ID, publishes and exposes nothing itself, and binds RCON to
// the sidecar's game-network address (SPEC 4.13).
func (l *sidecarLab) checkSharedSpec(t *testing.T, id string, sc docker.Container) {
	t.Helper()
	cont := l.e.inspect(t, id)
	if want := "container:" + sc.ID; cont.NetworkMode != want {
		t.Fatalf("the game's network mode is %q, want %q", cont.NetworkMode, want)
	}
	// Docker merges the image's EXPOSE lines into the container's; the
	// spec must add none and publish none (Docker would refuse a publication
	// in another container's network).
	imageExposed := mustDocker(t, "image", "inspect", "--format", "{{json .Config.ExposedPorts}}", l.image)
	exposed := mustDocker(t, "inspect", "--format", "{{json .Config.ExposedPorts}}", cont.ID)
	if exposed != imageExposed {
		t.Fatalf("the game exposes %s, the image itself %s: the spec added ports", exposed, imageExposed)
	}
	if bound := mustDocker(t, "inspect", "--format", "{{json .HostConfig.PortBindings}}", cont.ID); bound != "{}" && bound != "null" {
		t.Fatalf("the game publishes ports itself: %s", bound)
	}
	for port, bs := range cont.Ports {
		if len(bs) > 0 {
			t.Fatalf("Docker reports the game publishing %s: %v", port, bs)
		}
	}
	bind, _ := cont.EnvValue(gameenv.RCONBind)
	if want := net.JoinHostPort(sc.Networks[l.gameNet].IPAddress, RCONPort); bind != want {
		t.Fatalf("%s is %q, want %q", gameenv.RCONBind, bind, want)
	}
}

// noSidecarSecret checks the relayed lines for the sidecar's secret.
func (l *sidecarLab) noSidecarSecret(t *testing.T, c *Controller, id string) {
	t.Helper()
	for i, line := range logLines(c, id) {
		if strings.Contains(line, l.secret) {
			t.Fatalf("relayed line %d holds the sidecar's secret", i+1)
		}
	}
}

func (l *sidecarLab) console(t *testing.T, c *Controller, id, version string) {
	t.Helper()
	_, reply, err := c.Console(context.Background(), id, admin, "/version", "/version")
	if err != nil || !strings.Contains(reply, version) {
		t.Fatalf("/version replied %q, err %v, want it to name %s", reply, err, version)
	}
}

// sidecarStep runs a named step and stops the test at its first failure,
// since later steps follow from earlier ones.
func sidecarStep(t *testing.T, tag, name string, f func(t *testing.T)) {
	t.Helper()
	begin := time.Now()
	ok := t.Run(name, f)
	t.Logf("TIMING %s %s: %s", tag, name, time.Since(begin).Round(100*time.Millisecond))
	if !ok {
		t.FailNow()
	}
}

// TestRealGameSidecarNamespace starts a real game in a sidecar's network
// that is also on a second, shared network with an unrelated service, and
// checks RCON's binding from inside and outside the namespace, a Foreman
// restart and a graceful stop (SPEC 4.13, 14.16, 3.8, 7.5).
func TestRealGameSidecarNamespace(t *testing.T) {
	forEachSidecarTag(t, func(t *testing.T, e *gameEnv, tag, fixture, version string) {
		ctx := context.Background()
		l := newSidecarLab(t, e, tag, version)
		sc := l.startSidecar(t, false)
		// An unrelated service on the shared network is no reason to refuse.
		other := l.newContainer(t, "--network", l.otherNet)
		mustDocker(t, "start", other)
		c, stopMonitor := l.controller(t)
		id := e.newWorld(t, tag, version, "sidecar", fixture, map[string]time.Duration{"main.zip": time.Hour})
		gameIP, otherIP := sc.Networks[l.gameNet].IPAddress, sc.Networks[l.otherNet].IPAddress

		sidecarStep(t, tag, "start in the shared network reaches InGame", func(t *testing.T) {
			if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
				t.Fatalf("start: %v", err)
			}
			st := c.Status(id)
			if st.State != StateRunning || st.GameVersion != version {
				t.Fatalf("state %s (%s), version %q, want running %s", st.State, st.Reason, st.GameVersion, version)
			}
			if st.LANUnpublished != l.name {
				t.Fatalf("LANUnpublished = %q, want %q: the sidecar publishes no game port", st.LANUnpublished, l.name)
			}
			l.checkSharedSpec(t, id, sc)
			l.console(t, c, id, version)
		})

		sidecarStep(t, tag, "RCON listens only on the game-network address", func(t *testing.T) {
			want := []string{net.JoinHostPort(gameIP, RCONPort)}
			if got := listeners(t, sc.ID); !slices.Equal(got, want) {
				t.Fatalf("listening on the RCON port in the namespace: %v, want only %v", got, want)
			}
			// Positive controls first, so a refusal below means no listener.
			if p := hostDial(t, gameIP); p != probeOpen {
				t.Fatalf("from the host, %s:%s is %s, want open", gameIP, RCONPort, p)
			}
			if p := nsDial(t, sc.ID, gameIP); p != probeOpen {
				t.Fatalf("inside the namespace, %s:%s is %s, want open", gameIP, RCONPort, p)
			}
			if p := hostDial(t, otherIP); p != probeRefused {
				t.Fatalf("from the host, the shared network's %s:%s is %s, want refused", otherIP, RCONPort, p)
			}
			if p := nsDial(t, sc.ID, otherIP); p != probeRefused {
				t.Fatalf("inside the namespace, %s:%s is %s, want refused", otherIP, RCONPort, p)
			}
			if p := nsDial(t, sc.ID, "127.0.0.1"); p != probeRefused {
				t.Fatalf("inside the namespace, loopback port %s is %s, want refused", RCONPort, p)
			}
			t.Logf("EVIDENCE interfaces in the namespace: %s", interfaces(t, sc.ID))
		})

		sidecarStep(t, tag, "no secret in the relayed log", func(t *testing.T) {
			e.noSecretInLog(t, c, id)
			l.noSidecarSecret(t, c, id)
		})

		sidecarStep(t, tag, "a new controller re-attaches over RCON without restarting the game", func(t *testing.T) {
			first := e.inspect(t, id)
			stopMonitor()
			c2, _ := l.controller(t)
			st := waitState(t, c2, id, StateRunning, 60*time.Second)
			cont := e.inspect(t, id)
			if cont.ID != first.ID || !cont.State.StartedAt.Equal(first.State.StartedAt) || cont.RestartCount != 0 {
				t.Fatalf("the game was restarted: id %s to %s, restarts %d", first.ID, cont.ID, cont.RestartCount)
			}
			if st.LANUnpublished != l.name {
				t.Fatalf("after re-attach LANUnpublished = %q, want %q", st.LANUnpublished, l.name)
			}
			if _, err := c2.Players(ctx, id); err != nil {
				t.Fatalf("players through the new controller: %v", err)
			}
			l.console(t, c2, id, version)

			before := e.saveTime(t, id, "main.zip")
			began := time.Now()
			if err := c2.Stop(ctx, id, admin, StopOptions{Countdown: 2 * time.Second}); err != nil {
				t.Fatalf("graceful stop: %v", err)
			}
			if st := c2.Status(id); st.State != StateStopped {
				t.Fatalf("state %s (%s), want stopped", st.State, st.Reason)
			}
			if after := e.saveTime(t, id, "main.zip"); !after.After(before) || after.Before(began.Add(-time.Second)) {
				t.Fatalf("save time %v, before %v, stop began %v: the stop did not save", after, before, began)
			}
			if cont := e.inspect(t, id); cont.State.Running {
				t.Fatalf("container status %s after the stop", cont.State.Status)
			}
		})
	})
}

// TestRealGameSidecarLifecycle restarts and switches worlds through RCON
// with the game in a sidecar's network that publishes the game port
// (SPEC 4.13, 7.5, 7.8, 7.10).
func TestRealGameSidecarLifecycle(t *testing.T) {
	forEachSidecarTag(t, func(t *testing.T, e *gameEnv, tag, fixture, version string) {
		ctx := context.Background()
		l := newSidecarLab(t, e, tag, version)
		sc := l.startSidecar(t, true)
		c, _ := l.controller(t)
		st, err := store.Open(ctx, filepath.Join(t.TempDir(), "foreman.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		a := e.newWorld(t, tag, version, "sc-a", fixture, map[string]time.Duration{"a.zip": time.Hour})
		b := e.newWorld(t, tag, version, "sc-b", fixture, map[string]time.Duration{"b.zip": time.Hour})
		if err := st.SetActiveWorld(ctx, a, time.Now()); err != nil {
			t.Fatal(err)
		}
		c.switches = st

		sidecarStep(t, tag, "start with a published sidecar shows no LAN warning", func(t *testing.T) {
			if err := c.Start(ctx, a, admin, StartOptions{}); err != nil {
				t.Fatalf("start: %v", err)
			}
			if s := c.Status(a); s.State != StateRunning || s.LANUnpublished != "" {
				t.Fatalf("state %s (%s), LANUnpublished %q, want running and no warning", s.State, s.Reason, s.LANUnpublished)
			}
			l.checkSharedSpec(t, a, sc)
			l.console(t, c, a, version)
		})

		sidecarStep(t, tag, "restart saves, makes a fresh container and reconnects", func(t *testing.T) {
			first := e.inspect(t, a)
			before := e.saveTime(t, a, "a.zip")
			if err := c.Restart(ctx, a, admin, StopOptions{}, StartOptions{}); err != nil {
				t.Fatalf("restart: %v", err)
			}
			if s := c.Status(a); s.State != StateRunning {
				t.Fatalf("state %s (%s), want running", s.State, s.Reason)
			}
			if cont := e.inspect(t, a); cont.ID == first.ID {
				t.Fatal("the restart kept the old container")
			}
			if after := e.saveTime(t, a, "a.zip"); !after.After(before) {
				t.Fatalf("save time %v, before %v: the restart did not save", after, before)
			}
			l.checkSharedSpec(t, a, sc)
			l.console(t, c, a, version)
		})

		sidecarStep(t, tag, "switch to another world and back", func(t *testing.T) {
			for _, sw := range [][2]string{{a, b}, {b, a}} {
				from, to := sw[0], sw[1]
				if err := c.Switch(ctx, from, to, admin, StopOptions{}, StartOptions{}, true); err != nil {
					t.Fatalf("switch %s to %s: %v", from, to, err)
				}
				if s := c.Status(to); s.State != StateRunning {
					t.Fatalf("%s after the switch: state %s (%s), want running", to, s.State, s.Reason)
				}
				if s := c.Status(from); s.State != StateStopped {
					t.Fatalf("%s after the switch: state %s (%s), want stopped", from, s.State, s.Reason)
				}
				if active, _, err := st.ActiveWorld(ctx); err != nil || active != to {
					t.Fatalf("active world %q, err %v, want %q", active, err, to)
				}
				l.checkSharedSpec(t, to, sc)
				l.console(t, c, to, version)
			}
			if err := c.Stop(ctx, a, admin, StopOptions{}); err != nil {
				t.Fatalf("graceful stop: %v", err)
			}
			l.noSidecarSecret(t, c, a)
		})
	})
}

// TestRealGameSidecarRestartAndReplace restarts and then replaces the
// sidecar under a running game, measuring what happens, and checks the
// manual recovery SPEC 4.13 asks for: Foreman never claims a save it
// could not make, never follows a replacement on its own, and a forced
// stop and a start join the sidecar as it now is.
func TestRealGameSidecarRestartAndReplace(t *testing.T) {
	forEachSidecarTag(t, func(t *testing.T, e *gameEnv, tag, fixture, version string) {
		ctx := context.Background()
		l := newSidecarLab(t, e, tag, version)
		sc := l.startSidecar(t, false)
		c, stopMonitor := l.controller(t)
		id := e.newWorld(t, tag, version, "sc-restart", fixture, map[string]time.Duration{"main.zip": time.Hour})

		// unreachable checks that a game cut off from its sidecar is not
		// reported as saved or stopped by a graceful stop.
		unreachable := func(t *testing.T, c *Controller) {
			t.Helper()
			_, _, err := c.Console(ctx, id, admin, "/version", "/version")
			if err == nil {
				t.Fatal("the console answered a game whose sidecar was restarted or replaced")
			}
			t.Logf("EVIDENCE console: %v", err)
			time.Sleep(3 * time.Second) // several monitor looks
			s := c.Status(id)
			t.Logf("EVIDENCE status after the monitor looked: state %s, reason %q, LANUnpublished %q", s.State, s.Reason, s.LANUnpublished)
			before := e.saveTime(t, id, "main.zip")
			err = c.Stop(ctx, id, admin, StopOptions{})
			if err == nil {
				t.Fatal("a graceful stop succeeded without RCON")
			}
			t.Logf("EVIDENCE graceful stop: %v", err)
			if after := e.saveTime(t, id, "main.zip"); !after.Equal(before) {
				t.Fatalf("the save changed from %v to %v", before, after)
			}
			if cont := e.inspect(t, id); !cont.State.Running {
				t.Fatalf("the failed graceful stop left the game %s", cont.State.Status)
			}
			if s := c.Status(id); s.State == StateStopped {
				t.Fatal("the failed graceful stop reported the game stopped")
			}
			l.noSidecarSecret(t, c, id)
			if strings.Contains(err.Error(), l.secret) {
				t.Fatal("the stop's error holds the sidecar's secret")
			}
		}
		// recoverGame is the documented manual recovery: a forced stop, then a start.
		recoverGame := func(t *testing.T, c *Controller, sc docker.Container) {
			t.Helper()
			if err := c.ForceStop(ctx, id, admin); err != nil {
				t.Fatalf("force stop: %v", err)
			}
			if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
				t.Fatalf("start after the forced stop: %v", err)
			}
			l.checkSharedSpec(t, id, sc)
			l.console(t, c, id, version)
			want := []string{net.JoinHostPort(sc.Networks[l.gameNet].IPAddress, RCONPort)}
			if got := listeners(t, sc.ID); !slices.Equal(got, want) {
				t.Fatalf("listening on the RCON port: %v, want %v", got, want)
			}
		}

		sidecarStep(t, tag, "start", func(t *testing.T) {
			if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
				t.Fatalf("start: %v", err)
			}
			l.console(t, c, id, version)
		})

		sidecarStep(t, tag, "restarting the sidecar cuts the running game off", func(t *testing.T) {
			game := e.inspect(t, id)
			mustDocker(t, "restart", "-t", "1", sc.ID)
			now, err := e.docker.InspectContainer(ctx, l.name)
			if err != nil {
				t.Fatal(err)
			}
			if now.ID != sc.ID {
				t.Fatalf("docker restart changed the sidecar's ID from %s to %s", sc.ID, now.ID)
			}
			after := e.inspect(t, id)
			t.Logf("EVIDENCE after the sidecar restarted: game %s, restarts %d to %d, same container %v",
				after.State.Status, game.RestartCount, after.RestartCount, after.ID == game.ID)
			t.Logf("EVIDENCE sidecar game-network address %s before, %s after",
				sc.Networks[l.gameNet].IPAddress, now.Networks[l.gameNet].IPAddress)
			if after.State.Running {
				t.Logf("EVIDENCE interfaces the game sees: %s; the sidecar sees: %s", interfaces(t, after.ID), interfaces(t, sc.ID))
				t.Logf("EVIDENCE RCON listeners in the sidecar's new namespace: %v", listeners(t, sc.ID))
			}
			unreachable(t, c)
			sc = now
		})

		sidecarStep(t, tag, "a forced stop and a start recover after the restart", func(t *testing.T) {
			recoverGame(t, c, sc)
		})

		sidecarStep(t, tag, "a replaced sidecar is not followed, even after a Foreman restart", func(t *testing.T) {
			old := sc
			mustDocker(t, "rm", "-f", old.ID)
			sc = l.startSidecar(t, false)
			if sc.ID == old.ID {
				t.Fatal("the replacement has the old sidecar's ID")
			}
			game := e.inspect(t, id)
			t.Logf("EVIDENCE after the sidecar was replaced: game %s, network mode still the old ID %v",
				game.State.Status, game.NetworkMode == "container:"+old.ID)
			t.Logf("EVIDENCE sidecar game-network address %s before, %s after",
				old.Networks[l.gameNet].IPAddress, sc.Networks[l.gameNet].IPAddress)
			// A listener in the replacement's namespace at its game-network
			// address records every connection: the stale game's RCON
			// login, with that world's password, must never reach it.
			newIP := sc.Networks[l.gameNet].IPAddress
			if newIP == old.Networks[l.gameNet].IPAddress {
				t.Log("COVERAGE: same-address replacement tests cached-address redial and name-following")
			} else {
				t.Log("COVERAGE LIMIT: replacement address changed; this run tests name-following only, cached-address redial is covered by the unit regression")
			}
			stopTrap := startTrap(t, sc.ID, newIP)
			if p := hostDial(t, newIP); p != probeOpen {
				t.Fatalf("the trap at %s is %s, want open", newIP, p)
			}
			// The client may still connect to the cached address before its
			// check (rcon.NewChecked); what must never reach the replacement
			// is a byte of the login, which carries the world's password.
			_, before := trapLog(t, sc.ID)
			noLogin := func(t *testing.T, when string) {
				t.Helper()
				conns, bytes := trapLog(t, sc.ID)
				t.Logf("EVIDENCE %s: the replacement's address accepted %d connections in all", when, conns)
				if bytes != before {
					t.Fatalf("%s: %d bytes reached the replacement sidecar's address", when, bytes-before)
				}
			}
			unreachable(t, c)
			noLogin(t, "the running controller")

			stopMonitor()
			c2, _ := l.controller(t)
			unreachable(t, c2)
			noLogin(t, "after a Foreman restart")
			if got := e.inspect(t, id).NetworkMode; got != "container:"+old.ID {
				t.Fatalf("the game's network mode changed to %q", got)
			}
			stopTrap()
			if got := listeners(t, sc.ID); len(got) != 0 {
				t.Fatalf("something still listens on the RCON port in the replacement's namespace: %v", got)
			}
			recoverGame(t, c2, sc)
			if err := c2.Stop(ctx, id, admin, StopOptions{}); err != nil {
				t.Fatalf("graceful stop: %v", err)
			}
		})
	})
}

// startTrap listens on addr:27015 inside container id's network namespace,
// with the image's Perl, and counts each connection it accepts in a file
// there. The returned function ends it.
func startTrap(t *testing.T, id, addr string) func() {
	t.Helper()
	const script = `use IO::Socket::INET;
my $s = IO::Socket::INET->new(LocalAddr => $ARGV[0], LocalPort => $ARGV[1], Listen => 5, ReuseAddr => 1) or die "listen: $!";
open(my $p, ">", "/tmp/trap.pid") or die; print $p "$$"; close $p;
while (my $c = $s->accept) {
  my $r = ""; vec($r, fileno($c), 1) = 1;
  my $n = select($r, undef, undef, 5) ? (sysread($c, my $b, 4096) // 0) : 0;
  open(my $f, ">>", "/tmp/trap") or die; print $f "connection $n\n"; close $f; close $c;
}`
	mustDocker(t, "exec", "-d", id, "perl", "-e", script, addr, RCONPort)
	waitFor(t, 10*time.Second, "the trap to listen", func() bool {
		return slices.Contains(listeners(t, id), net.JoinHostPort(addr, RCONPort))
	})
	var once sync.Once
	stop := func() {
		once.Do(func() { mustDocker(t, "exec", id, "sh", "-c", `kill "$(cat /tmp/trap.pid)"`) })
		waitFor(t, 10*time.Second, "the trap to end", func() bool { return len(listeners(t, id)) == 0 })
	}
	return stop
}

// trapLog is how many connections the trap in container id accepted, and
// how many bytes they sent before closing (each read waits at most 5 s).
func trapLog(t *testing.T, id string) (conns, bytes int) {
	t.Helper()
	out := mustDocker(t, "exec", id, "sh", "-c", "touch /tmp/trap && cat /tmp/trap")
	for line := range strings.SplitSeq(out, "\n") {
		n, ok := strings.CutPrefix(line, "connection ")
		if !ok {
			continue
		}
		b, err := strconv.Atoi(n)
		if err != nil {
			t.Fatalf("the trap wrote %q", line)
		}
		conns, bytes = conns+1, bytes+b
	}
	return conns, bytes
}

// TestRealGameSidecarRefusals checks each sidecar a game cannot share is
// refused by the real Docker before any container is made or save touched
// (SPEC 4.13, 14.16), and records how Docker itself treats a game whose
// sidecar is stopped or gone.
func TestRealGameSidecarRefusals(t *testing.T) {
	g := gameTags[len(gameTags)-1]
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" {
		for _, x := range gameTags {
			if x.tag == only {
				g = x
			}
		}
	}
	ctx := context.Background()
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(g.version))
	l := newSidecarLab(t, e, g.tag, g.version)
	id := e.newWorld(t, g.tag, g.version, "sc-refuse", g.fixture, map[string]time.Duration{"main.zip": time.Hour})
	saved := e.saveTime(t, id, "main.zip")

	refused := func(t *testing.T, kind string) {
		t.Helper()
		c, _ := l.controller(t)
		err := c.Start(ctx, id, admin, StartOptions{})
		ne, ok := errors.AsType[*NetworkError](err)
		if !ok || ne.Kind != kind || !errors.Is(err, ErrNetwork) {
			t.Fatalf("start = %v, want a %s refusal", err, kind)
		}
		if strings.Contains(err.Error(), l.secret) {
			t.Fatal("the refusal shows the sidecar's environment")
		}
		if _, err := e.docker.InspectContainer(ctx, ContainerName(id)); !errors.Is(err, docker.ErrNotFound) {
			t.Fatalf("a game container exists after the refusal: %v", err)
		}
		if got := e.saveTime(t, id, "main.zip"); !got.Equal(saved) {
			t.Fatalf("the save changed from %v to %v", saved, got)
		}
		t.Logf("EVIDENCE %s: %v", kind, err)
	}

	t.Run("missing", func(t *testing.T) { refused(t, NetworkSidecarMissing) })

	var stopped string
	t.Run("stopped", func(t *testing.T) {
		// Started, then stopped, as a sidecar the operator stopped would be.
		stopped = l.newContainer(t, "--name", l.name, "--network", l.gameNet, "-e", "TS_AUTHKEY="+l.secret)
		mustDocker(t, "start", stopped)
		mustDocker(t, "stop", "-t", "0", stopped)
		refused(t, NetworkSidecarStopped)
	})

	t.Run("Docker create and start of a game whose sidecar is stopped or gone", func(t *testing.T) {
		if stopped == "" {
			t.Skip("the stopped sidecar was not made")
		}
		peer := func(t *testing.T, label string) {
			t.Helper()
			created, cerr := dockerCLI("create", "--label", "foreman-e3test=1", "--network", "container:"+stopped,
				"--entrypoint", "sleep", l.image, "infinity")
			if cerr != nil {
				t.Logf("EVIDENCE %s: docker create refused: %s", label, created)
				return
			}
			t.Cleanup(func() { _, _ = dockerCLI("rm", "-f", created) })
			t.Logf("EVIDENCE %s: docker create accepted the network mode", label)
			out, err := dockerCLI("start", created)
			if err == nil {
				t.Fatalf("%s: docker started a game in the network of a container that is not running", label)
			}
			t.Logf("EVIDENCE %s: docker start refused: %s", label, out)
		}
		peer(t, "stopped sidecar")
		mustDocker(t, "rm", "-f", stopped)
		peer(t, "removed sidecar")
	})

	t.Run("host network", func(t *testing.T) {
		mustDocker(t, "start", l.newContainer(t, "--name", l.name, "--network", "host"))
		refused(t, NetworkSidecarHost)
		mustDocker(t, "rm", "-f", l.name)
	})

	t.Run("another container's network", func(t *testing.T) {
		owner := l.newContainer(t, "--network", l.gameNet)
		mustDocker(t, "start", owner)
		mustDocker(t, "start", l.newContainer(t, "--name", l.name, "--network", "container:"+owner))
		refused(t, NetworkSidecarNested)
		mustDocker(t, "rm", "-f", l.name)
	})

	t.Run("off the game network", func(t *testing.T) {
		mustDocker(t, "start", l.newContainer(t, "--name", l.name, "--network", l.otherNet))
		refused(t, NetworkSidecarOffNetwork)
		mustDocker(t, "rm", "-f", l.name)
	})
}
