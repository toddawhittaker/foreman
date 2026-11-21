package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/auth"
)

// openAPIDoc is the part of the document the tests read; schemas stay raw so
// they can be compared byte for byte.
type openAPIDoc struct {
	OpenAPI string `json:"openapi"`
	Info    struct{ Title, Version string }
	Paths   map[string]map[string]struct {
		OperationID string `json:"operationId"`
		Summary     string
		Description string
		ReadOnly    bool `json:"x-foreman-read-only"`
		TwoStep     bool `json:"x-foreman-two-step"`
		RequestBody struct {
			Content map[string]struct{ Schema json.RawMessage }
		} `json:"requestBody"`
		Responses map[string]struct {
			Content map[string]struct {
				Schema struct {
					Properties map[string]json.RawMessage
				}
			}
		}
	}
	Components struct {
		SecuritySchemes map[string]struct{ Type, Scheme string }
	}
	Security []map[string][]string
}

// compact normalizes raw JSON so schemas compare by content.
func compact(t *testing.T, raw []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatalf("not JSON: %q", raw)
	}
	return b.String()
}

// SPEC 23.10: the document lists exactly the token scope's tools, with each
// tool's schemas unchanged, and requires the bearer token.
func TestAPIOpenAPI(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	counts := map[string]int{}
	for _, scope := range []string{auth.ScopeRead, auth.ScopeOperate, auth.ScopeAdmin} {
		secret, _ := apiToken(t, hn, u, "Script "+scope, scope)

		rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret))
		var list struct {
			Tools []struct {
				Name         string
				ReadOnly     bool            `json:"read_only"`
				TwoStep      bool            `json:"two_step"`
				InputSchema  json.RawMessage `json:"input_schema"`
				OutputSchema json.RawMessage `json:"output_schema"`
			}
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil {
			t.Fatalf("%s list: %d %q", scope, rec.Code, rec.Body)
		}

		rec = apiReq(hn, http.MethodGet, PathAPIOpenAPI, "", bearer(secret))
		var doc openAPIDoc
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &doc) != nil {
			t.Fatalf("%s document: %d %q", scope, rec.Code, rec.Body)
		}
		if doc.OpenAPI != "3.1.0" || doc.Info.Title != "Foreman REST API" || doc.Info.Version != "1" {
			t.Errorf("%s: openapi %q, info %+v", scope, doc.OpenAPI, doc.Info)
		}
		if ss := doc.Components.SecuritySchemes["bearerAuth"]; ss.Type != "http" || ss.Scheme != "bearer" {
			t.Errorf("%s: security scheme %+v", scope, ss)
		}
		if len(doc.Security) != 1 {
			t.Errorf("%s: global security %v", scope, doc.Security)
		} else if _, ok := doc.Security[0]["bearerAuth"]; !ok {
			t.Errorf("%s: global security %v does not require bearerAuth", scope, doc.Security)
		}

		var want, got []string
		for _, tool := range list.Tools {
			want = append(want, toolPath(tool.Name))
		}
		want = append(want, PathAPITools)
		for p := range doc.Paths {
			got = append(got, p)
		}
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: paths %q, want %q", scope, got, want)
		}
		counts[scope] = len(list.Tools)

		for _, tool := range list.Tools {
			op, ok := doc.Paths[toolPath(tool.Name)]["post"]
			if !ok {
				t.Errorf("%s: no POST for %s", scope, tool.Name)
				continue
			}
			if op.OperationID != tool.Name || op.Summary == "" || op.Description == "" {
				t.Errorf("%s: %s operation %q, summary %q", scope, tool.Name, op.OperationID, op.Summary)
			}
			if op.ReadOnly != tool.ReadOnly || op.TwoStep != tool.TwoStep {
				t.Errorf("%s: %s flags read-only %v two-step %v, list says %v %v", scope, tool.Name, op.ReadOnly, op.TwoStep, tool.ReadOnly, tool.TwoStep)
			}
			in := op.RequestBody.Content["application/json"].Schema
			if compact(t, in) != compact(t, tool.InputSchema) {
				t.Errorf("%s: %s input schema differs:\n%s\n%s", scope, tool.Name, in, tool.InputSchema)
			}
			result := op.Responses["200"].Content["application/json"].Schema.Properties["result"]
			if len(tool.OutputSchema) > 0 && compact(t, result) != compact(t, tool.OutputSchema) {
				t.Errorf("%s: %s output schema differs:\n%s\n%s", scope, tool.Name, result, tool.OutputSchema)
			}
			for _, code := range []string{"200", "400", "401", "403", "404", "413", "415", "422", "429"} {
				if _, ok := op.Responses[code]; !ok {
					t.Errorf("%s: %s has no %s response", scope, tool.Name, code)
				}
			}
		}
		if _, ok := doc.Paths[PathAPITools]["get"]; !ok {
			t.Errorf("%s: no GET %s", scope, PathAPITools)
		}
	}
	if counts[auth.ScopeRead] == 0 || counts[auth.ScopeRead] >= counts[auth.ScopeOperate] || counts[auth.ScopeOperate] >= counts[auth.ScopeAdmin] {
		t.Errorf("tool counts by scope: %v", counts)
	}
}

// SPEC 23.10, 14.19: the document passes the gate and token checks of the
// tool list.
func TestAPIOpenAPIGate(t *testing.T) {
	hn := newHarness(t, nil)
	u := mcpOn(t, hn)
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	if rec := apiReq(hn, http.MethodGet, PathAPIOpenAPI, "", bearer(secret)); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "off") {
		t.Errorf("API off: %d %q", rec.Code, rec.Body)
	}
	apiOnFor(t, hn)
	if rec := apiReq(hn, http.MethodGet, PathAPIOpenAPI, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d %q", rec.Code, rec.Body)
	}
	if rec := apiReq(hn, http.MethodPost, PathAPIOpenAPI, "", bearer(secret)); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
	if rec := apiReq(hn, http.MethodGet, PathAPIOpenAPI, "", bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("with a token: %d %q", rec.Code, rec.Body)
	}
}
