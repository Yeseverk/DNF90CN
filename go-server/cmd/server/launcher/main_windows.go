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
	windowTitle     = "DNF90CN 登录器"

	wmCreate          = 0x0001
	wmDestroy         = 0x0002
	wmClose           = 0x0010
	wmCommand         = 0x0111
	wmKeyDown         = 0x0100
	wmSetFont         = 0x0030
	wmGetTextLen      = 0x000E
	wmAppResult       = 0x8001
	wmAppStage        = 0x8002
	wmAppChooseClient = 0x8003
	bmGetCheck        = 0x00F0
	bmSetCheck        = 0x00F1
	bstChecked        = 1
	bnClicked         = 0
	swShow            = 5
	swMinimize        = 6
	swRestore         = 9
	idcArrow          = 32512
	vkReturn          = 0x0D

	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsTabStop     = 0x00010000
	wsClipChild   = 0x02000000

	esAutoHScroll = 0x0080
	esPassword    = 0x0020
	bsAutoCheck   = 0x0003
	ssCenter      = 0x0001

	controlChooseClient = 1001
	controlSlot1        = 1002
	controlSlot2        = 1003
	controlUsername     = 1004
	controlPassword     = 1005
	controlRemember     = 1006
	controlPrimary      = 1007
	controlRegister     = 1008
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
	procGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	procGetFocus            = user32.NewProc("GetFocus")
	procSetFocus            = user32.NewProc("SetFocus")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	globalLauncher          launcherState
	launcherDPI             int32 = 96
	windowProcedureCallback       = windows.NewCallback(windowProcedure)
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

type accountFields struct {
	username string
	password string
}

type taskResult struct {
	message          string
	success          bool
	minimize         bool
	clearPassword    bool
	credentialSlot   int
	configuredClient bool
	clientDirectory  string
}

type launcherState struct {
	mu              sync.Mutex
	projectRoot     string
	controlExe      string
	credential      [2]string
	account         [2]accountFields
	activeSlot      int
	clientDirectory string

	window        uintptr
	accentBar     uintptr
	title         uintptr
	subtitle      uintptr
	clientLabel   uintptr
	clientPath    uintptr
	chooseClient  uintptr
	divider       uintptr
	accountLabel  uintptr
	slot          [2]uintptr
	usernameLabel uintptr
	username      uintptr
	passwordLabel uintptr
	password      uintptr
	remember      uintptr
	primary       uintptr
	register      uintptr
	stageLabels   [4]uintptr
	status        uintptr

	busy          bool
	result        taskResult
	progressStage launcherStage
	stageFailed   bool
	stageMessage  string
	hidden        []hiddenClientWindow
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
	if err := procGetDpiForSystem.Find(); err == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi >= 96 && dpi <= 480 {
			launcherDPI = int32(dpi)
		}
	}
	if err := initLauncherTheme(); err != nil {
		return fmt.Errorf("初始化登录器界面失败: %w", err)
	}
	defer releaseLauncherTheme()

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
		Background: globalTheme.backgroundBrush,
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
		uintptr(scalePixel(560)),
		uintptr(scalePixel(670)),
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
		if message.Message == wmKeyDown && message.WParam == vkReturn {
			focus, _, _ := procGetFocus.Call()
			if focus == globalLauncher.username || focus == globalLauncher.password {
				beginLauncherTask(launcherActionLogin)
				continue
			}
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
	lParam *drawItemStruct,
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
			case controlChooseClient:
				beginClientSelection(false)
			case controlSlot1:
				selectAccountSlot(0)
			case controlSlot2:
				selectAccountSlot(1)
			case controlPrimary:
				beginLauncherTask(launcherActionLogin)
			case controlRegister:
				beginLauncherTask(launcherActionRegister)
			}
		}
		return 0
	case wmDrawItem:
		drawLauncherButton(lParam)
		return 1
	case wmCtlColor, wmCtlColorEdit, wmCtlColorBtn:
		return themeControlColor(
			message,
			wParam,
			uintptr(unsafe.Pointer(lParam)),
		)
	case wmAppStage:
		refreshLauncherProgress()
		return 0
	case wmAppChooseClient:
		beginClientSelection(wParam != 0)
		return 0
	case wmAppResult:
		finishLauncherTask()
		return 0
	case wmClose:
		globalLauncher.mu.Lock()
		busy := globalLauncher.busy
		globalLauncher.mu.Unlock()
		if busy {
			setWindowText(globalLauncher.status, "当前操作正在进行，请稍候。")
			return 0
		}
		restoreAllHiddenClientWindows()
		procDestroyWindow.Call(window)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	default:
		result, _, _ := procDefWindowProcW.Call(
			window,
			uintptr(message),
			wParam,
			uintptr(unsafe.Pointer(lParam)),
		)
		return result
	}
}

