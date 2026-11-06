package fakegame

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// The hooks act on a container by name, because Foreman makes a new
// container for every start. Those that set a behaviour (crash loop, RCON
// refusal, hanging save) keep it across containers of that name.

// running returns the live game in the named container.
func (g *Game) running(name string) (*instance, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, in := range g.insts {
		if in.name == name {
			return in, nil
		}
	}
	return nil, fmt.Errorf("no game is running in a container named %q", name)
}

// Crash makes the game in the named container exit by itself, as a crash or
// the out-of-memory killer would. Docker's restart policy then applies.
func (g *Game) Crash(name string, exitCode int, oom bool) error {
	in, err := g.running(name)
	if err != nil {
		return err
	}
	g.died(in, exitCode, oom)
	return nil
}

// SetCrashLoop makes every start of the named container fail to load its
// save (the failure in the 1d transcripts), so Docker's restart policy
// restarts it again and again. Turning it on also crashes a running game.
// Turning it off lets the next restart load normally.
func (g *Game) SetCrashLoop(name string, on bool) {
	g.mu.Lock()
	b := g.behave[name]
	b.crashLoop = on
	g.behave[name] = b
	g.mu.Unlock()
	if !on {
		return
	}
	if in, err := g.running(name); err == nil {
		g.died(in, 1, false)
	}
}

// Join adds a player to the game, with the line the game logs. The log
// format has not been seen on a real server yet.
func (g *Game) Join(name, player string) error {
	return g.movePlayer(name, player, true)
}

// Leave removes a player.
func (g *Game) Leave(name, player string) error {
	return g.movePlayer(name, player, false)
}

func (g *Game) movePlayer(name, player string, join bool) error {
	if player == "" {
		return errors.New("a player needs a name")
	}
	in, err := g.running(name)
	if err != nil {
		return err
	}
	in.mu.Lock()
	if !in.inGame {
		in.mu.Unlock()
		return fmt.Errorf("the game in %q has not reached InGame", name)
	}
	present := slices.Contains(in.players, player)
	switch {
	case join && present:
		in.mu.Unlock()
		return fmt.Errorf("%s is already on", player)
	case !join && !present:
		in.mu.Unlock()
		return fmt.Errorf("%s is not on", player)
	case join:
		in.players = append(in.players, player)
	default:
		in.players = slices.DeleteFunc(in.players, func(p string) bool { return p == player })
	}
	in.mu.Unlock()

	what := "left"
	tag := "LEAVE"
	if join {
		what, tag = "joined", "JOIN"
	}
	in.emitText(fmt.Sprintf("%s [%s] %s %s the game", time.Now().UTC().Format("2006-01-02 15:04:05"), tag, player, what))
	return nil
}

// SetRCONRefuse makes RCON refuse every login, now and in later containers
// of that name, or accept them again.
func (g *Game) SetRCONRefuse(name string, on bool) {
	g.mu.Lock()
	b := g.behave[name]
	b.rconRefuse = on
	g.behave[name] = b
	g.mu.Unlock()
	if in, err := g.running(name); err == nil {
		in.setRefuse(on)
	}
}

// SetSaveHangs makes /server-save answer as usual but never log that the
// save finished.
func (g *Game) SetSaveHangs(name string, on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	b := g.behave[name]
	b.saveHangs = on
	g.behave[name] = b
}

// PreviewMode is how the fake game's preview containers end.
type PreviewMode string

// The modes of SetPreviewMode.
const (
	PreviewOK    PreviewMode = "ok"   // draw the pictures and exit 0
	PreviewFails PreviewMode = "fail" // exit 1 without drawing
	PreviewHangs PreviewMode = "hang" // keep running until killed
)

