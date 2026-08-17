//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowClassName = "DNF90LocalLoginWindow"
	windowTitle     = "DNF90 本地登录器"

	wmCreate       = 0x0001
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmCommand      = 0x0111
	wmSetFont      = 0x0030
	wmGetTextLen   = 0x000E
	wmAppResult    = 0x8001
	bmGetCheck     = 0x00F0
	bmSetCheck     = 0x00F1
	bstChecked     = 1
	bnClicked      = 0
	swShow         = 5
	swMinimize     = 6
	swRestore      = 9
	colorWindow    = 5
	defaultGUIFont = 17
	idcArrow       = 32512

	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsTabStop     = 0x00010000
	wsClipChild   = 0x02000000

	wsExClientEdge = 0x00000200

	esAutoHScroll = 0x0080
	esPassword    = 0x0020
	bsAutoCheck   = 0x0003
	bsDefPush     = 0x0001
	ssCenter      = 0x0001

	controlUsername  = 1001
	controlPassword  = 1002
	controlRemember  = 1003
	controlRegister  = 1004
	controlLogin     = 1005
	controlUsername2 = 1006
	controlPassword2 = 1007
	controlRegister2 = 1008
	controlLogin2    = 1009
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	gdi32                   = windows.NewLazySystemDLL("gdi32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procIsDialogMessageW    = user32.NewProc("IsDialogMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procSetWindowTextW      = user32.NewProc("SetWindowTextW")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procEnableWindow        = user32.NewProc("EnableWindow")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procSetProcessDPIAware  = user32.NewProc("SetProcessDPIAware")
	procGetStockObject      = gdi32.NewProc("GetStockObject")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	globalLauncher          launcherState
	windowProcedureCallback = windows.NewCallback(windowProcedure)
)

type point struct {
	X int32
	Y int32
}

type windowMessage struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type windowClassEx struct {
	Size        uint32
	Style       uint32
	WndProc     uintptr
	ClassExtra  int32
	WindowExtra int32
	Instance    uintptr
	Icon        uintptr
	Cursor      uintptr
	Background  uintptr
	MenuName    *uint16
	ClassName   *uint16
	SmallIcon   uintptr
}

type taskResult struct {
	message        string
	success        bool
	minimize       bool
	clearPassword  bool
	credentialSlot int
}

type launcherState struct {
	mu          sync.Mutex
	projectRoot string
	controlExe  string
	credential  [2]string
	window      uintptr
	username    [2]uintptr
	password    [2]uintptr
	remember    uintptr
	register    [2]uintptr
	login       [2]uintptr
	status      uintptr
	busy        bool
	result      taskResult
	hidden      []hiddenClientWindow
}

func main() {
	runtime.LockOSThread()
	projectRoot, controlExe, err := discoverLauncherProject()
	if err != nil {
		messageBox(0, err.Error(), windowTitle)
		return
	}
	globalLauncher.projectRoot = projectRoot
	globalLauncher.controlExe = controlExe
	globalLauncher.credential[0] = credentialTarget(projectRoot)
	globalLauncher.credential[1] = credentialTarget(projectRoot) + "/Slot2"
	if err := runWindow(); err != nil {
		messageBox(0, err.Error(), windowTitle)
	}
}