func createLauncherControls(window uintptr) {
	globalLauncher.accentBar = createControl(
		0, "STATIC", "", wsChild|wsVisible, 0, 0, 560, 4, window, 0,
	)
	globalLauncher.title = createControl(
		0, "STATIC", "DNF90CN", wsChild|wsVisible,
		32, 22, 480, 32, window, 0,
	)
	globalLauncher.subtitle = createControl(
		0, "STATIC", "本地开发登录器", wsChild|wsVisible,
		32, 57, 480, 23, window, 0,
	)
	globalLauncher.clientLabel = createControl(
		0, "STATIC", "游戏客户端", wsChild|wsVisible,
		32, 91, 360, 20, window, 0,
	)
	globalLauncher.clientPath = createControl(
		0, "EDIT", "尚未选择客户端",
		wsChild|wsVisible|wsTabStop|wsBorder|esAutoHScroll|esReadOnly,
		32, 113, 356, 34, window, 0,
	)
	globalLauncher.chooseClient = createControl(
		0, "BUTTON", "选择客户端",
		wsChild|wsVisible|wsTabStop|bsOwnerDraw,
		402, 113, 110, 34, window, controlChooseClient,
	)
	globalLauncher.divider = createControl(
		0, "STATIC", "", wsChild|wsVisible,
		32, 163, 480, 1, window, 0,
	)
	globalLauncher.accountLabel = createControl(
		0, "STATIC", "登录账号", wsChild|wsVisible,
		32, 181, 240, 20, window, 0,
	)
	globalLauncher.slot[0] = createControl(
		0, "BUTTON", "账号 1",
		wsChild|wsVisible|wsTabStop|bsOwnerDraw,
		32, 204, 116, 34, window, controlSlot1,
	)
	globalLauncher.slot[1] = createControl(
		0, "BUTTON", "账号 2",
		wsChild|wsVisible|wsTabStop|bsOwnerDraw,
		154, 204, 116, 34, window, controlSlot2,
	)
	globalLauncher.usernameLabel = createControl(
		0, "STATIC", "账号", wsChild|wsVisible,
		32, 253, 480, 20, window, 0,
	)
	globalLauncher.username = createControl(
		0, "EDIT", "",
		wsChild|wsVisible|wsTabStop|wsBorder|esAutoHScroll,
		32, 275, 480, 36, window, controlUsername,
	)
	globalLauncher.passwordLabel = createControl(
		0, "STATIC", "密码", wsChild|wsVisible,
		32, 324, 480, 20, window, 0,
	)
	globalLauncher.password = createControl(
		0, "EDIT", "",
		wsChild|wsVisible|wsTabStop|wsBorder|esAutoHScroll|esPassword,
		32, 346, 480, 36, window, controlPassword,
	)
	globalLauncher.remember = createControl(
		0, "BUTTON", "记住登录信息",
		wsChild|wsVisible|wsTabStop|bsAutoCheck,
		32, 393, 200, 26, window, controlRemember,
	)
	for index, label := range []string{
		"1  环境检查",
		"2  服务启动",
		"3  账号验证",
		"4  客户端启动",
	} {
		globalLauncher.stageLabels[index] = createControl(
			0, "STATIC", label, wsChild|wsVisible|ssCenter,
			32+int32(index)*120, 430, 112, 24, window, 0,
		)
	}
	globalLauncher.primary = createControl(
		0, "BUTTON", "进入游戏",
		wsChild|wsVisible|wsTabStop|bsOwnerDraw,
		32, 466, 480, 44, window, controlPrimary,
	)
	globalLauncher.register = createControl(
		0, "BUTTON", "注册并进入",
		wsChild|wsVisible|wsTabStop|bsOwnerDraw,
		32, 521, 160, 36, window, controlRegister,
	)
	globalLauncher.status = createControl(
		0, "STATIC", "准备就绪。", wsChild|wsVisible,
		32, 571, 480, 48, window, 0,
	)

	setControlFont(globalLauncher.title, globalTheme.titleFont)
	setControlFont(globalLauncher.subtitle, globalTheme.bodyFont)
	for _, control := range []uintptr{
		globalLauncher.clientLabel,
		globalLauncher.accountLabel,
		globalLauncher.usernameLabel,
		globalLauncher.passwordLabel,
	} {
		setControlFont(control, globalTheme.labelFont)
	}
	for _, control := range []uintptr{
		globalLauncher.clientPath,
		globalLauncher.username,
		globalLauncher.password,
		globalLauncher.remember,
		globalLauncher.status,
	} {
		setControlFont(control, globalTheme.bodyFont)
	}
	for _, control := range []uintptr{
		globalLauncher.chooseClient,
		globalLauncher.slot[0],
		globalLauncher.slot[1],
		globalLauncher.primary,
		globalLauncher.register,
	} {
		setControlFont(control, globalTheme.buttonFont)
	}
	for _, control := range globalLauncher.stageLabels {
		setControlFont(control, globalTheme.smallFont)
	}

	selectedSlot := 0
	remembered := false
	for slot := range globalLauncher.credential {
		username, password, found, err := readCredential(
			globalLauncher.credential[slot],
		)
		if err != nil || !found {
			continue
		}
		globalLauncher.account[slot] = accountFields{
			username: username,
			password: password,
		}
		if !remembered {
			selectedSlot = slot
		}
		remembered = true
	}
	if remembered {
		procSendMessageW.Call(
			globalLauncher.remember,
			bmSetCheck,
			bstChecked,
			0,
		)
	}
	globalLauncher.activeSlot = selectedSlot
	loadActiveAccountFields()

	directory, found, err := configuredClientDirectory(
		globalLauncher.projectRoot,
	)
	switch {
	case err != nil:
		setWindowText(globalLauncher.clientPath, "客户端配置不可用")
		setLauncherProgress(
			launcherStageEnvironment,
			"客户端配置不可用，请重新选择 DNF.exe。",
			true,
		)
		procPostMessageW.Call(window, wmAppChooseClient, 1, 0)
	case found:
		globalLauncher.clientDirectory = directory
		setWindowText(globalLauncher.clientPath, directory)
		setLauncherProgress(
			launcherStageIdle,
			"准备就绪。",
			false,
		)
	default:
		setWindowText(globalLauncher.clientPath, "尚未选择客户端")
		setLauncherProgress(
			launcherStageEnvironment,
			"首次使用，请选择 DNF.exe 或 NoPack.exe。",
			false,
		)
		procPostMessageW.Call(window, wmAppChooseClient, 1, 0)
	}
}

