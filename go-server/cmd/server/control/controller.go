package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type controller struct {
	paths  projectPaths
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func newController(paths projectPaths, stdout, stderr io.Writer) *controller {
	return &controller{
		paths:  paths,
		stdin:  os.Stdin,
		stdout: stdout,
		stderr: stderr,
	}
}

func (c *controller) build(ctx context.Context, force bool) error {
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	if state, found, err := c.loadManagedProcess(cfg.InstallationID, true); err != nil {
		return err
	} else if found {
		return fmt.Errorf("refusing to rebuild while DNF90 is running as PID %d", state.PID)
	}
	if state, found, err := c.loadManagedMySQLProcess(cfg, true); err != nil {
		return err
	} else if found {
		return fmt.Errorf(
			"refusing to rebuild runtime configuration while portable MySQL is running as PID %d; run STOP.bat first",
			state.PID,
		)
	}
	if err := generateRuntimeConfigs(c.paths, cfg); err != nil {
		return err
	}
	if err := validateAssets(c.paths, c.stdout); err != nil {
		return err
	}
	if err := c.buildBinaries(ctx, cfg, force); err != nil {
		return err
	}
	if err := writeAssetState(c.paths); err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, "Build completed:", c.paths.serverExe)
	return nil
}

func (c *controller) check(ctx context.Context, opts checkOptions) error {
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	if state, found, err := c.loadManagedProcess(cfg.InstallationID, true); err != nil {
		return err
	} else if found {
		return fmt.Errorf(
			"refusing to rewrite or preflight runtime configuration while DNF90 is running as PID %d; use STATUS.bat instead",
			state.PID,
		)
	}
	if state, found, err := c.loadManagedMySQLProcess(cfg, true); err != nil {
		return err
	} else if found {
		return fmt.Errorf(
			"refusing to rewrite or preflight runtime configuration while portable MySQL is running as PID %d; run STOP.bat first",
			state.PID,
		)
	}
	if err := generateRuntimeConfigs(c.paths, cfg); err != nil {
		return err
	}
	if err := validateAssets(c.paths, c.stdout); err != nil {
		return err
	}
	if opts.checkClient || strings.TrimSpace(cfg.Client.Directory) != "" {
		if _, _, err := validateClient(c.paths, cfg, "", c.stdout); err != nil {
			return err
		}
	}
	if !isRegularFile(c.paths.doctorExe) {
		return fmt.Errorf("doctor binary is missing; run control build first")
	}
	return c.runDoctor(ctx, cfg, doctorOptions{
		skipDatabase: opts.skipDatabase,
		skipPorts:    opts.skipPorts,
	})
}

