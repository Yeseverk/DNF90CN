package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type projectPaths struct {
	projectRoot          string
	goServerRoot         string
	runtimeRoot          string
	runtimeBin           string
	runtimeConfig        string
	runtimeConfigs       string
	runtimeData          string
	runtimeLogs          string
	runtimeState         string
	instance             string
	instanceExample      string
	assetManifest        string
	clientManifest       string
	mysqlManifest        string
	vcRuntimeManifest    string
	vcRuntimeRoot        string
	channelAsset         string
	serverExe            string
	doctorExe            string
	launcherExe          string
	controlExe           string
	runtimeVersionSource string
	runtimeVersion       string
	processState         string
	binaryInstallState   string
	mysqlRoot            string
	mysqlServer          string
	mysqlData            string
	mysqlConfig          string
	mysqlPIDFile         string
	mysqldExe            string
	mysqlProcessState    string
	mysqlInstallState    string
	mysqlDataState       string
	mysqlLegacyDataState string
}

func discoverPaths() (projectPaths, error) {
	if configured := strings.TrimSpace(os.Getenv("DNF90_PROJECT_ROOT")); configured != "" {
		root, err := filepath.Abs(configured)
		if err != nil {
			return projectPaths{}, fmt.Errorf("resolve DNF90_PROJECT_ROOT: %w", err)
		}
		if isProjectRoot(root) {
			return newProjectPaths(root), nil
		}
		return projectPaths{}, fmt.Errorf("DNF90_PROJECT_ROOT is not a DNF90 project: %s", root)
	}

	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	seen := make(map[string]struct{})
	for _, start := range starts {
		current, err := filepath.Abs(start)
		if err != nil {
			continue
		}
		for {
			key := strings.ToLower(filepath.Clean(current))
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				if isProjectRoot(current) {
					return newProjectPaths(current), nil
				}
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return projectPaths{}, errors.New("cannot find DNF90 project root; set DNF90_PROJECT_ROOT")
}

func isProjectRoot(root string) bool {
	return isRegularFile(filepath.Join(root, "go-server", "go.mod")) &&
		isRegularFile(filepath.Join(root, "deploy", "templates", "instance.example.json"))
}

func newProjectPaths(root string) projectPaths {
	root = filepath.Clean(root)
	runtimeRoot := filepath.Join(root, "runtime")
	return projectPaths{
		projectRoot:          root,
		goServerRoot:         filepath.Join(root, "go-server"),
		runtimeRoot:          runtimeRoot,
		runtimeBin:           filepath.Join(runtimeRoot, "bin"),
		runtimeConfig:        filepath.Join(runtimeRoot, "config"),
		runtimeConfigs:       filepath.Join(runtimeRoot, "configs"),
		runtimeData:          filepath.Join(runtimeRoot, "data", "dnf"),
		runtimeLogs:          filepath.Join(runtimeRoot, "logs"),
		runtimeState:         filepath.Join(runtimeRoot, "state"),
		instance:             filepath.Join(runtimeRoot, "config", "instance.json"),
		instanceExample:      filepath.Join(root, "deploy", "templates", "instance.example.json"),
		assetManifest:        filepath.Join(root, "deploy", "assets", "manifest.json"),
		clientManifest:       filepath.Join(root, "deploy", "assets", "client-compatibility.json"),
		mysqlManifest:        filepath.Join(root, "deploy", "assets", "mysql-portable.json"),
		vcRuntimeManifest:    filepath.Join(root, "deploy", "assets", "vcruntime-app-local.json"),
		vcRuntimeRoot:        filepath.Join(root, "deploy", "vendor", "vcruntime", "x64"),
		channelAsset:         filepath.Join(root, "deploy", "assets", "channel_info.etc"),
		serverExe:            filepath.Join(runtimeRoot, "bin", "DNF90Server.exe"),
		doctorExe:            filepath.Join(runtimeRoot, "bin", "DNF90Doctor.exe"),
		launcherExe:          filepath.Join(runtimeRoot, "bin", "DNF90Launcher.exe"),
		controlExe:           filepath.Join(runtimeRoot, "bin", "DNF90Control.exe"),
		runtimeVersionSource: filepath.Join(root, "deploy", "windows", "runtime.version"),
		runtimeVersion:       filepath.Join(runtimeRoot, "bin", "DNF90Build.version"),
		processState:         filepath.Join(runtimeRoot, "state", "server-process.json"),
		binaryInstallState:   filepath.Join(runtimeRoot, "state", "binary-install.json"),
		mysqlRoot:            filepath.Join(runtimeRoot, "mysql"),
		mysqlServer:          filepath.Join(runtimeRoot, "mysql", "server"),
		mysqlData:            filepath.Join(runtimeRoot, "mysql", "data"),
		mysqlConfig:          filepath.Join(runtimeRoot, "config", "mysql.ini"),
		mysqlPIDFile:         filepath.Join(runtimeRoot, "state", "mysql.pid"),
		mysqldExe:            filepath.Join(runtimeRoot, "mysql", "server", "bin", "mysqld.exe"),
		mysqlProcessState:    filepath.Join(runtimeRoot, "state", "mysql-process.json"),
		mysqlInstallState:    filepath.Join(runtimeRoot, "mysql", "server", ".dnf90-install.json"),
		mysqlDataState:       filepath.Join(runtimeRoot, "mysql", "data-state.json"),
		mysqlLegacyDataState: filepath.Join(runtimeRoot, "mysql", "data", ".dnf90-data.json"),
	}
}

func (p projectPaths) ensureDirectories() error {
	for _, path := range []string{
		p.runtimeRoot,
		p.runtimeBin,
		p.runtimeConfig,
		p.runtimeConfigs,
		filepath.Join(p.runtimeConfigs, "dnf"),
		filepath.Join(p.runtimeConfigs, "servergroup"),
		p.runtimeData,
		p.runtimeLogs,
		p.runtimeState,
		p.mysqlRoot,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create runtime directory %s: %w", path, err)
		}
	}
	return nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
