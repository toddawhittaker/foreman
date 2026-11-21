package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/store"
)

// The two-step confirm (SPEC 21.8, 14.19). A two-step tool called without
// confirm_code changes nothing and returns a preview with a code; called
// again with the same arguments and the code, it spends the code, runs
// every check again and acts. mcpconfirm_test.go holds a worked example.

// confirmTTL is how long a confirm code works (SPEC 14.19).
const confirmTTL = 5 * time.Minute

// maxConfirmsPerToken bounds the codes a token holds; an eleventh drops the
// oldest (SPEC 14.19).
const maxConfirmsPerToken = 10

// confirmCodeKey is the argument that carries the code; it is left out of
// the arguments' hash.
const confirmCodeKey = "confirm_code"

// errConfirmRefused is the one refusal for every bad code, so a caller
// learns nothing about why (SPEC 14.19).
var errConfirmRefused = errors.New("the confirm code was refused: it may be wrong, used, expired, or for another call. " +
	"Call the tool again without confirm_code for a fresh preview and code")

// The DestructiveHint every admin tool sets: true on the two-step tools,
// false on the rest, since MCP reads a missing hint as true.
var (
	twoStep = &mcp.ToolAnnotations{DestructiveHint: new(true)}
	oneStep = &mcp.ToolAnnotations{DestructiveHint: new(false)}
)

// mcpConfirmIn is embedded in a two-step tool's arguments.
type mcpConfirmIn struct {
	ConfirmCode string `json:"confirm_code,omitempty" jsonschema:"leave out to get a preview and a code; send the code back with the same arguments to go ahead"`
}