func (c *controller) start(ctx context.Context, rebuild bool) error {
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	if state, found, err := c.loadManagedProcess(cfg.InstallationID, true); err != nil {
		return err
	} else if found {
		versionCurrent, err := runtimeBuildVersionCurrent(c.paths)
		if err != nil {
			return err
		}
		if !versionCurrent {
			return errors.New(
				"DNF90 runtime update is pending; close game clients and run LOGIN.bat",
			)
		}
		state, err = c.validateRunningServerConfig(cfg, state)
		if err != nil {
			return err
		}
		readyURL, urlErr := readinessURL(cfg)
		if urlErr != nil {
			return urlErr
		}
		if !c.httpReady(ctx, readyURL) {
			return fmt.Errorf(
				"DNF90 PID %d is running but not ready; run STATUS.bat or STOP.bat before retrying",
				state.PID,
			)
		}
		fmt.Fprintf(c.stdout, "DNF90 is already READY. PID=%d\n", state.PID)
		return nil
	}
	if mysqlState, found, err := c.loadManagedMySQLProcess(cfg, true); err != nil {
		return err
	} else if found {
		if err := validateRunningDatabaseConfig(c.paths, cfg, mysqlState); err != nil {
			return err
		}
	}

	fmt.Fprintln(c.stdout, "DNF90 project:", c.paths.projectRoot)
	if err := generateRuntimeConfigs(c.paths, cfg); err != nil {
		return err
	}
	if err := validateAssets(c.paths, c.stdout); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Client.Directory) != "" {
		if _, _, err := validateClient(c.paths, cfg, "", c.stdout); err != nil {
			return err
		}
	}
	if err := c.buildBinaries(ctx, cfg, rebuild); err != nil {
		return err
	}
	databaseStarted, err := c.startDatabase(ctx, cfg)
	if err != nil {
		if databaseStarted {
			return errors.Join(err, c.rollbackDatabase(cfg))
		}
		return err
	}
	rollbackDatabase := func() error {
		if databaseStarted {
			return c.rollbackDatabase(cfg)
		}
		return nil
	}
	if err := c.runDoctor(ctx, cfg, doctorOptions{}); err != nil {
		return errors.Join(
			fmt.Errorf("DNF90 preflight failed: %w", err),
			rollbackDatabase(),
		)
	}
	if err := writeAssetState(c.paths); err != nil {
		return errors.Join(err, rollbackDatabase())
	}

	state, err := c.startServerProcess(cfg)
	if err != nil {
		if state.PID > 0 {
			// A verified child may still exist. Keep the database up and
			// surface its PID instead of producing a live process with a dead DB.
			return err
		}
		return errors.Join(err, rollbackDatabase())
	}
	cleanup := func() error {
		if err := forceTerminateProcess(
			state.PID,
			c.paths.serverExe,
			state.ProcessCreatedAt,
		); err != nil {
			// Keep the state file and database intact so STOP.bat can still
			// identify and control the verified process.
			return err
		}
		removeErr := os.Remove(c.paths.processState)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		return errors.Join(removeErr, rollbackDatabase())
	}
	if err := c.waitReady(ctx, cfg, state, 120*time.Second); err != nil {
		c.showStartupLogs(state, 100)
		return errors.Join(err, cleanup())
	}
	if err := c.runDoctor(ctx, cfg, doctorOptions{
		skipDatabase: true,
		expectListen: true,
	}); err != nil {
		return errors.Join(
			fmt.Errorf("server became ready but runtime listeners failed validation: %w", err),
			cleanup(),
		)
	}

	readyURL, _ := readinessURL(cfg)
	fmt.Fprintln(c.stdout)
	fmt.Fprintln(c.stdout, "DNF90 is READY.")
	fmt.Fprintln(c.stdout, "PID:", state.PID)
	fmt.Fprintln(c.stdout, "Channel:", cfg.Server.ChannelListen)
	fmt.Fprintln(c.stdout, "Admin readiness:", readyURL)
	return nil
}

func (c *controller) rollbackDatabase(cfg instanceConfig) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := c.stopDatabase(rollbackCtx, cfg); err != nil {
		return fmt.Errorf("roll back database started by this run: %w", err)
	}
	return nil
}

