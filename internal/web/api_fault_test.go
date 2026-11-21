package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// faultCall runs apiCall against an SDK handler that answers as given, and
// returns the REST answer and what the server logged.
func faultCall(t *testing.T, status int, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var logs bytes.Buffer
	s := &server{log: slog.New(slog.NewTextHandler(&logs, nil))}
	sdk := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	r := httptest.NewRequest(http.MethodPost, toolPath("t"), nil)
	r.SetPathValue("name", "t")
	rec := httptest.NewRecorder()
	s.apiCall(rec, r, sdk, []apiTool{{Name: "t"}})
	return rec, logs.String()
}

// SPEC 23.5: a JSON-RPC internal error is Foreman's fault and answers 500;
// the SDK's other refusals answer 422.
func TestAPIJSONRPCErrors(t *testing.T) {
	rec, logs := faultCall(t, http.StatusOK, `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"panic in tool: boom"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("-32603: %d %q", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "boom") || !strings.Contains(logs, "boom") {
		t.Errorf("the reason belongs in the log only: answer %q, log %q", rec.Body, logs)
	}
	for _, code := range []string{"-32602", "-32601", "-32600"} {
		rec, _ := faultCall(t, http.StatusOK, `{"jsonrpc":"2.0","id":1,"error":{"code":`+code+`,"message":"bad arguments"}}`)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "bad arguments") {
			t.Errorf("%s: %d %q", code, rec.Code, rec.Body)
		}
	}
}

// A non-200 SDK answer is a fault, and the log keeps only its first 200
// bytes.
func TestAPILogsOnlyTheStartOfAnSDKFault(t *testing.T) {
	body := strings.Repeat("a", 200) + strings.Repeat("z", 5000)
	rec, logs := faultCall(t, http.StatusBadGateway, body)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
	if !strings.Contains(logs, "answered 502") || !strings.Contains(logs, strings.Repeat("a", 200)) {
		t.Errorf("log lacks the status or the start: %q", logs)
	}
	if strings.Contains(logs, "z") || len(logs) > 600 {
		t.Errorf("log holds more than the first 200 bytes (%d bytes): %q", len(logs), logs)
	}
}
