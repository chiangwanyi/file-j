package remote

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/jiangwanyi/file-j/backend/internal/config"
	"github.com/jiangwanyi/file-j/backend/internal/credentials"
)

// Conn is one live SSH + SFTP connection.
type Conn struct {
	Session config.Session
	SSH     *ssh.Client
	SFTP    *sftp.Client

	home     string
	openedAt time.Time
	lastUsed atomic.Int64 // unix nano
	closed   atomic.Bool
}

// Touch marks the connection as recently used (idle reaper input).
func (c *Conn) Touch() { c.lastUsed.Store(time.Now().UnixNano()) }

// IdleFor returns how long the connection has been unused.
func (c *Conn) IdleFor() time.Duration {
	return time.Since(time.Unix(0, c.lastUsed.Load()))
}

// Close tears the connection down. Safe to call twice.
func (c *Conn) Close() {
	if c.closed.Swap(true) {
		return
	}
	if c.SFTP != nil {
		_ = c.SFTP.Close()
	}
	if c.SSH != nil {
		_ = c.SSH.Close()
	}
}

// Status is the connection state surfaced to the UI.
type Status struct {
	SessionID string    `json:"sessionId"`
	Connected bool      `json:"connected"`
	Home      string    `json:"home,omitempty"`
	Since     time.Time `json:"since,omitempty"`
}

// Manager keeps one connection per session ID alive and reuses it. Reconnecting
// on every directory change would be unusably slow over SSH.
type Manager struct {
	store  *config.Store
	creds  *credentials.Store
	logger *slog.Logger

	mu    sync.Mutex
	conns map[string]*Conn

	stopOnce sync.Once
	stop     chan struct{}
}

// NewManager wires the manager to the config store (for sessions and remote
// policy) and the credential store.
func NewManager(store *config.Store, creds *credentials.Store, logger *slog.Logger) *Manager {
	m := &Manager{
		store:  store,
		creds:  creds,
		logger: logger,
		conns:  map[string]*Conn{},
		stop:   make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

// Connect dials a saved session, reusing an existing connection when present.
// password may be empty when a credential is already stored or when key/agent
// authentication is used.
func (m *Manager) Connect(ctx context.Context, sessionID, password string) (*Conn, error) {
	m.mu.Lock()
	if c, ok := m.conns[sessionID]; ok {
		m.mu.Unlock()
		c.Touch()
		return c, nil
	}
	m.mu.Unlock()

	sess, ok := m.store.Session(sessionID)
	if !ok {
		return nil, fmt.Errorf("会话不存在: %s", sessionID)
	}
	cfg := m.store.Config()

	secret := password
	if secret == "" && sess.CredentialRef != "" {
		if v, ok := m.creds.Get(sess.CredentialRef); ok {
			secret = v
		}
	}

	conn, err := m.dial(ctx, sess, secret, cfg)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if existing, ok := m.conns[sessionID]; ok {
		// Another goroutine won the race; keep the older connection.
		m.mu.Unlock()
		conn.Close()
		existing.Touch()
		return existing, nil
	}
	m.conns[sessionID] = conn
	m.mu.Unlock()

	m.store.TouchSession(sessionID)
	m.logger.Info("ssh connected", "session", sess.Name, "host", sess.Host, "user", sess.User)
	return conn, nil
}

// Test dials with an unsaved session definition (the "test connection" button).
func (m *Manager) Test(ctx context.Context, sess config.Session, password string) (*Conn, error) {
	cfg := m.store.Config()
	conn, err := m.dial(ctx, sess, password, cfg)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Get returns a live connection for a session, dialling it if needed.
func (m *Manager) Get(ctx context.Context, sessionID string) (*Conn, error) {
	m.mu.Lock()
	c, ok := m.conns[sessionID]
	m.mu.Unlock()
	if ok && !c.closed.Load() {
		c.Touch()
		return c, nil
	}
	return m.Connect(ctx, sessionID, "")
}

// Disconnect closes and forgets a session's connection.
func (m *Manager) Disconnect(sessionID string) {
	m.mu.Lock()
	c, ok := m.conns[sessionID]
	delete(m.conns, sessionID)
	m.mu.Unlock()
	if ok {
		c.Close()
		m.logger.Info("ssh disconnected", "session", sessionID)
	}
}

// Statuses reports every live connection.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.conns))
	for id, c := range m.conns {
		out = append(out, Status{SessionID: id, Connected: !c.closed.Load(), Home: c.home, Since: c.openedAt})
	}
	return out
}

// Close shuts every connection down.
func (m *Manager) Close() {
	m.stopOnce.Do(func() { close(m.stop) })
	m.mu.Lock()
	conns := m.conns
	m.conns = map[string]*Conn{}
	m.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

func (m *Manager) reapLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			minutes := m.store.Config().Remote.IdleDisconnectMinutes
			if minutes <= 0 {
				continue
			}
			maxIdle := time.Duration(minutes) * time.Minute
			var stale []string
			m.mu.Lock()
			for id, c := range m.conns {
				if c.IdleFor() > maxIdle {
					stale = append(stale, id)
				}
			}
			m.mu.Unlock()
			for _, id := range stale {
				m.logger.Info("closing idle ssh connection", "session", id)
				m.Disconnect(id)
			}
		}
	}
}

