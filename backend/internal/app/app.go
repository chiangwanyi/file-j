// Package app wires configuration, file-system providers, SSH sessions and the
// transfer engine into the set of RPC methods the UI calls.
//
// This is the only package that knows about all the others; every other package
// is reusable in isolation and is tested that way.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jiangwanyi/file-j/backend/internal/config"
	"github.com/jiangwanyi/file-j/backend/internal/credentials"
	"github.com/jiangwanyi/file-j/backend/internal/fsx"
	"github.com/jiangwanyi/file-j/backend/internal/ipc"
	"github.com/jiangwanyi/file-j/backend/internal/protocol"
	"github.com/jiangwanyi/file-j/backend/internal/remote"
	"github.com/jiangwanyi/file-j/backend/internal/transfer"
)

// Options configures the backend process.
type Options struct {
	Version   string
	ConfigDir string
	Token     string
	Logger    *slog.Logger
}

// App is the composed backend.
type App struct {
	version string
	started time.Time
	logger  *slog.Logger

	store  *config.Store
	creds  *credentials.Store
	local  *fsx.Local
	remote *remote.Manager
	engine *transfer.Engine
	ipc    *ipc.Server

	stopOnce sync.Once
	stopCh   chan struct{}
}

// New builds every subsystem and registers all RPC handlers.
func New(opts Options) (*App, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	store, err := config.Open(opts.ConfigDir)
	if err != nil {
		return nil, err
	}
	local, err := fsx.NewLocal()
	if err != nil {
		return nil, err
	}
	creds := credentials.New("com.jiangwanyi.filej")
	manager := remote.NewManager(store, creds, opts.Logger)

	a := &App{
		version: opts.Version,
		started: time.Now(),
		logger:  opts.Logger,
		store:   store,
		creds:   creds,
		local:   local,
		remote:  manager,
		ipc:     ipc.New(opts.Token, opts.Logger),
		stopCh:  make(chan struct{}),
	}

	a.engine = transfer.New(
		func(ctx context.Context, ep transfer.Endpoint) (fsx.Provider, error) {
			return a.provider(ctx, ep.Side, ep.SessionID)
		},
		func(event string, payload any) { a.ipc.Broadcast(event, payload) },
		func() config.Config { return store.Config() },
	)

	a.registerHandlers()
	return a, nil
}

// Store exposes the config store (used by main for logging paths).
func (a *App) Store() *config.Store { return a.store }

// Handler returns the HTTP handler, mostly useful in tests.
func (a *App) Handler() http.Handler { return a.ipc.Handler() }

// Listen binds the loopback address (port 0 picks a free port).
func (a *App) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// Serve blocks until the HTTP server stops.
func (a *App) Serve(l net.Listener) error {
	srv := &http.Server{
		Handler:           a.ipc.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	a.logger.Info("backend listening", "addr", l.Addr().String(), "configDir", a.store.Dir())
	return srv.Serve(l)
}

// StopCh is closed when the UI asks the backend to quit.
func (a *App) StopCh() <-chan struct{} { return a.stopCh }

// Close releases every resource. Safe to call more than once.
func (a *App) Close() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		a.engine.Close()
		a.remote.Close()
		a.ipc.CloseAll()
	})
}

