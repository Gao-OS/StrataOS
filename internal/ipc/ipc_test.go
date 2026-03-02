package ipc

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// --- types tests ---

func TestSuccessResponse(t *testing.T) {
	resp := SuccessResponse("req-1", map[string]string{"key": "value"})
	if !resp.OK {
		t.Error("OK should be true")
	}
	if resp.V != 1 {
		t.Errorf("V = %d, want 1", resp.V)
	}
	if resp.ReqID != "req-1" {
		t.Errorf("ReqID = %q, want %q", resp.ReqID, "req-1")
	}
	if resp.Error != nil {
		t.Error("Error should be nil")
	}
}

func TestErrorResponse(t *testing.T) {
	resp := ErrorResponse("req-2", ErrPermDenied, "access denied")
	if resp.OK {
		t.Error("OK should be false")
	}
	if resp.Error == nil {
		t.Fatal("Error should not be nil")
	}
	if resp.Error.Code != ErrPermDenied {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, ErrPermDenied)
	}
	if resp.Error.Name != "PERMISSION_DENIED" {
		t.Errorf("Error.Name = %q, want %q", resp.Error.Name, "PERMISSION_DENIED")
	}
	if resp.Error.Message != "access denied" {
		t.Errorf("Error.Message = %q, want %q", resp.Error.Message, "access denied")
	}
}

func TestFullErrorResponse(t *testing.T) {
	details := map[string]any{"field": "path"}
	resp := FullErrorResponse("req-f", ErrInvalidRequest, "INVALID_ARGUMENT", "bad path", details)
	if resp.OK {
		t.Error("OK should be false")
	}
	if resp.Error.Name != "INVALID_ARGUMENT" {
		t.Errorf("Error.Name = %q, want %q", resp.Error.Name, "INVALID_ARGUMENT")
	}
	if resp.Error.Details["field"] != "path" {
		t.Errorf("Error.Details[field] = %v, want %q", resp.Error.Details["field"], "path")
	}
}

func TestErrorResponse_AllCodes(t *testing.T) {
	codes := map[int]string{
		ErrInvalidRequest:  "INVALID_ARGUMENT",
		ErrAuthRequired:    "UNAUTHENTICATED",
		ErrPermDenied:      "PERMISSION_DENIED",
		ErrNotFound:        "NOT_FOUND",
		ErrInternal:        "INTERNAL",
		ErrUnavailable:     "UNAVAILABLE",
		ErrResourceExhaust: "RESOURCE_EXHAUSTED",
		ErrConflict:        "CONFLICT",
	}
	for code, name := range codes {
		resp := ErrorResponse("req", code, "msg")
		if resp.Error.Name != name {
			t.Errorf("code %d: Name = %q, want %q", code, resp.Error.Name, name)
		}
	}
}

func TestResponseJSON_RoundTrip(t *testing.T) {
	resp := SuccessResponse("req-3", map[string]any{"count": float64(42)})
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Response
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ReqID != "req-3" {
		t.Errorf("ReqID = %q, want %q", got.ReqID, "req-3")
	}
	if !got.OK {
		t.Error("OK should be true")
	}
}

func TestRequestJSON_RoundTrip(t *testing.T) {
	req := Request{
		V:      1,
		ReqID:  "req-4",
		Method: "fs.open",
		Auth:   &Auth{Token: "v2.public.xxx"},
		Params: map[string]any{"path": "/tmp/foo"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Method != "fs.open" {
		t.Errorf("Method = %q, want %q", got.Method, "fs.open")
	}
	if got.Auth == nil || got.Auth.Token != "v2.public.xxx" {
		t.Error("Auth.Token mismatch")
	}
}

// --- frame tests ---

func TestWriteReadFrame_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	defer os.Remove(sock)

	payload := map[string]string{"hello": "world"}
	errCh := make(chan error, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		errCh <- WriteFrame(conn, payload)
	}()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	data, err := ReadFrame(conn)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	if wErr := <-errCh; wErr != nil {
		t.Fatalf("WriteFrame: %v", wErr)
	}

	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["hello"] != "world" {
		t.Errorf("got %v, want hello=world", got)
	}
}

func TestReadFrame_TooLarge(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	defer os.Remove(sock)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Write a header claiming 2 MiB (exceeds 1 MiB limit).
		header := make([]byte, 4)
		binary.BigEndian.PutUint32(header, 2<<20)
		conn.Write(header)
	}()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	_, err = ReadFrame(conn)
	if err == nil {
		t.Error("expected error for oversized frame")
	}
}

// --- Server integration test ---

func TestServer_HandleAndSendRequest(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "server.sock")

	srv := NewServer(sock)
	srv.Handle("test.echo", func(req *Request) Response {
		return SuccessResponse(req.ReqID, req.Params)
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	resp, err := SendRequest(sock, &Request{
		V:      1,
		ReqID:  "echo-1",
		Method: "test.echo",
		Params: map[string]any{"msg": "hello"},
	})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected OK, got error: %v", resp.Error)
	}
	if resp.ReqID != "echo-1" {
		t.Errorf("ReqID = %q, want %q", resp.ReqID, "echo-1")
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "server.sock")

	srv := NewServer(sock)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	resp, err := SendRequest(sock, &Request{
		V:      1,
		ReqID:  "unk-1",
		Method: "nonexistent.method",
	})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if resp.OK {
		t.Error("expected error for unknown method")
	}
	if resp.Error.Code != ErrInvalidRequest {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, ErrInvalidRequest)
	}
}

func TestServer_BadProtocolVersion(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "server.sock")

	srv := NewServer(sock)
	srv.Handle("test.ping", func(req *Request) Response {
		return SuccessResponse(req.ReqID, nil)
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	resp, err := SendRequest(sock, &Request{
		V:      99,
		ReqID:  "bad-v",
		Method: "test.ping",
	})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if resp.OK {
		t.Error("expected error for bad protocol version")
	}
}
