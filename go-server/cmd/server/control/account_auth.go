package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	mysql "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

const (
	localLoginAccountTable = "dnf_login_accounts"
	localAccountStateLive  = "active"
)

type localLoginAccount struct {
	AccountID string
	Username  string
	State     string
	CreatedAt time.Time
	LastLogin sql.NullTime
	ActiveNow bool
}

func (c *controller) account(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printAccountUsage(c.stdout)
		return flag.ErrHelp
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "register":
		fs := newFlagSet("account register", c.stderr)
		username := fs.String("username", "", "login username")
		passwordStdin := fs.Bool("password-stdin", false, "read one password line from stdin")
		keepDatabase := fs.Bool("keep-database", false, "leave MySQL running after the command")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		if !*passwordStdin {
			return errors.New("account registration requires --password-stdin")
		}
		password, err := readLocalAccountPassword(c.stdin)
		if err != nil {
			return err
		}
		return c.registerLocalLoginAccount(
			ctx,
			*username,
			password,
			*keepDatabase,
		)
	case "login":
		fs := newFlagSet("account login", c.stderr)
		username := fs.String("username", "", "login username")
		passwordStdin := fs.Bool("password-stdin", false, "read one password line from stdin")
		keepDatabase := fs.Bool("keep-database", false, "leave MySQL running after the command")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		if !*passwordStdin {
			return errors.New("account login requires --password-stdin")
		}
		password, err := readLocalAccountPassword(c.stdin)
		if err != nil {
			return err
		}
		return c.loginLocalAccount(
			ctx,
			*username,
			password,
			*keepDatabase,
		)
	case "list":
		fs := newFlagSet("account list", c.stderr)
		keepDatabase := fs.Bool("keep-database", false, "leave MySQL running after the command")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		return c.listLocalLoginAccounts(ctx, *keepDatabase)
	default:
		fmt.Fprintf(c.stderr, "unknown account command %q\n\n", args[0])
		printAccountUsage(c.stderr)
		return flag.ErrHelp
	}
}

func printAccountUsage(w io.Writer) {
	fmt.Fprintln(w, `DNF90 local account commands

Usage:
  DNF90Control.exe account register --username NAME --password-stdin
  DNF90Control.exe account login --username NAME --password-stdin
  DNF90Control.exe account list

Passwords are accepted only through stdin. The Windows launcher uses these
commands without exposing a password in the process command line or logs.`)
}

func (c *controller) registerLocalLoginAccount(
	ctx context.Context,
	username string,
	password string,
	keepDatabase bool,
) error {
	username, err := normalizeLocalUsername(username)
	if err != nil {
		return err
	}
	if err := validateLocalPassword(password); err != nil {
		return err
	}
	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return fmt.Errorf("hash local account password: %w", err)
	}
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	return c.withLocalLoginDatabase(
		ctx,
		cfg,
		keepDatabase,
		func(db *sql.DB) error {
			var existing int
			err := db.QueryRowContext(
				ctx,
				"SELECT 1 FROM "+localLoginAccountTable+" WHERE username = ?",
				username,
			).Scan(&existing)
			switch {
			case err == nil:
				return errors.New("this username is already registered")
			case !errors.Is(err, sql.ErrNoRows):
				return fmt.Errorf("check local username: %w", err)
			}

			var accountCount int
			if err := db.QueryRowContext(
				ctx,
				"SELECT COUNT(*) FROM "+localLoginAccountTable,
			).Scan(&accountCount); err != nil {
				return fmt.Errorf("count local login accounts: %w", err)
			}
			accountID := cfg.Server.AccountID
			if accountCount > 0 {
				accountID, err = randomCredential(defaultAccountIDPrefix)
				if err != nil {
					return err
				}
			}
			_, err = db.ExecContext(
				ctx,
				"INSERT INTO "+localLoginAccountTable+
					" (account_id, username, password_hash, state, created_at, updated_at) "+
					"VALUES (?, ?, ?, ?, NOW(6), NOW(6))",
				accountID,
				username,
				passwordHash,
				localAccountStateLive,
			)
			if err != nil {
				var mysqlErr *mysql.MySQLError
				if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
					return errors.New("this username is already registered")
				}
				return fmt.Errorf("register local account: %w", err)
			}
			fmt.Fprintln(c.stdout, "Account registered:", username)
			return nil
		},
	)
}

