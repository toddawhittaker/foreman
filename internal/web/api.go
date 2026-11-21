package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The REST API's paths (SPEC 23.1). PathAPI covers every other path under
// /api/, so the on/off gate refuses them too (SPEC 23.8).
const (
	PathAPI      = "/api/"
	PathAPITools = "/api/v1/tools"
	PathAPITool  = "/api/v1/tools/{name}"
)

// jsonRPCInternalError is the JSON-RPC code for a fault on the server's side.
const jsonRPCInternalError = -32603

// apiLogBody is how much of an unexpected SDK answer the log keeps.
const apiLogBody = 200

// apiTool is one entry of GET /api/v1/tools (SPEC 23.1).
type apiTool struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ReadOnly     bool   `json:"read_only"`
	TwoStep      bool   `json:"two_step"`
	InputSchema  any    `json:"input_schema"`
	OutputSchema any    `json:"output_schema,omitempty"`
}

// apiRoutes adds the REST API (SPEC 23). Each request passes, in order: API
// on (403), no foreign Origin (403), the path's method (405), then the
// checks /mcp shares (tokenChecks). A call becomes an MCP tools/call handed
// in process to the token scope's SDK server, so it runs exactly the code an
// MCP call runs (SPEC 23.2).
func (s *server) apiRoutes(checks func(http.Handler) http.Handler, sdk http.Handler, servers map[string]*mcp.Server) {
	tools := apiToolLists(servers)
	list := checks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := mcpRequestToken(mcpauth.TokenInfoFromContext(r.Context())).Scope
		writeJSON(w, http.StatusOK, map[string]any{"tools": tools[scope]})
	}))
	call := checks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := mcpRequestToken(mcpauth.TokenInfoFromContext(r.Context())).Scope
		s.apiCall(w, r, sdk, tools[scope])
	}))
	s.mux.Handle(PathAPITools, s.apiGate(http.MethodGet, list))
	s.mux.Handle(PathAPITool, s.apiGate(http.MethodPost, call))
	s.mux.Handle(PathAPIOpenAPI, s.apiGate(http.MethodGet, checks(apiOpenAPI(tools))))
	s.mux.Handle(PathAPI, s.apiGate("", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiError(w, http.StatusNotFound, "No such API path; GET "+PathAPITools+" lists the tools.")
	})))
}

// apiGate refuses a request while API access is off, before its token is
// read (SPEC 14.19), then one with a foreign Origin, then any method but
// method; an empty method allows any.
func (s *server) apiGate(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.apiOn(r.Context()) {
			http.Error(w, "REST API access is off. An admin can turn it on on Foreman's API tokens page.", http.StatusForbidden)
			return
		}
		if !s.mcpOrigin(r) {
			http.Error(w, "Requests from a web page are refused.", http.StatusForbidden)
			return
		}
		if method != "" && r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiToolLists lists each scope's tools once, at startup, by asking its SDK
// server over an in-memory connection, so the list is the one /mcp gives.
// Each list keeps the server's order.
func apiToolLists(servers map[string]*mcp.Server) map[string][]apiTool {
	ctx := context.Background()
	out := map[string][]apiTool{}
	for scope, srv := range servers {
		serverEnd, clientEnd := mcp.NewInMemoryTransports()
		ss, err := srv.Connect(ctx, serverEnd, nil)
		if err != nil {
			panic(fmt.Sprintf("list the %s tools: %v", scope, err))
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "foreman-api", Version: "1"}, nil).Connect(ctx, clientEnd, nil)
		if err != nil {
			panic(fmt.Sprintf("list the %s tools: %v", scope, err))
		}
		for tool, err := range cs.Tools(ctx, nil) {
			if err != nil {
				panic(fmt.Sprintf("list the %s tools: %v", scope, err))
			}
			out[scope] = append(out[scope], apiToolOf(tool))
		}
		_ = cs.Close()
		_ = ss.Wait()
	}
	return out
}

func apiToolOf(t *mcp.Tool) apiTool {
	a := apiTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, OutputSchema: t.OutputSchema}
	if t.Annotations != nil {
		a.ReadOnly = t.Annotations.ReadOnlyHint
		a.TwoStep = t.Annotations.DestructiveHint != nil && *t.Annotations.DestructiveHint
	}
	return a
}