func (c *controller) stop(ctx context.Context, keepDatabase bool) error {
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	state, found, err := c.loadManagedProcess(cfg.InstallationID, true)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(c.stdout, "DNF90 server is not running.")
	} else {
		if err := c.gracefulStop(ctx, cfg, state); err != nil {
			return err
		}
		fmt.Fprintln(c.stdout, "DNF90 server stopped.")
	}
	if !keepDatabase {
		if err := c.stopDatabase(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (c *controller) status(ctx context.Context) error {
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	if err := validateAssets(c.paths, c.stdout); err != nil {
		return err
	}
	mysqlState, mysqlFound, err := c.loadManagedMySQLProcess(cfg, false)
	if err != nil {
		return err
	}
	if mysqlFound {
		if err := validateRunningDatabaseConfig(c.paths, cfg, mysqlState); err != nil {
			return err
		}
		mysqlReady := testTCP(ctx, cfg.Database.Host, cfg.Database.Port, time.Second)
		fmt.Fprintln(c.stdout, "MySQL: RUNNING")
		fmt.Fprintln(c.stdout, "MySQL PID:", mysqlState.PID)
		fmt.Fprintln(c.stdout, "MySQL Ready:", mysqlReady)
		if !mysqlReady {
			return errors.New("portable MySQL is running but not accepting connections")
		}
	} else {
		fmt.Fprintln(c.stdout, "MySQL: STOPPED")
	}

	state, found, err := c.loadManagedProcess(cfg.InstallationID, false)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(c.stdout, "Server: STOPPED")
		return errors.New("server is not running")
	}
	state, err = c.validateRunningServerConfig(cfg, state)
	if err != nil {
		return err
	}
	readyURL, err := readinessURL(cfg)
	if err != nil {
		return err
	}
	ready := c.httpReady(ctx, readyURL)
	fmt.Fprintln(c.stdout, "Server: RUNNING")
	fmt.Fprintln(c.stdout, "PID:", state.PID)
	fmt.Fprintln(c.stdout, "Started:", state.StartedAt.UTC().Format(time.RFC3339Nano))
	fmt.Fprintln(c.stdout, "Ready:", ready)
	fmt.Fprintln(c.stdout, "Channel:", cfg.Server.ChannelListen)
	fmt.Fprintf(
		c.stdout,
		"Database: %s %s@%s:%d/%s\n",
		cfg.Database.Mode,
		cfg.Database.User,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.Name,
	)
	if isRegularFile(c.paths.doctorExe) {
		if err := c.runDoctor(ctx, cfg, doctorOptions{expectListen: true}); err != nil {
			return fmt.Errorf("runtime doctor reported a failure: %w", err)
		}
	}
	c.showLogTail(state.Stderr, 20)
	if !ready {
		return errors.New("server is running but not ready")
	}
	return nil
}

func (c *controller) launchClient(
	ctx context.Context,
	directoryOverride string,
	multiInstance bool,
	username string,
	password string,
) error {
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	state, found, err := c.loadManagedProcess(cfg.InstallationID, false)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("DNF90 server is not running; run START.bat first")
	}
	if _, err := c.validateRunningServerConfig(cfg, state); err != nil {
		return err
	}
	clientRoot, executable, err := validateClient(c.paths, cfg, directoryOverride, c.stdout)
	if err != nil {
		return err
	}
	readyURL, err := readinessURL(cfg)
	if err != nil {
		return err
	}
	if !c.httpReady(ctx, readyURL) {
		return errors.New("DNF90 server is not ready; run control start first")
	}
	accountID := cfg.Server.AccountID
	if strings.TrimSpace(username) != "" {
		accountID, err = c.authenticateRunningLocalAccount(
			ctx,
			cfg,
			username,
			password,
		)
		if err != nil {
			return err
		}
	}
	argument, err := clientLaunchArgument(cfg)
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, argument)
	cmd.Dir = clientRoot
	hookCreate := "0"
	if cfg.Client.HookCreate {
		hookCreate = "1"
	}
	multiClient := "0"
	if multiInstance {
		multiClient = "1"
	}
	cmd.Env = mergeEnvironment(os.Environ(), map[string]string{
		"DNF_HOOK_CREATE":  hookCreate,
		"DNF_MULTI_CLIENT": multiClient,
	})
	configureClientProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start client: %w", err)
	}
	pid := cmd.Process.Pid
	if err := c.registerClientAccount(ctx, cfg, pid, accountID); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return fmt.Errorf("bind client PID %d to authenticated account: %w", pid, err)
	}
	if multiInstance {
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = cmd.Process.Release()
			return ctx.Err()
		case <-timer.C:
		}
		process, inspectErr := inspectProcess(pid)
		if inspectErr != nil {
			_ = cmd.Process.Release()
			return fmt.Errorf("inspect additional client PID %d: %w", pid, inspectErr)
		}
		if !process.running {
			_ = cmd.Process.Release()
			return fmt.Errorf(
				"additional client PID %d exited during the 5-second startup check",
				pid,
			)
		}
		if !sameExecutable(process.executable, executable) {
			_ = cmd.Process.Release()
			return fmt.Errorf(
				"additional client PID %d executable changed from %s to %s",
				pid,
				executable,
				process.executable,
			)
		}
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release client process handle: %w", err)
	}
	if multiInstance {
		fmt.Fprintf(
			c.stdout,
			"Additional client survived startup check: %s (PID=%d)\n",
			executable,
			pid,
		)
	} else {
		fmt.Fprintf(c.stdout, "Client started: %s (PID=%d)\n", executable, pid)
	}
	return nil
}

