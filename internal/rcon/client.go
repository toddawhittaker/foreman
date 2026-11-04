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
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// DefaultTimeout bounds a call whose context has no deadline.
const DefaultTimeout = 10 * time.Second

// ErrLogin is returned when the server refuses the password.
var ErrLogin = errors.New("rcon: the server refused the login")

// ErrBadCommand is returned for a command that cannot be framed.
var ErrBadCommand = errors.New("rcon: a command cannot contain a zero byte")

// Client talks to one RCON server. Create it with New. It dials on the first
// command and keeps the connection until a call fails; the next call then
// dials again.
type Client struct {
	addr     string
	password secret.Secret

	mu     sync.Mutex // held for a whole command, so only one runs at a time
	conn   net.Conn
	lastID int32
}

// New returns a client for host:port. It does not connect.
func New(addr string, password secret.Secret) *Client {
	return &Client{addr: addr, password: password}
}

// Close drops the connection, if any.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
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
// covers dialling, login and the reply; without one, DefaultTimeout applies.
func (c *Client) Exec(ctx context.Context, command string) (string, error) {
	if strings.IndexByte(command, 0) >= 0 {
		return "", ErrBadCommand
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
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

// connect dials and logs in, within ctx. The password is sent but never
// placed in an error.
func (c *Client) connect(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, fmt.Errorf("rcon: connect: %w", err)
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
