//go:build darwin

package credentials

import (
	"fmt"
	"os/exec"
	"strings"
)

// macOS adapter: delegate to the system Keychain through `security(1)`.
//
// -U updates an existing item, -A avoids an access prompt when the app is
// rebuilt and its code signature changes.

func osStoreAvailable() bool { return true }

func osStoreSet(service, account, secret string) error {
	out, err := exec.Command("security", "add-generic-password",
		"-U", "-A",
		"-s", service,
		"-a", account,
		"-w", secret,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("写入钥匙串失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func osStoreGet(service, account string) (string, bool) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", service, "-a", account, "-w",
	).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

func osStoreDelete(service, account string) error {
	// A missing item is not an error for the caller.
	_ = exec.Command("security", "delete-generic-password", "-s", service, "-a", account).Run()
	return nil
}
