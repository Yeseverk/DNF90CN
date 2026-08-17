package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	portableMySQLPhaseInitializing = "initializing"
	portableMySQLPhaseInsecure     = "initialized-insecure"
	portableMySQLPhaseReady        = "ready"
	portableMySQLDataRelative      = "mysql/data"
)

func (c *controller) startDatabase(ctx context.Context, cfg instanceConfig) (bool, error) {
	if cfg.Database.Mode != "portable" {
		return false, fmt.Errorf("unsupported database mode %q", cfg.Database.Mode)
	}
	if err := preparePortableMySQL(ctx, c.paths, c.stdout); err != nil {
		return false, err
	}

	state, found, err := c.loadManagedMySQLProcess(cfg, true)
	if err != nil {
		return false, err
	}
	if found {
		if !testTCP(ctx, cfg.Database.Host, cfg.Database.Port, time.Second) {
			return false, fmt.Errorf(
				"managed MySQL PID %d is running but TCP port %d is not ready",
				state.PID,
				cfg.Database.Port,
			)
		}
		if err := c.ensurePortableDatabase(ctx, cfg); err != nil {
			return false, err
		}
		if err := writeJSON(c.paths.mysqlProcessState, state, 0o600); err != nil {
			return false, fmt.Errorf(
				"persist verified portable MySQL process identity: %w",
				err,
			)
		}
		fmt.Fprintf(c.stdout, "Portable MySQL is already READY. PID=%d\n", state.PID)
		return false, nil
	}
	if testTCP(ctx, cfg.Database.Host, cfg.Database.Port, time.Second) {
		return false, fmt.Errorf(
			"TCP port %d is occupied by a process outside this DNF90 installation",
			cfg.Database.Port,
		)
	}
	if _, err := c.ensurePortableMySQLData(ctx, cfg); err != nil {
		return false, err
	}

	state, err = c.startPortableMySQLProcess(ctx, cfg)
	if err != nil {
		return state.PID > 0, err
	}
	if err := waitTCP(ctx, cfg.Database.Host, cfg.Database.Port, 120*time.Second); err != nil {
		c.showStartupLogs(state, 100)
		return true, err
	}
	if err := c.ensurePortableDatabase(ctx, cfg); err != nil {
		c.showStartupLogs(state, 100)
		return true, err
	}
	fmt.Fprintf(c.stdout, "Portable MySQL is READY. PID=%d\n", state.PID)
	return true, nil
}

func (c *controller) stopDatabase(ctx context.Context, cfg instanceConfig) error {
	state, found, err := c.loadManagedMySQLProcess(cfg, true)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(c.stdout, "Portable MySQL is not running; data was preserved.")
		return nil
	}
	shutdownCfg, err := managedMySQLInstanceConfig(cfg, state)
	if err != nil {
		return err
	}

	mysqldSHA256, err := fileSHA256(c.paths.mysqldExe)
	if err != nil {
		return err
	}
	if err := migrateLegacyPortableMySQLDataState(
		c.paths,
		shutdownCfg,
		mysqldSHA256,
	); err != nil {
		return err
	}
	var dataState portableMySQLDataState
	if err := readStrictJSON(c.paths.mysqlDataState, &dataState); err != nil {
		return fmt.Errorf("read portable MySQL ownership state before shutdown: %w", err)
	}
	if err := validatePortableMySQLDataState(
		dataState,
		c.paths,
		shutdownCfg,
		mysqldSHA256,
	); err != nil {
		return fmt.Errorf("validate portable MySQL ownership before shutdown: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	db, openErr := openPortableRootDB(
		shutdownCtx,
		shutdownCfg,
		shutdownCfg.Database.Password,
		"",
	)
	if openErr != nil && dataState.Phase == portableMySQLPhaseInsecure {
		db, openErr = openPortableRootDB(shutdownCtx, shutdownCfg, "", "")
	}
	if openErr != nil {
		cancel()
		return fmt.Errorf(
			"connect for graceful MySQL shutdown (process was left running): %w",
			openErr,
		)
	} else {
		if identityErr := verifyConnectedPortableMySQLIdentity(
			shutdownCtx,
			db,
			c.paths,
			shutdownCfg.Database.Port,
		); identityErr != nil {
			_ = db.Close()
			cancel()
			return fmt.Errorf(
				"refusing to shut down a MySQL connection that does not match this installation: %w",
				identityErr,
			)
		}
		if _, shutdownErr := db.ExecContext(shutdownCtx, "SHUTDOWN"); shutdownErr != nil {
			fmt.Fprintln(c.stderr, "Warning: graceful MySQL shutdown returned:", shutdownErr)
		}
		_ = db.Close()
	}
	cancel()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		live, inspectErr := inspectProcess(state.PID)
		if inspectErr != nil {
			return inspectErr
		}
		if !live.running {
			launcherRunning, launcherErr := c.managedMySQLLauncherRunning(state)
			if launcherErr != nil {
				return launcherErr
			}
			if launcherRunning {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(250 * time.Millisecond):
				}
				continue
			}
			if err := os.Remove(c.paths.mysqlProcessState); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stopped MySQL process state: %w", err)
			}
			fmt.Fprintln(c.stdout, "Portable MySQL stopped; data was preserved.")
			return nil
		}
		if !sameExecutable(live.executable, c.paths.mysqldExe) {
			return fmt.Errorf("MySQL PID %d changed executable ownership while stopping", state.PID)
		}
		if !live.createdAt.Equal(state.ProcessCreatedAt) {
			return fmt.Errorf("MySQL PID %d changed process creation identity while stopping", state.PID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}

	return fmt.Errorf(
		"portable MySQL PID %d did not finish graceful shutdown within 2m; it was not force-terminated and its process state was preserved",
		state.PID,
	)
}