func (c *controller) configureClient(directory string) error {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return errors.New("client directory is required")
	}
	if !filepath.IsAbs(directory) {
		return errors.New("client directory must be an absolute path")
	}

	resolved, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	clientRoot, _, err := validateClient(
		c.paths,
		resolved,
		directory,
		c.stdout,
	)
	if err != nil {
		return err
	}

	// loadInstance resolves AUTO_DETECT values for this process. Re-read the
	// persisted form so changing the client path does not freeze those values.
	persisted, err := decodeInstance(c.paths.instance)
	if err != nil {
		return err
	}
	persisted.Client.Directory = clientRoot
	data, err := marshalInstance(persisted)
	if err != nil {
		return err
	}
	if err := writeFile(c.paths.instance, data, 0o600); err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, "Client directory configured:", clientRoot)
	return nil
}

func clientAccountURL(cfg instanceConfig) (string, error) {
	port, err := listenPort(cfg.Server.AdminListen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://127.0.0.1:%d/local/client-account", port), nil
}

func (c *controller) registerClientAccount(
	ctx context.Context,
	cfg instanceConfig,
	pid int,
	accountID string,
) error {
	if pid <= 0 {
		return errors.New("client PID must be positive")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return errors.New("client account ID is required")
	}
	url, err := clientAccountURL(cfg)
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		PID       uint32 `json:"pid"`
		AccountID string `json:"account_id"`
	}{
		PID:       uint32(pid),
		AccountID: accountID,
	})
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		url,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Admin-Token", cfg.Server.AdminToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"client account registration returned %s: %s",
			response.Status,
			strings.TrimSpace(string(responseBody)),
		)
	}
	return nil
}

func clientLaunchArgument(cfg instanceConfig) (string, error) {
	loginHost, _, err := net.SplitHostPort(cfg.Server.ChannelListen)
	if err != nil {
		return "", fmt.Errorf("parse client login endpoint: %w", err)
	}
	channelPort, err := listenPort(cfg.Server.ChannelListen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"99?%s?%d?%d?%s?01?1?0?0?0?0?1?9n2b1c8r3w7y?0?0?19847",
		loginHost,
		channelPort,
		cfg.Client.InitialGamePort,
		cfg.Protocol.GameOuterToken,
	), nil
}

type doctorOptions struct {
	skipDatabase bool
	skipPorts    bool
	expectListen bool
}

func (c *controller) runDoctor(ctx context.Context, cfg instanceConfig, opts doctorOptions) error {
	if !isRegularFile(c.paths.doctorExe) {
		return fmt.Errorf("doctor binary is missing: %s", c.paths.doctorExe)
	}
	args := []string{
		"-root", c.paths.runtimeRoot,
		"-config", "configs/dnfbridge.toml",
		"-logic-config", "configs/dnf/logic.toml",
		"-channel-info", filepath.ToSlash(cfg.Game.ChannelInfoPath),
		"-channel-listen", cfg.Server.ChannelListen,
		"-game-listen-host", cfg.Server.AdvertiseIP,
	}
	if opts.skipDatabase {
		args = append(args, "-skip-database")
	}
	if opts.skipPorts {
		args = append(args, "-skip-ports")
	}
	if opts.expectListen {
		args = append(args, "-expect-listening")
	}
	cmd := exec.CommandContext(ctx, c.paths.doctorExe, args...)
	cmd.Dir = c.paths.runtimeRoot
	cmd.Env = runtimeEnvironment(cfg)
	cmd.Stdout = c.stdout
	cmd.Stderr = c.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("doctor: %w", err)
	}
	return nil
}

