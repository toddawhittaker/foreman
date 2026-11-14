package fakegame

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
)

// containerCall matches /containers/{id} and /containers/{id}/{action}, with
// or without the API version prefix.
var containerCall = regexp.MustCompile(`^(?:/v\d+\.\d+)?/containers/([^/]+)(?:/([a-z]+))?$`)

// serveDocker passes every call to the dockertest daemon and then makes the
// game follow what the call did: a start boots the game, a stop writes the
// shutdown log first, a kill or removal ends it, and an inspect reports the
// container's loopback address. Calls that start nothing and stream (logs,
// image pulls) go straight through.
func (g *Game) serveDocker(w http.ResponseWriter, r *http.Request) {
	m := containerCall.FindStringSubmatch(r.URL.Path)
	if m == nil || m[1] == "json" || m[1] == "create" || m[2] == "logs" {
		g.daemon.ServeHTTP(w, r)
		return
	}
	c, found := g.daemon.Container(m[1])
	action := m[2]
	if r.Method == http.MethodDelete {
		action = "remove"
	}
	if found {
		g.beforeCall(r, action, c.ID, c.Status == "running")
	}

	// The reply is held until the game has caught up, so a caller that
	// inspects right after a start sees the new state.
	rec := httptest.NewRecorder()
	g.daemon.ServeHTTP(rec, r)
	body := rec.Body.Bytes()
	if found {
		body = g.afterCall(action, c.ID, c.Status, rec.Code, body)
	}
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.Header().Del("Content-Length")
	w.WriteHeader(rec.Code)
	_, _ = w.Write(body)
}

// beforeCall runs ahead of the call itself.
func (g *Game) beforeCall(r *http.Request, action, id string, running bool) {
	switch {
	case r.Method == http.MethodPost && action == "stop":
		// A person is stopping it: no restart, whatever the state.
		g.setManual(id, true)
		if in := g.instance(id); in != nil {
			in.shutdownLog()
		}
	case r.Method == http.MethodPost && action == "kill" && running:
		g.setManual(id, true)
	}
}

// afterCall runs once the daemon has answered, and returns the body to send.
// before is the container's status when the call came in.
func (g *Game) afterCall(action, id, before string, code int, body []byte) []byte {
	switch action {
	case "start":
		if code == http.StatusNoContent {
			g.setManual(id, false)
			g.boot(id)
		}
	case "stop":
		if code < 300 {
			g.endInstance(id)
		} else if code >= 400 {
			// Docker refused the stop, so its restart policy still applies.
			g.refused(id, before)
		}
	case "kill":
		if code == http.StatusNoContent {
			g.endInstance(id)
		} else if code >= 400 {
			g.refused(id, before)
		}
	case "remove":
		if code == http.StatusNoContent {
			g.endInstance(id)
			g.forget(id)
		}
	case "json":
		if code == http.StatusOK {
			return g.rewriteInspect(body)
		}
	}
	return body
}

// refused undoes the no-restart mark of a stop or kill Docker refused. A game
// that died while the call was in flight skipped its restart under that mark,
// so it is restarted now, as Docker's restart policy would have done.
func (g *Game) refused(id, before string) {
	g.setManual(id, false)
	if c, ok := g.daemon.Container(id); ok && before != "exited" && c.Status == "exited" {
		g.scheduleRestart(id)
	}
}

func (g *Game) setManual(id string, on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if on {
		g.manual[id] = true
		return
	}
	delete(g.manual, id)
	delete(g.backoff, id)
}

// forget releases what the fake kept for a removed container.
func (g *Game) forget(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.addrs, id)
	delete(g.manual, id)
	delete(g.backoff, id)
}

func (g *Game) instance(id string) *instance {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.insts[id]
}

func (g *Game) endInstance(id string) {
	if in := g.instance(id); in != nil {
		g.drop(in)
	}
}

// shutdownLog writes what the game prints on SIGTERM, if it is far enough
// along to have a save to write.
func (in *instance) shutdownLog() {
	in.mu.Lock()
	ready := in.inGame
	in.mu.Unlock()
	if ready {
		in.emit(in.v.shutdown, runSave)
	}
}

// rewriteInspect replaces the address Docker's fake reports with the
// loopback address the container's RCON listens on. A container the fake
// never ran a game in, such as one a test seeded, keeps its own address.
func (g *Game) rewriteInspect(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc map[string]any
	if dec.Decode(&doc) != nil {
		return body
	}
	id, _ := doc["Id"].(string)
	g.mu.Lock()
	_, ran := g.addrs[id]
	g.mu.Unlock()
	if !ran {
		return body
	}
	settings, _ := doc["NetworkSettings"].(map[string]any)
	nets, _ := settings["Networks"].(map[string]any)
	for _, n := range nets {
		// An empty address means the container is not running.
		if nw, ok := n.(map[string]any); ok && nw["IPAddress"] != "" {
			g.mu.Lock()
			nw["IPAddress"] = g.addrLocked(id)
			g.mu.Unlock()
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return append(out, '\n')
}