func runWindow() error {
	_, _, _ = procSetProcessDPIAware.Call()
	instance, _, callErr := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return fmt.Errorf("获取程序模块失败: %v", callErr)
	}
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	className := utf16Ptr(windowClassName)
	class := windowClassEx{
		Size:       uint32(unsafe.Sizeof(windowClassEx{})),
		WndProc:    windowProcedureCallback,
		Instance:   instance,
		Cursor:     cursor,
		Background: colorWindow + 1,
		ClassName:  className,
	}
	atom, _, callErr := procRegisterClassExW.Call(
		uintptr(unsafe.Pointer(&class)),
	)
	if atom == 0 {
		return fmt.Errorf("注册登录窗口失败: %v", callErr)
	}
	window, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr(windowTitle))),
		wsOverlapped|wsCaption|wsSysMenu|wsMinimizeBox|wsClipChild,
		uintptr(uint32(0x80000000)),
		uintptr(uint32(0x80000000)),
		520,
		560,
		0,
		0,
		instance,
		0,
	)
	if window == 0 {
		return fmt.Errorf("创建登录窗口失败: %v", callErr)
	}
	globalLauncher.window = window
	procShowWindow.Call(window, swShow)
	procUpdateWindow.Call(window)

	var message windowMessage
	for {
		result, _, err := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&message)),
			0,
			0,
			0,
		)
		if int32(result) == -1 {
			return fmt.Errorf("读取窗口消息失败: %v", err)
		}
		if result == 0 {
			return nil
		}
		if handled, _, _ := procIsDialogMessageW.Call(
			window,
			uintptr(unsafe.Pointer(&message)),
		); handled != 0 {
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func windowProcedure(
	window uintptr,
	message uint32,
	wParam uintptr,
	lParam uintptr,
) uintptr {
	switch message {
	case wmCreate:
		createLauncherControls(window)
		return 0
	case wmCommand:
		controlID := uint16(wParam & 0xFFFF)
		notification := uint16((wParam >> 16) & 0xFFFF)
		if notification == bnClicked {
			switch controlID {
			case controlRegister:
				beginLauncherTask(0, false)
			case controlLogin:
				beginLauncherTask(0, true)
			case controlRegister2:
				beginLauncherTask(1, false)
			case controlLogin2:
				beginLauncherTask(1, true)
			}
		}
		return 0
	case wmAppResult:
		finishLauncherTask()
		return 0
	case wmClose:
		globalLauncher.mu.Lock()
		busy := globalLauncher.busy
		globalLauncher.mu.Unlock()
		if busy {
			setWindowText(
				globalLauncher.status,
				"当前操作尚未完成，请完成后再关闭登录器。",
			)
			return 0
		}
		restoreAllHiddenClientWindows()
		procDestroyWindow.Call(window)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	default:
		result, _, _ := procDefWindowProcW.Call(window, uintptr(message), wParam, lParam)
		return result
	}
}

func createLauncherControls(window uintptr) {
	title := createControl(
		0,
		"STATIC",
		"DNF90 本地账号登录",
		wsChild|wsVisible|ssCenter,
		40,
		20,
		430,
		34,
		window,
		0,
	)
	subtitle := createControl(
		0,
		"STATIC",
		"两组账号可分别注册和登录；登录不会重启已在线服务",
		wsChild|wsVisible|ssCenter,
		40,
		55,
		430,
		22,
		window,
		0,
	)
	usernameLabel1 := createControl(
		0,
		"STATIC",
		"账号 1",
		wsChild|wsVisible,
		70,
		95,
		75,
		24,
		window,
		0,
	)
	globalLauncher.username[0] = createControl(
		wsExClientEdge,
		"EDIT",
		"",
		wsChild|wsVisible|wsTabStop|esAutoHScroll,
		145,
		89,
		285,
		30,
		window,
		controlUsername,
	)
	passwordLabel1 := createControl(
		0,
		"STATIC",
		"密码 1",
		wsChild|wsVisible,
		70,
		136,
		75,
		24,
		window,
		0,
	)
	globalLauncher.password[0] = createControl(
		wsExClientEdge,
		"EDIT",
		"",
		wsChild|wsVisible|wsTabStop|esAutoHScroll|esPassword,
		145,
		130,
		285,
		30,
		window,
		controlPassword,
	)
	usernameLabel2 := createControl(
		0,
		"STATIC",
		"账号 2",
		wsChild|wsVisible,
		70,
		236,
		75,
		24,
		window,
		0,
	)
	globalLauncher.username[1] = createControl(
		wsExClientEdge,
		"EDIT",
		"",
		wsChild|wsVisible|wsTabStop|esAutoHScroll,
		145,
		230,
		285,
		30,
		window,
		controlUsername2,
	)
	passwordLabel2 := createControl(
		0,
		"STATIC",
		"密码 2",
		wsChild|wsVisible,
		70,
		277,
		75,
		24,
		window,
		0,
	)
	globalLauncher.password[1] = createControl(
		wsExClientEdge,
		"EDIT",
		"",
		wsChild|wsVisible|wsTabStop|esAutoHScroll|esPassword,
		145,
		271,
		285,
		30,
		window,
		controlPassword2,
	)
	globalLauncher.remember = createControl(
		0,
		"BUTTON",
		"记住两组账号密码（Windows 凭据管理器）",
		wsChild|wsVisible|wsTabStop|bsAutoCheck,
		145,
		365,
		285,
		26,
		window,
		controlRemember,
	)
	globalLauncher.register[0] = createControl(
		0,
		"BUTTON",
		"注册账号 1",
		wsChild|wsVisible|wsTabStop,
		95,
		170,
		140,
		38,
		window,
		controlRegister,
	)
	globalLauncher.login[0] = createControl(
		0,
		"BUTTON",
		"登录账号 1",
		wsChild|wsVisible|wsTabStop,
		270,
		170,
		165,
		38,
		window,
		controlLogin,
	)
	globalLauncher.register[1] = createControl(
		0,
		"BUTTON",
		"注册账号 2",
		wsChild|wsVisible|wsTabStop,
		95,
		311,
		140,
		38,
		window,
		controlRegister2,
	)
	globalLauncher.login[1] = createControl(
		0,
		"BUTTON",
		"登录账号 2",
		wsChild|wsVisible|wsTabStop|bsDefPush,
		270,
		311,
		165,
		38,
		window,
		controlLogin2,
	)
	globalLauncher.status = createControl(
		0,
		"STATIC",
		"请输入账号和密码；首次使用请先注册对应账号。",
		wsChild|wsVisible|ssCenter,
		45,
		410,
		420,
		65,
		window,
		0,
	)
	font, _, _ := procGetStockObject.Call(defaultGUIFont)
	for _, control := range []uintptr{
		title,
		subtitle,
		usernameLabel1,
		globalLauncher.username[0],
		passwordLabel1,
		globalLauncher.password[0],
		usernameLabel2,
		globalLauncher.username[1],
		passwordLabel2,
		globalLauncher.password[1],
		globalLauncher.remember,
		globalLauncher.register[0],
		globalLauncher.login[0],
		globalLauncher.register[1],
		globalLauncher.login[1],
		globalLauncher.status,
	} {
		procSendMessageW.Call(control, wmSetFont, font, 1)
	}
	remembered := false
	for slot := range globalLauncher.credential {
		username, password, found, err := readCredential(
			globalLauncher.credential[slot],
		)
		if err != nil || !found {
			continue
		}
		setWindowText(globalLauncher.username[slot], username)
		setWindowText(globalLauncher.password[slot], password)
		remembered = true
	}
	if remembered {
		procSendMessageW.Call(globalLauncher.remember, bmSetCheck, bstChecked, 0)
	}
}

func beginLauncherTask(slot int, login bool) {
	if slot < 0 || slot >= len(globalLauncher.username) {
		return
	}
	username := getWindowText(globalLauncher.username[slot])
	password := getWindowText(globalLauncher.password[slot])
	if strings.TrimSpace(username) == "" || password == "" {
		setWindowText(
			globalLauncher.status,
			fmt.Sprintf("账号 %d 和密码 %d 不能为空。", slot+1, slot+1),
		)
		return
	}
	status := fmt.Sprintf("正在注册账号 %d，请稍候……", slot+1)
	if login {
		status = fmt.Sprintf("正在验证账号 %d 并启动游戏，请稍候……", slot+1)
	}
	remember := isButtonChecked(globalLauncher.remember)
	beginBackgroundTask(status, func() taskResult {
		result := runLauncherTask(
			login,
			username,
			password,
			remember,
			globalLauncher.credential[slot],
		)
		result.clearPassword = result.success
		result.credentialSlot = slot
		return result
	})
}

func beginBackgroundTask(
	status string,
	run func() taskResult,
) {
	globalLauncher.mu.Lock()
	if globalLauncher.busy {
		globalLauncher.mu.Unlock()
		return
	}
	globalLauncher.busy = true
	globalLauncher.mu.Unlock()
	enableLauncherButtons(false)
	setWindowText(globalLauncher.status, status)
	go func() {
		result := run()
		globalLauncher.mu.Lock()
		globalLauncher.result = result
		globalLauncher.mu.Unlock()
		procPostMessageW.Call(globalLauncher.window, wmAppResult, 0, 0)
	}()
}

func runLauncherTask(
	login bool,
	username string,
	password string,
	remember bool,
	credentialTarget string,
) taskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if !login {
		output, err := runControl(
			ctx,
			[]string{
				"account",
				"register",
				"--username",
				username,
				"--password-stdin",
			},
			password,
		)
		if err != nil {
			return taskResult{message: summarizeControlError(output, err)}
		}
		if remember {
			if err := writeCredential(
				credentialTarget,
				username,
				password,
			); err != nil {
				return taskResult{
					success: true,
					message: "账号注册成功，但保存登录信息失败：" + err.Error(),
				}
			}
		}
		return taskResult{
			success: true,
			message: "账号注册成功。现在可以点击“登录并启动游戏”。",
		}
	}

	output, err := runAuthenticatedClientLaunch(ctx, username, password)
	if err != nil {
		return taskResult{message: summarizeControlError(output, err)}
	}
	if remember {
		if err := writeCredential(
			credentialTarget,
			username,
			password,
		); err != nil {
			return taskResult{
				success:  true,
				minimize: true,
				message:  "游戏已启动，但保存登录信息失败：" + err.Error(),
			}
		}
	} else if err := deleteCredential(credentialTarget); err != nil {
		return taskResult{
			success:  true,
			minimize: true,
			message:  "游戏已启动，但清除旧登录信息失败：" + err.Error(),
		}
	}
	return taskResult{
		success:  true,
		minimize: true,
		message:  "登录成功，游戏已启动。",
	}
}