// SetPreviewMode sets how later preview containers end (SPEC 5.6). The
// default is PreviewOK.
func (g *Game) SetPreviewMode(mode PreviewMode) error {
	switch mode {
	case PreviewOK, PreviewFails, PreviewHangs:
	default:
		return fmt.Errorf("a preview mode is %q, %q or %q", PreviewOK, PreviewFails, PreviewHangs)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.preview = mode
	return nil
}

// SetImage makes an image present or missing.
func (g *Game) SetImage(ref string, present bool) {
	if present {
		g.daemon.AddImage(ref)
	} else {
		g.daemon.RemoveImage(ref)
	}
}

// ContainerState is what State reports about one container.
type ContainerState struct {
	Name         string   `json:"name"`
	ID           string   `json:"id"`
	Image        string   `json:"image"`
	Status       string   `json:"status"` // created, running or exited
	ExitCode     int      `json:"exit_code"`
	RestartCount int      `json:"restart_count"`
	IP           string   `json:"ip"` // where RCON listens, while running
	Players      []string `json:"players"`
	Commands     []string `json:"commands"` // RCON commands received, over all containers of this name
	CrashLoop    bool     `json:"crash_loop"`
	RCONRefuse   bool     `json:"rcon_refuse"`
	SaveHangs    bool     `json:"save_hangs"`
}

// State is the whole fake, for tests to read.
type State struct {
	Containers []ContainerState `json:"containers"`
	Calls      []string         `json:"calls"` // every Docker call received, as "METHOD /path"
}

// State returns a snapshot.
func (g *Game) State() State {
	out := State{Containers: []ContainerState{}, Calls: []string{}}
	for _, c := range g.daemon.Containers() {
		if c.ID == g.selfID {
			continue
		}
		cs := ContainerState{
			Name: c.Name, ID: c.ID, Image: c.Spec.Image, Status: c.Status,
			ExitCode: c.ExitCode, RestartCount: c.RestartCount,
			Players: []string{}, Commands: []string{},
		}
		g.mu.Lock()
		b := g.behave[c.Name]
		cs.CrashLoop, cs.RCONRefuse, cs.SaveHangs = b.crashLoop, b.rconRefuse, b.saveHangs
		cs.Commands = append(cs.Commands, g.commands[c.Name]...)
		in := g.insts[c.ID]
		if in != nil {
			cs.IP = in.ip
		}
		g.mu.Unlock()
		if in != nil {
			in.mu.Lock()
			cs.Players = append(cs.Players, in.players...)
			in.mu.Unlock()
		}
		out.Containers = append(out.Containers, cs)
	}
	for _, c := range g.daemon.Calls() {
		out.Calls = append(out.Calls, c.Method+" "+c.Path)
	}
	return out
}

// hookRequest is the JSON body of every hook call. Each hook reads the
// fields it needs.
type hookRequest struct {
	Name   string `json:"name"`   // container name
	Player string `json:"player"` // join, leave
	Ref    string `json:"ref"`    // image: the full reference
	Code   int    `json:"code"`   // crash: exit code, 1 when left out
	OOM    bool   `json:"oom"`    // crash: the out-of-memory killer did it
	On     bool   `json:"on"`     // crash-loop, rcon-refuse, save-hangs, image
	Mode   string `json:"mode"`   // preview-mode
}

// HookHandler serves the test hooks over HTTP. Every hook is a POST with a
// JSON body (hookRequest) and answers 204, or 400 with {"error": "..."}.
// GET /state answers with State.
//
//	POST /crash         name, code, oom
//	POST /crash-loop    name, on
//	POST /join          name, player
//	POST /leave         name, player
//	POST /rcon-refuse   name, on
//	POST /save-hangs    name, on
//	POST /image         ref, on (the image is present)
//	POST /preview-mode  mode ("ok", "fail" or "hang")
func (g *Game) HookHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(g.State())
	})
	hook := func(path string, do func(hookRequest) error) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			var req hookRequest
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
			dec.DisallowUnknownFields()
			err := dec.Decode(&req)
			if err == nil {
				err = do(req)
			}
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	hook("/crash", func(q hookRequest) error {
		if q.Code == 0 {
			q.Code = 1
		}
		return g.Crash(q.Name, q.Code, q.OOM)
	})
	hook("/crash-loop", func(q hookRequest) error { g.SetCrashLoop(q.Name, q.On); return nil })
	hook("/join", func(q hookRequest) error { return g.Join(q.Name, q.Player) })
	hook("/leave", func(q hookRequest) error { return g.Leave(q.Name, q.Player) })
	hook("/rcon-refuse", func(q hookRequest) error { g.SetRCONRefuse(q.Name, q.On); return nil })
	hook("/save-hangs", func(q hookRequest) error { g.SetSaveHangs(q.Name, q.On); return nil })
	hook("/preview-mode", func(q hookRequest) error { return g.SetPreviewMode(PreviewMode(q.Mode)) })
	hook("/image", func(q hookRequest) error {
		if q.Ref == "" {
			return errors.New("an image needs a ref")
		}
		g.SetImage(q.Ref, q.On)
		return nil
	})
	return mux
}