func (c *controller) managedMySQLLauncherRunning(
	state processState,
) (bool, error) {
	if state.LauncherPID <= 0 {
		return false, nil
	}
	if state.LauncherCreatedAt.IsZero() {
		return false, errors.New("managed MySQL launcher has no creation identity")
	}
	live, err := inspectProcess(state.LauncherPID)
	if err != nil {
		return false, err
	}
	if !live.running || !live.createdAt.Equal(state.LauncherCreatedAt) {
		return false, nil
	}
	if !sameExecutable(live.executable, c.paths.mysqldExe) {
		return false, fmt.Errorf(
			"managed MySQL launcher PID %d executable identity changed while stopping",
			state.LauncherPID,
		)
	}
	return true, nil
}

func managedMySQLInstanceConfig(
	current instanceConfig,
	state processState,
) (instanceConfig, error) {
	if state.ServicePort < 1 || state.ServicePort > 65535 {
		return instanceConfig{}, errors.New("managed MySQL state has an invalid port")
	}
	if state.DatabaseHost != "127.0.0.1" && state.DatabaseHost != "localhost" {
		return instanceConfig{}, errors.New("managed MySQL state is not loopback-only")
	}
	if state.DatabaseUser != "root" {
		return instanceConfig{}, errors.New("managed MySQL state is not owned by root")
	}
	if !sqlIdentifierPattern.MatchString(state.DatabaseName) {
		return instanceConfig{}, errors.New("managed MySQL state has an invalid database name")
	}
	if err := validateSafeValue(
		"managed MySQL password",
		state.DatabasePassword,
	); err != nil {
		return instanceConfig{}, err
	}
	active := current
	active.Database.Mode = "portable"
	active.Database.Host = state.DatabaseHost
	active.Database.Port = state.ServicePort
	active.Database.Name = state.DatabaseName
	active.Database.User = state.DatabaseUser
	active.Database.Password = state.DatabasePassword
	return active, nil
}

func (c *controller) ensurePortableMySQLData(
	ctx context.Context,
	cfg instanceConfig,
) (portableMySQLDataState, error) {
	var empty portableMySQLDataState
	mysqldSHA256, err := fileSHA256(c.paths.mysqldExe)
	if err != nil {
		return empty, err
	}
	if err := migrateLegacyPortableMySQLDataState(
		c.paths,
		cfg,
		mysqldSHA256,
	); err != nil {
		return empty, err
	}
	systemReady, err := portableMySQLSystemDataReady(c.paths.mysqlData)
	if err != nil {
		return empty, err
	}

	if isRegularFile(c.paths.mysqlDataState) {
		var state portableMySQLDataState
		if err := readStrictJSON(c.paths.mysqlDataState, &state); err != nil {
			return empty, err
		}
		if err := validatePortableMySQLDataState(
			state,
			c.paths,
			cfg,
			mysqldSHA256,
		); err != nil {
			return empty, err
		}
		if state.Phase == portableMySQLPhaseInitializing {
			if err := c.preserveInterruptedMySQLData(state); err != nil {
				return empty, err
			}
			return c.initializePortableMySQLData(ctx, cfg, mysqldSHA256)
		}
		if !systemReady {
			return empty, fmt.Errorf(
				"portable MySQL state exists but initialized system data is incomplete: %s",
				c.paths.mysqlData,
			)
		}
		return state, nil
	}
	if systemReady {
		return empty, fmt.Errorf(
			"portable MySQL data exists without this installation's ownership state; refusing to initialize or adopt %s",
			c.paths.mysqlData,
		)
	}

	entries, err := os.ReadDir(c.paths.mysqlData)
	if err != nil && !os.IsNotExist(err) {
		return empty, fmt.Errorf("inspect portable MySQL data directory: %w", err)
	}
	if len(entries) != 0 {
		return empty, fmt.Errorf(
			"portable MySQL data directory is non-empty but has no valid ownership state: %s",
			c.paths.mysqlData,
		)
	}
	return c.initializePortableMySQLData(ctx, cfg, mysqldSHA256)
}