func beginClientSelection(automatic bool) {
	globalLauncher.mu.Lock()
	busy := globalLauncher.busy
	initialDirectory := globalLauncher.clientDirectory
	globalLauncher.mu.Unlock()
	if busy {
		return
	}
	selected, chosen, err := chooseClientExecutable(
		globalLauncher.window,
		initialDirectory,
	)
	if err != nil {
		setLauncherProgress(
			launcherStageEnvironment,
			err.Error(),
			true,
		)
		return
	}
	if !chosen {
		if automatic && initialDirectory == "" {
			setLauncherProgress(
				launcherStageEnvironment,
				"尚未选择客户端。",
				false,
			)
		}
		return
	}
	directory := filepath.Dir(selected)
	beginBackgroundTask(
		launcherStageEnvironment,
		"正在验证客户端文件…",
		func() taskResult {
			return runConfigureClientTask(directory)
		},
	)
}

func runConfigureClientTask(directory string) taskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	output, err := launcherRunControl(
		ctx,
		[]string{"configure-client", "--directory", directory},
		"",
	)
	if err != nil {
		return taskResult{message: summarizeControlError(output, err)}
	}
	return taskResult{
		success:          true,
		configuredClient: true,
		clientDirectory:  filepath.Clean(directory),
		message:          "客户端验证通过，路径已保存。",
	}
}

