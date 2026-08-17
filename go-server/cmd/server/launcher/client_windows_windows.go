//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const gwOwner = 4

var (
	procEnumWindows              = user32.NewProc("EnumWindows")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	clientWindowEnumCallback     = windows.NewCallback(enumClientWindow)
	clientWindowEnumMu           sync.Mutex
	clientWindowEnumCurrent      *clientWindowEnumState
)

type hiddenClientWindow struct {
	handle     uintptr
	processID  uint32
	executable string
	createdAt  time.Time
}

type clientWindowEnumState struct {
	executables map[string]struct{}
	hidden      []hiddenClientWindow
	err         error
}

type launcherInstance struct {
	Client struct {
		Directory string `json:"directory"`
	} `json:"client"`
}

func hideConfiguredClientWindows() (int, error) {
	executables, err := configuredClientExecutables(
		globalLauncher.projectRoot,
	)
	if err != nil {
		return 0, err
	}
	hidden, err := hideVisibleWindowsForExecutables(executables)
	if err != nil {
		return 0, err
	}
	globalLauncher.mu.Lock()
	defer globalLauncher.mu.Unlock()
	known := make(map[[2]uintptr]struct{}, len(globalLauncher.hidden))
	for _, entry := range globalLauncher.hidden {
		known[[2]uintptr{entry.handle, uintptr(entry.processID)}] = struct{}{}
	}
	added := 0
	for _, entry := range hidden {
		key := [2]uintptr{entry.handle, uintptr(entry.processID)}
		if _, exists := known[key]; exists {
			continue
		}
		globalLauncher.hidden = append(globalLauncher.hidden, entry)
		known[key] = struct{}{}
		added++
	}
	if added == 0 && len(globalLauncher.hidden) == 0 {
		return 0, errors.New(
			"没有找到正在显示的 DNF.exe 或 NoPack.exe 游戏窗口",
		)
	}
	return added, nil
}

func showHiddenClientWindows() (int, error) {
	globalLauncher.mu.Lock()
	hidden := append([]hiddenClientWindow(nil), globalLauncher.hidden...)
	globalLauncher.mu.Unlock()
	if len(hidden) == 0 {
		return 0, errors.New("本次登录器运行期间没有隐藏过游戏窗口")
	}
	restored, remaining, err := restoreHiddenWindows(hidden)
	globalLauncher.mu.Lock()
	globalLauncher.hidden = remaining
	globalLauncher.mu.Unlock()
	return restored, err
}

func restoreAllHiddenClientWindows() {
	globalLauncher.mu.Lock()
	hidden := append([]hiddenClientWindow(nil), globalLauncher.hidden...)
	globalLauncher.hidden = nil
	globalLauncher.mu.Unlock()
	_, _, _ = restoreHiddenWindows(hidden)
}

func configuredClientExecutables(projectRoot string) ([]string, error) {
	configPath := filepath.Join(
		projectRoot,
		"runtime",
		"config",
		"instance.json",
	)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取客户端配置失败: %w", err)
	}
	var cfg launcherInstance
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析客户端配置失败: %w", err)
	}
	clientRoot := strings.TrimSpace(cfg.Client.Directory)
	if clientRoot == "" {
		return nil, errors.New(
			"请先在 instance.json 中配置 client.directory",
		)
	}
	if !filepath.IsAbs(clientRoot) {
		return nil, errors.New("client.directory 必须是绝对路径")
	}
	var executables []string
	for _, name := range []string{"DNF.exe", "NoPack.exe"} {
		candidate := filepath.Join(clientRoot, name)
		if regularFile(candidate) {
			executables = append(executables, filepath.Clean(candidate))
		}
	}
	if len(executables) == 0 {
		return nil, errors.New(
			"配置的客户端目录中没有 DNF.exe 或 NoPack.exe",
		)
	}
	return executables, nil
}