func migrateLegacyPortableMySQLDataState(
	paths projectPaths,
	cfg instanceConfig,
	mysqldSHA256 string,
) error {
	if isRegularFile(paths.mysqlDataState) || !isRegularFile(paths.mysqlLegacyDataState) {
		return nil
	}
	var state portableMySQLDataState
	if err := readStrictJSON(paths.mysqlLegacyDataState, &state); err != nil {
		return fmt.Errorf("read legacy portable MySQL state: %w", err)
	}
	if sameExecutable(state.DataDirectory, paths.mysqlData) {
		state.DataDirectory = portableMySQLDataRelative
	}
	if state.Phase != portableMySQLPhaseInitializing && state.AutoCNFSHA256 == "" {
		autoCNFSHA256, err := fileSHA256(filepath.Join(paths.mysqlData, "auto.cnf"))
		if err != nil {
			return fmt.Errorf("migrate portable MySQL auto.cnf identity: %w", err)
		}
		state.AutoCNFSHA256 = autoCNFSHA256
	}
	if err := validatePortableMySQLDataState(
		state,
		paths,
		cfg,
		mysqldSHA256,
	); err != nil {
		return fmt.Errorf("validate legacy portable MySQL state: %w", err)
	}
	if err := writeJSON(paths.mysqlDataState, state, 0o600); err != nil {
		return fmt.Errorf("migrate portable MySQL state: %w", err)
	}
	if err := os.Remove(paths.mysqlLegacyDataState); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove migrated legacy portable MySQL state: %w", err)
	}
	return nil
}

func (c *controller) initializePortableMySQLData(
	ctx context.Context,
	cfg instanceConfig,
	mysqldSHA256 string,
) (portableMySQLDataState, error) {
	var empty portableMySQLDataState
	if err := os.MkdirAll(c.paths.mysqlData, 0o700); err != nil {
		return empty, fmt.Errorf("create portable MySQL data directory: %w", err)
	}
	state := portableMySQLDataState{
		SchemaVersion:  1,
		InstallationID: cfg.InstallationID,
		Phase:          portableMySQLPhaseInitializing,
		DataDirectory:  portableMySQLDataRelative,
		MysqldSHA256:   mysqldSHA256,
		InitializedAt:  time.Now().UTC(),
	}
	if err := writeJSON(c.paths.mysqlDataState, state, 0o600); err != nil {
		return empty, fmt.Errorf("write portable MySQL initializing state: %w", err)
	}

	fmt.Fprintln(c.stdout, "Initializing bundled MySQL data (first start only)...")
	cmd := exec.CommandContext(
		ctx,
		c.paths.mysqldExe,
		"--defaults-file="+c.paths.mysqlConfig,
		"--initialize-insecure",
		"--console",
	)
	cmd.Dir = c.paths.mysqlServer
	cmd.Stdout = c.stdout
	cmd.Stderr = c.stderr
	if err := cmd.Run(); err != nil {
		return empty, fmt.Errorf("initialize bundled MySQL: %w", err)
	}
	systemReady, err := portableMySQLSystemDataReady(c.paths.mysqlData)
	if err != nil {
		return empty, err
	}
	if !systemReady {
		return empty, errors.New("bundled MySQL produced incomplete system data")
	}
	autoCNFSHA256, err := fileSHA256(filepath.Join(c.paths.mysqlData, "auto.cnf"))
	if err != nil {
		return empty, err
	}
	state.Phase = portableMySQLPhaseInsecure
	state.AutoCNFSHA256 = autoCNFSHA256
	if err := writeJSON(c.paths.mysqlDataState, state, 0o600); err != nil {
		return empty, fmt.Errorf("write portable MySQL ownership state: %w", err)
	}
	fmt.Fprintln(c.stdout, "Bundled MySQL data initialized.")
	return state, nil
}

func portableMySQLSystemDataReady(dataDirectory string) (bool, error) {
	required := []string{
		filepath.Join(dataDirectory, "mysql"),
		filepath.Join(dataDirectory, "mysql.ibd"),
		filepath.Join(dataDirectory, "auto.cnf"),
	}
	for index, requiredPath := range required {
		info, err := os.Stat(requiredPath)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect portable MySQL system data: %w", err)
		}
		if index == 0 {
			if !info.IsDir() {
				return false, fmt.Errorf("portable MySQL system path is not a directory: %s", requiredPath)
			}
		} else if !info.Mode().IsRegular() || info.Size() == 0 {
			return false, fmt.Errorf("portable MySQL system file is invalid: %s", requiredPath)
		}
	}
	return true, nil
}

func (c *controller) preserveInterruptedMySQLData(
	state portableMySQLDataState,
) error {
	if state.Phase != portableMySQLPhaseInitializing ||
		state.InstallationID == "" ||
		filepath.ToSlash(filepath.Clean(state.DataDirectory)) != portableMySQLDataRelative {
		return errors.New("refusing to preserve unowned interrupted MySQL data")
	}
	if _, err := os.Stat(c.paths.mysqlData); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect interrupted MySQL data: %w", err)
	}
	destination := filepath.Join(
		c.paths.mysqlRoot,
		"data.interrupted-"+time.Now().UTC().Format("20060102-150405"),
	)
	if !pathInside(c.paths.mysqlRoot, destination) {
		return fmt.Errorf("unsafe interrupted MySQL backup path: %s", destination)
	}
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("interrupted MySQL backup already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect interrupted MySQL backup path: %w", err)
	}
	if err := os.Rename(c.paths.mysqlData, destination); err != nil {
		return fmt.Errorf("preserve interrupted MySQL data: %w", err)
	}
	fmt.Fprintln(c.stdout, "Preserved interrupted MySQL initialization at:", destination)
	return nil
}

