package dockertest

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

type inspected struct {
	State struct {
		Status    string
		Running   bool
		ExitCode  int
		OOMKilled bool
		Error     string
	}
}

func call(t *testing.T, d *Daemon, method, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, d.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, b.String()
}

func inspect(t *testing.T, d *Daemon, ref string) inspected {
	t.Helper()
	status, body := call(t, d, "GET", "/v1.52/containers/"+ref+"/json")
	if status != http.StatusOK {
		t.Fatalf("inspect %s = %d %s", ref, status, body)
	}
	var out inspected
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// capturedPortInUse reads the message of the Engine API start reply in the
// real capture, with its container name, endpoint ID and port.
func capturedPortInUse(t *testing.T) (message, name, endpoint, port string) {
	t.Helper()
	raw, err := os.ReadFile("../../game/testdata/docker/port-in-use.log")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(l, `{"message":`) {
			continue
		}
		var body struct{ Message string }
		if err := json.Unmarshal([]byte(l), &body); err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`endpoint (\S+) \(([0-9a-f]{64})\): Bind for 0\.0\.0\.0:(\d+) failed`).FindStringSubmatch(body.Message)
		if m == nil {
			t.Fatalf("the capture's message has an unexpected shape: %s", body.Message)
		}
		return body.Message, m[1], m[2], m[3]
	}
	t.Fatal("the capture has no Engine API reply")
	return
}

func TestPortInUseMessageIsTheCapturedText(t *testing.T) {
	message, name, endpoint, port := capturedPortInUse(t)
	if got := PortInUseMessage(name, endpoint, port); got != message {
		t.Errorf("message =\n%s\nwant\n%s", got, message)
	}
}

func TestStartFailsWithPortInUseOnceThenSucceeds(t *testing.T) {
	d := New(t)
	d.Seed(Container{Name: "foreman-w1", ID: "aaa", Status: "created"})
	d.Seed(Container{Name: "foreman-w2", ID: "bbb", Status: "created"})
	d.FailStartPortInUse("foreman-w1", "34197")

	status, body := call(t, d, "POST", "/v1.52/containers/aaa/start")
	var reply struct{ Message string }
	_ = json.Unmarshal([]byte(body), &reply)
	endpoint := endpointID("aaa")
	want := PortInUseMessage("foreman-w1", endpoint, "34197")
	if status != http.StatusInternalServerError || reply.Message != want {
		t.Fatalf("start = %d %s, want 500 with %q", status, body, want)
	}
	if !strings.HasSuffix(want, "Bind for 0.0.0.0:34197 failed: port is already allocated") {
		t.Errorf("message = %q", want)
	}
	// Left "created", exit 128, with the same text in State.Error.
	st := inspect(t, d, "aaa").State
	if st.Status != "created" || st.Running || st.ExitCode != 128 || st.Error != want {
		t.Errorf("state after the failed start = %+v", st)
	}

	// The second start of the same container succeeds, as on a real daemon.
	if status, _ := call(t, d, "POST", "/v1.52/containers/aaa/start"); status != http.StatusNoContent {
		t.Errorf("second start = %d, want 204", status)
	}
	st = inspect(t, d, "aaa").State
	if st.Status != "running" || st.Error != "" || st.ExitCode != 0 {
		t.Errorf("state after the second start = %+v", st)
	}

	// Another container was never affected.
	if status, _ := call(t, d, "POST", "/v1.52/containers/bbb/start"); status != http.StatusNoContent {
		t.Errorf("start of an unarmed container = %d, want 204", status)
	}
}

func TestPortInUseIsArmedByNameForAContainerMadeLater(t *testing.T) {
	d := New(t)
	d.FailStartPortInUse("foreman-w1", "34197")
	d.Seed(Container{Name: "foreman-w1", ID: "aaa", Status: "created"})
	if status, _ := call(t, d, "POST", "/v1.52/containers/foreman-w1/start"); status != http.StatusInternalServerError {
		t.Errorf("start = %d, want 500", status)
	}
}

func TestOOMKilledExitIsTheCapturedState(t *testing.T) {
	d := New(t)
	d.Seed(Container{Name: "foreman-w1", ID: "aaa", Status: "running"})
	d.Exit("aaa", 137, true)
	st := inspect(t, d, "aaa").State
	if st.Status != "exited" || st.ExitCode != 137 || !st.OOMKilled || st.Error != "" {
		t.Errorf("state = %+v, want exited, 137, OOMKilled, no error", st)
	}
	// A restarted attempt reads OOMKilled false until it is killed again.
	d.Restart("aaa")
	if st := inspect(t, d, "aaa").State; st.OOMKilled || !st.Running {
		t.Errorf("state during the restart = %+v", st)
	}
	d.Exit("aaa", 137, true)
	if !inspect(t, d, "aaa").State.OOMKilled {
		t.Error("OOMKilled is false after the next kill")
	}
}
