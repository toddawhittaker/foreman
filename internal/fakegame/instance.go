package fakegame

import (
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/rcon/rcontest"
	"github.com/toddawhittaker/foreman/internal/world"
)

// instance is one run of the game process inside a container. Docker's
// restart policy makes a new instance for the same container.
type instance struct {
	g        *Game
	id, name string
	ip       string
	save     string // file name of the save the container loads
	password string
	v        *version

	mu      sync.Mutex
	dead    bool
	inGame  bool
	players []string
	base    float64   // elapsed seconds on the last line written...
	baseAt  time.Time // ...and when it was written
	rcon    *rconServer
	proxy   net.Listener
	conns   map[net.Conn]struct{}
	wg      sync.WaitGroup
}

// rconServer is an rcontest server and the cleanups it registered.
type rconServer struct {
	tb  *tb
	srv *rcontest.Server
}

// boot runs the game for a container that Docker has just started.
func (g *Game) boot(id string) {
	c, ok := g.daemon.Container(id)
	if !ok || c.Status != "running" {
		return
	}
	in := &instance{
		g: g, id: id, name: c.Name, v: versionFor(c.Spec.Image),
		save: saveName(c.Spec), password: rconPassword(c),
		baseAt: time.Now(), conns: map[net.Conn]struct{}{},
	}
	g.mu.Lock()
	if g.closed || g.insts[id] != nil {
		g.mu.Unlock()
		return
	}
	in.ip = g.addrLocked(id)
	g.insts[id] = in
	loops := g.behave[c.Name].crashLoop
	if !loops {
		delete(g.backoff, id)
	}
	g.mu.Unlock()

	if loops {
		in.emit(in.v.failed, failedSave)
		g.died(in, in.v.failExit, false)
		return
	}
	in.emit(in.v.head, runSave)
	if g.cfg.LoadDelay <= 0 {
		in.finishLoad()
		return
	}
	g.after(g.cfg.LoadDelay, in.finishLoad)
}

// finishLoad is the rest of a start: the game opens RCON and reaches InGame.
func (in *instance) finishLoad() {
	if err := in.startRCON(); err != nil {
		in.g.report(err.Error())
		return
	}
	in.emit(in.v.tail, runSave)
	in.mu.Lock()
	in.inGame = true
	in.mu.Unlock()
}

// died ends a game that exited by itself and, under a restart policy that
// asks for it, schedules the restart.
func (g *Game) died(in *instance, code int, oom bool) {
	g.drop(in)
	g.daemon.Exit(in.id, code, oom)
	g.scheduleRestart(in.id)
}

// drop forgets an instance and stops everything it served.
func (g *Game) drop(in *instance) {
	g.mu.Lock()
	if g.insts[in.id] == in {
		delete(g.insts, in.id)
	}
	g.mu.Unlock()
	in.shutdown()
}

// scheduleRestart follows unless-stopped (and always): a container that
// exited without a stop or kill through the API starts again after a delay
// that doubles each time.
func (g *Game) scheduleRestart(id string) {
	c, ok := g.daemon.Container(id)
	if !ok {
		return
	}
	if p := c.Spec.HostConfig.RestartPolicy.Name; p != "unless-stopped" && p != "always" {
		return
	}
	g.mu.Lock()
	if g.closed || g.manual[id] {
		g.mu.Unlock()
		return
	}
	delay := g.backoff[id]
	if delay == 0 {
		delay = firstBackoff
	}
	g.backoff[id] = min(2*delay, maxBackoff)
	g.mu.Unlock()
	g.daemon.WillRestart(id)

	g.after(delay, func() {
		g.mu.Lock()
		c, ok := g.daemon.Container(id)
		again := ok && c.Status == "restarting" && !g.manual[id] && !g.closed
		if again {
			g.daemon.Restart(id)
		}
		g.mu.Unlock()
		if again {
			g.boot(id)
		}
	})
}