func selectAccountSlot(slot int) {
	if slot < 0 || slot >= len(globalLauncher.account) ||
		slot == globalLauncher.activeSlot {
		return
	}
	saveActiveAccountFields()
	globalLauncher.activeSlot = slot
	loadActiveAccountFields()
	for _, control := range globalLauncher.slot {
		procInvalidateRect.Call(control, 0, 1)
	}
	procSetFocus.Call(globalLauncher.username)
}

func saveActiveAccountFields() {
	slot := globalLauncher.activeSlot
	if slot < 0 || slot >= len(globalLauncher.account) {
		return
	}
	globalLauncher.account[slot] = accountFields{
		username: getWindowText(globalLauncher.username),
		password: getWindowText(globalLauncher.password),
	}
}

func loadActiveAccountFields() {
	slot := globalLauncher.activeSlot
	if slot < 0 || slot >= len(globalLauncher.account) {
		return
	}
	setWindowText(globalLauncher.username, globalLauncher.account[slot].username)
	setWindowText(globalLauncher.password, globalLauncher.account[slot].password)
	for _, control := range globalLauncher.slot {
		procInvalidateRect.Call(control, 0, 1)
	}
}

func beginLauncherTask(action launcherAction) {
	saveActiveAccountFields()
	slot := globalLauncher.activeSlot
	if slot < 0 || slot >= len(globalLauncher.account) {
		return
	}
	username := strings.TrimSpace(globalLauncher.account[slot].username)
	password := globalLauncher.account[slot].password
	if username == "" || password == "" {
		setLauncherProgress(
			launcherStageAccount,
			"账号和密码不能为空。",
			true,
		)
		return
	}
	globalLauncher.account[slot].username = username
	setWindowText(globalLauncher.username, username)
	if strings.TrimSpace(globalLauncher.clientDirectory) == "" {
		beginClientSelection(false)
		return
	}
	remember := isButtonChecked(globalLauncher.remember)
	beginBackgroundTask(
		launcherStageEnvironment,
		"正在检查客户端配置…",
		func() taskResult {
			result := runLauncherTask(
				action,
				username,
				password,
				remember,
				globalLauncher.credential[slot],
			)
			result.clearPassword = result.success
			result.credentialSlot = slot
			return result
		},
	)
}

func beginBackgroundTask(
	stage launcherStage,
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
	enableLauncherControls(false)
	setLauncherProgress(stage, status, false)
	go func() {
		result := run()
		globalLauncher.mu.Lock()
		globalLauncher.result = result
		globalLauncher.mu.Unlock()
		procPostMessageW.Call(globalLauncher.window, wmAppResult, 0, 0)
	}()
}

func runLauncherTask(
	action launcherAction,
	username string,
	password string,
	remember bool,
	credentialTarget string,
) taskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	output, err := runLauncherWorkflow(
		ctx,
		action,
		username,
		password,
		func() error {
			_, err := configuredClientExecutables(
				globalLauncher.projectRoot,
			)
			return err
		},
		launcherRunControl,
		queueLauncherStage,
	)
	if err != nil {
		return taskResult{message: summarizeControlError(output, err)}
	}

	warning := ""
	if remember {
		if err := writeCredential(
			credentialTarget,
			username,
			password,
		); err != nil {
			warning = " 但保存登录信息失败：" + err.Error()
		}
	} else if err := deleteCredential(credentialTarget); err != nil {
		warning = " 但清除旧登录信息失败：" + err.Error()
	}
	message := "登录成功，游戏已启动。"
	if action == launcherActionRegister {
		message = "注册成功，游戏已启动。"
	}
	return taskResult{
		success:  true,
		minimize: true,
		message:  message + warning,
	}
}

