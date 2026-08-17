package player

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const mysqlProfileTable = "player_profiles"

var errProfileRowsReq = errors.New("mysql profile rows are required")

// MySQLStoreOptions 是 MySQL 玩家 Profile 存储的连接池、表名和建表配置。
type MySQLStoreOptions struct {
	DSN             string
	TableName       string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	EnsureSchema    bool
}

// MySQLStore 基于 MySQL JSON 列保存玩家 Profile 的字段化数据。
type MySQLStore struct {
	db           *sql.DB
	initErr      error
	tableName    string
	ensureSchema bool
}

// NewMySQLStore 创建 MySQL 玩家 Profile 存储。
func NewMySQLStore(options MySQLStoreOptions) *MySQLStore {
	options = normMySQLStoreOpts(options)
	conn, err := sql.Open("mysql", options.DSN)
	if err == nil {
		conn.SetMaxOpenConns(options.MaxOpenConns)
		conn.SetMaxIdleConns(options.MaxIdleConns)
		conn.SetConnMaxLifetime(options.ConnMaxLifetime)
	}
	return &MySQLStore{
		db:           conn,
		initErr:      err,
		tableName:    options.TableName,
		ensureSchema: options.EnsureSchema,
	}
}

func normMySQLStoreOpts(options MySQLStoreOptions) MySQLStoreOptions {
	options.DSN = strings.TrimSpace(options.DSN)
	if options.DSN == "" {
		options.DSN = "longheng:longheng@tcp(127.0.0.1:3306)/longheng?parseTime=true&charset=utf8mb4,utf8"
	}
	options.TableName = strings.TrimSpace(options.TableName)
	if options.TableName == "" {
		options.TableName = mysqlProfileTable
	}
	if options.MaxOpenConns <= 0 {
		options.MaxOpenConns = 32
	}
	if options.MaxIdleConns <= 0 {
		options.MaxIdleConns = 8
	}
	if options.ConnMaxLifetime <= 0 {
		options.ConnMaxLifetime = 5 * time.Minute
	}
	return options
}

// Load 按账号 ID 从 MySQL 读取玩家 Profile。
func (s *MySQLStore) Load(ctx context.Context, accountID string) (Profile, bool, error) {
	if err := s.ready(ctx); err != nil {
		return Profile{}, false, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Profile{}, false, fmt.Errorf("account id is required")
	}
	query := fmt.Sprintf( // #nosec G201 -- 表名和列名均由框架固定常量或 mysqlQuoteIdentifier 生成，业务值仍使用占位符。
		"SELECT %s FROM %s WHERE %s = ?",
		mysqlSelectColumns(),
		mysqlQuoteIdentifier(s.tableName),
		mysqlQuoteIdentifier("account_id"),
	)
	return s.loadProfile(ctx, query, accountID)
}

// LoadByRoleID 按角色 ID 从 MySQL 读取玩家 Profile。
func (s *MySQLStore) LoadByRoleID(ctx context.Context, roleID string) (Profile, bool, error) {
	if err := s.ready(ctx); err != nil {
		return Profile{}, false, err
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return Profile{}, false, fmt.Errorf("role id is required")
	}
	query := fmt.Sprintf( // #nosec G201 -- 表名、列名和值占位符均由框架白名单字段生成，业务值通过参数绑定写入。
		"SELECT %s FROM %s WHERE %s = ? LIMIT 1",
		mysqlSelectColumns(),
		mysqlQuoteIdentifier(s.tableName),
		mysqlQuoteIdentifier("role_id"),
	)
	return s.loadProfile(ctx, query, roleID)
}

