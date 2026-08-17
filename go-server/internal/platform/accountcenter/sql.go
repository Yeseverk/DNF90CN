package accountcenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrSQLDBRequired = errors.New("account center sql db is required")
	ErrInvalidTable  = errors.New("account center sql table is invalid")
)

const (
	defaultSQLTable = "account_center_state"
	sqlStateName    = "default"
)

var sqlTablePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type SQLStore struct {
	db    *sql.DB
	stmts SQLStatements
}

type SQLOptions struct {
	DB    *sql.DB
	Table string
}

type SQLStatements struct {
	Table        string
	EnsureSchema string
	Load         string
	Save         string
}

func NewSQLStore(options SQLOptions) (*SQLStore, error) {
	if options.DB == nil {
		return nil, ErrSQLDBRequired
	}
	stmts, err := NewSQLStatements(options.Table)
	if err != nil {
		return nil, err
	}
	return &SQLStore{db: options.DB, stmts: stmts}, nil
}

func NewSQLStatements(table string) (SQLStatements, error) {
	table, err := normalizeSQLTable(table)
	if err != nil {
		return SQLStatements{}, err
	}
	quoted := "`" + table + "`"
	return SQLStatements{
		Table: table,
		EnsureSchema: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  name VARCHAR(64) NOT NULL,
  payload LONGTEXT NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, quoted),
		Load: fmt.Sprintf(`SELECT payload FROM %s WHERE name=?`, quoted),
		Save: fmt.Sprintf(`INSERT INTO %s (name, payload) VALUES (?, ?) ON DUPLICATE KEY UPDATE payload=VALUES(payload), updated_at=CURRENT_TIMESTAMP(6)`, quoted),
	}, nil
}

func (s *SQLStore) EnsureSchema(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrSQLDBRequired
	}
	_, err := s.db.ExecContext(ctx, s.stmts.EnsureSchema)
	return err
}

func (s *SQLStore) Load(ctx context.Context) (Export, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Export{}, false, err
	}
	if s == nil || s.db == nil {
		return Export{}, false, ErrSQLDBRequired
	}
	var payload []byte
	if err := s.db.QueryRowContext(ctx, s.stmts.Load, sqlStateName).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Export{}, false, nil
		}
		return Export{}, false, err
	}
	var exported Export
	if err := json.Unmarshal(payload, &exported); err != nil {
		return Export{}, false, err
	}
	return exported, true, nil
}

func (s *SQLStore) Save(ctx context.Context, exported Export) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrSQLDBRequired
	}
	payload, err := json.Marshal(exported)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.stmts.Save, sqlStateName, payload)
	return err
}

func (s *SQLStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func normalizeSQLTable(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = defaultSQLTable
	}
	if !sqlTablePattern.MatchString(table) {
		return "", ErrInvalidTable
	}
	return table, nil
}
