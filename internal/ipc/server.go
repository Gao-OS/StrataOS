package ipc

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
)

// Handler processes a single IPC request and returns a response.
type Handler func(req *Request) Response

// Server listens on a Unix domain socket and dispatches requests to handlers.
type Server struct {
	socketPath string
	handlers   map[string]Handler
	listener   net.Listener
	mu         sync.RWMutex
	done       chan struct{}
}

func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
		done:       make(chan struct{}),
	}
}

// Handle registers a method handler. Must be called before Start.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

func (s *Server) Start() error {
	os.Remove(s.socketPath)
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.socketPath, err)
	}
	s.listener = ln
	log.Printf("[ipc] listening on %s", s.socketPath)

	go s.acceptLoop()
	return nil
}

func (s *Server) Stop() {
	close(s.done)
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.socketPath)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("[ipc] accept error: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	for {
		req, err := ReadRequest(conn)
		if err != nil {
			return
		}
		if req.V != 1 {
			WriteFrame(conn, ErrorResponse(req.ReqID, ErrInvalidRequest, "unsupported protocol version"))
			continue
		}

		s.mu.RLock()
		h, ok := s.handlers[req.Method]
		s.mu.RUnlock()

		if !ok {
			WriteFrame(conn, ErrorResponse(req.ReqID, ErrInvalidRequest, fmt.Sprintf("unknown method: %s", req.Method)))
			continue
		}
		WriteFrame(conn, h(req))
	}
}

// SendRequest connects to a UDS, sends one request, and reads one response.
func SendRequest(socketPath string, req *Request) (*Response, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := WriteFrame(conn, req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	data, err := ReadFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}