func (c *controller) loginLocalAccount(
	ctx context.Context,
	username string,
	password string,
	keepDatabase bool,
) error {
	username, err := normalizeLocalUsername(username)
	if err != nil {
		return err
	}
	if err := validateLocalPassword(password); err != nil {
		return err
	}
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	return c.withLocalLoginDatabase(
		ctx,
		cfg,
		keepDatabase,
		func(db *sql.DB) error {
			var accountID string
			var passwordHash []byte
			var state string
			err := db.QueryRowContext(
				ctx,
				"SELECT account_id, password_hash, state FROM "+
					localLoginAccountTable+" WHERE username = ?",
				username,
			).Scan(&accountID, &passwordHash, &state)
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("username or password is incorrect")
			}
			if err != nil {
				return fmt.Errorf("load local login account: %w", err)
			}
			if bcrypt.CompareHashAndPassword(passwordHash, []byte(password)) != nil {
				return errors.New("username or password is incorrect")
			}
			if state != localAccountStateLive {
				return errors.New("this account is disabled")
			}
			if accountID != cfg.Server.AccountID {
				if err := c.requireLocalServerStopped(cfg); err != nil {
					return err
				}
				if err := writeActiveLocalAccountID(c.paths, accountID); err != nil {
					return err
				}
			}
			if _, err := db.ExecContext(
				ctx,
				"UPDATE "+localLoginAccountTable+
					" SET last_login_at = NOW(6), updated_at = NOW(6) WHERE account_id = ?",
				accountID,
			); err != nil {
				if accountID != cfg.Server.AccountID {
					rollbackErr := writeActiveLocalAccountID(
						c.paths,
						cfg.Server.AccountID,
					)
					return errors.Join(
						fmt.Errorf("record local account login: %w", err),
						rollbackErr,
					)
				}
				return fmt.Errorf("record local account login: %w", err)
			}
			fmt.Fprintln(c.stdout, "Account login successful:", username)
			return nil
		},
	)
}

// authenticateRunningLocalAccount validates credentials without changing the
// instance-wide fallback account. It is used for per-process launches while
// the server and other accounts remain online.
func (c *controller) authenticateRunningLocalAccount(
	ctx context.Context,
	cfg instanceConfig,
	username string,
	password string,
) (string, error) {
	username, err := normalizeLocalUsername(username)
	if err != nil {
		return "", err
	}
	if err := validateLocalPassword(password); err != nil {
		return "", err
	}
	var accountID string
	err = c.withLocalLoginDatabase(
		ctx,
		cfg,
		true,
		func(db *sql.DB) error {
			var passwordHash []byte
			var state string
			err := db.QueryRowContext(
				ctx,
				"SELECT account_id, password_hash, state FROM "+
					localLoginAccountTable+" WHERE username = ?",
				username,
			).Scan(&accountID, &passwordHash, &state)
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("username or password is incorrect")
			}
			if err != nil {
				return fmt.Errorf("load local login account: %w", err)
			}
			if bcrypt.CompareHashAndPassword(passwordHash, []byte(password)) != nil {
				return errors.New("username or password is incorrect")
			}
			if state != localAccountStateLive {
				return errors.New("this account is disabled")
			}
			if _, err := db.ExecContext(
				ctx,
				"UPDATE "+localLoginAccountTable+
					" SET last_login_at = NOW(6), updated_at = NOW(6) WHERE account_id = ?",
				accountID,
			); err != nil {
				return fmt.Errorf("record local account login: %w", err)
			}
			return nil
		},
	)
	if err != nil {
		return "", err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return "", errors.New("authenticated account has no account ID")
	}
	return accountID, nil
}

func (c *controller) listLocalLoginAccounts(
	ctx context.Context,
	keepDatabase bool,
) error {
	cfg, err := loadInstance(c.paths)
	if err != nil {
		return err
	}
	return c.withLocalLoginDatabase(
		ctx,
		cfg,
		keepDatabase,
		func(db *sql.DB) error {
			rows, err := db.QueryContext(
				ctx,
				"SELECT account_id, username, state, created_at, last_login_at "+
					"FROM "+localLoginAccountTable+" ORDER BY created_at, account_id",
			)
			if err != nil {
				return fmt.Errorf("query local login accounts: %w", err)
			}
			defer rows.Close()
			fmt.Fprintln(c.stdout, "Local login accounts:")
			accountCount := 0
			for rows.Next() {
				var account localLoginAccount
				if err := rows.Scan(
					&account.AccountID,
					&account.Username,
					&account.State,
					&account.CreatedAt,
					&account.LastLogin,
				); err != nil {
					return fmt.Errorf("scan local login account: %w", err)
				}
				marker := " "
				if account.AccountID == cfg.Server.AccountID {
					marker = "*"
				}
				fmt.Fprintf(
					c.stdout,
					"%s %s  account_id=%s  state=%s\n",
					marker,
					account.Username,
					account.AccountID,
					account.State,
				)
				accountCount++
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("read local login accounts: %w", err)
			}
			if accountCount == 0 {
				fmt.Fprintln(c.stdout, "(none; register through LOGIN.bat)")
			} else {
				fmt.Fprintln(c.stdout, "* active account")
			}
			return nil
		},
	)
}