// apiCall runs one tool for POST /api/v1/tools/{name} (SPEC 23.4, 23.5).
// The token checks have run and the body is in memory.
func (s *server) apiCall(w http.ResponseWriter, r *http.Request, sdk http.Handler, tools []apiTool) {
	name := r.PathValue("name")
	if !apiHasTool(tools, name) {
		apiError(w, http.StatusNotFound, fmt.Sprintf("No tool named %q for this token; GET %s lists them.", name, PathAPITools))
		return
	}
	body, _ := io.ReadAll(r.Body)
	args := bytes.TrimSpace(body)
	if ct := r.Header.Get("Content-Type"); ct != "" || len(args) > 0 {
		if mt, _, err := mime.ParseMediaType(ct); err != nil || mt != "application/json" {
			apiError(w, http.StatusUnsupportedMediaType, "Send the arguments as application/json.")
			return
		}
	}
	if len(args) == 0 {
		args = []byte("{}")
	}
	if args[0] != '{' || !json.Valid(args) {
		apiError(w, http.StatusBadRequest, "The body must be one JSON object holding the tool's arguments.")
		return
	}
	msg, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": json.RawMessage(args)}})
	if err != nil {
		s.apiFault(w, "build a tool call", err)
		return
	}
	// The clone keeps the request's context, which holds the checked token
	// the tool handlers read.
	inner := r.Clone(r.Context())
	inner.Method = http.MethodPost
	inner.Body = io.NopCloser(bytes.NewReader(msg))
	inner.ContentLength = int64(len(msg))
	inner.Header = http.Header{
		"Content-Type": {"application/json"},
		"Accept":       {"application/json, text/event-stream"},
	}
	rec := httptest.NewRecorder()
	sdk.ServeHTTP(rec, inner)
	if rec.Code != http.StatusOK {
		// The body may say more than the log should hold.
		shown := bytes.TrimSpace(rec.Body.Bytes())
		if len(shown) > apiLogBody {
			shown = shown[:apiLogBody]
		}
		s.apiFault(w, "run a tool", fmt.Errorf("the MCP handler answered %d: %s", rec.Code, shown))
		return
	}
	var answer struct {
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Structured json.RawMessage `json:"structuredContent"`
			IsError    bool            `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		s.apiFault(w, "read a tool's answer", err)
		return
	}
	switch {
	case answer.Error != nil && answer.Error.Code == jsonRPCInternalError:
		// Foreman's own fault, such as a recovered panic in a tool.
		s.apiFault(w, "run a tool", fmt.Errorf("internal error: %s", answer.Error.Message))
	case answer.Error != nil:
		// The SDK refused the call before the tool ran, for example
		// arguments that do not fit the schema (SPEC 23.5, every refusal).
		apiError(w, http.StatusUnprocessableEntity, answer.Error.Message)
	case answer.Result == nil:
		s.apiFault(w, "read a tool's answer", fmt.Errorf("neither result nor error"))
	default:
		var texts []string
		for _, c := range answer.Result.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
		text := strings.Join(texts, "\n")
		result := answer.Result.Structured
		if answer.Result.IsError {
			out := map[string]any{"error": text}
			if len(result) > 0 && string(result) != "null" {
				out["result"] = result
			}
			writeJSON(w, http.StatusUnprocessableEntity, out)
			return
		}
		if len(result) == 0 {
			result = json.RawMessage("null")
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": text, "result": result})
	}
}

func apiHasTool(tools []apiTool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func apiError(w http.ResponseWriter, status int, text string) {
	writeJSON(w, status, map[string]string{"error": text})
}

// apiFault answers a fault Foreman cannot word, logged and said only in
// general terms, as mcpFail does.
func (s *server) apiFault(w http.ResponseWriter, what string, err error) {
	s.log.Error("api: "+what, "error", err)
	apiError(w, http.StatusInternalServerError, "Foreman could not "+what+"; the reason is in its log.")
}

// writeJSON answers with v as JSON. An answer may hold a world's live
// state, so no cache keeps it.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
