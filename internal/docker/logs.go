package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Stream says which output a log line came from.
type Stream byte

// The numbers are the ones in Docker's log framing.
const (
	Stdout Stream = 1
	Stderr Stream = 2
)

// LogLine is one line of container output, without its line ending.
type LogLine struct {
	Stream Stream
	Text   string
}

// LogOptions picks what Logs returns.
type LogOptions struct {
	Follow bool      // keep the stream open for new output
	Since  time.Time // only output at or after this time; zero means from the start
	// Timestamps asks Docker to start each line with the time it was
	// written, in RFC 3339 with nanoseconds, and a space.
	Timestamps bool
}

const (
	// maxFrame is the largest log frame accepted from the daemon.
	maxFrame = 1 << 20
	// maxLine is the longest line returned; longer ones are cut there.
	maxLine = 64 << 10
)

// LogStream reads a container's output as separate lines. It is for
// containers without a terminal, whose output Docker frames by stream.
type LogStream struct {
	body    io.ReadCloser
	r       *bufio.Reader
	partial [3][]byte // text of a line not yet ended, by Stream
	queue   []LogLine
}

// Logs opens a container's output. With Follow, the stream stays open until
// the container stops or ctx ends; ctx is the only limit. Close it when done.
func (c *Client) Logs(ctx context.Context, id string, opts LogOptions) (*LogStream, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	q := url.Values{
		"stdout": {"1"}, "stderr": {"1"},
		"follow": {strconv.FormatBool(opts.Follow)},
		"tail":   {"all"},
	}
	if opts.Timestamps {
		q.Set("timestamps", "1")
	}
	if !opts.Since.IsZero() {
		q.Set("since", fmt.Sprintf("%d.%09d", opts.Since.Unix(), opts.Since.Nanosecond()))
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/containers/" + id + "/logs", query: q, long: opts.Follow}) //nolint:bodyclose // the LogStream closes it
	if err != nil {
		return nil, err
	}
	return newLogStream(resp.Body), nil
}

func newLogStream(body io.ReadCloser) *LogStream {
	return &LogStream{body: body, r: bufio.NewReader(body)}
}

// Close ends the stream.
func (s *LogStream) Close() error { return s.body.Close() }

// Next returns the next line, and io.EOF once the stream has ended and every
// line, including a last one with no line ending, has been returned.
func (s *LogStream) Next() (LogLine, error) {
	for len(s.queue) == 0 {
		if err := s.readFrame(); err != nil {
			return LogLine{}, err
		}
	}
	line := s.queue[0]
	s.queue = s.queue[1:]
	return line, nil
}

func (s *LogStream) readFrame() error {
	var hdr [8]byte
	_, err := io.ReadFull(s.r, hdr[:])
	if errors.Is(err, io.EOF) {
		for _, st := range []Stream{Stdout, Stderr} {
			if len(s.partial[st]) > 0 {
				s.emit(st)
			}
		}
		if len(s.queue) > 0 {
			return nil
		}
		return io.EOF
	}
	if err != nil {
		return fmt.Errorf("the log stream broke off: %w", err)
	}
	st := Stream(hdr[0])
	size := binary.BigEndian.Uint32(hdr[4:])
	if (st != Stdout && st != Stderr) || hdr[1]|hdr[2]|hdr[3] != 0 || size > maxFrame {
		return errors.New("the log stream is not in Docker's framed format; the container may have a terminal")
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(s.r, payload); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return fmt.Errorf("the log stream broke off: %w", err)
	}
	for len(payload) > 0 {
		seg, rest, ended := bytes.Cut(payload, []byte("\n"))
		payload = rest
		for len(s.partial[st])+len(seg) > maxLine {
			room := maxLine - len(s.partial[st])
			s.partial[st] = append(s.partial[st], seg[:room]...)
			seg = seg[room:]
			s.emit(st)
		}
		s.partial[st] = append(s.partial[st], seg...)
		if ended {
			s.emit(st)
		}
	}
	return nil
}

func (s *LogStream) emit(st Stream) {
	text := bytes.TrimSuffix(s.partial[st], []byte("\r"))
	s.queue = append(s.queue, LogLine{Stream: st, Text: string(text)})
	s.partial[st] = s.partial[st][:0]
}
