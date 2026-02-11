package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
)

const maxFrameSize = 1 << 20 // 1 MiB

// WriteFrame marshals v as JSON and writes it as a length-prefixed frame.
// Wire format: 4-byte big-endian length || JSON payload.
func WriteFrame(conn net.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(data)))
	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// ReadFrame reads a length-prefixed frame and returns the raw JSON bytes.
func ReadFrame(conn net.Conn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header)
	if size > maxFrameSize {
		return nil, fmt.Errorf("frame too large: %d bytes", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	return data, nil
}

// ReadRequest reads a single framed Request from the connection.
func ReadRequest(conn net.Conn) (*Request, error) {
	data, err := ReadFrame(conn)
	if err != nil {
		return nil, err
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("unmarshal request: %w", err)
	}
	return &req, nil
}
