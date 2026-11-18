package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
)

// The worked example of a two-step tool (SPEC 21.8). Each two-step admin
// tool follows exampleDelete's shape:
//
//  1. Without confirm_code: run the page's checks, build the preview from
//     the same page struct the UI's confirm page uses, and return it with
//     s.mcpAsk's code. Nothing changes.
//  2. With confirm_code: spend the code first (s.spendIfCode), before any
//     check, so a wrong or refused use spends it too. Run every check again. If the state the preview showed
//     has moved, refuse with a fresh preview and code. Otherwise act.
//
// The tool is registered with Annotations: twoStep.
//
// The seen state must include the resolved world id and anything else the
// arguments leave unresolved (a version picked as "latest", a save left
// empty). Two-step tools require world_id and never default to the active
// world. Real tools hold the world's lock through mcpRun or the lifted
// function, not a mutex like the example's notes.mu.

// exampleNotes stands in for a world's files.
type exampleNotes struct {
	mu    sync.Mutex
	notes map[string]string
}

type exampleDeleteIn struct {
	mcpConfirmIn
	Name string `json:"name" jsonschema:"the note to delete"`
}

type exampleDeleteOut struct {
	mcpPreview
	Name string `json:"name"`
}

// exampleDelete deletes a note in two steps; the note's text is the state
// the preview shows.
func (s *server) exampleDelete(notes *exampleNotes) mcp.ToolHandlerFor[exampleDeleteIn, exampleDeleteOut] {
	preview := func(req *mcp.CallToolRequest, in exampleDeleteIn, text string) (*mcp.CallToolResult, exampleDeleteOut, error) {
		p, err := s.mcpAsk(req, in, text)
		if err != nil {
			return nil, exampleDeleteOut{}, err
		}
		p.Will, p.Snapshot = []string{"Note " + in.Name + " will be deleted."}, "Before delete"
		return mcpText("Nothing has changed yet. Call "+mcpTool(req)+" again with the same arguments and confirm_code to delete %s.", in.Name),
			exampleDeleteOut{mcpPreview: p, Name: in.Name}, nil
	}
	return func(_ context.Context, req *mcp.CallToolRequest, in exampleDeleteIn) (*mcp.CallToolResult, exampleDeleteOut, error) {
		notes.mu.Lock()
		defer notes.mu.Unlock()
		// The code is spent first, before any check, so a refused confirm
		// spends it too.
		seen, err := s.spendIfCode(req, in, in.ConfirmCode)
		if err != nil {
			return nil, exampleDeleteOut{}, err
		}
		// The page's checks run on both calls.
		text, ok := notes.notes[in.Name]
		if !ok {
			return nil, exampleDeleteOut{}, fmt.Errorf("there is no note %s", in.Name)
		}
		if in.ConfirmCode == "" {
			return preview(req, in, text)
		}
		if seen.moved(text) {
			res, out, err := preview(req, in, text)
			if res != nil {
				res = mcpText("Note %s changed since the preview, so nothing was deleted. Here is a fresh preview and code.", in.Name)
				res.IsError = true
			}
			return res, out, err
		}
		delete(notes.notes, in.Name)
		return mcpText("Note %s was deleted.", in.Name), exampleDeleteOut{mcpPreview: mcpPreview{Done: true}, Name: in.Name}, nil
	}
}

// confirmRig is a server with only the example tool, and a client session
// for each token id given.
type confirmRig struct {
	s     *server
	clock *fakeClock
	notes *exampleNotes
}

func newConfirmRig() *confirmRig {
	clock := &fakeClock{t: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}
	return &confirmRig{
		s:     &server{confirms: newConfirmStore(clock.Now), now: clock.Now, log: slog.New(slog.DiscardHandler)},
		clock: clock,
		notes: &exampleNotes{notes: map[string]string{"todo": "one", "done": "two"}},
	}
}

