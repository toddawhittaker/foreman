// Package rcon is Foreman's hand-written RCON client (STACK.md section 6,
// SPEC 8.4). It sends one command at a time and never puts the password or
// the command text in an error (SPEC 14.8): a command can carry a secret.
package rcon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// DefaultTimeout bounds a call whose context has no deadline.
const DefaultTimeout = 10 * time.Second

// ErrLogin is returned when the server refuses the password.
var ErrLogin = errors.New("rcon: the server refused the login")

// ErrBadCommand is returned for a command that cannot be framed.
var ErrBadCommand = errors.New("rcon: a command cannot contain a zero byte")

// Client talks to one RCON server. Create it with New or NewChecked; the
// zero value is not usable. It dials on the first command and keeps the
// connection until a call fails; the next call then dials again.
type Client struct {
	addr     string
	password secret.Secret
	check    func(context.Context) error // nil: none

	// sem is a one-slot semaphore held for a whole command, so only one
	// runs at a time. Unlike a mutex, a caller waiting for it can give up
	// when its context ends.
	sem    chan struct{}
	conn   net.Conn
	lastID int32
}

// New returns a client for host:port. It does not connect.
func New(addr string, password secret.Secret) *Client {
	return &Client{addr: addr, password: password, sem: make(chan struct{}, 1)}
}

// NewChecked is New with a check run on each new connection after it is
// made and before the login is sent, within the call's context. When the
// check fails the connection is closed unused, the call fails with the
// check's error, and the next call dials and checks again. check runs while
// the client is held, so it must not wait for anything that waits for this
// client. It narrows, but cannot close, the time between the check and the
// login in which the server at addr could change.
func NewChecked(addr string, password secret.Secret, check func(context.Context) error) *Client {
	c := New(addr, password)
	c.check = check
	return c
}

// Close drops the connection, if any. It waits for a command running now.
func (c *Client) Close() error {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	return c.drop()
}

func (c *Client) drop() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Exec runs one command and returns its reply. The context's deadline
// covers waiting for a command already running, dialling, login and the
// reply; without one, DefaultTimeout applies.
func (c *Client) Exec(ctx context.Context, command string) (string, error) {
	if strings.IndexByte(command, 0) >= 0 {
		return "", ErrBadCommand
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return "", fmt.Errorf("rcon: %w", ctx.Err())
	}
	defer func() { <-c.sem }()

	reply, err := c.exec(ctx, command)
	if err != nil {
		// The stream may be out of step; start clean next time.
		_ = c.drop()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("rcon: %w", ctxErr)
		}
		// The socket can time out a moment before the context reports it.
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return "", fmt.Errorf("rcon: %w", context.DeadlineExceeded)
		}
		return "", err
	}
	return reply, nil
}

func (c *Client) exec(ctx context.Context, command string) (string, error) {
	if c.conn == nil {
		conn, err := c.connect(ctx)
		if err != nil {
			return "", err
		}
		c.conn = conn
	}
	conn := c.conn
	stop := limit(ctx, conn)
	defer stop()
	if err := setDeadline(ctx, conn); err != nil {
		return "", err
	}

	id := c.nextID()
	if err := writePacket(conn, packet{id: id, kind: typeCommand, body: command}); err != nil {
		return "", fmt.Errorf("rcon: send command: %w", err)
	}
	// The game answers every non-empty command with one packet, however long.
	for {
		p, err := readPacket(conn)
		if err != nil {
			return "", fmt.Errorf("rcon: read reply: %w", err)
		}
		if p.id == id && p.kind == typeResponse {
			return p.body, nil
		}
	}
}

// connect dials, runs the check, and logs in, within ctx. The password is
// sent but never placed in an error.
func (c *Client) connect(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, fmt.Errorf("rcon: connect: %w", err)
	}
	// Check after connecting and before sending credentials. This reads
	// current state; it cannot prove which container accepted the connection.
	if c.check != nil {
		if err := c.check(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("rcon: check before login: %w", err)
		}
	}
	stop := limit(ctx, conn)
	defer stop()
	if err := setDeadline(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	id := c.nextID()
	if err := writePacket(conn, packet{id: id, kind: typeAuth, body: c.password.Reveal()}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rcon: send login: %w", err)
	}
	for {
		p, err := readPacket(conn)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("rcon: read login reply: %w", err)
		}
		if p.kind != typeAuthResp {
			continue // some servers send an empty response first
		}
		if p.id != id {
			_ = conn.Close()
			return nil, ErrLogin
		}
		return conn, nil
	}
}

// limit makes a cancelled context unblock a read or write on conn by
// expiring its deadline. Call the returned function when done.
func limit(ctx context.Context, conn net.Conn) (stop func() bool) {
	return context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
}

func setDeadline(ctx context.Context, conn net.Conn) error {
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("rcon: set deadline: %w", err)
	}
	return nil
}

// nextID returns a packet id: positive, never -1 (which means a failed login).
func (c *Client) nextID() int32 {
	if c.lastID >= math.MaxInt32-1 {
		c.lastID = 0
	}
	c.lastID++
	return c.lastID
}
