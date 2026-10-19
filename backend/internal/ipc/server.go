// Package ipc exposes the backend over a loopback-only WebSocket.
//
// Why WebSocket and not Tauri `invoke`?
//   - The Go process owns all business logic, so it needs its own transport; a
//     Rust command would have to proxy every call, putting logic back in Rust.
//   - Progress events for transfers are push-shaped; a socket gives the backend
//     a natural event channel.
//   - The same protocol works in a plain browser during front-end development.
package ipc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jiangwanyi/file-j/backend/internal/protocol"
)

// Handler implements one RPC method.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Server routes requests and fans events out to every connected window.
type Server struct {
	token    string
	logger   *slog.Logger
	upgrader websocket.Upgrader

	mu       sync.RWMutex
	handlers map[string]Handler
	clients  map[*client]struct{}
	closed   bool
}

// New creates a server that requires token on the WebSocket handshake.
func New(token string, logger *slog.Logger) *Server {
	s := &Server{
		token:    token,
		logger:   logger,
		handlers: map[string]Handler{},
		clients:  map[*client]struct{}{},
	}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
		// The backend only listens on 127.0.0.1 and additionally checks the
		// Origin header below, so a permissive check here is safe.
		CheckOrigin: s.checkOrigin,
	}
	return s
}

// Handle registers a method.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// Handler returns the HTTP mux: /ws for the socket, /health for the Rust shell.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/ws", s.handleWS)
	return mux
}

// Broadcast pushes an event to all connected clients. Slow clients are dropped
// rather than allowed to block the backend.
func (s *Server) Broadcast(event string, payload any) {
	msg, err := json.Marshal(protocol.Event{Type: "event", Event: event, Payload: payload})
	if err != nil {
		s.logger.Error("marshal event", "event", event, "err", err)
		return
	}
	s.mu.RLock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()

	for _, c := range clients {
		if err := c.write(websocket.TextMessage, msg); err != nil {
			s.logger.Warn("drop client", "err", err)
			c.close()
		}
	}
}

// ClientCount reports connected windows (used by app.info and tests).
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// CloseAll disconnects every client.
func (s *Server) CloseAll() {
	s.mu.Lock()
	s.closed = true
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clients = map[*client]struct{}{}
	s.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		s.logger.Warn("rejected websocket handshake", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("websocket upgrade failed", "err", err)
		return
	}
	c := &client{srv: s, conn: conn}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		conn.Close()
		return
	}
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	s.logger.Info("client connected", "remote", r.RemoteAddr)
	go c.readLoop()
}

func (s *Server) authorized(r *http.Request) bool {
	// Token check: constant time compare to avoid a timing oracle even on
	// loopback (another local user can still reach 127.0.0.1).
	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		return false
	}
	return s.checkOrigin(r)
}

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser client (curl, Go tests). Token already proved identity.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "tauri":
		return true
	case "http", "https":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return true
		}
		// Tauri serves the bundled front-end from these virtual hosts on Windows
		// and Linux; harmless to allow on macOS too.
		return host == "tauri.localhost"
	}
	return false
}

// client is one WebSocket connection.
type client struct {
	srv  *Server
	conn *websocket.Conn

	writeMu sync.Mutex
	once    sync.Once
}

func (c *client) close() {
	c.once.Do(func() {
		_ = c.conn.Close()
		c.srv.mu.Lock()
		delete(c.srv.clients, c)
		c.srv.mu.Unlock()
	})
}

func (c *client) write(kind int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(kind, data)
}

func (c *client) readLoop() {
	defer c.close()
	c.conn.SetReadLimit(8 << 20)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.srv.logger.Debug("client read error", "err", err)
			}
			return
		}
		var req protocol.Request
		if err := json.Unmarshal(data, &req); err != nil {
			c.srv.logger.Warn("bad frame", "err", err)
			continue
		}
		if req.Type != "request" || req.Method == "" {
			continue
		}
		// Each request runs on its own goroutine: a 15 second SSH handshake must
		// not block a directory refresh from the same window.
		go c.serve(req)
	}
}

func (c *client) serve(req protocol.Request) {
	resp := protocol.Response{ID: req.ID, Type: "response"}
	defer func() {
		if r := recover(); r != nil {
			c.srv.logger.Error("handler panic", "method", req.Method, "panic", r)
			resp.OK = false
			resp.Result = nil
			resp.Error = protocol.Errorf(protocol.CodeInternal, "后端处理请求时发生内部错误")
		}
		c.send(resp)
	}()

	c.srv.mu.RLock()
	h, ok := c.srv.handlers[req.Method]
	c.srv.mu.RUnlock()
	if !ok {
		resp.Error = protocol.Errorf(protocol.CodeBadRequest, "未知的请求方法: "+req.Method)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := h(ctx, req.Params)
	if err != nil {
		resp.Error = normalizeError(err)
		return
	}
	resp.OK = true
	resp.Result = result
}

func (c *client) send(resp protocol.Response) {
	msg, err := json.Marshal(resp)
	if err != nil {
		c.srv.logger.Error("marshal response", "id", resp.ID, "err", err)
		return
	}
	if err := c.write(websocket.TextMessage, msg); err != nil {
		c.close()
	}
}

// normalizeError converts Go errors into protocol errors the UI can branch on.
func normalizeError(err error) *protocol.Error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return pe
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return protocol.Wrapf(protocol.CodeNotFound, "路径不存在", err.Error())
	case errors.Is(err, fs.ErrPermission):
		return protocol.Wrapf(protocol.CodePermission, "没有权限访问该路径", err.Error())
	case errors.Is(err, context.Canceled):
		return protocol.Wrapf(protocol.CodeCanceled, "操作已取消", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return protocol.Wrapf(protocol.CodeInternal, "操作超时", err.Error())
	default:
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return protocol.Wrapf(protocol.CodeConnect, "网络连接超时", err.Error())
		}
		return protocol.Wrapf(protocol.CodeInternal, err.Error(), "")
	}
}