func validatePortableMySQLDataState(
	state portableMySQLDataState,
	paths projectPaths,
	cfg instanceConfig,
	mysqldSHA256 string,
) error {
	if state.SchemaVersion != 1 ||
		state.InstallationID != cfg.InstallationID ||
		filepath.ToSlash(filepath.Clean(state.DataDirectory)) != portableMySQLDataRelative ||
		!strings.EqualFold(state.MysqldSHA256, mysqldSHA256) {
		return errors.New("portable MySQL ownership state does not match this installation")
	}
	switch state.Phase {
	case portableMySQLPhaseInitializing:
		if state.AutoCNFSHA256 != "" || state.Database != "" || !state.ReadyAt.IsZero() {
			return errors.New("portable MySQL initializing state contains ready-only fields")
		}
	case portableMySQLPhaseInsecure:
		if state.AutoCNFSHA256 == "" || state.Database != "" || !state.ReadyAt.IsZero() {
			return errors.New("portable MySQL insecure state contains ready-only fields")
		}
	case portableMySQLPhaseReady:
		if state.AutoCNFSHA256 == "" ||
			state.Database != cfg.Database.Name ||
			state.ReadyAt.IsZero() {
			return errors.New("portable MySQL ready state does not match configured database")
		}
	default:
		return fmt.Errorf("portable MySQL ownership phase %q is invalid", state.Phase)
	}
	if state.Phase != portableMySQLPhaseInitializing {
		actualAutoCNFSHA256, err := fileSHA256(filepath.Join(paths.mysqlData, "auto.cnf"))
		if err != nil {
			return fmt.Errorf("hash portable MySQL auto.cnf: %w", err)
		}
		if !strings.EqualFold(actualAutoCNFSHA256, state.AutoCNFSHA256) {
			return errors.New("portable MySQL ownership state does not match data server UUID")
		}
	}
	return nil
}

func (c *controller) ensurePortableDatabase(ctx context.Context, cfg instanceConfig) error {
	var state portableMySQLDataState
	if err := readStrictJSON(c.paths.mysqlDataState, &state); err != nil {
		return err
	}
	mysqldSHA256, err := fileSHA256(c.paths.mysqldExe)
	if err != nil {
		return err
	}
	if err := validatePortableMySQLDataState(
		state,
		c.paths,
		cfg,
		mysqldSHA256,
	); err != nil {
		return err
	}

	configuredDB, configuredErr := openPortableRootDB(
		ctx,
		cfg,
		cfg.Database.Password,
		"",
	)
	if configuredErr == nil {
		defer func() { _ = configuredDB.Close() }()
		if err := verifyConnectedPortableMySQLIdentity(
			ctx,
			configuredDB,
			c.paths,
			cfg.Database.Port,
		); err != nil {
			return err
		}
		if err := provisionPortableDatabase(ctx, configuredDB, cfg); err != nil {
			return err
		}
		if err := c.verifyConfiguredPortableDatabase(ctx, cfg); err != nil {
			return err
		}
		return c.markPortableMySQLReady(cfg, state)
	}
	if state.Phase != portableMySQLPhaseInsecure {
		return fmt.Errorf(
			"portable MySQL rejected configured root credentials after bootstrap: %w",
			configuredErr,
		)
	}

	insecureDB, insecureErr := openPortableRootDB(ctx, cfg, "", "")
	if insecureErr != nil {
		return fmt.Errorf(
			"portable MySQL bootstrap credentials failed (configured: %v; initial: %w)",
			configuredErr,
			insecureErr,
		)
	}
	if err := verifyConnectedPortableMySQLIdentity(
		ctx,
		insecureDB,
		c.paths,
		cfg.Database.Port,
	); err != nil {
		_ = insecureDB.Close()
		return err
	}
	if err := provisionPortableDatabase(ctx, insecureDB, cfg); err != nil {
		_ = insecureDB.Close()
		return err
	}
	_ = insecureDB.Close()

	if err := c.verifyConfiguredPortableDatabase(ctx, cfg); err != nil {
		return err
	}
	return c.markPortableMySQLReady(cfg, state)
}

func (c *controller) verifyConfiguredPortableDatabase(
	ctx context.Context,
	cfg instanceConfig,
) error {
	verifiedDB, err := openPortableRootDB(ctx, cfg, cfg.Database.Password, cfg.Database.Name)
	if err != nil {
		return fmt.Errorf("verify configured portable MySQL credentials: %w", err)
	}
	if err := verifyConnectedPortableMySQLIdentity(
		ctx,
		verifiedDB,
		c.paths,
		cfg.Database.Port,
	); err != nil {
		_ = verifiedDB.Close()
		return err
	}
	if err := verifyPortableDatabase(ctx, verifiedDB, cfg.Database.Name); err != nil {
		_ = verifiedDB.Close()
		return err
	}
	_ = verifiedDB.Close()
	return nil
}

