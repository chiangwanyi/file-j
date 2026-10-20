//go:build !darwin && !windows

package app

import "github.com/jiangwanyi/file-j/backend/internal/protocol"

// Fallback adapter: no shell integration on this platform.

func openPath(string) error {
	return protocol.Errorf(protocol.CodeUnsupported, "当前平台不支持打开文件")
}

func revealPath(string) error {
	return protocol.Errorf(protocol.CodeUnsupported, "当前平台不支持显示文件位置")
}