func finishLauncherTask() {
	globalLauncher.mu.Lock()
	result := globalLauncher.result
	globalLauncher.result = taskResult{}
	globalLauncher.busy = false
	globalLauncher.mu.Unlock()
	enableLauncherControls(true)
	if result.configuredClient && result.success {
		globalLauncher.mu.Lock()
		globalLauncher.clientDirectory = result.clientDirectory
		globalLauncher.mu.Unlock()
		setWindowText(globalLauncher.clientPath, result.clientDirectory)
		setLauncherProgress(
			launcherStageIdle,
			result.message,
			false,
		)
	} else {
		globalLauncher.mu.Lock()
		stage := globalLauncher.progressStage
		globalLauncher.mu.Unlock()
		if result.success {
			stage = launcherStageComplete
		}
		setLauncherProgress(stage, result.message, !result.success)
	}
	if result.clearPassword &&
		result.credentialSlot >= 0 &&
		result.credentialSlot < len(globalLauncher.account) &&
		!isButtonChecked(globalLauncher.remember) {
		globalLauncher.account[result.credentialSlot].password = ""
		if result.credentialSlot == globalLauncher.activeSlot {
			setWindowText(globalLauncher.password, "")
		}
	}
	if result.minimize {
		procShowWindow.Call(globalLauncher.window, swMinimize)
	}
}

func queueLauncherStage(stage launcherStage, message string) {
	globalLauncher.mu.Lock()
	globalLauncher.progressStage = stage
	globalLauncher.stageFailed = false
	globalLauncher.stageMessage = message
	window := globalLauncher.window
	globalLauncher.mu.Unlock()
	if window != 0 {
		procPostMessageW.Call(window, wmAppStage, 0, 0)
	}
}

func setLauncherProgress(
	stage launcherStage,
	message string,
	failed bool,
) {
	globalLauncher.mu.Lock()
	globalLauncher.progressStage = stage
	globalLauncher.stageFailed = failed
	globalLauncher.stageMessage = message
	globalLauncher.mu.Unlock()
	refreshLauncherProgress()
}

func refreshLauncherProgress() {
	globalLauncher.mu.Lock()
	stage := globalLauncher.progressStage
	failed := globalLauncher.stageFailed
	message := globalLauncher.stageMessage
	globalLauncher.mu.Unlock()
	if globalLauncher.status != 0 {
		setWindowText(globalLauncher.status, message)
		procInvalidateRect.Call(globalLauncher.status, 0, 1)
	}
	labels := []string{"环境检查", "服务启动", "账号验证", "客户端启动"}
	for index, control := range globalLauncher.stageLabels {
		step := launcherStage(index + 1)
		prefix := fmt.Sprintf("%d", index+1)
		switch {
		case failed && step == stage:
			prefix = "×"
		case stage == launcherStageComplete || step < stage:
			prefix = "✓"
		case step == stage:
			prefix = "•"
		}
		setWindowText(control, prefix+"  "+labels[index])
		procInvalidateRect.Call(control, 0, 1)
	}
}

var launcherRunControl launcherCommandRunner = runControl

func runAuthenticatedClientLaunch(
	ctx context.Context,
	username string,
	password string,
) (string, error) {
	return runLauncherWorkflow(
		ctx,
		launcherActionLogin,
		username,
		password,
		nil,
		launcherRunControl,
		nil,
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
		uintptr(uint32(scalePixel(x))),
		uintptr(uint32(scalePixel(y))),
		uintptr(uint32(scalePixel(width))),
		uintptr(uint32(scalePixel(height))),
		parent,
		uintptr(id),
		0,
		0,
	)
	return control
}

func setControlFont(control, font uintptr) {
	procSendMessageW.Call(control, wmSetFont, font, 1)
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

func enableLauncherControls(enabled bool) {
	value := uintptr(0)
	if enabled {
		value = 1
	}
	for _, control := range []uintptr{
		globalLauncher.chooseClient,
		globalLauncher.slot[0],
		globalLauncher.slot[1],
		globalLauncher.username,
		globalLauncher.password,
		globalLauncher.remember,
		globalLauncher.primary,
		globalLauncher.register,
	} {
		procEnableWindow.Call(control, value)
		procInvalidateRect.Call(control, 0, 1)
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

func scalePixel(value int32) int32 {
	return value * launcherDPI / 96
}

func utf16Ptr(value string) *uint16 {
	pointer, _ := windows.UTF16PtrFromString(value)
	return pointer
}
