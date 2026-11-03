package rcon

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Packet types of the Source RCON protocol. A command and a login reply
// share the value 2; the direction tells them apart.
const (
	typeResponse int32 = 0
	typeCommand  int32 = 2
	typeAuthResp int32 = 2
	typeAuth     int32 = 3
)

const (
	// A packet is id, type, body and two zero bytes after the size field.
	minPacketSize = 10
	// Larger than any reply the game sends; stops a bad peer from making us
	// allocate without limit.
	maxPacketSize = 1 << 20
)

type packet struct {
	id   int32
	kind int32
	body string
}

func writePacket(w io.Writer, p packet) error {
	buf := make([]byte, 0, 4+minPacketSize+len(p.body))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(minPacketSize+len(p.body))) //nolint:gosec // length is bounded by the caller
	buf = binary.LittleEndian.AppendUint32(buf, uint32(p.id))                      //nolint:gosec // ids are small positive numbers or -1
	buf = binary.LittleEndian.AppendUint32(buf, uint32(p.kind))                    //nolint:gosec // kinds are small constants
	buf = append(buf, p.body...)
	buf = append(buf, 0, 0)
	_, err := w.Write(buf)
	return err
}

func readPacket(r io.Reader) (packet, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return packet{}, err
	}
	size := int32(binary.LittleEndian.Uint32(head[:])) //nolint:gosec // checked against the limits just below
	if size < minPacketSize || size > maxPacketSize {
		return packet{}, fmt.Errorf("rcon: packet size %d is outside what the protocol allows", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return packet{}, err
	}
	return packet{
		id:   int32(binary.LittleEndian.Uint32(buf[0:4])), //nolint:gosec // wire value, -1 is meaningful
		kind: int32(binary.LittleEndian.Uint32(buf[4:8])), //nolint:gosec // wire value
		body: string(buf[8 : size-2]),
	}, nil
}
