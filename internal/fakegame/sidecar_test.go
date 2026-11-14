package fakegame_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
)

const (
	gameNet = "foreman-games"
	vpnName = "vpn"
)

// sidecarRig is a fake with the sidecar seeded and the game network named.
func sidecarRig(t *testing.T, port string) *rig {
	t.Helper()
	return newRig(t, fakegame.Config{SelfNetwork: gameNet, Sidecar: vpnName, SidecarPort: port})
}

// rawInspect reads an inspect reply as JSON, for the fields the typed client
// does not have to carry (the environment, network mode and ports).
func (r *rig) rawInspect(ref string) map[string]any {
	r.t.Helper()
	cl := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", r.game.Socket())
		},
	}}
	resp, err := cl.Get("http://docker/v1.43/containers/" + ref + "/json")
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		r.t.Fatal(err)
	}
	return doc
}

func at(doc map[string]any, keys ...string) any {
	var cur any = doc
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func (r *rig) sidecarID() string {
	r.t.Helper()
	s := r.game.State().Sidecar
	if s == nil {
		r.t.Fatal("State has no sidecar")
	}
	return s.ID
}

// startShared creates and starts a game that joins the sidecar's namespace
// and binds RCON at bind, and returns its ID.
func (r *rig) startShared(name, holder, bind, policy string) string {
	r.t.Helper()
	spec := r.spec(image21, policy)
	spec.HostConfig.NetworkMode = "container:" + holder
	if bind != "" {
		spec.Env = []string{gameenv.RCONBind + "=" + bind}
	}
	ctx := context.Background()
	id, err := r.docker.CreateContainer(ctx, name, spec)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		r.t.Fatal(err)
	}
	return id
}

func rconAnswers(addr string) bool {
	c := rcon.New(addr, secret.New(password))
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Exec(ctx, "/players online")
	return err == nil
}

func TestSidecarIsSeededOnTheGameNetwork(t *testing.T) {
	r := sidecarRig(t, "34197")
	id := r.sidecarID()
	st := r.game.State()
	sc := st.Sidecar
	if sc.Name != vpnName || sc.Status != "running" || sc.Port != "34197" || !strings.HasPrefix(sc.IP, "127.") || sc.IP != r.game.SidecarAddress() {
		t.Errorf("sidecar state %+v", sc)
	}
	if len(st.Containers) != 0 {
		t.Errorf("State lists %d containers, want none: the sidecar is not a game", len(st.Containers))
	}

	c, err := r.docker.InspectContainer(context.Background(), vpnName)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := c.Networks[gameNet]
	if !ok || len(c.Networks) != 1 || n.IPAddress != sc.IP || n.IPPrefixLen != 24 || c.ID != id || !c.State.Running {
		t.Errorf("sidecar inspect %+v", c)
	}

	// Foreman's network-member check finds it on the game network.
	list, err := r.docker.ListContainersOnNetwork(context.Background(), gameNet)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list {
		found = found || s.ID == id
	}
	if !found {
		t.Errorf("the game network lists %+v, not the sidecar", list)
	}

	doc := r.rawInspect(vpnName)
	if got := at(doc, "HostConfig", "NetworkMode"); got != gameNet {
		t.Errorf("NetworkMode = %v", got)
	}
	env, _ := at(doc, "Config", "Env").([]any)
	if len(env) != 2 || env[1] != "TS_AUTHKEY="+fakegame.SidecarSecret {
		t.Errorf("sidecar Env = %v", env)
	}
	bindings, _ := at(doc, "NetworkSettings", "Ports", fakegame.SidecarPublishedPort).([]any)
	if len(bindings) == 0 {
		t.Fatalf("Ports = %v, want 34197/udp published", at(doc, "NetworkSettings", "Ports"))
	}
	for _, b := range bindings {
		if hp := at(map[string]any{"b": b}, "b", "HostPort"); hp != "34197" {
			t.Errorf("binding %v", b)
		}
	}
}