func (c *controller) buildBinaries(ctx context.Context, cfg instanceConfig, force bool) error {
	versionCurrent, err := runtimeBuildVersionCurrent(c.paths)
	if err != nil {
		return err
	}
	if !force &&
		isRegularFile(c.paths.serverExe) &&
		isRegularFile(c.paths.doctorExe) &&
		isRegularFile(c.paths.launcherExe) &&
		versionCurrent {
		fmt.Fprintln(c.stdout, "Binaries are present.")
		return nil
	}
	// LOGIN.bat may discover a standard Windows Go installation that is not on
	// PATH. The wrapper passes that absolute executable through the environment
	// so the freshly built controller can use the same toolchain for the full
	// server/doctor/launcher set.
	goExecutable := goExecutableForBuild(cfg)
	if _, err := exec.LookPath(goExecutable); err != nil {
		return fmt.Errorf(
			"Go is required to build the server; install the version from go-server/go.mod: %w",
			err,
		)
	}
	version := exec.CommandContext(ctx, goExecutable, "version")
	version.Dir = c.paths.goServerRoot
	version.Stdout = c.stdout
	version.Stderr = c.stderr
	if err := version.Run(); err != nil {
		return fmt.Errorf("run go version: %w", err)
	}
	targets := []builtBinary{
		{destination: c.paths.serverExe, pkg: `.\cmd\server\dnf90`, label: "server"},
		{destination: c.paths.doctorExe, pkg: `.\cmd\server\doctor`, label: "doctor"},
		{
			destination: c.paths.launcherExe,
			pkg:         `.\cmd\server\launcher`,
			label:       "launcher",
			ldflags:     "-H=windowsgui",
		},
	}
	for index := range targets {
		target := &targets[index]
		temp, err := os.CreateTemp(
			filepath.Dir(target.destination),
			"."+filepath.Base(target.destination)+".build-*",
		)
		if err != nil {
			return fmt.Errorf("reserve DNF90 %s build path: %w", target.label, err)
		}
		target.temp = temp.Name()
		if closeErr := temp.Close(); closeErr != nil {
			return fmt.Errorf("close DNF90 %s build placeholder: %w", target.label, closeErr)
		}
		if removeErr := os.Remove(target.temp); removeErr != nil {
			return fmt.Errorf("release DNF90 %s build placeholder: %w", target.label, removeErr)
		}
	}
	defer func() {
		if _, err := os.Lstat(c.paths.binaryInstallState); err == nil || !os.IsNotExist(err) {
			return
		}
		for _, target := range targets {
			_ = os.Remove(target.temp)
		}
	}()
	for _, target := range targets {
		args := []string{
			"build",
			"-buildvcs=false",
			"-mod=readonly",
			"-trimpath",
		}
		if target.ldflags != "" {
			args = append(args, "-ldflags", target.ldflags)
		}
		args = append(args, "-o", target.temp, target.pkg)
		cmd := exec.CommandContext(ctx, goExecutable, args...)
		cmd.Dir = c.paths.goServerRoot
		cmd.Stdout = c.stdout
		cmd.Stderr = c.stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("DNF90 %s build failed: %w", target.label, err)
		}
	}
	if err := installBuiltBinarySet(c.paths, targets); err != nil {
		return err
	}
	return installRuntimeBuildVersion(c.paths)
}

func goExecutableForBuild(cfg instanceConfig) string {
	if executable := strings.TrimSpace(os.Getenv("DNF90_GO_EXE")); executable != "" {
		return executable
	}
	if executable := strings.TrimSpace(cfg.Build.GoExecutable); executable != "" {
		return executable
	}
	return "go"
}

type builtBinary struct {
	destination string
	temp        string
	pkg         string
	label       string
	ldflags     string
}

