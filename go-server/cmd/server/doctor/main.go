package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/channelinfo"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	appkit "longheng.io/server/internal/platform/app"
	platformconfig "longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/servergroup"
	logicdnf "longheng.io/server/internal/services/logic/dnf"
)

type options struct {
	root         string
	servicePath  string
	logicPath    string
	channelPath  string
	channelAddr  string
	gameHost     string
	timeout      time.Duration
	skipDatabase bool
	skipPorts    bool
	expectListen bool
}

func main() {
	var opts options
	flag.StringVar(&opts.root, "root", ".", "runtime working directory")
	flag.StringVar(&opts.servicePath, "config", "configs/dnfbridge.toml", "service config path, relative to root")
	flag.StringVar(&opts.logicPath, "logic-config", "configs/dnf/logic.toml", "repository config path, relative to root")
	flag.StringVar(&opts.channelPath, "channel-info", "data/dnf/channel_info.etc", "channel_info.etc path, relative to root")
	flag.StringVar(&opts.channelAddr, "channel-listen", ":7001", "channel TCP listen address")
	flag.StringVar(&opts.gameHost, "game-listen-host", "", "host used by all game TCP listeners")
	flag.DurationVar(&opts.timeout, "timeout", 5*time.Second, "database and network check timeout")
	flag.BoolVar(&opts.skipDatabase, "skip-database", false, "skip MySQL connectivity check")
	flag.BoolVar(&opts.skipPorts, "skip-ports", false, "skip TCP listen-port availability checks")
	flag.BoolVar(&opts.expectListen, "expect-listening", false, "require server TCP ports to be accepting connections")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("OK: DNF90 runtime preflight passed")
}

func run(opts options) error {
	root, err := filepath.Abs(strings.TrimSpace(opts.root))
	if err != nil {
		return fmt.Errorf("resolve runtime root: %w", err)
	}
	if err := os.Chdir(root); err != nil {
		return fmt.Errorf("enter runtime root %s: %w", root, err)
	}
	fmt.Println("runtime:", root)

	serviceCfg, err := platformconfig.Load(opts.servicePath, "dnfbridge")
	if err != nil {
		return fmt.Errorf("service config %s: %w", opts.servicePath, err)
	}
	fmt.Println("service config: valid")

	env := &appkit.Env{Config: serviceCfg}
	logicCfg, err := logicdnf.LoadConfigForEnv(opts.logicPath, env)
	if err != nil {
		return fmt.Errorf("repository config %s: %w", opts.logicPath, err)
	}
	fmt.Println("repository config: valid")

	if err := checkFile(serviceCfg.PVF.Path, "Script.pvf", serviceCfg.PVF.MaxBytes); err != nil {
		return err
	}
	index, err := channelinfo.LoadFile(opts.channelPath)
	if err != nil {
		return fmt.Errorf("channel info %s: %w", opts.channelPath, err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{
		ServerID:     dnfenum.LoginChannelServerIndex,
		GamePortBase: dnfenum.GamePortBase,
	})
	if err != nil {
		return fmt.Errorf("build channel catalog: %w", err)
	}
	ports := catalog.GamePorts()
	fmt.Printf("channels: %d, game ports: %s\n", len(catalog.Channels()), joinInts(ports))

	planPath := strings.TrimSpace(logicCfg.Repository.ServerGroupPlanFile)
	if planPath == "" {
		planPath = strings.TrimSpace(serviceCfg.ServerGroup.PlanFile)
	}
	dbPlan, err := loadDatabasePlan(planPath, logicCfg.Repository.ShardID)
	if err != nil {
		return err
	}
	if len(dbPlan.WriteDatabases) != 1 || len(dbPlan.ReadDatabases) != 1 ||
		dbPlan.WriteDatabases[0] != dbPlan.ReadDatabases[0] {
		return fmt.Errorf(
			"local deployment requires exactly one shared read/write database; got read=%v write=%v",
			dbPlan.ReadDatabases,
			dbPlan.WriteDatabases,
		)
	}
	fmt.Printf("database route: read=%s write=%s\n",
		strings.Join(dbPlan.ReadDatabases, ","),
		strings.Join(dbPlan.WriteDatabases, ","))

	if !opts.skipDatabase {
		schemaReady, err := checkDatabase(
			logicCfg.Repository.MySQLDSN,
			dbPlan.WriteDatabases[0],
			logicCfg.Repository.TablePrefix,
			logicCfg.Repository.AutoCreateSchema,
			opts.timeout,
		)
		if err != nil {
			return err
		}
		fmt.Println("mysql: reachable")
		if schemaReady {
			fmt.Println("mysql schema: ready")
		} else {
			fmt.Println("mysql schema: pending first-start auto creation")
		}
	}
	if logicCfg.Repository.RedisEnabled {
		if err := checkTCP(logicCfg.Repository.RedisAddress, opts.timeout); err != nil {
			return fmt.Errorf("redis %s: %w", logicCfg.Repository.RedisAddress, err)
		}
		fmt.Println("redis: reachable")
	}

	if !opts.skipPorts {
		addresses := make([]string, 0, len(ports)+2)
		addresses = append(addresses, opts.channelAddr, serviceCfg.Admin.Listen)
		for _, port := range ports {
			addresses = append(addresses, net.JoinHostPort(strings.Trim(opts.gameHost, "[]"), strconv.Itoa(port)))
		}
		if opts.expectListen {
			if err := checkListeningAddresses(addresses, opts.timeout); err != nil {
				return err
			}
			fmt.Printf("listen ports: accepting connections (%d checked)\n", len(addresses))
		} else {
			if err := checkListenAddresses(addresses); err != nil {
				return err
			}
			fmt.Printf("listen ports: available (%d checked)\n", len(addresses))
		}
	}
	return nil
}

func checkFile(path, label string, maxBytes int64) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s path is empty", label)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %s: %w", label, path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("%s %s is not a non-empty regular file", label, path)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return fmt.Errorf("%s %s is %d bytes, over configured max %d", label, path, info.Size(), maxBytes)
	}
	fmt.Printf("%s: %s (%d bytes)\n", label, path, info.Size())
	return nil
}