// registerHandlers declares the full RPC surface. Keeping it in one place makes
// the API contract easy to audit against the front-end client.
func (a *App) registerHandlers() {
	h := a.ipc.Handle

	h("app.info", a.handleInfo)
	h("app.shutdown", a.handleShutdown)
	h("config.get", a.handleConfigGet)
	h("config.set", a.handleConfigSet)
	h("config.reveal", a.handleConfigReveal)

	h("session.list", a.handleSessionList)
	h("session.save", a.handleSessionSave)
	h("session.delete", a.handleSessionDelete)
	h("session.connect", a.handleSessionConnect)
	h("session.disconnect", a.handleSessionDisconnect)
	h("session.test", a.handleSessionTest)
	h("session.status", a.handleSessionStatus)

	h("fs.list", a.handleList)
	h("fs.stat", a.handleStat)
	h("fs.home", a.handleHome)
	h("fs.places", a.handlePlaces)
	h("fs.mkdir", a.handleMkdir)
	h("fs.rename", a.handleRename)
	h("fs.delete", a.handleDelete)
	h("fs.open", a.handleOpen)
	h("fs.reveal", a.handleReveal)

	h("transfer.create", a.handleTransferCreate)
	h("transfer.list", a.handleTransferList)
	h("transfer.cancel", a.handleTransferCancel)
	h("transfer.retry", a.handleTransferRetry)
	h("transfer.remove", a.handleTransferRemove)
	h("transfer.clear", a.handleTransferClear)

	h("ui.state.get", a.handleUIStateGet)
	h("ui.state.set", a.handleUIStateSet)

	h("known_hosts.list", a.handleKnownHosts)
	h("known_hosts.delete", a.handleKnownHostDelete)
}

// ---------------------------------------------------------------- app / config

func (a *App) handleInfo(context.Context, json.RawMessage) (any, error) {
	cfg := a.store.Config()
	return map[string]any{
		"version":         a.version,
		"pid":             os.Getpid(),
		"platform":        runtime.GOOS,
		"arch":            runtime.GOARCH,
		"goVersion":       runtime.Version(),
		"configDir":       a.store.Dir(),
		"credentialStore": a.creds.Available(),
		"startedAt":       a.started,
		"concurrency":     cfg.Transfer.Concurrency,
	}, nil
}

func (a *App) handleShutdown(context.Context, json.RawMessage) (any, error) {
	// Answer first, then let main unblock and exit cleanly.
	go func() {
		time.Sleep(120 * time.Millisecond)
		a.Close()
	}()
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleConfigGet(context.Context, json.RawMessage) (any, error) {
	return map[string]any{
		"config": a.store.Config(),
		"paths": map[string]string{
			"configDir":  a.store.Dir(),
			"config":     a.store.Path("config.yaml"),
			"sessions":   a.store.Path("sessions.yaml"),
			"uiState":    a.store.Path("ui-state.yaml"),
			"knownHosts": a.store.Path("known_hosts.yaml"),
		},
		"credentialStore": a.creds.Available(),
	}, nil
}

func (a *App) handleConfigSet(_ context.Context, raw json.RawMessage) (any, error) {
	var in config.Config
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	out, err := a.store.SetConfig(in)
	if err != nil {
		return nil, protocol.Wrapf(protocol.CodeInternal, "保存配置失败", err.Error())
	}
	return out, nil
}

func (a *App) handleConfigReveal(context.Context, json.RawMessage) (any, error) {
	return nil, reveal(a.store.Dir())
}

// --------------------------------------------------------------------- sessions

func (a *App) handleSessionList(context.Context, json.RawMessage) (any, error) {
	sessions := a.store.Sessions()
	live := map[string]bool{}
	for _, st := range a.remote.Statuses() {
		live[st.SessionID] = st.Connected
	}
	out := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, map[string]any{
			"session":       s,
			"connected":     live[s.ID],
			"hasCredential": s.CredentialRef != "" && a.creds.Available(),
		})
	}
	return out, nil
}

type sessionSaveParams struct {
	Session       config.Session `json:"session"`
	Password      string         `json:"password"`
	ClearPassword bool           `json:"clearPassword"`
}