func (c *controller) startServerProcess(cfg instanceConfig) (processState, error) {
	if !isRegularFile(c.paths.serverExe) {
		return processState{}, fmt.Errorf("server binary is missing: %s", c.paths.serverExe)
	}
	executableSHA256, err := fileSHA256(c.paths.serverExe)
	if err != nil {
		return processState{}, err
	}
	runtimeConfigSHA256, err := desiredServerRuntimeConfigSHA256(cfg)
	if err != nil {
		return processState{}, err
	}
	stamp := time.Now().Format("20060102-150405")
	stdoutPath := filepath.Join(c.paths.runtimeLogs, "server-"+stamp+".stdout.log")
	stderrPath := filepath.Join(c.paths.runtimeLogs, "server-"+stamp+".stderr.log")
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return processState{}, fmt.Errorf("open server stdout log: %w", err)
	}
	defer func() { _ = stdoutFile.Close() }()
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return processState{}, fmt.Errorf("open server stderr log: %w", err)
	}
	defer func() { _ = stderrFile.Close() }()

	cmd := exec.Command(c.paths.serverExe, "-config", "configs/dnfbridge.toml")
	cmd.Dir = c.paths.runtimeRoot
	cmd.Env = runtimeEnvironment(cfg)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	configureServerProcess(cmd)
	if err := cmd.Start(); err != nil {
		return processState{}, fmt.Errorf("start DNF90 server: %w", err)
	}
	state := processState{
		PID:                 cmd.Process.Pid,
		StartedAt:           time.Now().UTC(),
		Executable:          c.paths.serverExe,
		ExecutableSHA256:    executableSHA256,
		InstallationID:      cfg.InstallationID,
		RuntimeConfigSHA256: runtimeConfigSHA256,
		Stdout:              stdoutPath,
		Stderr:              stderrPath,
	}
	if err := c.verifyStartedProcess(&state); err != nil {
		return containStartedProcessFailure(
			cmd,
			state,
			c.paths.serverExe,
			c.paths.processState,
			err,
		)
	}
	if err := writeJSON(c.paths.processState, state, 0o600); err != nil {
		return containStartedProcessFailure(
			cmd,
			state,
			c.paths.serverExe,
			c.paths.processState,
			fmt.Errorf("persist started DNF90 process state: %w", err),
		)
	}
	if err := cmd.Process.Release(); err != nil {
		return containStartedProcessFailure(
			cmd,
			state,
			c.paths.serverExe,
			c.paths.processState,
			fmt.Errorf("release server process handle: %w", err),
		)
	}
	return state, nil
}

func (c *controller) validateRunningServerConfig(
	cfg instanceConfig,
	state processState,
) (processState, error) {
	desiredSHA256, err := desiredServerRuntimeConfigSHA256(cfg)
	if err != nil {
		return processState{}, err
	}
	currentSHA256, err := currentServerRuntimeConfigSHA256(c.paths, cfg)
	if err != nil {
		return processState{}, err
	}
	if !strings.EqualFold(desiredSHA256, currentSHA256) {
		return processState{}, errors.New(
			"DNF90 is running with a different runtime configuration; run STOP.bat before applying configuration changes",
		)
	}
	if state.RuntimeConfigSHA256 == "" {
		return processState{}, errors.New(
			"running DNF90 process state predates runtime configuration binding; use STOP.bat, then START.bat to restart it safely",
		)
	}
	if !strings.EqualFold(state.RuntimeConfigSHA256, desiredSHA256) ||
		!strings.EqualFold(state.RuntimeConfigSHA256, currentSHA256) {
		return processState{}, errors.New(
			"DNF90 runtime configuration changed after launch; run STOP.bat before retrying",
		)
	}
	return state, nil
}

