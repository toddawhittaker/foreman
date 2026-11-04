// Package rcontest is a fake RCON server for tests. It frames packets on its
// own, so it checks the client against the protocol and not against the
// client's own code. It behaves like the real game: an empty command gets no
// reply, and every other command gets exactly one reply packet of any length.
package rcontest

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
)

const (
	typeResponse = 0
	typeCommand  = 2
	typeAuth     = 3
)

// Config scripts the server. The zero value accepts the password "" and
// answers every command with an empty reply.
type Config struct {
	// Password is the one the server accepts.
	Password string
	// RefuseLogin rejects every login, whatever the password.
	RefuseLogin bool
	// Reply returns the reply to a command. Nil means an empty reply.
	Reply func(command string) string
	// Hang makes the server read commands and never answer them.
	Hang bool
}

// Server is a running fake.
type Server struct {
	cfg Config
	ln  net.Listener

	mu       sync.Mutex
	commands []string
	logins   int
	conns    []net.Conn
	closed   bool
	wg       sync.WaitGroup
}

// Start listens on a free local port and stops when the test ends.
func Start(t testing.TB, cfg Config) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("rcontest: listen: %v", err)
	}
	s := &Server{cfg: cfg, ln: ln}
	s.wg.Add(1)
	go s.accept()
	t.Cleanup(func() {
		_ = ln.Close()
		s.mu.Lock()
		s.closed = true
		for _, c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

// Addr is the host:port to dial.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Commands returns every non-empty command received, in order.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// Logins returns how many login attempts the server has seen.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { _ = conn.Close() }()
			s.serve(conn)
		}()
	}
}

func (s *Server) serve(conn net.Conn) {
	for {
		id, kind, body, err := read(conn)
		if err != nil {
			return
		}
		switch kind {
		case typeAuth:
			s.mu.Lock()
			s.logins++
			s.mu.Unlock()
			if s.cfg.RefuseLogin || body != s.cfg.Password {
				id = -1
			}
			if write(conn, id, typeCommand, "") != nil {
				return
			}
		case typeCommand:
			if !s.command(conn, id, body) {
				return
			}
		}
	}
}

// command answers one command; false means the connection is done.
func (s *Server) command(conn net.Conn, id int32, body string) bool {
	if s.cfg.Hang {
		return true
	}
	if body == "" {
		return true // the real game never answers an empty command
	}
	s.mu.Lock()
	s.commands = append(s.commands, body)
	s.mu.Unlock()
	reply := ""
	if s.cfg.Reply != nil {
		reply = s.cfg.Reply(body)
	}
	return write(conn, id, typeResponse, reply) == nil
}

func read(r io.Reader) (id, kind int32, body string, err error) {
	var head [4]byte
	if _, err = io.ReadFull(r, head[:]); err != nil {
		return 0, 0, "", err
	}
	buf := make([]byte, binary.LittleEndian.Uint32(head[:]))
	if _, err = io.ReadFull(r, buf); err != nil {
		return 0, 0, "", err
	}
	id = int32(binary.LittleEndian.Uint32(buf[0:4]))   //nolint:gosec // wire value
	kind = int32(binary.LittleEndian.Uint32(buf[4:8])) //nolint:gosec // wire value
	return id, kind, string(buf[8 : len(buf)-2]), nil
}

func write(w io.Writer, id, kind int32, body string) error {
	buf := binary.LittleEndian.AppendUint32(nil, uint32(10+len(body))) //nolint:gosec // test sizes are small
	buf = binary.LittleEndian.AppendUint32(buf, uint32(id))            //nolint:gosec // wire value
	buf = binary.LittleEndian.AppendUint32(buf, uint32(kind))          //nolint:gosec // wire value
	buf = append(buf, body...)
	buf = append(buf, 0, 0)
	_, err := w.Write(buf)
	return err
}