func TestSidecarPublicationFollowsPortAndState(t *testing.T) {
	r := sidecarRig(t, "")
	ports := func() map[string]any {
		p, _ := at(r.rawInspect(vpnName), "NetworkSettings", "Ports").(map[string]any)
		if p == nil {
			t.Fatal("inspect has no NetworkSettings.Ports")
		}
		return p
	}
	if p := ports(); len(p) != 0 {
		t.Errorf("an unpublished sidecar reports ports %v", p)
	}

	// A different host port is reported as it is.
	if err := r.game.SetSidecarPort("40000"); err != nil {
		t.Fatal(err)
	}
	b, _ := ports()[fakegame.SidecarPublishedPort].([]any)
	if len(b) == 0 || at(map[string]any{"b": b[0]}, "b", "HostPort") != "40000" {
		t.Errorf("ports = %v", ports())
	}

	// A stopped container publishes nothing and has no address.
	if err := r.docker.StopContainer(context.Background(), vpnName, 1); err != nil {
		t.Fatal(err)
	}
	if p := ports(); len(p) != 0 {
		t.Errorf("a stopped sidecar reports ports %v", p)
	}
	c, err := r.docker.InspectContainer(context.Background(), vpnName)
	if err != nil {
		t.Fatal(err)
	}
	if c.State.Running || c.Networks[gameNet].IPAddress != "" {
		t.Errorf("stopped sidecar %+v", c)
	}

	if err := r.game.SetSidecarPort("x"); err == nil {
		t.Error("a bad port was accepted")
	}
	if err := r.game.SetSidecarPort(""); err != nil {
		t.Error(err)
	}
}

func TestSharedGameBindsRCONOnTheSidecarAddress(t *testing.T) {
	r := sidecarRig(t, "34197")
	sidecar := r.sidecarID()
	ip := r.game.SidecarAddress()
	bind := net.JoinHostPort(ip, "27015")
	id := r.startShared("foreman-w1", sidecar, bind, "unless-stopped")

	eventually(t, "RCON on the sidecar address", func() bool { return rconAnswers(bind) })

	c, err := r.docker.InspectContainer(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Networks) != 0 || !c.State.Running {
		t.Errorf("a shared-namespace game has networks %v", c.Networks)
	}
	doc := r.rawInspect(id)
	if got := at(doc, "HostConfig", "NetworkMode"); got != "container:"+sidecar {
		t.Errorf("NetworkMode = %v", got)
	}
	if env, _ := at(doc, "Config", "Env").([]any); len(env) != 1 || env[0] != gameenv.RCONBind+"="+bind {
		t.Errorf("Env = %v", env)
	}
	if nets, _ := at(doc, "NetworkSettings", "Networks").(map[string]any); nets == nil || len(nets) != 0 {
		t.Errorf("Networks = %v, want an empty object", at(doc, "NetworkSettings", "Networks"))
	}

	// The game is not on the game network's list, and State sees its address.
	list, err := r.docker.ListContainersOnNetwork(context.Background(), gameNet)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == id {
			t.Errorf("the game network lists the shared game: %+v", s)
		}
	}
	if cs := r.game.State().Containers; len(cs) != 1 || cs[0].IP != ip {
		t.Errorf("State containers = %+v, want one at %s", cs, ip)
	}

	// RCON is on the bind address only: not on the game's usual loopback
	// address of another container, and not on this namespace's loopback.
	for _, other := range []string{"127.0.0.1:27015", net.JoinHostPort(otherLoopback(ip), "27015")} {
		if rconAnswers(other) {
			t.Errorf("RCON answers on %s", other)
		}
	}
	if got := r.exec2(bind, "/players online"); !strings.Contains(got, "Online players") {
		t.Errorf("/players online = %q", got)
	}
}

// exec2 runs a command on the RCON at addr.
func (r *rig) exec2(addr, command string) string {
	r.t.Helper()
	c := rcon.New(addr, secret.New(password))
	r.t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := c.Exec(ctx, command)
	if err != nil {
		r.t.Fatalf("%s: %v", command, err)
	}
	return out
}

// otherLoopback is an address in the fake's range that is not ip.
func otherLoopback(ip string) string {
	parts := strings.Split(ip, ".")
	if parts[3] == "250" {
		parts[3] = "249"
	} else {
		parts[3] = "250"
	}
	return strings.Join(parts, ".")
}

func TestSharedGameWithTheWrongBindExits(t *testing.T) {
	for name, bind := range map[string]string{
		"another address": "", // filled in below
		"no endpoint":     "-",
		"garbage":         "not-an-endpoint",
		"no port":         "127.0.0.9",
		"bad port":        "127.0.0.9:0",
	} {
		t.Run(name, func(t *testing.T) {
			r := sidecarRig(t, "")
			switch bind {
			case "":
				bind = net.JoinHostPort(otherLoopback(r.game.SidecarAddress()), "27015")
			case "-":
				bind = ""
			}
			id := r.startShared("foreman-w1", r.sidecarID(), bind, "no")
			eventually(t, "the game to exit", func() bool { return r.status(id) == "exited" })
			if c := r.inspect(id); c.State.ExitCode != 1 {
				t.Errorf("exit code %d, want 1", c.State.ExitCode)
			}
			if log := r.log(id); !strings.Contains(log, "cannot bind RCON") {
				t.Errorf("the log does not say why:\n%s", log)
			}
		})
	}
}