func loadDatabasePlan(path, shardID string) (dnfrepo.DatabasePlan, error) {
	if strings.TrimSpace(path) == "" {
		return dnfrepo.DatabasePlan{}, fmt.Errorf("server-group plan path is empty")
	}
	store, err := servergroup.NewFileStore(path)
	if err != nil {
		return dnfrepo.DatabasePlan{}, fmt.Errorf("server-group plan %s: %w", path, err)
	}
	plan, err := store.Load(context.Background())
	if err != nil {
		return dnfrepo.DatabasePlan{}, fmt.Errorf("load server-group plan %s: %w", path, err)
	}
	manager, err := servergroup.New(plan)
	if err != nil {
		return dnfrepo.DatabasePlan{}, fmt.Errorf("validate server-group plan %s: %w", path, err)
	}
	dbPlan, err := dnfrepo.ResolveDatabasePlan(context.Background(), manager, shardID)
	if err != nil {
		return dnfrepo.DatabasePlan{}, fmt.Errorf("resolve database route: %w", err)
	}
	return dbPlan, nil
}

func checkDatabase(dsn, expectedDatabase, tablePrefix string, autoCreate bool, timeout time.Duration) (bool, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return false, fmt.Errorf("open mysql: %w", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return false, fmt.Errorf("mysql is not reachable: %w", err)
	}
	var selectedDatabase string
	var currentUser string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE(), CURRENT_USER()").Scan(&selectedDatabase, &currentUser); err != nil {
		return false, fmt.Errorf("query mysql identity: %w", err)
	}
	if selectedDatabase != expectedDatabase {
		return false, fmt.Errorf("mysql DSN selects database %q, route requires %q", selectedDatabase, expectedDatabase)
	}
	if !strings.HasPrefix(strings.ToLower(currentUser), "root@") {
		return false, fmt.Errorf("mysql account must be root, server reports %q", currentUser)
	}
	if _, err := db.ExecContext(ctx, "CREATE TEMPORARY TABLE dnf90_preflight_write (id INT NOT NULL)"); err != nil {
		return false, fmt.Errorf("mysql root account cannot create a temporary table: %w", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TEMPORARY TABLE dnf90_preflight_write"); err != nil {
		return false, fmt.Errorf("drop mysql preflight temporary table: %w", err)
	}
	tablePrefix = strings.TrimSpace(tablePrefix)
	if tablePrefix == "" {
		tablePrefix = "dnf"
	}
	var tableCount int
	if err := db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		expectedDatabase,
		tablePrefix+"_accounts",
	).Scan(&tableCount); err != nil {
		return false, fmt.Errorf("inspect mysql schema: %w", err)
	}
	if tableCount == 0 && !autoCreate {
		return false, fmt.Errorf("mysql schema is missing and repository auto_create_schema is disabled")
	}
	return tableCount > 0, nil
}

func checkTCP(address string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

func checkListenAddresses(addresses []string) error {
	seen := make(map[string]struct{}, len(addresses))
	listeners := make([]net.Listener, 0, len(addresses))
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("listen address %s is unavailable: %w", address, err)
		}
		listeners = append(listeners, listener)
	}
	return nil
}

func checkListeningAddresses(addresses []string, timeout time.Duration) error {
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		address = dialAddress(address)
		if address == "" {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		conn, err := net.DialTimeout("tcp", address, timeout)
		if err != nil {
			return fmt.Errorf("listen address %s is not accepting connections: %w", address, err)
		}
		_ = conn.Close()
	}
	return nil
}

func dialAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}
