//go:build !windows

package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

// dialAgent connects to ssh-agent over its unix socket (SSH_AUTH_SOCK).
func dialAgent() (io.ReadWriteCloser, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK 未设置，无法使用 ssh-agent")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("连接 ssh-agent 失败: %w", err)
	}
	return conn, nil
}