func containStartedProcessFailure(
	cmd *exec.Cmd,
	state processState,
	expectedExecutable string,
	statePath string,
	cause error,
) (processState, error) {
	terminateErr := forceTerminateProcess(
		state.PID,
		expectedExecutable,
		state.ProcessCreatedAt,
	)
	if terminateErr == nil {
		_ = cmd.Wait()
		removeErr := os.Remove(statePath)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		return processState{}, errors.Join(cause, removeErr)
	}

	live, inspectErr := inspectProcess(state.PID)
	if inspectErr == nil && !live.running {
		_ = cmd.Wait()
		removeErr := os.Remove(statePath)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		return processState{}, errors.Join(cause, terminateErr, removeErr)
	}

	var ownershipErr error
	if inspectErr != nil {
		ownershipErr = inspectErr
	} else if !sameExecutable(live.executable, expectedExecutable) {
		ownershipErr = fmt.Errorf(
			"PID %d no longer belongs to expected executable %s",
			state.PID,
			expectedExecutable,
		)
	} else if live.createdAt.IsZero() {
		ownershipErr = fmt.Errorf("PID %d has no process creation identity", state.PID)
	} else {
		state.ProcessCreatedAt = live.createdAt
		if err := writeJSON(statePath, state, 0o600); err != nil {
			ownershipErr = fmt.Errorf(
				"persist emergency process ownership state: %w",
				err,
			)
		}
	}
	releaseErr := cmd.Process.Release()
	if releaseErr != nil {
		releaseErr = fmt.Errorf("release failed process handle: %w", releaseErr)
	}
	return state, errors.Join(
		cause,
		terminateErr,
		ownershipErr,
		releaseErr,
		fmt.Errorf(
			"PID %d may still be running; its database dependency was left running and STOP.bat should be used before retrying",
			state.PID,
		),
	)
}