func (a *App) handleSessionSave(_ context.Context, raw json.RawMessage) (any, error) {
	var p sessionSaveParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	saved, err := a.store.SaveSession(p.Session)
	if err != nil {
		return nil, protocol.Wrapf(protocol.CodeBadRequest, err.Error(), "")
	}
	ref := "session/" + saved.ID

	if p.ClearPassword {
		_ = a.creds.Delete(ref)
		saved.CredentialRef = ""
		if _, err := a.store.SaveSession(saved); err != nil {
			return nil, err
		}
	} else if p.Password != "" {
		if !a.creds.Available() {
			saved.CredentialRef = ref
			_, _ = a.store.SaveSession(saved)
			return map[string]any{
				"session":   saved,
				"persisted": false,
				"warning":   "系统钥匙串不可用，密码仅在本次运行内有效",
			}, nil
		}
		if err := a.creds.Set(ref, p.Password); err != nil {
			// Keep the session usable for this run even when the OS Keychain is
			// unavailable, but tell the UI so it can warn the user.
			saved.CredentialRef = ref
			_, _ = a.store.SaveSession(saved)
			return map[string]any{
				"session":   saved,
				"persisted": false,
				"warning":   err.Error(),
			}, nil
		}
		saved.CredentialRef = ref
		if _, err := a.store.SaveSession(saved); err != nil {
			return nil, err
		}
	}
	return map[string]any{"session": saved, "persisted": true}, nil
}

func (a *App) handleSessionDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	a.remote.Disconnect(p.ID)
	_ = a.creds.Delete("session/" + p.ID)
	if err := a.store.DeleteSession(p.ID); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

type connectParams struct {
	ID       string `json:"id"`
	Password string `json:"password"`
}

func (a *App) handleSessionConnect(ctx context.Context, raw json.RawMessage) (any, error) {
	var p connectParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	conn, err := a.remote.Connect(ctx, p.ID, p.Password)
	if err != nil {
		return nil, mapRemoteError(err)
	}
	home, _ := conn.Home(ctx)
	return map[string]any{
		"sessionId":     p.ID,
		"connected":     true,
		"home":          home,
		"serverVersion": string(conn.SSH.ServerVersion()),
	}, nil
}