func TestSharedGameWithAnAddressOfItsOwnKeepsOrdinaryRCON(t *testing.T) {
	r := sidecarRig(t, "34197")
	id := r.start("foreman-w1", image21, "unless-stopped")
	c := r.inspect(id)
	ip := c.Networks["bridge"].IPAddress
	if !strings.HasPrefix(ip, "127.") || ip == r.game.SidecarAddress() {
		t.Fatalf("ordinary game address %q beside sidecar %q", ip, r.game.SidecarAddress())
	}
	eventually(t, "ordinary RCON", func() bool { return rconAnswers(ip + ":27015") })
	if doc := r.rawInspect(id); at(doc, "HostConfig", "NetworkMode") != "" {
		t.Errorf("an ordinary game reports NetworkMode %v", at(doc, "HostConfig", "NetworkMode"))
	}
}

func TestSidecarRestartCutsRCONUntilTheGameStartsAgain(t *testing.T) {
	r := sidecarRig(t, "34197")
	bind := net.JoinHostPort(r.game.SidecarAddress(), "27015")
	id := r.startShared("foreman-w1", r.sidecarID(), bind, "unless-stopped")
	eventually(t, "RCON", func() bool { return rconAnswers(bind) })

	if err := r.game.RestartSidecar(); err != nil {
		t.Fatal(err)
	}
	if rconAnswers(bind) {
		t.Error("RCON still answers after the sidecar restarted")
	}
	if r.status(id) != "running" {
		t.Errorf("the game is %s, want running: it keeps running in the old namespace", r.status(id))
	}
	if sc := r.game.State().Sidecar; sc.Status != "running" || sc.IP != r.game.SidecarAddress() {
		t.Errorf("sidecar after a restart %+v", sc)
	}

	// The operator restarts the game in Foreman: a stop and a new start.
	ctx := context.Background()
	if err := r.docker.StopContainer(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	eventually(t, "RCON after the game starts again", func() bool { return rconAnswers(bind) })
}

func TestSidecarHooks(t *testing.T) {
	r := sidecarRig(t, "")
	h := r.game.HookHandler()
	bind := net.JoinHostPort(r.game.SidecarAddress(), "27015")
	r.startShared("foreman-w1", r.sidecarID(), bind, "unless-stopped")
	eventually(t, "RCON", func() bool { return rconAnswers(bind) })

	if rec := hook(t, h, "POST", "/sidecar-port", `{"port":"34197"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("sidecar-port = %d %s", rec.Code, rec.Body)
	}
	var st fakegame.State
	if err := json.Unmarshal(hook(t, h, "GET", "/state", "").Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Sidecar == nil || st.Sidecar.Port != "34197" {
		t.Errorf("state sidecar = %+v", st.Sidecar)
	}
	if rec := hook(t, h, "POST", "/sidecar-port", `{"port":"nope"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad port = %d", rec.Code)
	}
	if rec := hook(t, h, "POST", "/sidecar-restart", `{}`); rec.Code != http.StatusNoContent {
		t.Errorf("sidecar-restart = %d %s", rec.Code, rec.Body)
	}
	if rconAnswers(bind) {
		t.Error("RCON answers after the hook restarted the sidecar")
	}
}

func TestSidecarHooksWithoutASidecar(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	h := r.game.HookHandler()
	for _, path := range []string{"/sidecar-port", "/sidecar-restart"} {
		if rec := hook(t, h, "POST", path, `{"port":"34197"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s without a sidecar = %d", path, rec.Code)
		}
	}
	var st fakegame.State
	if err := json.Unmarshal(hook(t, h, "GET", "/state", "").Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Sidecar != nil {
		t.Errorf("state sidecar = %+v, want none", st.Sidecar)
	}
}

func TestSidecarConfigIsChecked(t *testing.T) {
	for name, cfg := range map[string]fakegame.Config{
		"no game network":         {Sidecar: vpnName},
		"a port without sidecar":  {SelfNetwork: gameNet, SidecarPort: "34197"},
		"a bad name":              {SelfNetwork: gameNet, Sidecar: "-vpn"},
		"a bad network":           {SelfNetwork: "a b", Sidecar: vpnName},
		"a port that is not one":  {SelfNetwork: gameNet, Sidecar: vpnName, SidecarPort: "99999"},
		"a port with a plus sign": {SelfNetwork: gameNet, Sidecar: vpnName, SidecarPort: "+80"},
	} {
		t.Run(name, func(t *testing.T) {
			g, err := fakegame.Start(cfg)
			if err == nil {
				g.Close()
				t.Fatal("Start accepted it")
			}
		})
	}
}
