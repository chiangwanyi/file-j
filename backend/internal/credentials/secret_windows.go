//go:build windows

package credentials

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows adapter: the Credential Manager through CredWriteW / CredReadW /
// CredDeleteW (advapi32) — the same store Windows itself uses for mapped
// network drives and generic credentials.
//
// The calls are bound directly instead of pulling in a wincred dependency:
// the ABI is a single flat struct and three functions, and secrets are the one
// place where an auditable minimal implementation is worth more than a helper
// library.
//
// The secret is stored as an opaque UTF-8 blob under the generic credential
// type; YAML keeps only the credential reference, exactly like on macOS.

const (
	credTypeGeneric           = 1
	credPersistLocalMachine   = 2 // survives logon for the same user
)

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

// winCredential mirrors CREDENTIALW from <wincred.h>.
type winCredential struct {
	flags              uint32
	credType           uint32
	targetName         *uint16
	comment            *uint16
	lastWrittenLow     uint32
	lastWrittenHigh    uint32
	credentialBlobSize uint32
	credentialBlob     *byte
	persist            uint32
	attributeCount     uint32
	attributes         uintptr
	targetAlias        *uint16
	userName           *uint16
}

func osStoreAvailable() bool { return true }

func credTarget(service, account string) string { return service + "/" + account }

func osStoreSet(service, account, secret string) error {
	target, err := windows.UTF16PtrFromString(credTarget(service, account))
	if err != nil {
		return err
	}
	user, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return err
	}
	blob := []byte(secret)
	cred := &winCredential{
		credType:           credTypeGeneric,
		targetName:         target,
		userName:           user,
		credentialBlobSize: uint32(len(blob)),
		credentialBlob:     &blob[0],
		persist:            credPersistLocalMachine,
	}
	r, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(cred)), 0)
	runtime.KeepAlive(blob)
	if r == 0 {
		return fmt.Errorf("写入凭据管理器失败: %w", callErr)
	}
	return nil
}

func osStoreGet(service, account string) (string, bool) {
	target, err := windows.UTF16PtrFromString(credTarget(service, account))
	if err != nil {
		return "", false
	}
	var cred *winCredential
	r, _, _ := procCredRead.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&cred)),
	)
	if r == 0 || cred == nil {
		return "", false
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	if cred.credentialBlob == nil || cred.credentialBlobSize == 0 {
		return "", false
	}
	blob := unsafe.Slice(cred.credentialBlob, cred.credentialBlobSize)
	return string(blob), true
}

func osStoreDelete(service, account string) error {
	target, err := windows.UTF16PtrFromString(credTarget(service, account))
	if err != nil {
		return err
	}
	// A missing item is not an error for the caller; CredDeleteW reports it as
	// ERROR_NOT_FOUND which we deliberately swallow.
	procCredDelete.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0)
	return nil
}