func (a *App) handleSessionDisconnect(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	a.remote.Disconnect(p.ID)
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleSessionStatus(context.Context, json.RawMessage) (any, error) {
	return a.remote.Statuses(), nil
}

type testParams struct {
	Session  config.Session `json:"session"`
	Password string         `json:"password"`
}

func (a *App) handleSessionTest(ctx context.Context, raw json.RawMessage) (any, error) {
	var p testParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	conn, err := a.remote.Test(ctx, p.Session, p.Password)
	if err != nil {
		return nil, mapRemoteError(err)
	}
	home, _ := conn.Home(ctx)
	version := string(conn.SSH.ServerVersion())
	conn.Close()
	return map[string]any{"ok": true, "home": home, "serverVersion": version}, nil
}

// ------------------------------------------------------------------ filesystem

type providerParams struct {
	Side      string `json:"side"`
	SessionID string `json:"sessionId"`
}

func (a *App) provider(ctx context.Context, side, sessionID string) (fsx.Provider, error) {
	switch side {
	case "local", "":
		return a.local, nil
	case "remote":
		if sessionID == "" {
			return nil, protocol.Errorf(protocol.CodeNotConnected, "尚未选择远程会话")
		}
		conn, err := a.remote.Get(ctx, sessionID)
		if err != nil {
			return nil, mapRemoteError(err)
		}
		return conn, nil
	default:
		return nil, protocol.Errorf(protocol.CodeBadRequest, "未知的文件系统类型: "+side)
	}
}

type listParams struct {
	providerParams
	Path       string `json:"path"`
	ShowHidden *bool  `json:"showHidden"`
}

func (a *App) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
	var p listParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	prov, err := a.provider(ctx, p.Side, p.SessionID)
	if err != nil {
		return nil, err
	}
	showHidden := a.store.Config().UI.ShowHiddenFiles
	if p.ShowHidden != nil {
		showHidden = *p.ShowHidden
	}
	res, err := prov.List(ctx, p.Path, showHidden)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (a *App) handleStat(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		providerParams
		Path string `json:"path"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	prov, err := a.provider(ctx, p.Side, p.SessionID)
	if err != nil {
		return nil, err
	}
	return prov.Stat(ctx, p.Path)
}

func (a *App) handleHome(ctx context.Context, raw json.RawMessage) (any, error) {
	var p providerParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	prov, err := a.provider(ctx, p.Side, p.SessionID)
	if err != nil {
		return nil, err
	}
	home, err := prov.Home(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"path": home}, nil
}

func (a *App) handlePlaces(context.Context, json.RawMessage) (any, error) {
	return a.local.QuickPlaces(), nil
}

type mkdirParams struct {
	providerParams
	Path string `json:"path"`
	Name string `json:"name"`
}

func (a *App) handleMkdir(ctx context.Context, raw json.RawMessage) (any, error) {
	var p mkdirParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := validateName(p.Name); err != nil {
		return nil, err
	}
	prov, err := a.provider(ctx, p.Side, p.SessionID)
	if err != nil {
		return nil, err
	}
	target := fsx.Join(p.Path, p.Name)
	if p.Side == "local" {
		target = a.local.Resolve(target)
	}
	if _, err := prov.Stat(ctx, target); err == nil {
		return nil, protocol.Errorf(protocol.CodeConflict, "同名文件或文件夹已存在")
	}
	if err := prov.MkdirAll(ctx, target, 0o755); err != nil {
		return nil, err
	}
	return map[string]string{"path": target}, nil
}

type renameParams struct {
	providerParams
	Path    string `json:"path"`
	NewName string `json:"newName"`
}

func (a *App) handleRename(ctx context.Context, raw json.RawMessage) (any, error) {
	var p renameParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := validateName(p.NewName); err != nil {
		return nil, err
	}
	prov, err := a.provider(ctx, p.Side, p.SessionID)
	if err != nil {
		return nil, err
	}
	from := p.Path
	if p.Side == "local" {
		from = a.local.Resolve(from)
	}
	to := fsx.Join(fsx.Parent(from), p.NewName)
	if from == to {
		return map[string]string{"path": to}, nil
	}
	if _, err := prov.Stat(ctx, to); err == nil {
		return nil, protocol.Errorf(protocol.CodeConflict, "目标名称已存在")
	}
	if err := prov.Rename(ctx, from, to); err != nil {
		return nil, err
	}
	return map[string]string{"path": to}, nil
}

type deleteParams struct {
	providerParams
	Paths     []string `json:"paths"`
	Permanent bool     `json:"permanent"`
}

func (a *App) handleDelete(ctx context.Context, raw json.RawMessage) (any, error) {
	var p deleteParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if len(p.Paths) == 0 {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "没有选择要删除的项目")
	}
	prov, err := a.provider(ctx, p.Side, p.SessionID)
	if err != nil {
		return nil, err
	}
	type failure struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	}
	var trashed, deleted int
	failures := []failure{}
	for _, path := range p.Paths {
		real := path
		if p.Side == "local" {
			real = a.local.Resolve(real)
		}
		if p.Side == "local" && !p.Permanent {
			ok, err := a.local.Trash(real)
			if err == nil && ok {
				trashed++
				continue
			}
		}
		if err := prov.Remove(ctx, real); err != nil {
			failures = append(failures, failure{Path: path, Message: err.Error()})
			continue
		}
		deleted++
	}
	return map[string]any{"trashed": trashed, "deleted": deleted, "failures": failures}, nil
}

func (a *App) handleOpen(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if runtime.GOOS != "darwin" {
		return nil, protocol.Errorf(protocol.CodeUnsupported, "仅支持 macOS")
	}
	if err := exec.Command("open", a.local.Resolve(p.Path)).Start(); err != nil {
		return nil, protocol.Wrapf(protocol.CodeInternal, "无法打开文件", err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleReveal(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	return nil, reveal(a.local.Resolve(p.Path))
}

// -------------------------------------------------------------------- transfer

func (a *App) handleTransferCreate(_ context.Context, raw json.RawMessage) (any, error) {
	var req transfer.CreateRequest
	if err := decode(raw, &req); err != nil {
		return nil, err
	}
	// Local paths are normalised here so the engine never has to know about "~".
	for i := range req.Items {
		if req.Items[i].From.Side == "local" {
			req.Items[i].From.Path = a.local.Resolve(req.Items[i].From.Path)
		}
		if req.Items[i].To.Side == "local" {
			req.Items[i].To.Path = a.local.Resolve(req.Items[i].To.Path)
		}
	}
	tasks, err := a.engine.Create(req)
	if err != nil {
		return nil, protocol.Wrapf(protocol.CodeBadRequest, err.Error(), "")
	}
	return tasks, nil
}

func (a *App) handleTransferList(context.Context, json.RawMessage) (any, error) {
	return a.engine.List(), nil
}

func (a *App) handleTransferCancel(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := a.engine.Cancel(p.ID); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleTransferRetry(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	t, err := a.engine.Retry(p.ID)
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, err.Error())
	}
	return t, nil
}

func (a *App) handleTransferRemove(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	a.engine.Remove(p.ID)
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleTransferClear(context.Context, json.RawMessage) (any, error) {
	a.engine.ClearFinished()
	return map[string]bool{"ok": true}, nil
}

// -------------------------------------------------------------------- ui state

func (a *App) handleUIStateGet(context.Context, json.RawMessage) (any, error) {
	return a.store.UIState(), nil
}

func (a *App) handleUIStateSet(_ context.Context, raw json.RawMessage) (any, error) {
	var st config.UIState
	if err := decode(raw, &st); err != nil {
		return nil, err
	}
	if err := a.store.SetUIState(st); err != nil {
		return nil, err
	}
	return st, nil
}

// ---------------------------------------------------------------- known hosts

func (a *App) handleKnownHosts(context.Context, json.RawMessage) (any, error) {
	return a.store.KnownHosts(), nil
}

func (a *App) handleKnownHostDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Host string `json:"host"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := a.store.DeleteKnownHost(p.Host); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

// -------------------------------------------------------------------- helpers

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return protocol.Errorf(protocol.CodeBadRequest, "缺少参数")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return protocol.Wrapf(protocol.CodeBadRequest, "参数格式错误", err.Error())
	}
	return nil
}