func openPortableRootDB(
	ctx context.Context,
	cfg instanceConfig,
	password string,
	database string,
) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&timeout=5s&readTimeout=5s&writeTimeout=5s",
		cfg.Database.User,
		password,
		cfg.Database.Host,
		cfg.Database.Port,
		database,
	)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func verifyConnectedPortableMySQLIdentity(
	ctx context.Context,
	db *sql.DB,
	paths projectPaths,
	expectedPort int,
) error {
	expectedUUID, err := readPortableMySQLServerUUID(
		filepath.Join(paths.mysqlData, "auto.cnf"),
	)
	if err != nil {
		return err
	}
	var actualUUID string
	var actualPort int
	var actualDataDirectory string
	if err := db.QueryRowContext(
		ctx,
		"SELECT @@GLOBAL.server_uuid, @@GLOBAL.port, @@GLOBAL.datadir",
	).Scan(&actualUUID, &actualPort, &actualDataDirectory); err != nil {
		return fmt.Errorf("query connected portable MySQL identity: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(actualUUID), expectedUUID) {
		return fmt.Errorf(
			"connected MySQL server UUID %q does not match owned data UUID %q",
			strings.TrimSpace(actualUUID),
			expectedUUID,
		)
	}
	if actualPort != expectedPort {
		return fmt.Errorf(
			"connected MySQL port %d does not match owned port %d",
			actualPort,
			expectedPort,
		)
	}
	if !sameFilesystemPath(actualDataDirectory, paths.mysqlData) {
		return fmt.Errorf(
			"connected MySQL data directory %q does not match owned directory %q",
			actualDataDirectory,
			paths.mysqlData,
		)
	}
	return nil
}

func readPortableMySQLServerUUID(autoCNFPath string) (string, error) {
	data, err := os.ReadFile(autoCNFPath)
	if err != nil {
		return "", fmt.Errorf("read portable MySQL server UUID: %w", err)
	}
	inAutoSection := false
	var found string
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inAutoSection = strings.EqualFold(
				strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")),
				"auto",
			)
			continue
		}
		if !inAutoSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "server-uuid") {
			continue
		}
		value = strings.TrimSpace(value)
		if found != "" {
			return "", errors.New("portable MySQL auto.cnf contains duplicate server UUID entries")
		}
		found = value
	}
	if !validPortableMySQLServerUUID(found) {
		return "", errors.New("portable MySQL auto.cnf contains an invalid server UUID")
	}
	return found, nil
}

func validPortableMySQLServerUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return false
			}
		default:
			if !((character >= '0' && character <= '9') ||
				(character >= 'a' && character <= 'f') ||
				(character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}

func sameFilesystemPath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(strings.TrimSpace(left))
	rightAbsolute, rightErr := filepath.Abs(strings.TrimSpace(right))
	if leftErr != nil || rightErr != nil {
		return false
	}
	if evaluated, err := filepath.EvalSymlinks(leftAbsolute); err == nil {
		leftAbsolute = evaluated
	}
	if evaluated, err := filepath.EvalSymlinks(rightAbsolute); err == nil {
		rightAbsolute = evaluated
	}
	return strings.EqualFold(
		filepath.Clean(leftAbsolute),
		filepath.Clean(rightAbsolute),
	)
}

func provisionPortableDatabase(
	ctx context.Context,
	db *sql.DB,
	cfg instanceConfig,
) error {
	var currentUser string
	if err := db.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&currentUser); err != nil {
		return fmt.Errorf("query portable MySQL identity: %w", err)
	}
	if !strings.HasPrefix(strings.ToLower(currentUser), "root@") {
		return fmt.Errorf("portable MySQL account must be root, got %q", currentUser)
	}
	password := cfg.Database.Password
	statements := []string{
		fmt.Sprintf("ALTER USER 'root'@'localhost' IDENTIFIED BY '%s'", password),
		fmt.Sprintf("CREATE USER IF NOT EXISTS 'root'@'127.0.0.1' IDENTIFIED BY '%s'", password),
		fmt.Sprintf("ALTER USER 'root'@'127.0.0.1' IDENTIFIED BY '%s'", password),
		"GRANT ALL PRIVILEGES ON *.* TO 'root'@'127.0.0.1' WITH GRANT OPTION",
		fmt.Sprintf(
			"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci",
			cfg.Database.Name,
		),
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("provision portable MySQL: %w", err)
		}
	}
	return nil
}