func (m *Manager) dial(ctx context.Context, sess config.Session, password string, cfg config.Config) (*Conn, error) {
	config.NormalizeSession(&sess)
	if sess.Host == "" {
		return nil, errors.New("主机地址不能为空")
	}

	auths, closers, err := m.authMethods(sess, password, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	if len(auths) == 0 {
		return nil, errors.New("没有可用的认证方式")
	}

	hostport := net.JoinHostPort(sess.Host, strconv.Itoa(sess.Port))
	sshCfg := &ssh.ClientConfig{
		User: sess.User,
		Auth: auths,
		// Host key verification is handled by our own callback which also
		// records TOFU fingerprints in known_hosts.yaml.
		HostKeyCallback: hostKeyCallback(cfg.Remote.HostKeyPolicy, m.store, hostport, ""),
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512,
			ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSA,
		},
		Timeout: time.Duration(cfg.Remote.ConnectTimeoutSeconds) * time.Second,
	}

	timeout := sshCfg.Timeout
	dialer := &net.Dialer{Timeout: timeout}
	netConn, err := dialer.DialContext(ctx, "tcp", hostport)
	if err != nil {
		return nil, fmt.Errorf("无法连接到 %s: %w", hostport, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = netConn.SetDeadline(deadline)
	} else {
		_ = netConn.SetDeadline(time.Now().Add(timeout))
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, hostport, sshCfg)
	if err != nil {
		netConn.Close()
		var hkErr *HostKeyError
		if errors.As(err, &hkErr) {
			return nil, hkErr
		}
		return nil, fmt.Errorf("SSH 认证失败: %w", err)
	}
	_ = netConn.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, chans, reqs)

	opts := []sftp.ClientOption{
		sftp.MaxPacket(cfg.Remote.MaxPacketSize),
		sftp.UseConcurrentWrites(true),
		sftp.UseConcurrentReads(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	}
	sc, err := sftp.NewClient(client, opts...)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("SFTP 子系统启动失败: %w", err)
	}

	home, err := sc.Getwd()
	if err != nil || home == "" {
		home = "."
	}

	conn := &Conn{Session: sess, SSH: client, SFTP: sc, home: home, openedAt: time.Now()}
	conn.Touch()

	if cfg.Remote.KeepAliveSeconds > 0 {
		interval := time.Duration(cfg.Remote.KeepAliveSeconds) * time.Second
		go func() {
			t := time.NewTicker(interval)
			defer t.Stop()
			for range t.C {
				if conn.closed.Load() {
					return
				}
				if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
					m.logger.Warn("ssh keepalive failed", "session", sess.Name, "err", err)
					return
				}
			}
		}()
	}
	return conn, nil
}

// authMethods builds the ssh.AuthMethod list for a session, preferring the
// explicitly configured method and falling back to agent + keyboard-interactive
// so common server setups (2FA, agent forwarding) work without extra config.
func (m *Manager) authMethods(sess config.Session, password string, cfg config.Config) ([]ssh.AuthMethod, []interface{ Close() error }, error) {
	var auths []ssh.AuthMethod
	var closers []interface{ Close() error }

	switch sess.AuthMethod {
	case "key":
		keyPath := sess.KeyPath
		if keyPath == "" {
			keyPath = "~/.ssh/id_ed25519"
		}
		keyPath = expandHome(keyPath)
		raw, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("读取私钥失败 %s: %w", keyPath, err)
		}
		var signer ssh.Signer
		if password != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(password))
		} else {
			signer, err = ssh.ParsePrivateKey(raw)
		}
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				return nil, nil, errors.New("私钥已加密，需要提供口令")
			}
			return nil, nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	case "agent":
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, nil, errors.New("SSH_AUTH_SOCK 未设置，无法使用 ssh-agent")
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, nil, fmt.Errorf("连接 ssh-agent 失败: %w", err)
		}
		ag := agent.NewClient(conn)
		auths = append(auths, ssh.PublicKeysCallback(ag.Signers))
		closers = append(closers, conn)
	default: // password
		if password == "" {
			return nil, nil, errors.New("需要密码")
		}
		auths = append(auths, ssh.Password(password))
		// Many servers only offer keyboard-interactive; reuse the password for it.
		auths = append(auths, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = password
			}
			return answers, nil
		}))
	}
	return auths, closers, nil
}

func expandHome(p string) string {
	if p == "~" || len(p) > 1 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			if p == "~" {
				return home
			}
			return home + p[1:]
		}
	}
	return p
}
