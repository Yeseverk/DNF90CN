//go:build windows

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credentialTypeGeneric      = 1
	credentialPersistLocalHost = 2
	errorNotFound              = syscall.Errno(1168)
)

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW = advapi32.NewProc("CredWriteW")
	procCredReadW  = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

type windowsCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func credentialTarget(projectRoot string) string {
	normalized := strings.ToLower(filepath.Clean(projectRoot))
	digest := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("DNF90/LocalLogin/%x", digest[:16])
}

func writeCredential(target string, username string, password string) error {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	usernamePointer, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	commentPointer, _ := windows.UTF16PtrFromString(
		"DNF90 本地登录器保存的账号密码",
	)
	blob := []byte(password)
	var blobPointer *byte
	if len(blob) > 0 {
		blobPointer = &blob[0]
	}
	credential := windowsCredential{
		Type:               credentialTypeGeneric,
		TargetName:         targetPointer,
		Comment:            commentPointer,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     blobPointer,
		Persist:            credentialPersistLocalHost,
		UserName:           usernamePointer,
	}
	result, _, callErr := procCredWriteW.Call(
		uintptr(unsafe.Pointer(&credential)),
		0,
	)
	runtime.KeepAlive(blob)
	runtime.KeepAlive(targetPointer)
	runtime.KeepAlive(usernamePointer)
	if result == 0 {
		return fmt.Errorf("写入 Windows 凭据失败: %w", callErr)
	}
	return nil
}

func readCredential(target string) (
	username string,
	password string,
	found bool,
	err error,
) {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", "", false, err
	}
	var credential *windowsCredential
	result, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		credentialTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&credential)),
	)
	if result == 0 {
		if errors.Is(callErr, errorNotFound) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("读取 Windows 凭据失败: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential == nil || credential.UserName == nil {
		return "", "", false, errors.New("Windows 凭据内容无效")
	}
	username = windows.UTF16PtrToString(credential.UserName)
	if credential.CredentialBlobSize > 0 &&
		credential.CredentialBlob != nil {
		blob := unsafe.Slice(
			credential.CredentialBlob,
			int(credential.CredentialBlobSize),
		)
		password = string(blob)
	}
	return username, password, true, nil
}

func deleteCredential(target string) error {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	result, _, callErr := procCredDelete.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		credentialTypeGeneric,
		0,
	)
	if result == 0 && !errors.Is(callErr, errorNotFound) {
		return fmt.Errorf("删除 Windows 凭据失败: %w", callErr)
	}
	return nil
}
