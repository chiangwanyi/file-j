//go:build windows

package remote

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Microsoft/go-winio"
)

// OpenSSH for Windows exposes the agent on a named pipe instead of a unix
// socket, so SSH_AUTH_SOCK is usually unset. When it is set (Git Bash, WSL
// interop) it may point at the pipe too; anything else is ignored in favour of
// the standard pipe path.

const defaultAgentPipe = `\\.\pipe\openssh-ssh-agent`

func dialAgent() (io.ReadWriteCloser, error) {
	addr := os.Getenv("SSH_AUTH_SOCK")
	if addr == "" || !strings.HasPrefix(addr, `\\.\pipe\`) {
		addr = defaultAgentPipe
	}
	conn, err := winio.DialPipe(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("连接 ssh-agent 失败: %w", err)
	}
	return conn, nil
}