func (c *controller) requireLocalServerStopped(cfg instanceConfig) error {
	state, found, err := c.loadManagedProcess(cfg.InstallationID, true)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf(
			"DNF90 is running as PID %d; stop it before switching accounts",
			state.PID,
		)
	}
	return nil
}

func (c *controller) withLocalLoginDatabase(
	ctx context.Context,
	cfg instanceConfig,
	keepDatabase bool,
	run func(*sql.DB) error,
) (resultErr error) {
	mysqlState, mysqlRunning, err := c.loadManagedMySQLProcess(cfg, true)
	if err != nil {
		return err
	}
	if mysqlRunning {
		if err := validateRunningDatabaseConfig(c.paths, cfg, mysqlState); err != nil {
			return err
		}
	} else if err := generateRuntimeConfigs(c.paths, cfg); err != nil {
		return err
	}
	started, err := c.startDatabase(ctx, cfg)
	if err != nil {
		if started {
			return errors.Join(err, c.rollbackDatabase(cfg))
		}
		return err
	}
	if started && !keepDatabase {
		defer func() {
			resultErr = errors.Join(resultErr, c.rollbackDatabase(cfg))
		}()
	}
	db, err := openPortableRootDB(
		ctx,
		cfg,
		cfg.Database.Password,
		cfg.Database.Name,
	)
	if err != nil {
		return fmt.Errorf("open local login database: %w", err)
	}
	defer db.Close()
	if err := verifyConnectedPortableMySQLIdentity(
		ctx,
		db,
		c.paths,
		cfg.Database.Port,
	); err != nil {
		return err
	}
	if err := ensureLocalLoginAccountSchema(ctx, db); err != nil {
		return err
	}
	return run(db)
}

func ensureLocalLoginAccountSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dnf_login_accounts (
  account_id VARCHAR(128) NOT NULL,
  username VARCHAR(64) NOT NULL,
  password_hash VARBINARY(255) NOT NULL,
  state VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  last_login_at DATETIME(6) NULL,
  PRIMARY KEY (account_id),
  UNIQUE KEY uk_dnf_login_accounts_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return fmt.Errorf("ensure local login account schema: %w", err)
	}
	return nil
}

func normalizeLocalUsername(username string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	count := utf8.RuneCountInString(username)
	if !utf8.ValidString(username) || count < 3 || count > 32 {
		return "", errors.New("username must contain 3 to 32 characters")
	}
	for _, character := range username {
		if unicode.IsLetter(character) ||
			unicode.IsDigit(character) ||
			character == '_' ||
			character == '-' ||
			character == '.' {
			continue
		}
		return "", errors.New(
			"username may contain only letters, numbers, '_', '-', or '.'",
		)
	}
	return username, nil
}

func validateLocalPassword(password string) error {
	if !utf8.ValidString(password) {
		return errors.New("password must be valid UTF-8")
	}
	if len(password) < 8 || len(password) > 72 {
		return errors.New("password must contain 8 to 72 UTF-8 bytes")
	}
	for _, character := range password {
		if unicode.IsControl(character) {
			return errors.New("password must not contain control characters")
		}
	}
	return nil
}

func readLocalAccountPassword(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("password input is unavailable")
	}
	reader := bufio.NewReader(io.LimitReader(r, 4096))
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if password == "" {
		return "", errors.New("password from stdin is empty")
	}
	return password, nil
}

func writeActiveLocalAccountID(paths projectPaths, accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" ||
		len(accountID) > 128 ||
		!safeValuePattern.MatchString(accountID) {
		return errors.New("active account id is invalid")
	}
	cfg, err := decodeInstance(paths.instance)
	if err != nil {
		return err
	}
	cfg.Server.AccountID = accountID
	data, err := marshalInstance(cfg)
	if err != nil {
		return err
	}
	if err := writeFile(paths.instance, data, 0o600); err != nil {
		return fmt.Errorf("write active local account: %w", err)
	}
	return nil
}