func hideVisibleWindowsForExecutables(
	executables []string,
) ([]hiddenClientWindow, error) {
	state := &clientWindowEnumState{
		executables: make(map[string]struct{}, len(executables)),
	}
	for _, executable := range executables {
		state.executables[normalizeExecutablePath(executable)] = struct{}{}
	}
	clientWindowEnumMu.Lock()
	clientWindowEnumCurrent = state
	result, _, callErr := procEnumWindows.Call(clientWindowEnumCallback, 0)
	clientWindowEnumCurrent = nil
	clientWindowEnumMu.Unlock()
	if result == 0 && state.err == nil {
		return nil, fmt.Errorf("枚举游戏窗口失败: %w", callErr)
	}
	if state.err != nil {
		return state.hidden, state.err
	}
	return state.hidden, nil
}

func enumClientWindow(window uintptr, _ uintptr) uintptr {
	state := clientWindowEnumCurrent
	if state == nil {
		return 0
	}
	visible, _, _ := procIsWindowVisible.Call(window)
	if visible == 0 {
		return 1
	}
	owner, _, _ := procGetWindow.Call(window, gwOwner)
	if owner != 0 {
		return 1
	}
	processID, executable, createdAt, err := windowProcessIdentity(window)
	if err != nil {
		return 1
	}
	if _, matches := state.executables[normalizeExecutablePath(executable)]; !matches {
		return 1
	}
	procShowWindow.Call(window, 0)
	visible, _, _ = procIsWindowVisible.Call(window)
	if visible != 0 {
		state.err = errors.New("隐藏游戏窗口失败")
		return 0
	}
	state.hidden = append(state.hidden, hiddenClientWindow{
		handle:     window,
		processID:  processID,
		executable: executable,
		createdAt:  createdAt,
	})
	return 1
}

func restoreHiddenWindows(
	hidden []hiddenClientWindow,
) (restored int, remaining []hiddenClientWindow, resultErr error) {
	for _, entry := range hidden {
		exists, _, _ := procIsWindow.Call(entry.handle)
		if exists == 0 {
			continue
		}
		processID, executable, createdAt, err := windowProcessIdentity(
			entry.handle,
		)
		if err != nil ||
			processID != entry.processID ||
			!sameExecutablePath(executable, entry.executable) ||
			!createdAt.Equal(entry.createdAt) {
			continue
		}
		procShowWindow.Call(entry.handle, swRestore)
		visible, _, _ := procIsWindowVisible.Call(entry.handle)
		if visible == 0 {
			remaining = append(remaining, entry)
			resultErr = errors.Join(
				resultErr,
				errors.New("显示游戏窗口失败"),
			)
			continue
		}
		restored++
	}
	return restored, remaining, resultErr
}

func windowProcessIdentity(
	window uintptr,
) (processID uint32, executable string, createdAt time.Time, resultErr error) {
	procGetWindowThreadProcessID.Call(
		window,
		uintptr(unsafe.Pointer(&processID)),
	)
	if processID == 0 {
		return 0, "", time.Time{}, errors.New("窗口没有所属进程")
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		processID,
	)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, windows.CloseHandle(handle))
	}()
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(
		handle,
		0,
		&buffer[0],
		&size,
	); err != nil {
		return 0, "", time.Time{}, err
	}
	var creation windows.Filetime
	var exit windows.Filetime
	var kernel windows.Filetime
	var user windows.Filetime
	if err := windows.GetProcessTimes(
		handle,
		&creation,
		&exit,
		&kernel,
		&user,
	); err != nil {
		return 0, "", time.Time{}, err
	}
	return processID,
		windows.UTF16ToString(buffer[:size]),
		time.Unix(0, creation.Nanoseconds()).UTC(),
		nil
}

func sameExecutablePath(left string, right string) bool {
	return normalizeExecutablePath(left) == normalizeExecutablePath(right)
}

func normalizeExecutablePath(path string) string {
	return strings.ToLower(filepath.Clean(strings.TrimSpace(path)))
}
