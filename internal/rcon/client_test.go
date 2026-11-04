package rcon_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/rcon/rcontest"
	"github.com/toddawhittaker/foreman/internal/secret"
)

const (
	testPassword = "s3cret-rcon-pw" //nolint:gosec // a fixture, not a credential
	testCommand  = "/config set game-password hunter2"
)

func newClient(t *testing.T, addr string) *rcon.Client {
	t.Helper()
	c := rcon.New(addr, secret.New(testPassword))
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func ctxWithTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// requireClean fails if an error shows the password or the command text.
func requireClean(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, leak := range []string{testPassword, "hunter2"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q contains %q", err, leak)
		}
	}
}

func TestExecReturnsReply(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{
		Password: testPassword,
		Reply:    func(cmd string) string { return "ran " + cmd },
	})
	c := newClient(t, srv.Addr())
	got, err := c.Exec(ctxWithTimeout(t, 5*time.Second), "/players")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ran /players" {
		t.Errorf("reply = %q", got)
	}
	if cmds := srv.Commands(); len(cmds) != 1 || cmds[0] != "/players" {
		t.Errorf("server saw %q", cmds)
	}
}

func TestExecEmptyReply(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: testPassword})
	c := newClient(t, srv.Addr())
	got, err := c.Exec(ctxWithTimeout(t, 5*time.Second), "/silent")
	if err != nil || got != "" {
		t.Fatalf("reply = %q, err = %v", got, err)
	}
}

func TestExecReadsLongReplyInOnePacket(t *testing.T) {
	// The real game sent 63,000 bytes as a single packet.
	long := strings.Repeat("0123456789", 6300)
	srv := rcontest.Start(t, rcontest.Config{
		Password: testPassword,
		Reply:    func(string) string { return long },
	})
	c := newClient(t, srv.Addr())
	got, err := c.Exec(ctxWithTimeout(t, 5*time.Second), "/big")
	if err != nil {
		t.Fatal(err)
	}
	if got != long {
		t.Errorf("reply has %d bytes, want %d", len(got), len(long))
	}
	// Back-to-back commands are answered in order, one packet each.
	for _, cmd := range []string{"/a", "/b"} {
		if _, err := c.Exec(ctxWithTimeout(t, 5*time.Second), cmd); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecSendsOnlyTheCommand(t *testing.T) {
	// A server that answers only non-empty commands must not be asked an empty one.
	srv := rcontest.Start(t, rcontest.Config{
		Password: testPassword,
		Reply:    func(cmd string) string { return "ok " + cmd },
	})
	c := newClient(t, srv.Addr())
	start := time.Now()
	got, err := c.Exec(ctxWithTimeout(t, 5*time.Second), "/players")
	if err != nil || got != "ok /players" {
		t.Fatalf("reply = %q, err = %v", got, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}

func TestWrongPasswordIsErrLogin(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: "something-else"})
	c := newClient(t, srv.Addr())
	_, err := c.Exec(ctxWithTimeout(t, 5*time.Second), testCommand)
	if !errors.Is(err, rcon.ErrLogin) {
		t.Fatalf("err = %v, want ErrLogin", err)
	}
	requireClean(t, err)
}

func TestRefusedLoginIsErrLogin(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: testPassword, RefuseLogin: true})
	c := newClient(t, srv.Addr())
	_, err := c.Exec(ctxWithTimeout(t, 5*time.Second), "/players")
	if !errors.Is(err, rcon.ErrLogin) {
		t.Fatalf("err = %v, want ErrLogin", err)
	}
}

func TestHangingServerTimesOut(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: testPassword, Hang: true})
	c := newClient(t, srv.Addr())
	start := time.Now()
	_, err := c.Exec(ctxWithTimeout(t, 200*time.Millisecond), testCommand)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	requireClean(t, err)
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestCancelStopsAHangingCommand(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: testPassword, Hang: true})
	c := newClient(t, srv.Addr())
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	done := make(chan error, 1)
	go func() {
		_, err := c.Exec(ctx, "/players")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Exec did not return after cancel")
	}
}

func TestSilentServerDuringLoginTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	c := newClient(t, ln.Addr().String())
	_, err = c.Exec(ctxWithTimeout(t, 200*time.Millisecond), "/players")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	requireClean(t, err)
}

func TestConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	c := newClient(t, addr)
	_, err = c.Exec(ctxWithTimeout(t, 5*time.Second), testCommand)
	requireClean(t, err)
}

func TestExpiredContextFailsAtOnce(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: testPassword})
	c := newClient(t, srv.Addr())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Exec(ctx, "/players")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
}

func TestCommandsRunOneAtATime(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{
		Password: testPassword,
		Reply:    func(cmd string) string { return "echo:" + cmd },
	})
	c := newClient(t, srv.Addr())
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := fmt.Sprintf("/cmd-%d", i)
			got, err := c.Exec(ctxWithTimeout(t, 5*time.Second), cmd)
			if err == nil && got != "echo:"+cmd {
				err = fmt.Errorf("command %q got reply %q", cmd, got)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if n := srv.Logins(); n != 1 {
		t.Errorf("logins = %d, want one shared connection", n)
	}
}

func TestReconnectsAfterClose(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: testPassword})
	c := newClient(t, srv.Addr())
	for range 2 {
		if _, err := c.Exec(ctxWithTimeout(t, 5*time.Second), "/players"); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if n := srv.Logins(); n != 2 {
		t.Errorf("logins = %d, want 2", n)
	}
}

func TestZeroByteInCommandIsRefused(t *testing.T) {
	srv := rcontest.Start(t, rcontest.Config{Password: testPassword})
	c := newClient(t, srv.Addr())
	_, err := c.Exec(ctxWithTimeout(t, 5*time.Second), "/a\x00b")
	if !errors.Is(err, rcon.ErrBadCommand) {
		t.Fatalf("err = %v, want ErrBadCommand", err)
	}
	if len(srv.Commands()) != 0 || srv.Logins() != 0 {
		t.Error("nothing should reach the server")
	}
}

func TestOversizedPacketIsRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var buf [64]byte
		_, _ = conn.Read(buf[:])
		_, _ = conn.Write(binary.LittleEndian.AppendUint32(nil, 0x7fffffff))
	}()
	c := newClient(t, ln.Addr().String())
	_, err = c.Exec(ctxWithTimeout(t, 5*time.Second), "/players")
	requireClean(t, err)
}