// ListProfiles 按账号 ID 游标从 MySQL 扫描玩家 Profile。
func (s *MySQLStore) ListProfiles(ctx context.Context, afterAccountID string, limit int) (out []Profile, err error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	if limit > 1000 {
		limit = 1000
	}
	afterAccountID = strings.TrimSpace(afterAccountID)
	query := fmt.Sprintf( //nolint:gosec // G201：表名和列名均由框架固定常量或 mysqlQuoteIdentifier 生成，业务值仍使用占位符。
		"SELECT %s FROM %s WHERE %s > ? ORDER BY %s LIMIT ?",
		mysqlSelectColumns(),
		mysqlQuoteIdentifier(s.tableName),
		mysqlQuoteIdentifier("account_id"),
		mysqlQuoteIdentifier("account_id"),
	)
	rows, err := s.db.QueryContext(ctx, query, afterAccountID, limit)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, errProfileRowsReq
	}
	defer closeMySQLRowsErr(rows, &err)

	out = make([]Profile, 0, limit)
	for rows.Next() {
		profile, err := scanMySQLProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func closeMySQLRowsErr(rows interface{ Close() error }, err *error) {
	if rows == nil || err == nil {
		return
	}
	if closeErr := rows.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

func (s *MySQLStore) loadProfile(ctx context.Context, query string, args ...any) (Profile, bool, error) {
	profile, err := scanMySQLProfile(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, err
	}
	return profile, true, nil
}

type profileScanner interface {
	Scan(...any) error
}

func scanMySQLProfile(scanner profileScanner) (Profile, error) {
	var accountID, roleID string
	moduleFields := redisModuleFields()
	moduleValues := make([]sql.NullString, len(moduleFields))
	dest := make([]any, 0, 2+len(moduleFields))
	dest = append(dest, &accountID, &roleID)
	for idx := range moduleValues {
		dest = append(dest, &moduleValues[idx])
	}
	err := scanner.Scan(dest...)
	if err != nil {
		return Profile{}, err
	}
	modules := make(map[string][]byte, len(moduleFields))
	for idx, field := range moduleFields {
		modules[field] = nullableJSON(moduleValues[idx])
	}
	baseField, ok := profileDBHashField(ProfileFieldBase)
	if !ok {
		return Profile{}, fmt.Errorf("profile base field is not configured")
	}
	if len(modules[baseField]) == 0 {
		return Profile{}, fmt.Errorf("profile %s has no base module", accountID)
	}

	profile, err := profileFromRedis(accountID, modules)
	if err != nil {
		return Profile{}, err
	}
	if profile.RoleID == "" {
		profile.RoleID = roleID
	}
	profile, err = normMySQLIdentity(profile)
	if err != nil {
		return Profile{}, err
	}
	return cloneProfile(profile), nil
}

// Save 保存完整玩家 Profile 到 MySQL。
func (s *MySQLStore) Save(ctx context.Context, profile Profile) error {
	return s.SaveFields(ctx, profile, AllProfileFields()...)
}

// SaveFields 只保存玩家 Profile 的指定字段，必要时补写基础字段。
func (s *MySQLStore) SaveFields(ctx context.Context, profile Profile, fields ...ProfileField) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	profile, err := normMySQLIdentity(profile)
	if err != nil {
		return err
	}
	fields = normProfileFields(fields)
	if len(fields) == 0 {
		return nil
	}
	encoded, err := encodeProfileFields(profile, fields)
	if err != nil {
		return err
	}
	dirty := make(map[ProfileField]struct{}, len(fields))
	for _, field := range fields {
		dirty[field] = struct{}{}
	}
	if _, ok := dirty[ProfileFieldBase]; !ok {
		baseData, err := encodeProfileField(profile, ProfileFieldBase)
		if err != nil {
			return err
		}
		encoded[redisBaseField] = baseData
	}

	updatedAt := profile.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	columns := []string{
		mysqlQuoteIdentifier("account_id"),
		mysqlQuoteIdentifier("role_id"),
		mysqlQuoteIdentifier("updated_at"),
	}
	args := []any{profile.AccountID, profile.RoleID, updatedAt}
	updates := []string{mysqlQuoteIdentifier("updated_at") + " = VALUES(" + mysqlQuoteIdentifier("updated_at") + ")"}
	insertFields := fields
	if _, ok := dirty[ProfileFieldBase]; !ok {
		insertFields = append([]ProfileField{ProfileFieldBase}, fields...)
	}
	for _, field := range insertFields {
		hashField, ok := profileDBHashField(field)
		if !ok {
			return fmt.Errorf("unsupported profile field %q", field)
		}
		column, ok := mysqlProfileColumn(hashField)
		if !ok {
			return fmt.Errorf("unsupported mysql profile field %q", hashField)
		}
		column = mysqlQuoteIdentifier(column)
		columns = append(columns, column)
		args = append(args, string(encoded[hashField]))
		if _, shouldUpdate := dirty[field]; shouldUpdate {
			if field == ProfileFieldBase {
				roleIDColumn := mysqlQuoteIdentifier("role_id")
				updates = append(updates, roleIDColumn+" = VALUES("+roleIDColumn+")")
			}
			updates = append(updates, column+" = VALUES("+column+")")
		} else if field == ProfileFieldBase {
			roleIDColumn := mysqlQuoteIdentifier("role_id")
			updates = append(updates,
				roleIDColumn+" = IF("+roleIDColumn+" = '', VALUES("+roleIDColumn+"), "+roleIDColumn+")",
				column+" = COALESCE("+column+", VALUES("+column+"))",
			)
		}
	}

	valueMarkers := make([]string, len(columns))
	for i := range valueMarkers {
		valueMarkers[i] = "?"
	}
	query := fmt.Sprintf( //nolint:gosec // G201：表名、列名和值占位符均由框架白名单字段生成，业务值通过参数绑定写入。
		"INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
		mysqlQuoteIdentifier(s.tableName),
		strings.Join(columns, ", "),
		strings.Join(valueMarkers, ", "),
		strings.Join(updates, ", "),
	)
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

func normMySQLIdentity(profile Profile) (Profile, error) {
	profile = normProfileID(profile)
	if profile.AccountID == "" {
		return Profile{}, fmt.Errorf("account id is required")
	}
	return profile, nil
}

// Check 检查 MySQL 连接，并按配置确保 Profile 表存在。
func (s *MySQLStore) Check(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	if s.ensureSchema {
		_, err := s.db.ExecContext(ctx, mysqlProfileTableDDL(s.tableName))
		return err
	}
	return nil
}

// Close 关闭 MySQL 玩家 Profile 存储连接。
func (s *MySQLStore) Close(context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *MySQLStore) ready(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("mysql store is nil")
	}
	return s.initErr
}

// TableName 返回当前 MySQL 玩家 Profile 表名。
func (s *MySQLStore) TableName() string {
	if s == nil {
		return ""
	}
	return s.tableName
}

func mysqlProfileColumn(hashField string) (string, bool) {
	if _, ok := profileDBDescByHash(hashField); !ok {
		return "", false
	}
	return mysqlJSONColumn(hashField), true
}

func mysqlProfileTableDDL(tableName string) string {
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = mysqlProfileTable
	}
	columnDefs := []string{
		mysqlQuoteIdentifier("account_id") + " VARCHAR(128) NOT NULL",
		mysqlQuoteIdentifier("role_id") + " VARCHAR(128) NOT NULL DEFAULT ''",
	}
	for _, column := range mysqlJSONColumns() {
		columnDefs = append(columnDefs, mysqlQuoteIdentifier(column)+" JSON NULL")
	}
	columnDefs = append(columnDefs,
		mysqlQuoteIdentifier("updated_at")+" DATETIME(6) NOT NULL",
		"PRIMARY KEY ("+mysqlQuoteIdentifier("account_id")+")",
		"UNIQUE KEY "+mysqlQuoteIdentifier("uk_player_profiles_role_id")+" ("+mysqlQuoteIdentifier("role_id")+")",
		"KEY "+mysqlQuoteIdentifier("idx_player_profiles_updated_at")+" ("+mysqlQuoteIdentifier("updated_at")+")",
	)
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (\n  %s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
		mysqlQuoteIdentifier(tableName),
		strings.Join(columnDefs, ",\n  "),
	)
}

func mysqlSelectColumns() string {
	columns := mysqlProfileCols()
	out := make([]string, len(columns))
	for idx, column := range columns {
		out[idx] = mysqlQuoteIdentifier(column)
	}
	return strings.Join(out, ", ")
}

func mysqlProfileCols() []string {
	columns := []string{"account_id", "role_id"}
	columns = append(columns, mysqlJSONColumns()...)
	return columns
}

func mysqlJSONColumns() []string {
	hashFields := redisModuleFields()
	columns := make([]string, 0, len(hashFields))
	for _, hashField := range hashFields {
		columns = append(columns, mysqlJSONColumn(hashField))
	}
	return columns
}

func mysqlJSONColumn(hashField string) string {
	return hashField + "_json"
}

func mysqlQuoteIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func nullableJSON(value sql.NullString) []byte {
	if !value.Valid {
		return nil
	}
	return []byte(value.String)
}