// session connects a client whose calls carry the token with this id.
func (r *confirmRig) session(t *testing.T, tokenID int64) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "foreman", Version: "test"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "example_delete", Annotations: twoStep, Description: "Delete a note, in two steps."}, r.s.exampleDelete(r.notes))
	mcp.AddTool(srv, &mcp.Tool{Name: "example_other", Annotations: twoStep, Description: "Another two-step tool."}, r.s.exampleDelete(r.notes))
	ti := &mcpauth.TokenInfo{Extra: map[string]any{mcpTokenKey: store.APIToken{ID: tokenID, Name: "Claude"}}}
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if r, ok := req.(*mcp.CallToolRequest); ok {
				r.Extra = &mcp.RequestExtra{TokenInfo: ti}
			}
			return next(ctx, method, req)
		}
	})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// askCode makes a tool's first call and returns its confirm code, failing
// the test unless the call returned a preview.
func askCode(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, out := call(t, cs, name, args)
	code, _ := out[confirmCodeKey].(string)
	if res.IsError || out["done"] != false || code == "" || out["expires_at"] == "" {
		t.Fatalf("%s's first call: %s %s", name, resultText(res), asJSON(out))
	}
	return code
}

// confirmCall runs a two-step tool: the first call for its preview and
// code, then the same arguments with the code. It returns the preview and
// the second call's result.
func confirmCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, *mcp.CallToolResult, map[string]any) {
	t.Helper()
	_, preview := call(t, cs, name, args)
	code, _ := preview[confirmCodeKey].(string)
	if code == "" {
		t.Fatalf("%s returned no confirm code: %s", name, asJSON(preview))
	}
	again := maps.Clone(args)
	again[confirmCodeKey] = code
	res, out := call(t, cs, name, again)
	return preview, res, out
}

func withCode(args map[string]any, code string) map[string]any {
	out := maps.Clone(args)
	out[confirmCodeKey] = code
	return out
}