func (c *controller) verifyStartedProcess(state *processState) error {
	if state == nil {
		return errors.New("started process state is nil")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		live, err := inspectProcess(state.PID)
		if err == nil && live.running {
			if !sameExecutable(live.executable, c.paths.serverExe) {
				return fmt.Errorf(
					"started PID %d executable %s does not match %s",
					state.PID,
					live.executable,
					c.paths.serverExe,
				)
			}
			if live.createdAt.IsZero() {
				return fmt.Errorf("started PID %d has no creation time", state.PID)
			}
			state.ProcessCreatedAt = live.createdAt
			return nil
		}
		if err != nil && time.Now().After(deadline) {
			return err
		}
		if !live.running && time.Now().After(deadline) {
			return fmt.Errorf("started server PID %d exited before identity verification", state.PID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (c *controller) waitReady(
	ctx context.Context,
	cfg instanceConfig,
	state processState,
	timeout time.Duration,
) error {
	readyURL, err := readinessURL(cfg)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		live, inspectErr := inspectProcess(state.PID)
		if inspectErr != nil {
			return inspectErr
		}
		if !live.running {
			return fmt.Errorf("DNF90 server exited before becoming ready")
		}
		if !sameExecutable(live.executable, c.paths.serverExe) {
			return fmt.Errorf("PID %d changed executable ownership while starting", state.PID)
		}
		if !live.createdAt.Equal(state.ProcessCreatedAt) {
			return fmt.Errorf("PID %d changed process creation identity while starting", state.PID)
		}
		if c.httpReady(ctx, readyURL) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("DNF90 did not become ready within %s", timeout)
}

func readinessURL(cfg instanceConfig) (string, error) {
	port, err := listenPort(cfg.Server.AdminListen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://127.0.0.1:%d/healthz/ready", port), nil
}

func shutdownURL(cfg instanceConfig) (string, error) {
	port, err := listenPort(cfg.Server.AdminListen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://127.0.0.1:%d/local/shutdown", port), nil
}

func (c *controller) httpReady(ctx context.Context, url string) bool {
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode == http.StatusOK
}

func (c *controller) gracefulStop(ctx context.Context, cfg instanceConfig, state processState) error {
	url, err := shutdownURL(cfg)
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, nil)
	if err == nil {
		request.Header.Set("X-Admin-Token", cfg.Server.AdminToken)
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			fmt.Fprintln(c.stderr, "Warning: graceful shutdown request failed:", requestErr)
		} else {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				fmt.Fprintln(c.stderr, "Warning: graceful shutdown returned", response.Status)
			}
		}
	} else {
		fmt.Fprintln(c.stderr, "Warning: create graceful shutdown request:", err)
	}
	cancel()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		live, inspectErr := inspectProcess(state.PID)
		if inspectErr != nil {
			return inspectErr
		}
		if !live.running {
			if err := os.Remove(c.paths.processState); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stopped server process state: %w", err)
			}
			return nil
		}
		if !sameExecutable(live.executable, c.paths.serverExe) {
			return fmt.Errorf("PID %d changed ownership while stopping", state.PID)
		}
		if !live.createdAt.Equal(state.ProcessCreatedAt) {
			return fmt.Errorf("PID %d changed process creation identity while stopping", state.PID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	fmt.Fprintln(c.stderr, "Warning: graceful shutdown timed out; forcing verified DNF90 PID.")
	if err := forceTerminateProcess(state.PID, c.paths.serverExe, state.ProcessCreatedAt); err != nil {
		return err
	}
	return os.Remove(c.paths.processState)
}

func (c *controller) loadManagedProcess(
	expectedInstallationID string,
	removeStale bool,
) (processState, bool, error) {
	if !isRegularFile(c.paths.processState) {
		return processState{}, false, nil
	}
	var state processState
	if err := readStrictJSON(c.paths.processState, &state); err != nil {
		return processState{}, false, err
	}
	if state.PID <= 0 {
		return processState{}, false, fmt.Errorf("invalid managed PID %d", state.PID)
	}
	if state.ProcessCreatedAt.IsZero() {
		return processState{}, false, errors.New("managed process state has no process creation time")
	}
	live, err := inspectProcess(state.PID)
	if err != nil {
		return processState{}, false, err
	}
	if !live.running || !live.createdAt.Equal(state.ProcessCreatedAt) {
		if removeStale {
			if err := os.Remove(c.paths.processState); err != nil && !os.IsNotExist(err) {
				return processState{}, false, fmt.Errorf("remove stale process state: %w", err)
			}
		}
		return processState{}, false, nil
	}
	if state.InstallationID != expectedInstallationID {
		return processState{}, false, fmt.Errorf(
			"managed process installation %q does not match this installation",
			state.InstallationID,
		)
	}
	if !sameExecutable(state.Executable, c.paths.serverExe) {
		return processState{}, false, fmt.Errorf(
			"process state executable %s does not match %s",
			state.Executable,
			c.paths.serverExe,
		)
	}
	if !sameExecutable(live.executable, c.paths.serverExe) {
		return processState{}, false, fmt.Errorf(
			"PID %d exists but executable %s is not the managed DNF90 server",
			state.PID,
			live.executable,
		)
	}
	executableSHA256, err := fileSHA256(c.paths.serverExe)
	if err != nil {
		return processState{}, false, err
	}
	if !strings.EqualFold(executableSHA256, state.ExecutableSHA256) {
		return processState{}, false, fmt.Errorf(
			"managed server executable SHA256 changed while PID %d is running",
			state.PID,
		)
	}
	return state, true, nil
}

func writeJSON(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeFile(path, append(data, '\n'), mode)
}

func (c *controller) showStartupLogs(state processState, count int) {
	for _, path := range []string{state.Stderr, state.Stdout} {
		c.showLogTail(path, count)
	}
}

func (c *controller) showLogTail(path string, count int) {
	if count <= 0 || !isRegularFile(path) {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(c.stderr, "Warning: read log:", err)
		return
	}
	defer func() { _ = file.Close() }()
	lines := make([]string, count)
	total := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines[total%count] = scanner.Text()
		total++
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(c.stderr, "Warning: scan log:", err)
		return
	}
	if total == 0 {
		return
	}
	fmt.Fprintln(c.stdout)
	fmt.Fprintf(c.stdout, "--- last %d lines: %s ---\n", min(total, count), path)
	start := 0
	if total > count {
		start = total % count
	}
	limit := min(total, count)
	for index := 0; index < limit; index++ {
		fmt.Fprintln(c.stdout, lines[(start+index)%count])
	}
}

func testTCP(ctx context.Context, host string, port int, timeout time.Duration) bool {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