func verifyPortableDatabase(ctx context.Context, db *sql.DB, expected string) error {
	var selectedDatabase string
	var currentUser string
	if err := db.QueryRowContext(
		ctx,
		"SELECT DATABASE(), CURRENT_USER()",
	).Scan(&selectedDatabase, &currentUser); err != nil {
		return fmt.Errorf("verify portable MySQL identity: %w", err)
	}
	if selectedDatabase != expected || !strings.HasPrefix(strings.ToLower(currentUser), "root@") {
		return fmt.Errorf(
			"portable MySQL identity mismatch: database=%q user=%q",
			selectedDatabase,
			currentUser,
		)
	}
	if _, err := db.ExecContext(
		ctx,
		"CREATE TEMPORARY TABLE dnf90_portable_verify (id INT NOT NULL)",
	); err != nil {
		return fmt.Errorf("portable MySQL root write check: %w", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TEMPORARY TABLE dnf90_portable_verify"); err != nil {
		return fmt.Errorf("portable MySQL root cleanup check: %w", err)
	}
	return nil
}

func (c *controller) markPortableMySQLReady(
	cfg instanceConfig,
	state portableMySQLDataState,
) error {
	if state.Phase == portableMySQLPhaseReady {
		return nil
	}
	state.Phase = portableMySQLPhaseReady
	state.Database = cfg.Database.Name
	state.ReadyAt = time.Now().UTC()
	if err := writeJSON(c.paths.mysqlDataState, state, 0o600); err != nil {
		return fmt.Errorf("persist portable MySQL ready state: %w", err)
	}
	return nil
}

func (c *controller) startPortableMySQLProcess(
	ctx context.Context,
	cfg instanceConfig,
) (processState, error) {
	executableSHA256, err := fileSHA256(c.paths.mysqldExe)
	if err != nil {
		return processState{}, err
	}
	runtimeConfigSHA256, err := databaseRuntimeConfigSHA256(c.paths)
	if err != nil {
		return processState{}, err
	}
	if err := os.Remove(c.paths.mysqlPIDFile); err != nil && !os.IsNotExist(err) {
		return processState{}, fmt.Errorf("remove stale MySQL pid-file before start: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	stdoutPath := filepath.Join(c.paths.runtimeLogs, "mysql-"+stamp+".stdout.log")
	stderrPath := filepath.Join(c.paths.runtimeLogs, "mysql-"+stamp+".stderr.log")
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return processState{}, fmt.Errorf("open MySQL stdout log: %w", err)
	}
	defer func() { _ = stdoutFile.Close() }()
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return processState{}, fmt.Errorf("open MySQL stderr log: %w", err)
	}
	defer func() { _ = stderrFile.Close() }()

	cmd := exec.Command(
		c.paths.mysqldExe,
		"--defaults-file="+c.paths.mysqlConfig,
		"--console",
		"--no-monitor",
	)
	cmd.Dir = c.paths.mysqlServer
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	configureBackgroundProcess(cmd)
	if err := cmd.Start(); err != nil {
		return processState{}, fmt.Errorf("start portable MySQL: %w", err)
	}
	launcher := processState{
		PID:                 cmd.Process.Pid,
		StartedAt:           time.Now().UTC(),
		Executable:          c.paths.mysqldExe,
		ExecutableSHA256:    executableSHA256,
		InstallationID:      cfg.InstallationID,
		ServicePort:         cfg.Database.Port,
		RuntimeConfigSHA256: runtimeConfigSHA256,
		DatabaseHost:        cfg.Database.Host,
		DatabaseName:        cfg.Database.Name,
		DatabaseUser:        cfg.Database.User,
		DatabasePassword:    cfg.Database.Password,
		Stdout:              stdoutPath,
		Stderr:              stderrPath,
	}
	if err := verifyPortableProcess(
		&launcher,
		c.paths.mysqldExe,
		"MySQL launcher",
	); err != nil {
		return c.containStartedMySQLFailure(
			cmd,
			launcher,
			processState{},
			cfg,
			runtimeConfigSHA256,
			err,
		)
	}
	state, err := c.waitPortableMySQLServerProcess(ctx, launcher)
	if err != nil {
		return c.containStartedMySQLFailure(
			cmd,
			launcher,
			processState{},
			cfg,
			runtimeConfigSHA256,
			err,
		)
	}
	state.StartedAt = launcher.StartedAt
	state.ExecutableSHA256 = executableSHA256
	state.InstallationID = cfg.InstallationID
	state.ServicePort = cfg.Database.Port
	state.RuntimeConfigSHA256 = runtimeConfigSHA256
	state.DatabaseHost = cfg.Database.Host
	state.DatabaseName = cfg.Database.Name
	state.DatabaseUser = cfg.Database.User
	state.DatabasePassword = cfg.Database.Password
	state.Stdout = stdoutPath
	state.Stderr = stderrPath
	if state.PID != launcher.PID {
		state.LauncherPID = launcher.PID
		state.LauncherCreatedAt = launcher.ProcessCreatedAt
	}
	if err := writeJSON(c.paths.mysqlProcessState, state, 0o600); err != nil {
		return c.containStartedMySQLFailure(
			cmd,
			launcher,
			state,
			cfg,
			runtimeConfigSHA256,
			fmt.Errorf("persist started MySQL process state: %w", err),
		)
	}
	if err := cmd.Process.Release(); err != nil {
		return c.containStartedMySQLFailure(
			cmd,
			launcher,
			state,
			cfg,
			runtimeConfigSHA256,
			fmt.Errorf("release MySQL process handle: %w", err),
		)
	}
	return state, nil
}

func (c *controller) waitPortableMySQLServerProcess(
	ctx context.Context,
	launcher processState,
) (processState, error) {
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		server, err := c.inspectPortableMySQLPIDFileProcess(launcher)
		if err == nil {
			if server.PID != launcher.PID {
				return processState{}, fmt.Errorf(
					"portable MySQL ignored --no-monitor: launcher PID %d differs from server PID %d",
					launcher.PID,
					server.PID,
				)
			}
			return server, nil
		}
		lastErr = err
		launcherLive, launcherErr := inspectProcess(launcher.PID)
		if launcherErr != nil {
			return processState{}, launcherErr
		}
		if !launcherLive.running ||
			!launcherLive.createdAt.Equal(launcher.ProcessCreatedAt) {
			return processState{}, fmt.Errorf(
				"portable MySQL launcher PID %d exited before publishing its server pid-file",
				launcher.PID,
			)
		}
		select {
		case <-ctx.Done():
			return processState{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return processState{}, fmt.Errorf(
		"timed out waiting for the portable MySQL server pid-file: %w",
		lastErr,
	)
}

func (c *controller) inspectPortableMySQLPIDFileProcess(
	launcher processState,
) (processState, error) {
	serverPID, err := readMySQLPIDFile(c.paths.mysqlPIDFile)
	if err != nil {
		return processState{}, err
	}
	live, err := inspectProcess(serverPID)
	if err != nil {
		return processState{}, err
	}
	if !live.running {
		return processState{}, fmt.Errorf("MySQL pid-file PID %d is not running", serverPID)
	}
	if !sameExecutable(live.executable, c.paths.mysqldExe) {
		return processState{}, fmt.Errorf(
			"MySQL pid-file PID %d executable %s does not match %s",
			serverPID,
			live.executable,
			c.paths.mysqldExe,
		)
	}
	if live.createdAt.IsZero() ||
		(!launcher.ProcessCreatedAt.IsZero() &&
			live.createdAt.Before(launcher.ProcessCreatedAt)) {
		return processState{}, fmt.Errorf(
			"MySQL pid-file PID %d has an invalid creation identity",
			serverPID,
		)
	}
	if serverPID != launcher.PID {
		parentPID, err := processParentPID(serverPID)
		if err != nil {
			return processState{}, err
		}
		if parentPID != launcher.PID {
			return processState{}, fmt.Errorf(
				"MySQL pid-file PID %d parent PID %d does not match launcher PID %d",
				serverPID,
				parentPID,
				launcher.PID,
			)
		}
	}
	return processState{
		PID:              serverPID,
		ProcessCreatedAt: live.createdAt,
		Executable:       c.paths.mysqldExe,
	}, nil
}

func readMySQLPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read MySQL pid-file: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return 0, errors.New("MySQL pid-file is empty")
	}
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("MySQL pid-file contains invalid PID %q", value)
	}
	return pid, nil
}