// SPEC 21.8: the first call changes nothing and returns a preview; the
// second, with the code, acts.
func TestConfirmExampleTwoSteps(t *testing.T) {
	r := newConfirmRig()
	cs := r.session(t, 1)
	args := map[string]any{"name": "todo"}
	res, out := call(t, cs, "example_delete", args)
	code, _ := out[confirmCodeKey].(string)
	if res.IsError || out["done"] != false || len(code) != 22 || out["snapshot"] != "Before delete" ||
		out["expires_at"] != "2026-11-17T12:05:00Z" || !strings.Contains(asJSON(out["will"]), "todo will be deleted") {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if _, ok := r.notes.notes["todo"]; !ok {
		t.Fatal("the preview deleted the note")
	}
	res, out = call(t, cs, "example_delete", withCode(args, code))
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if _, ok := r.notes.notes["todo"]; ok {
		t.Error("the confirm did not delete the note")
	}

	_, res, out = confirmCall(t, cs, "example_delete", map[string]any{"name": "done"})
	if res.IsError || out["done"] != true {
		t.Errorf("confirmCall: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 21.8: the confirm call is refused with a fresh preview and code when
// the state the preview showed has moved.
func TestConfirmSeenMoved(t *testing.T) {
	r := newConfirmRig()
	cs := r.session(t, 1)
	args := map[string]any{"name": "todo"}
	code := askCode(t, cs, "example_delete", args)
	r.notes.notes["todo"] = "changed"
	res, out := call(t, cs, "example_delete", withCode(args, code))
	fresh, _ := out[confirmCodeKey].(string)
	if !res.IsError || out["done"] != false || fresh == "" || fresh == code || !strings.Contains(resultText(res), "changed since the preview") {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if _, ok := r.notes.notes["todo"]; !ok {
		t.Fatal("a moved state was acted on")
	}
	if res, out := call(t, cs, "example_delete", withCode(args, fresh)); res.IsError || out["done"] != true {
		t.Errorf("the fresh code: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 14.19: a code is refused, with one fixed message, for another token,
// another tool, other arguments, a second use, after a wrong use, after 5
// minutes, and when made up.
func TestConfirmRefusals(t *testing.T) {
	args := map[string]any{"name": "todo"}
	cases := map[string]func(t *testing.T, r *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult{
		"another token": func(t *testing.T, r *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			code := askCode(t, cs, "example_delete", args)
			res, _ := call(t, r.session(t, 2), "example_delete", withCode(args, code))
			return res
		},
		"another tool": func(t *testing.T, _ *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			code := askCode(t, cs, "example_delete", args)
			res, _ := call(t, cs, "example_other", withCode(args, code))
			return res
		},
		"other arguments": func(t *testing.T, _ *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			code := askCode(t, cs, "example_delete", args)
			res, _ := call(t, cs, "example_delete", withCode(map[string]any{"name": "done"}, code))
			return res
		},
		"second use": func(t *testing.T, r *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			code := askCode(t, cs, "example_delete", args)
			call(t, cs, "example_delete", withCode(args, code))
			r.notes.notes["todo"] = "back"
			res, _ := call(t, cs, "example_delete", withCode(args, code))
			return res
		},
		"after a wrong use": func(t *testing.T, _ *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			code := askCode(t, cs, "example_delete", args)
			call(t, cs, "example_other", withCode(args, code))
			res, _ := call(t, cs, "example_delete", withCode(args, code))
			return res
		},
		"after a refused confirm": func(t *testing.T, r *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			code := askCode(t, cs, "example_delete", args)
			delete(r.notes.notes, "todo")
			if res, _ := call(t, cs, "example_delete", withCode(args, code)); !res.IsError || !strings.Contains(resultText(res), "no note") {
				t.Fatalf("the check did not refuse: %q", resultText(res))
			}
			r.notes.notes["todo"] = "one"
			res, _ := call(t, cs, "example_delete", withCode(args, code))
			return res
		},
		"expired": func(t *testing.T, r *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			code := askCode(t, cs, "example_delete", args)
			r.clock.Advance(confirmTTL)
			res, _ := call(t, cs, "example_delete", withCode(args, code))
			return res
		},
		"made up": func(t *testing.T, _ *confirmRig, cs *mcp.ClientSession) *mcp.CallToolResult {
			res, _ := call(t, cs, "example_delete", withCode(args, strings.Repeat("A", 22)))
			return res
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			r := newConfirmRig()
			cs := r.session(t, 1)
			res := run(t, r, cs)
			if !res.IsError || resultText(res) != errConfirmRefused.Error() {
				t.Errorf("refusal: %q", resultText(res))
			}
			if _, ok := r.notes.notes["todo"]; !ok {
				t.Error("a refused code acted")
			}
		})
	}
}

// SPEC 14.19: a code works until just before 5 minutes.
func TestConfirmWorksWithinFiveMinutes(t *testing.T) {
	r := newConfirmRig()
	cs := r.session(t, 1)
	args := map[string]any{"name": "todo"}
	code := askCode(t, cs, "example_delete", args)
	r.clock.Advance(confirmTTL - time.Second)
	if res, out := call(t, cs, "example_delete", withCode(args, code)); res.IsError || out["done"] != true {
		t.Errorf("within 5 minutes: %s", resultText(res))
	}
}

// fakeTokenReq is a call to the tool t made with the token of this id.
func fakeTokenReq(id int64) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "t"}, Extra: &mcp.RequestExtra{TokenInfo: &mcpauth.TokenInfo{
		Extra: map[string]any{mcpTokenKey: store.APIToken{ID: id}}}}}
}

type confirmArgsIn struct {
	mcpConfirmIn
	World  string            `json:"world_id"`
	Values map[string]string `json:"values"`
}

// SPEC 14.19: the code is bound to the arguments, whatever their key order,
// and not to confirm_code itself.
func TestConfirmArgsCanonical(t *testing.T) {
	a := map[string]any{"b": 1, "a": map[string]any{"y": "2", "x": "1"}, confirmCodeKey: "one"}
	b := map[string]any{"a": map[string]any{"x": "1", "y": "2"}, "b": 1}
	ca, ha, err := confirmArgs(a)
	if err != nil {
		t.Fatal(err)
	}
	_, hb, _ := confirmArgs(b)
	if ha != hb || ca != "one" {
		t.Errorf("hashes differ for the same arguments, or code %q", ca)
	}
	if _, hc, _ := confirmArgs(map[string]any{"b": 2, "a": map[string]any{"x": "1", "y": "2"}}); hc == ha {
		t.Error("other arguments hash the same")
	}

	s := newConfirmRig().s
	in := confirmArgsIn{World: "w", Values: map[string]string{"z": "1", "a": "2"}}
	p, err := s.mcpAsk(fakeTokenReq(1), in, "seen")
	if err != nil {
		t.Fatal(err)
	}
	in.ConfirmCode = p.ConfirmCode
	if seen, err := s.mcpSpend(fakeTokenReq(1), in); err != nil || seen.moved("seen") || !seen.moved("other") {
		t.Errorf("spend: %v", err)
	}
}

// SPEC 14.19: a token holds at most 10 codes; an eleventh drops the oldest,
// and other tokens' codes stay.
func TestConfirmEleventhDropsOldest(t *testing.T) {
	r := newConfirmRig()
	s := r.s
	other, _ := s.mcpAsk(fakeTokenReq(2), confirmArgsIn{World: "x"}, "")
	var codes []string
	for i := range maxConfirmsPerToken + 1 {
		r.clock.Advance(time.Millisecond)
		p, err := s.mcpAsk(fakeTokenReq(1), confirmArgsIn{World: fmt.Sprint(i)}, "")
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, p.ConfirmCode)
	}
	spend := func(tok int64, world, code string) error {
		_, err := s.mcpSpend(fakeTokenReq(tok), confirmArgsIn{mcpConfirmIn: mcpConfirmIn{code}, World: world})
		return err
	}
	if err := spend(1, "0", codes[0]); !errors.Is(err, errConfirmRefused) {
		t.Errorf("the oldest code still works: %v", err)
	}
	for i := 1; i <= maxConfirmsPerToken; i++ {
		if err := spend(1, fmt.Sprint(i), codes[i]); err != nil {
			t.Errorf("code %d: %v", i, err)
		}
	}
	if err := spend(2, "x", other.ConfirmCode); err != nil {
		t.Errorf("another token's code was dropped: %v", err)
	}
}

// SPEC 14.19: a code is never logged.
func TestConfirmCodeNotLogged(t *testing.T) {
	var logs bytes.Buffer
	r := newConfirmRig()
	r.s.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cs := r.session(t, 1)
	args := map[string]any{"name": "todo"}
	code := askCode(t, cs, "example_delete", args)
	call(t, cs, "example_other", withCode(args, code))
	if strings.Contains(logs.String(), code) {
		t.Errorf("the code was logged: %s", logs.String())
	}
}

// SPEC 12.9: an action through a token is audited with the token's name.
func TestAuditCarriesTokenName(t *testing.T) {
	hn := newHarness(t, nil)
	s := &server{store: hn.store, now: hn.clock.Now, log: hn.slog}
	u := hn.addUser("alice", testPassword)
	s.audit(context.Background(), game.Actor{UserID: u.ID, Username: u.Username, Token: "Claude"}, AuditScheduleSet, "", "on")
	list, err := hn.store.ListAudit(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].TokenName != "Claude" || list[0].Username != "alice" || list[0].Action != AuditScheduleSet {
		t.Errorf("audit: %+v", list)
	}
}

// SPEC 21.5: detached work is waited for up to mcpWait, then said to be
// still running, and goes on.
func TestMCPDetached(t *testing.T) {
	s := &server{runs: newGameRuns(), log: slog.New(slog.DiscardHandler)}
	t.Cleanup(s.runs.close)
	if done, err := s.mcpDetached(context.Background(), func(context.Context) error { return errors.New("no") }); !done || err == nil {
		t.Errorf("quick work: %v %v", done, err)
	}
	was := mcpWait
	mcpWait = 10 * time.Millisecond
	t.Cleanup(func() { mcpWait = was })
	release, ended := make(chan struct{}), make(chan struct{})
	done, err := s.mcpDetached(context.Background(), func(context.Context) error {
		<-release
		close(ended)
		return nil
	})
	if done || err != nil {
		t.Errorf("slow work: %v %v", done, err)
	}
	close(release)
	<-ended
}

// Shutdown waits for detached work that is still running.
func TestMCPDetachedShutdownWaits(t *testing.T) {
	s := &server{runs: newGameRuns(), log: slog.New(slog.DiscardHandler)}
	was := mcpWait
	mcpWait = 10 * time.Millisecond
	t.Cleanup(func() { mcpWait = was })
	var ended atomic.Bool
	if done, _ := s.mcpDetached(context.Background(), func(context.Context) error {
		time.Sleep(200 * time.Millisecond)
		ended.Store(true)
		return nil
	}); done {
		t.Fatal("the work finished before mcpWait")
	}
	s.runs.close()
	if !ended.Load() {
		t.Error("shutdown did not wait for the detached work")
	}
}
