//go:build windows

package app

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/jiangwanyi/file-j/backend/internal/protocol"
)

// Windows adapter: the Shell API. openPath uses ShellExecuteW directly (no
// command line parsing, so spaces / quotes / Chinese in paths are safe);
// revealPath uses Explorer's /select, switch, which is the documented way to
// open a folder with one item selected.

var (
	shell32DLL         = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW  = shell32DLL.NewProc("ShellExecuteW")
)

// openPath opens a file with its default association, or a folder in Explorer.
func openPath(p string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	// ShellExecuteW returns a value > 32 on success; small values are error codes.
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, 0,
		uintptr(swShownormal),
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW 失败 (代码 %d)", ret)
	}
	return nil
}

const swShownormal = 1

// revealPath opens the parent folder in Explorer with the item selected.
func revealPath(p string) error {
	cmd := exec.Command("explorer.exe", `/select,"`+p+`"`)
	// Explorer is a GUI process; keep the console hidden in dev builds and
	// never wait for its (always weird) exit code.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if err := cmd.Start(); err != nil {
		return protocol.Wrapf(protocol.CodeInternal, "无法在资源管理器中显示", err.Error())
	}
	return nil
}