func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return protocol.Errorf(protocol.CodeBadRequest, "名称不能为空")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return protocol.Errorf(protocol.CodeBadRequest, "名称包含非法字符")
	}
	return nil
}

func reveal(path string) error {
	if runtime.GOOS != "darwin" {
		return protocol.Errorf(protocol.CodeUnsupported, "仅支持 macOS")
	}
	cmd := exec.Command("open", "-R", path)
	if err := cmd.Start(); err != nil {
		// "open -R" fails when the path itself is a folder on some versions;
		// fall back to opening the folder directly.
		if err2 := exec.Command("open", path).Start(); err2 != nil {
			return protocol.Wrapf(protocol.CodeInternal, "无法在 Finder 中显示", err.Error())
		}
	}
	return nil
}

// mapRemoteError turns SSH/SFTP failures into codes the UI can act on.
func mapRemoteError(err error) error {
	if err == nil {
		return nil
	}
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return pe
	}
	var hk *remote.HostKeyError
	if errors.As(err, &hk) {
		return protocol.Wrapf(protocol.CodeHostKey, hk.Error(), "")
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"),
		strings.Contains(msg, "no supported methods remain"),
		strings.Contains(msg, "需要密码"),
		strings.Contains(msg, "私钥"):
		return protocol.Wrapf(protocol.CodeAuthFailed, "认证失败，请检查用户名、密码或私钥", msg)
	case strings.Contains(msg, "connection refused"):
		return protocol.Wrapf(protocol.CodeConnect, "连接被拒绝，请检查主机地址与端口", msg)
	case strings.Contains(msg, "no such host"):
		return protocol.Wrapf(protocol.CodeConnect, "无法解析主机名", msg)
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "timed out"):
		return protocol.Wrapf(protocol.CodeConnect, "连接超时", msg)
	default:
		return protocol.Wrapf(protocol.CodeConnect, msg, "")
	}
}