func finishLauncherTask() {
	globalLauncher.mu.Lock()
	result := globalLauncher.result
	globalLauncher.result = taskResult{}
	globalLauncher.busy = false
	globalLauncher.mu.Unlock()
	enableLauncherButtons(true)
	setWindowText(globalLauncher.status, result.message)
	if result.clearPassword &&
		result.credentialSlot >= 0 &&
		result.credentialSlot < len(globalLauncher.password) &&
		!isButtonChecked(globalLauncher.remember) {
		setWindowText(globalLauncher.password[result.credentialSlot], "")
	}
	if result.minimize {
		procShowWindow.Call(globalLauncher.window, swMinimize)
	}
}

var launcherRunControl = runControl

func runAuthenticatedClientLaunch(
	ctx context.Context,
	username string,
	password string,
) (string, error) {
	output, err := launcherRunControl(ctx, []string{"start"}, "")
	if err != nil {
		return output, err
	}
	return launcherRunControl(
		ctx,
		[]string{
			"launch-client",
			"--multi-instance",
			"--username",
			username,
			"--password-stdin",
		},
		password,
	)
}

func runControl(
	ctx context.Context,
	args []string,
	password string,
) (string, error) {
	command := exec.CommandContext(ctx, globalLauncher.controlExe, args...)
	command.Dir = globalLauncher.projectRoot
	command.SysProcAttr = &windows.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if password != "" {
		command.Stdin = strings.NewReader(password + "\n")
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.String(), err
}

func discoverLauncherProject() (string, string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("获取登录器路径失败: %w", err)
	}
	current := filepath.Dir(executable)
	for {
		control := filepath.Join(current, "runtime", "bin", "DNF90Control.exe")
		template := filepath.Join(
			current,
			"deploy",
			"templates",
			"instance.example.json",
		)
		if regularFile(control) && regularFile(template) {
			return current, control, nil
		}
		if strings.EqualFold(filepath.Base(current), "bin") {
			control = filepath.Join(current, "DNF90Control.exe")
			root := filepath.Clean(filepath.Join(current, "..", ".."))
			template = filepath.Join(
				root,
				"deploy",
				"templates",
				"instance.example.json",
			)
			if regularFile(control) && regularFile(template) {
				return root, control, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", "", errors.New(
		"找不到 DNF90Control.exe 或发布目录，请把登录器保留在 runtime\\bin",
	)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func createControl(
	exStyle uintptr,
	className string,
	text string,
	style uintptr,
	x int32,
	y int32,
	width int32,
	height int32,
	parent uintptr,
	id uint16,
) uintptr {
	control, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		style,
		uintptr(uint32(x)),
		uintptr(uint32(y)),
		uintptr(uint32(width)),
		uintptr(uint32(height)),
		parent,
		uintptr(id),
		0,
		0,
	)
	return control
}

func setWindowText(window uintptr, value string) {
	procSetWindowTextW.Call(
		window,
		uintptr(unsafe.Pointer(utf16Ptr(value))),
	)
}

func getWindowText(window uintptr) string {
	length, _, _ := procSendMessageW.Call(window, wmGetTextLen, 0, 0)
	buffer := make([]uint16, int(length)+1)
	procGetWindowTextW.Call(
		window,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	return windows.UTF16ToString(buffer)
}

func enableLauncherButtons(enabled bool) {
	value := uintptr(0)
	if enabled {
		value = 1
	}
	for slot := range globalLauncher.register {
		procEnableWindow.Call(globalLauncher.register[slot], value)
		procEnableWindow.Call(globalLauncher.login[slot], value)
	}
}

func isButtonChecked(button uintptr) bool {
	result, _, _ := procSendMessageW.Call(button, bmGetCheck, 0, 0)
	return result == bstChecked
}

func messageBox(window uintptr, text string, title string) {
	procMessageBoxW.Call(
		window,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		0x10,
	)
}

func utf16Ptr(value string) *uint16 {
	pointer, _ := windows.UTF16PtrFromString(value)
	return pointer
}
