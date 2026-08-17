//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ofnExplorer        = 0x00080000
	ofnFileMustExist   = 0x00001000
	ofnPathMustExist   = 0x00000800
	ofnNoChangeDir     = 0x00000008
	ofnDontAddToRecent = 0x02000000
)

var (
	comdlg32                 = windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
)

type openFileName struct {
	StructSize      uint32
	Owner           uintptr
	Instance        uintptr
	Filter          *uint16
	CustomFilter    *uint16
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            *uint16
	MaxFile         uint32
	FileTitle       *uint16
	MaxFileTitle    uint32
	InitialDir      *uint16
	Title           *uint16
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          *uint16
	CustData        uintptr
	Hook            uintptr
	TemplateName    *uint16
	Reserved        uintptr
	ReservedValue   uint32
	FlagsEx         uint32
}

func chooseClientExecutable(
	owner uintptr,
	initialDirectory string,
) (string, bool, error) {
	filter := utf16Block(
		"DNF 客户端 (DNF.exe; NoPack.exe)\x00DNF.exe;NoPack.exe\x00" +
			"可执行文件 (*.exe)\x00*.exe\x00\x00",
	)
	var fileBuffer [32768]uint16
	dialog := openFileName{
		StructSize:  uint32(unsafe.Sizeof(openFileName{})),
		Owner:       owner,
		Filter:      &filter[0],
		FilterIndex: 1,
		File:        &fileBuffer[0],
		MaxFile:     uint32(len(fileBuffer)),
		Title:       utf16Ptr("选择 DNF.exe 或 NoPack.exe"),
		Flags: ofnExplorer |
			ofnFileMustExist |
			ofnPathMustExist |
			ofnNoChangeDir |
			ofnDontAddToRecent,
		DefExt: utf16Ptr("exe"),
	}
	if strings.TrimSpace(initialDirectory) != "" {
		dialog.InitialDir = utf16Ptr(initialDirectory)
	}
	result, _, _ := procGetOpenFileNameW.Call(
		uintptr(unsafe.Pointer(&dialog)),
	)
	if result == 0 {
		code, _, _ := procCommDlgExtendedError.Call()
		if code == 0 {
			return "", false, nil
		}
		return "", false, fmt.Errorf(
			"打开客户端选择窗口失败 (0x%X)",
			code,
		)
	}
	selected := filepath.Clean(windows.UTF16ToString(fileBuffer[:]))
	base := filepath.Base(selected)
	if !strings.EqualFold(base, "DNF.exe") &&
		!strings.EqualFold(base, "NoPack.exe") {
		return "", false, errorsf(
			"请选择 DNF.exe 或 NoPack.exe，当前选择的是 %s",
			base,
		)
	}
	return selected, true, nil
}

func utf16Block(value string) []uint16 {
	return utf16.Encode([]rune(value))
}

func errorsf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
