package web

// SPEC 23.1, 23.2: the REST API offers exactly the MCP tools of each scope,
// with the same schemas, and a tool outside the scope is unknown and never
// runs. SPEC 23.4: the content type rules for a body.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// asPlain turns v into plain JSON values, so two schemas compare by content.
func asPlain(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// apiList is GET /api/v1/tools for secret, decoded.
func apiList(t *testing.T, hn *harness, secret string) []apiTool {
	t.Helper()
	rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret))
	var list struct{ Tools []apiTool }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil {
		t.Fatalf("list: %d %q", rec.Code, rec.Body)
	}
	return list.Tools
}

// auditCount is how many audit entries the store holds.
func auditCount(t *testing.T, hn *harness) int {
	t.Helper()
	list, err := hn.store.ListAudit(context.Background(), store.AuditFilter{Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

func mcpTools(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	out := map[string]*mcp.Tool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		out[tool.Name] = tool
	}
	return out
}

func TestAPIParityEveryScope(t *testing.T) {
	// The mods rig registers every admin tool.
	g := newModsRig(t, false).gameRig
	apiOnFor(t, g.harness)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	for _, scope := range []string{auth.ScopeRead, auth.ScopeOperate, auth.ScopeAdmin} {
		t.Run(scope, func(t *testing.T) {
			secret, _ := apiToken(t, g.harness, u, "Parity "+scope, scope)
			want := mcpTools(t, mcpConnect(t, g.url, secret))
			got := apiList(t, g.harness, secret)
			var names []string
			for _, a := range got {
				names = append(names, a.Name)
				m, ok := want[a.Name]
				if !ok {
					t.Errorf("%s is listed on /api/ but not on /mcp", a.Name)
					continue
				}
				if a.Description != m.Description {
					t.Errorf("%s: descriptions differ", a.Name)
				}
				if !reflect.DeepEqual(asPlain(t, a.InputSchema), asPlain(t, m.InputSchema)) {
					t.Errorf("%s: input schemas differ\napi: %s\nmcp: %s", a.Name, asJSON(a.InputSchema), asJSON(m.InputSchema))
				}
				if !reflect.DeepEqual(asPlain(t, a.OutputSchema), asPlain(t, m.OutputSchema)) {
					t.Errorf("%s: output schemas differ\napi: %s\nmcp: %s", a.Name, asJSON(a.OutputSchema), asJSON(m.OutputSchema))
				}
				if a.InputSchema == nil {
					t.Errorf("%s: no input schema", a.Name)
				}
				readOnly := m.Annotations != nil && m.Annotations.ReadOnlyHint
				if a.ReadOnly != readOnly || a.TwoStep != isTwoStep(m) {
					t.Errorf("%s: read_only %v two_step %v, /mcp says %v %v", a.Name, a.ReadOnly, a.TwoStep, readOnly, isTwoStep(m))
				}
			}
			if len(names) != len(slices.Compact(slices.Sorted(slices.Values(names)))) {
				t.Errorf("a tool is listed twice: %q", names)
			}
			if len(got) != len(want) {
				t.Errorf("/api/ lists %d tools, /mcp %d", len(got), len(want))
			}
		})
	}
}

// SPEC 23.2: a tool outside the token's scope answers 404, as an unknown
// tool, and nothing it would do happens.
func TestAPIOutsideScopeNeverRuns(t *testing.T) {
	g := newModsRig(t, false).gameRig
	apiOnFor(t, g.harness)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	if err := g.ctrl.Start(context.Background(), id, game.Actor{UserID: u.ID, Username: u.Username}, game.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	adminSecret, _ := apiToken(t, g.harness, u, "Admin", auth.ScopeAdmin)
	every := apiList(t, g.harness, adminSecret)
	for _, scope := range []string{auth.ScopeRead, auth.ScopeOperate} {
		t.Run(scope, func(t *testing.T) {
			secret, _ := apiToken(t, g.harness, u, "Outside "+scope, scope)
			allowed := map[string]bool{}
			for _, a := range apiList(t, g.harness, secret) {
				allowed[a.Name] = true
			}
			audits := auditCount(t, g.harness)
			snaps, _ := g.worlds.Snapshots(id)
			state := g.ctrl.Status(id).State
			outside := 0
			for _, a := range every {
				if allowed[a.Name] {
					continue
				}
				outside++
				// Arguments that would work, with a made-up code, so only the
				// scope stands in the way.
				body := `{"world_id":"` + id + `","message":"hi","label":"x","command":"/time","name":"bob","snapshots":["x"],"confirm_code":"` + strings.Repeat("A", 22) + `"}`
				rec := apiReq(g.harness, http.MethodPost, toolPath(a.Name), body, bearer(secret))
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s: %d %q, want 404", a.Name, rec.Code, rec.Body)
				}
				if a := decodeAnswer(t, rec); a.Result != nil || a.Message != "" {
					t.Errorf("%s: a 404 carried a result: %q", a.Error, rec.Body)
				}
			}
			if outside == 0 {
				t.Fatal("no tool lies outside the scope")
			}
			if n := auditCount(t, g.harness); n != audits {
				t.Errorf("an out-of-scope call wrote %d audit entries", n-audits)
			}
			if now, _ := g.worlds.Snapshots(id); len(now) != len(snaps) {
				t.Errorf("an out-of-scope call took a snapshot: %d to %d", len(snaps), len(now))
			}
			if now := g.ctrl.Status(id).State; now != state {
				t.Errorf("an out-of-scope call moved the game from %s to %s", state, now)
			}
		})
	}
}

// SPEC 23.4: arguments come as application/json. A body with no content
// type, or a content type that is not JSON even with no body, answers 415.
func TestAPIContentTypeRules(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	for _, c := range []struct{ name, body, ct string }{
		{"{} with no content type", "{}", ""},
		{"an empty body as text/plain", "", "text/plain"},
	} {
		r := httptest.NewRequest(http.MethodPost, toolPath("list_worlds"), strings.NewReader(c.body))
		if c.ct != "" {
			r.Header.Set("Content-Type", c.ct)
		}
		r.Header.Set("Authorization", "Bearer "+secret)
		r.RemoteAddr = peerUntrusted
		rec := hn.serve(r)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: %d %q, want 415", c.name, rec.Code, rec.Body)
			continue
		}
		if a := decodeAnswer(t, rec); a.Error == "" || a.Result != nil {
			t.Errorf("%s: %q", c.name, rec.Body)
		}
	}
}

// SPEC 23.2, 21.7, 12.9: get_new_map and set_new_map over REST give the
// results /mcp gives, are refused for the same reasons, and are audited with
// the token's name.
func TestAPINewMapToolsMatchMCP(t *testing.T) {
	g := newModsRig(t, false).gameRig
	apiOnFor(t, g.harness)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, g.harness, u, "Script", auth.ScopeAdmin)
	fresh := func(name string) string {
		id, err := g.worlds.Generate(context.Background(), world.NewWorld{Name: name, GameVersion: "2.1.24", SpaceAge: true})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	viaAPI, viaMCP := fresh("Through REST"), fresh("Through MCP")
	args := func(id string) map[string]any {
		return map[string]any{"world_id": id, "preset": "death-world", "seed": "21",
			"controls": []any{map[string]any{"planet": "nauvis", "control": "coal", "size": 50}}}
	}
	post := func(tool string, a map[string]any) apiAnswer {
		body, _ := json.Marshal(a)
		rec := apiReq(g.harness, http.MethodPost, toolPath(tool), string(body), bearer(secret))
		ans := decodeAnswer(t, rec)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %q", tool, rec.Code, rec.Body)
		}
		return ans
	}

	rest := post("get_new_map", map[string]any{"world_id": viaAPI})
	mcpOut, isErr := mcpCall(t, g.harness, secret, "get_new_map", map[string]any{"world_id": viaMCP})
	if isErr || rest.Result["planets"] == nil || !reflect.DeepEqual(rest.Result["planets"], mcpOut["planets"]) {
		t.Errorf("get_new_map differs:\nrest %s\nmcp  %s", asJSON(rest.Result), asJSON(mcpOut))
	}

	rest = post("set_new_map", args(viaAPI))
	mcpOut, isErr = mcpCall(t, g.harness, secret, "set_new_map", args(viaMCP))
	if isErr || rest.Result["modified"] != true || rest.Result["seed"] != "21" ||
		rest.Result["modified"] != mcpOut["modified"] || rest.Result["seed"] != mcpOut["seed"] || rest.Result["preset"] != mcpOut["preset"] {
		t.Errorf("set_new_map differs:\nrest %s\nmcp  %s", asJSON(rest.Result), asJSON(mcpOut))
	}
	var entries []store.AuditEntry
	list, err := g.store.ListAudit(context.Background(), store.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.Action == AuditNewMap {
			entries = append(entries, e)
		}
	}
	if len(entries) != 2 || entries[0].TokenName != "Script" || entries[1].TokenName != "Script" || entries[0].Detail != entries[1].Detail {
		t.Errorf("audit %+v", entries)
	}

	// The same refusal, whichever way in: the world now has its map set but no save, so a
	// bad control refuses over REST as it does over /mcp.
	bad := args(viaAPI)
	bad["controls"] = []any{map[string]any{"planet": "nope", "control": "coal", "size": 100}}
	body, _ := json.Marshal(bad)
	rec := apiReq(g.harness, http.MethodPost, toolPath("set_new_map"), string(body), bearer(secret))
	_, mcpFailed := mcpCall(t, g.harness, secret, "set_new_map", bad)
	if rec.Code == http.StatusOK || !mcpFailed || !strings.Contains(rec.Body.String(), "nope") {
		t.Errorf("a bad planet over REST: %d %q (mcp refused: %v)", rec.Code, rec.Body, mcpFailed)
	}
}