func (c *controller) containStartedMySQLFailure(
	cmd *exec.Cmd,
	launcher processState,
	server processState,
	cfg instanceConfig,
	runtimeConfigSHA256 string,
	cause error,
) (processState, error) {
	if server.PID <= 0 {
		if discovered, err := c.inspectPortableMySQLPIDFileProcess(launcher); err == nil {
			server = discovered
		}
	}
	if server.PID > 0 {
		server.StartedAt = launcher.StartedAt
		server.ExecutableSHA256 = launcher.ExecutableSHA256
		server.InstallationID = cfg.InstallationID
		server.ServicePort = cfg.Database.Port
		server.RuntimeConfigSHA256 = runtimeConfigSHA256
		server.DatabaseHost = cfg.Database.Host
		server.DatabaseName = cfg.Database.Name
		server.DatabaseUser = cfg.Database.User
		server.DatabasePassword = cfg.Database.Password
		server.Stdout = launcher.Stdout
		server.Stderr = launcher.Stderr
		if server.PID != launcher.PID {
			server.LauncherPID = launcher.PID
			server.LauncherCreatedAt = launcher.ProcessCreatedAt
		}
	}

	var serverTerminateErr error
	if server.PID > 0 && server.PID != launcher.PID {
		serverTerminateErr = forceTerminateProcess(
			server.PID,
			c.paths.mysqldExe,
			server.ProcessCreatedAt,
		)
	}
	launcherTerminateErr := forceTerminateProcess(
		launcher.PID,
		c.paths.mysqldExe,
		launcher.ProcessCreatedAt,
	)
	if serverTerminateErr == nil && launcherTerminateErr == nil {
		_ = cmd.Wait()
		removeErr := os.Remove(c.paths.mysqlProcessState)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		return processState{}, errors.Join(cause, removeErr)
	}

	stateToPersist := server
	if stateToPersist.PID <= 0 || serverTerminateErr == nil {
		stateToPersist = launcher
		stateToPersist.ServicePort = cfg.Database.Port
		stateToPersist.RuntimeConfigSHA256 = runtimeConfigSHA256
		stateToPersist.DatabaseHost = cfg.Database.Host
		stateToPersist.DatabaseName = cfg.Database.Name
		stateToPersist.DatabaseUser = cfg.Database.User
		stateToPersist.DatabasePassword = cfg.Database.Password
	}
	persistErr := writeJSON(c.paths.mysqlProcessState, stateToPersist, 0o600)
	releaseErr := cmd.Process.Release()
	return stateToPersist, errors.Join(
		cause,
		serverTerminateErr,
		launcherTerminateErr,
		persistErr,
		releaseErr,
		fmt.Errorf(
			"portable MySQL PID %d may still be running; STOP.bat should be used before retrying",
			stateToPersist.PID,
		),
	)
}