// mcpPreview is the shared part of a two-step tool's answer. Done is false
// on a preview and true once the action ran. Will holds the UI confirm
// page's sentences in English, Snapshot the label of the snapshot the
// action will take ("" for none), and UntrustedOnline the players who will
// be warned.
type mcpPreview struct {
	Done            bool     `json:"done"`
	Will            []string `json:"will,omitempty"`
	Snapshot        string   `json:"snapshot,omitempty"`
	UntrustedOnline []string `json:"untrusted_online,omitempty"`
	ConfirmCode     string   `json:"confirm_code,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
}

// confirmEntry is one code. Only hashes of the arguments and of the seen
// state are kept, so no command, settings file or password sits in memory.
type confirmEntry struct {
	tokenID int64
	tool    string
	args    [sha256.Size]byte
	seen    confirmSeen
	created time.Time
	expires time.Time
}

// confirmStore holds codes in memory only; a restart voids them.
type confirmStore struct {
	mu  sync.Mutex
	m   map[string]confirmEntry
	now func() time.Time
}

func newConfirmStore(now func() time.Time) *confirmStore {
	return &confirmStore{m: map[string]confirmEntry{}, now: now}
}

// add stores e under a new code and returns the code, dropping the token's
// oldest code past the limit.
func (c *confirmStore) add(e confirmEntry) (string, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for code, o := range c.m {
		if !now.Before(o.expires) {
			delete(c.m, code)
		}
	}
	oldest, n := "", 0
	for code, o := range c.m {
		if o.tokenID != e.tokenID {
			continue
		}
		n++
		if oldest == "" || o.created.Before(c.m[oldest].created) {
			oldest = code
		}
	}
	if n >= maxConfirmsPerToken {
		delete(c.m, oldest)
	}
	e.created, e.expires = now, now.Add(confirmTTL)
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// The system's random source failing is not survivable.
		panic("read random bytes: " + err.Error())
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	c.m[code] = e
	return code, e.expires
}

// spend removes the code, whatever the call, and returns its seen state's
// hash when the call matches.
func (c *confirmStore) spend(code string, tokenID int64, tool string, args [sha256.Size]byte) (confirmSeen, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[code]
	delete(c.m, code)
	if !ok || !c.now().Before(e.expires) || e.tokenID != tokenID || e.tool != tool || e.args != args {
		return confirmSeen{}, errConfirmRefused
	}
	return e.seen, nil
}

// confirmSeen is the hash of the state a preview showed (SPEC 21.8).
type confirmSeen [sha256.Size]byte

func hashSeen(seen string) confirmSeen { return sha256.Sum256([]byte(seen)) }

// moved reports whether the state now differs from the state the preview
// showed.
func (h confirmSeen) moved(now string) bool { return h != hashSeen(now) }

// confirmArgs returns the confirm code in a tool's arguments and the
// SHA-256 of the rest as canonical JSON: keys sorted, confirm_code left out.
func confirmArgs(in any) (string, [sha256.Size]byte, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		return "", [sha256.Size]byte{}, err
	}
	code, _ := m[confirmCodeKey].(string)
	delete(m, confirmCodeKey)
	// encoding/json writes map keys sorted, at every depth.
	canon, err := json.Marshal(m)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	return code, sha256.Sum256(canon), nil
}

// mcpToken is the token a call came with; the verifier always stores it.
func mcpToken(req *mcp.CallToolRequest) store.APIToken {
	if req == nil || req.Extra == nil {
		return store.APIToken{}
	}
	return mcpRequestToken(req.Extra.TokenInfo)
}

// mcpTool is the name of the tool a call is for.
func mcpTool(req *mcp.CallToolRequest) string {
	if req == nil || req.Params == nil {
		return ""
	}
	return req.Params.Name
}

// mcpAsk issues a code for this call's token, tool and arguments, keeping a
// hash of seen: the state the preview showed, so the confirm call can tell
// it moved (SPEC 21.8). It returns the preview's code fields; the tool
// fills the rest.
func (s *server) mcpAsk(req *mcp.CallToolRequest, in any, seen string) (mcpPreview, error) {
	_, args, err := confirmArgs(in)
	if err != nil {
		return mcpPreview{}, s.mcpFail("prepare the confirm code", err)
	}
	code, expires := s.confirms.add(confirmEntry{tokenID: mcpToken(req).ID, tool: mcpTool(req), args: args, seen: hashSeen(seen)})
	return mcpPreview{ConfirmCode: code, ExpiresAt: mcpTime(expires)}, nil
}

// spendIfCode spends the confirm code of a two-step call, when it has one,
// and returns the hash of the state its preview showed; seen.moved(now)
// compares it. Every two-step tool calls it first, before any refusal
// check, so a refused confirm still spends the code. Every refusal is
// errConfirmRefused.
func (s *server) spendIfCode(req *mcp.CallToolRequest, in any) (confirmSeen, error) {
	code, args, err := confirmArgs(in)
	if err != nil {
		return confirmSeen{}, errConfirmRefused
	}
	if code == "" {
		return confirmSeen{}, nil
	}
	return s.confirms.spend(code, mcpToken(req).ID, mcpTool(req), args)
}

// mcpRetryPreview answers a confirm call whose state moved: a fresh preview
// and code, marked as an error so nothing reads as done.
func mcpRetryPreview[O any](preview func() (*mcp.CallToolResult, O, error), format string, a ...any) (*mcp.CallToolResult, O, error) {
	res, out, err := preview()
	if res != nil {
		res = mcpText(format, a...)
		res.IsError = true
	}
	return res, out, err
}

// mcpDetached runs work that may outlast the call and has no Server page
// result for mcpRun to report (a clone, a world or snapshot delete). It
// waits up to mcpWait; finished is false when the work is still running.
// The work goes on if the client leaves, and shutdown waits for it.
// server_status does not show detached work: its result shows in the audit
// log and on the pages. A failure is logged only when the caller stopped
// waiting; otherwise the caller words it and logs what needs logging.
func (s *server) mcpDetached(ctx context.Context, do func(ctx context.Context) error) (finished bool, err error) {
	if !s.runs.track() {
		return false, errors.New("foreman is shutting down")
	}
	// claimed is set by whichever comes first: the work handing its result
	// to the caller, or the caller giving up on it.
	var claimed atomic.Bool
	done := make(chan error, 1)
	go func() {
		defer s.runs.wg.Done()
		err := do(s.runs.ctx)
		done <- err
		if !claimed.CompareAndSwap(false, true) && err != nil {
			s.log.Error("mcp: detached work failed", "error", err)
		}
	}()
	timer := time.NewTimer(mcpWait)
	defer timer.Stop()
	giveUp := func(err error) (bool, error) {
		if claimed.CompareAndSwap(false, true) {
			return false, err
		}
		return true, <-done
	}
	select {
	case err := <-done:
		return true, err
	case <-timer.C:
		return giveUp(nil)
	case <-ctx.Done():
		return giveUp(ctx.Err())
	}
}
