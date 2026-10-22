// Command file-jd is the business backend of file-j.
//
// It is started as a Tauri sidecar, binds a random port on 127.0.0.1, and prints
// a single JSON "ready" line on stdout so the Rust shell can tell the front-end
// where to connect. All application logic lives here, never in Rust.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jiangwanyi/file-j/backend/internal/app"
)

// version is overridden at build time:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/file-jd
var version = "0.1.1-dev"

func main() {
	var (
		configDir = flag.String("config-dir", "", "YAML configuration directory (default: the per-user config dir + /file-j, e.g. ~/Library/Application Support/file-j on macOS, %APPDATA%\\file-j on Windows)")
		token     = flag.String("token", "", "shared secret required by the WebSocket handshake")
		addr      = flag.String("addr", "127.0.0.1:0", "loopback address to bind; port 0 lets the OS choose")
		logLevel  = flag.String("log-level", "info", "debug | info | warn | error")
	)
	flag.Parse()

	dir, err := resolveConfigDir(*configDir)
	if err != nil {
		fatal("resolve config dir", err)
	}

	logger, logFile, err := newLogger(dir, *logLevel)
	if err != nil {
		fatal("init logger", err)
	}
	if logFile != nil {
		defer logFile.Close()
	}
	slog.SetDefault(logger)

	handshakeToken := *token
	if handshakeToken == "" {
		handshakeToken = randomToken()
	}

	application, err := app.New(app.Options{
		Version:   version,
		ConfigDir: dir,
		Token:     handshakeToken,
		Logger:    logger,
	})
	if err != nil {
		fatal("start backend", err)
	}
	defer application.Close()

	listener, err := application.Listen(*addr)
	if err != nil {
		fatal("listen", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	// The handshake is intentionally the only thing ever written to stdout:
	// stdout belongs to the parent process, logs go to stderr.
	writeHandshake(map[string]any{
		"type":      "ready",
		"port":      port,
		"token":     handshakeToken,
		"pid":       os.Getpid(),
		"version":   version,
		"configDir": dir,
		"logFile":   loggerPath(logFile),
	})

	go func() {
		if err := application.Serve(listener); err != nil {
			logger.Debug("http server stopped", "err", err)
		}
	}()

	parent := os.Getppid()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	logger.Info("file-j backend ready", "version", version, "port", port)
	for {
		select {
		case <-sig:
			logger.Info("signal received, shutting down")
			return
		case <-application.StopCh():
			logger.Info("shutdown requested by client")
			return
		case <-ticker.C:
			// Orphan protection: if the Tauri window dies without cleaning up,
			// this process must not linger holding SSH connections.
			if parent > 1 && os.Getppid() != parent {
				logger.Info("parent process exited, shutting down")
				return
			}
		}
	}
}

func resolveConfigDir(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "file-j"), nil
}

func newLogger(dir, level string) (*slog.Logger, *os.File, error) {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}

	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(logDir, "backend.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 4<<20 {
		_ = os.Remove(path) // simple size cap; one previous run is never needed
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	w := io.MultiWriter(os.Stderr, f)
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lv})), f, nil
}

func loggerPath(f *os.File) string {
	if f == nil {
		return ""
	}
	return f.Name()
}

func writeHandshake(v map[string]any) {
	data, err := json.Marshal(v)
	if err != nil {
		fatal("marshal handshake", err)
	}
	fmt.Fprintln(os.Stdout, string(data))
	_ = os.Stdout.Sync()
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		fatal("generate token", err)
	}
	return hex.EncodeToString(b)
}

func fatal(what string, err error) {
	fmt.Fprintf(os.Stderr, "file-j backend: %s: %v\n", what, err)
	os.Exit(1)
}