func verifyPortableProcess(state *processState, expectedExecutable, label string) error {
	if state == nil {
		return fmt.Errorf("%s process state is nil", label)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		live, err := inspectProcess(state.PID)
		if err == nil && live.running {
			if !sameExecutable(live.executable, expectedExecutable) {
				return fmt.Errorf(
					"started %s PID %d executable %s does not match %s",
					label,
					state.PID,
					live.executable,
					expectedExecutable,
				)
			}
			if live.createdAt.IsZero() {
				return fmt.Errorf("started %s PID %d has no creation time", label, state.PID)
			}
			state.ProcessCreatedAt = live.createdAt
			return nil
		}
		if err != nil && time.Now().After(deadline) {
			return err
		}
		if !live.running && time.Now().After(deadline) {
			return fmt.Errorf("started %s PID %d exited before identity verification", label, state.PID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (c *controller) loadManagedMySQLProcess(
	cfg instanceConfig,
	removeStale bool,
) (processState, bool, error) {
	if !isRegularFile(c.paths.mysqlProcessState) {
		return processState{}, false, nil
	}
	var state processState
	if err := readStrictJSON(c.paths.mysqlProcessState, &state); err != nil {
		return processState{}, false, err
	}
	if state.PID <= 0 || state.ProcessCreatedAt.IsZero() {
		return processState{}, false, errors.New("invalid managed MySQL process state")
	}
	live, err := inspectProcess(state.PID)
	if err != nil {
		return processState{}, false, err
	}
	statePIDMatches := live.running && live.createdAt.Equal(state.ProcessCreatedAt)

	if state.LauncherPID == 0 {
		discovered, discoverErr := c.inspectPortableMySQLPIDFileProcess(state)
		if discoverErr == nil {
			if discovered.PID != state.PID {
				state.LauncherPID = state.PID
				state.LauncherCreatedAt = state.ProcessCreatedAt
				state.PID = discovered.PID
				state.ProcessCreatedAt = discovered.ProcessCreatedAt
				live, err = inspectProcess(state.PID)
				if err != nil {
					return processState{}, false, err
				}
				statePIDMatches = live.running &&
					live.createdAt.Equal(state.ProcessCreatedAt)
			}
		} else if !statePIDMatches {
			return c.removeStaleManagedMySQLState(removeStale)
		} else if state.ServicePort == 0 || state.RuntimeConfigSHA256 == "" {
			return processState{}, false, fmt.Errorf(
				"legacy MySQL process PID %d is running but its server pid-file cannot be verified: %w",
				state.PID,
				discoverErr,
			)
		}
	} else if !statePIDMatches {
		if state.LauncherCreatedAt.IsZero() {
			return processState{}, false, errors.New(
				"managed MySQL launcher state has no process creation time",
			)
		}
		launcher := processState{
			PID:              state.LauncherPID,
			ProcessCreatedAt: state.LauncherCreatedAt,
		}
		discovered, discoverErr := c.inspectPortableMySQLPIDFileProcess(launcher)
		if discoverErr != nil {
			return c.removeStaleManagedMySQLState(removeStale)
		}
		state.PID = discovered.PID
		state.ProcessCreatedAt = discovered.ProcessCreatedAt
		live, err = inspectProcess(state.PID)
		if err != nil {
			return processState{}, false, err
		}
		statePIDMatches = live.running && live.createdAt.Equal(state.ProcessCreatedAt)
	}
	if !statePIDMatches {
		return c.removeStaleManagedMySQLState(removeStale)
	}

	if state.InstallationID != cfg.InstallationID {
		return processState{}, false, fmt.Errorf(
			"managed MySQL installation %q does not match this installation",
			state.InstallationID,
		)
	}
	if !sameExecutable(live.executable, c.paths.mysqldExe) ||
		!sameExecutable(state.Executable, c.paths.mysqldExe) {
		return processState{}, false, fmt.Errorf(
			"PID %d executable identity does not match the managed MySQL at %s",
			state.PID,
			c.paths.mysqldExe,
		)
	}
	executableSHA256, err := fileSHA256(c.paths.mysqldExe)
	if err != nil {
		return processState{}, false, err
	}
	if !strings.EqualFold(executableSHA256, state.ExecutableSHA256) {
		return processState{}, false, fmt.Errorf(
			"managed MySQL executable SHA256 changed while PID %d is running",
			state.PID,
		)
	}
	if state.ServicePort == 0 ||
		state.RuntimeConfigSHA256 == "" ||
		state.DatabaseHost == "" ||
		state.DatabaseName == "" ||
		state.DatabaseUser == "" ||
		state.DatabasePassword == "" {
		configSHA256, err := databaseRuntimeConfigSHA256(c.paths)
		if err != nil {
			return processState{}, false, err
		}
		state.ServicePort = cfg.Database.Port
		state.RuntimeConfigSHA256 = configSHA256
		state.DatabaseHost = cfg.Database.Host
		state.DatabaseName = cfg.Database.Name
		state.DatabaseUser = cfg.Database.User
		state.DatabasePassword = cfg.Database.Password
	}
	return state, true, nil
}

func (c *controller) removeStaleManagedMySQLState(
	removeStale bool,
) (processState, bool, error) {
	if removeStale {
		if err := os.Remove(c.paths.mysqlProcessState); err != nil && !os.IsNotExist(err) {
			return processState{}, false, fmt.Errorf(
				"remove stale MySQL process state: %w",
				err,
			)
		}
	}
	return processState{}, false, nil
}

func waitTCP(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := fmt.Sprintf("%s:%d", host, port)
	for time.Now().Before(deadline) {
		if testTCP(ctx, host, port, time.Second) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for TCP %s", address)
}