// shutdown ends the instance: it stops RCON and waits for its goroutines.
// It does not touch the container's state in the daemon.
func (in *instance) shutdown() {
	in.mu.Lock()
	in.dead = true
	proxy, rcon := in.proxy, in.rcon
	in.proxy, in.rcon = nil, nil
	conns := slices.Collect(maps.Keys(in.conns))
	in.mu.Unlock()

	if proxy != nil {
		_ = proxy.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if rcon != nil {
		rcon.tb.runCleanups()
	}
	in.wg.Wait()
}

// emit writes transcript lines to the container's log, with elapsed
// seconds that never run backwards. saveFrom is the save name the
// transcript used, replaced by the one this container loads.
func (in *instance) emit(lines []line, saveFrom string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.dead || len(lines) == 0 {
		return
	}
	offset := max(0, in.elapsedLocked()-lines[0].elapsed)
	for _, l := range lines {
		in.writeLocked(l.format(l.elapsed+offset), saveFrom)
		in.base, in.baseAt = l.elapsed+offset, time.Now()
	}
}

// emitText writes one line that the game prints without the elapsed field,
// such as a chat or join line.
func (in *instance) emitText(text string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if !in.dead {
		in.writeLocked(text, "")
	}
}

func (in *instance) elapsedLocked() float64 { return in.base + time.Since(in.baseAt).Seconds() }

func (in *instance) writeLocked(text, saveFrom string) {
	if saveFrom != "" {
		text = strings.ReplaceAll(text, "/factorio/saves/"+saveFrom, "/factorio/saves/"+in.save)
	}
	in.g.daemon.EmitLog(in.id, docker.Stdout, text)
}

// The format of a player's reply and of join and leave lines has not been
// seen on a real server yet (internal/game/testdata/README.md, section 5).
func (in *instance) reply(command string) string {
	in.g.mu.Lock()
	in.g.commands[in.name] = append(in.g.commands[in.name], command)
	hangs := in.g.behave[in.name].saveHangs
	in.g.mu.Unlock()

	switch strings.TrimSpace(command) {
	case "/players online":
		in.mu.Lock()
		defer in.mu.Unlock()
		out := fmt.Sprintf("Online players (%d):\n", len(in.players))
		for _, p := range in.players {
			out += "  " + p + " (online)\n"
		}
		return out
	case "/server-save":
		// The reply is immediate; the save finishes later, shown in the log.
		in.emit(in.v.save[:1], runSave)
		if !hangs {
			in.g.after(saveTime, func() { in.emit(in.v.save[1:], runSave) })
		}
		return "Saving the map\n"
	}
	return ""
}

// startRCON opens the RCON port on the container's address. A forwarder
// sits in front of an rcontest server, because rcontest only listens on
// 127.0.0.1.
func (in *instance) startRCON() error {
	ln, err := net.Listen("tcp", net.JoinHostPort(in.ip, rconPort))
	if err != nil {
		return fmt.Errorf("RCON for %s cannot listen on %s: %w", in.name, in.ip, err)
	}
	in.g.mu.Lock()
	refuse := in.g.behave[in.name].rconRefuse
	in.g.mu.Unlock()
	rcon := in.newRCON(refuse)

	in.mu.Lock()
	if in.dead {
		in.mu.Unlock()
		_ = ln.Close()
		rcon.tb.runCleanups()
		return nil
	}
	in.proxy, in.rcon = ln, rcon
	in.wg.Add(1)
	in.mu.Unlock()
	go in.forward(ln)
	return nil
}

func (in *instance) newRCON(refuse bool) *rconServer {
	b := &tb{report: in.g.report}
	return &rconServer{tb: b, srv: rcontest.Start(b, rcontest.Config{
		Password: in.password, RefuseLogin: refuse, Reply: in.reply,
	})}
}

// setRefuse swaps the RCON server for one that accepts or refuses logins.
// Open connections are dropped, as they would be if the game restarted its
// RCON.
func (in *instance) setRefuse(refuse bool) {
	next := in.newRCON(refuse)
	in.mu.Lock()
	old := in.rcon
	if in.dead || old == nil {
		in.mu.Unlock()
		next.tb.runCleanups()
		return
	}
	in.rcon = next
	in.mu.Unlock()
	old.tb.runCleanups()
}

// forward accepts RCON connections and passes each to the current server.
func (in *instance) forward(ln net.Listener) {
	defer in.wg.Done()
	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		in.mu.Lock()
		if in.dead || in.rcon == nil {
			in.mu.Unlock()
			_ = client.Close()
			return
		}
		target := in.rcon.srv.Addr()
		in.conns[client] = struct{}{}
		in.wg.Add(1)
		in.mu.Unlock()
		go in.pipe(client, target)
	}
}

var connAddr = regexp.MustCompile(`\(\{[^}]*\}\)`)

func (in *instance) pipe(client net.Conn, target string) {
	defer in.wg.Done()
	defer func() {
		in.mu.Lock()
		delete(in.conns, client)
		in.mu.Unlock()
	}()
	server, err := net.Dial("tcp", target)
	if err != nil {
		_ = client.Close()
		return
	}
	conn := in.v.conn
	conn.text = connAddr.ReplaceAllString(conn.text, "({"+client.RemoteAddr().String()+"})")
	in.emit([]line{conn}, "")

	closeBoth := func() { _ = client.Close(); _ = server.Close() }
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(server, client)
		closeBoth()
		close(done)
	}()
	_, _ = io.Copy(client, server)
	closeBoth()
	<-done
}

// saveName finds the save a container is told to load: the last argument
// that ends in .zip.
func saveName(spec docker.ContainerSpec) string {
	for _, arg := range slices.Backward(spec.Cmd) {
		if strings.HasSuffix(arg, ".zip") {
			return path.Base(arg)
		}
	}
	return "save.zip"
}

// rconPassword reads the password the container's command would read from
// the world's file. A missing file gives the empty password.
func rconPassword(c dockertest.Container) string {
	for _, m := range c.Mounts {
		if m.Destination == "/factorio" {
			b, err := os.ReadFile(filepath.Join(m.Source, world.RCONPasswordFile))
			if err != nil {
				return ""
			}
			return strings.TrimRight(string(b), "\n")
		}
	}
	return ""
}
