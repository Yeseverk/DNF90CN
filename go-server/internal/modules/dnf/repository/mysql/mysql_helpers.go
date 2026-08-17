// 本文件提供 DNF MySQL 仓储的路由、JSON 和 SQL 拼装 helper。
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

type mysqlRouter struct {
	db          SQLDB
	readDBs     []string
	writeDBs    []string
	tablePrefix string
	now         func() time.Time
	lockReads   bool
}

type mysqlStoreBase struct {
	router mysqlRouter
}

func newMySQLRouter(db SQLDB, options MySQLGroupOptions) (mysqlRouter, error) {
	if db == nil {
		return mysqlRouter{}, ErrMySQLDBRequired
	}
	tablePrefix, err := normTablePrefix(options.TablePrefix)
	if err != nil {
		return mysqlRouter{}, err
	}
	writeDBs, err := repository.ValidateDatabases(options.DatabasePlan.SchemaDatabases())
	if err != nil {
		return mysqlRouter{}, err
	}
	if len(writeDBs) == 0 {
		return mysqlRouter{}, fmt.Errorf("%w: write databases are required", ErrMySQLConfigInvalid)
	}
	readDBs, err := repository.ValidateDatabases(options.DatabasePlan.ReadDatabases)
	if err != nil {
		return mysqlRouter{}, err
	}
	if len(readDBs) == 0 {
		readDBs = append([]string(nil), writeDBs...)
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return mysqlRouter{
		db:          db,
		readDBs:     append([]string(nil), readDBs...),
		writeDBs:    append([]string(nil), writeDBs...),
		tablePrefix: tablePrefix,
		now:         now,
	}, nil
}

// Check 检查 MySQL 连接可用性。
// 它不创建 schema，避免仓储预检阶段产生隐式写库副作用。
func (s mysqlStoreBase) Check(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s.router.db == nil {
		return ErrMySQLDBRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.router.db.PingContext(ctx)
}

func (r mysqlRouter) readTable(suffix, key string) (string, error) {
	return r.table(r.readDBs, suffix, key)
}

func (r mysqlRouter) writeTable(suffix, key string) (string, error) {
	return r.table(r.writeDBs, suffix, key)
}

func (r mysqlRouter) selectQuery(query string) string {
	if r.lockReads {
		return query + " FOR UPDATE"
	}
	return query
}

func (r mysqlRouter) table(databases []string, suffix, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", db.ErrRecordKeyRequired
	}
	if len(databases) == 0 {
		return "", fmt.Errorf("%w: database list is empty", ErrMySQLConfigInvalid)
	}
	database := databases[pickDatabase(key, len(databases))]
	return mysqlTable(database, r.tablePrefix, suffix), nil
}

func pickDatabase(key string, count int) int {
	if count <= 1 {
		return 0
	}
	return int(crc32.ChecksumIEEE([]byte(key)) % uint32(count))
}

func dbRecordKey[T any](keyFn func(T) string, record T) (string, error) {
	return db.RecordKey(keyFn, record)
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func timeOrNow(value time.Time, now func() time.Time) time.Time {
	if !value.IsZero() {
		return value.UTC()
	}
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}

func sqlTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func scanTime(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func jsonArg(value any) (any, error) {
	if isNilJSON(value) {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

func scanJSON(value sql.NullString, out any) error {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return json.Unmarshal([]byte(value.String), out)
}

func isNilJSON(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case map[string]string:
		return typed == nil
	case map[string]int64:
		return typed == nil
	case map[string]repository.ItemStack:
		return typed == nil
	case map[string]repository.EquipmentEntry:
		return typed == nil
	case map[string]repository.PetEntry:
		return typed == nil
	case map[string]repository.MailRecord:
		return typed == nil
	case map[int64]repository.QuestState:
		return typed == nil
	case map[int64]repository.SkillState:
		return typed == nil
	case map[int64]time.Time:
		return typed == nil
	case repository.CharacterRoster:
		return isZeroCharacterRoster(typed)
	default:
		return false
	}
}

func placeholders(count int) string {
	out := make([]string, count)
	for idx := range out {
		out[idx] = "?"
	}
	return strings.Join(out, ", ")
}

func quotedColumns(columns []string) string {
	out := make([]string, len(columns))
	for idx, column := range columns {
		out[idx] = quoteSQLIdentifier(column)
	}
	return strings.Join(out, ", ")
}

func updateValue(column string) string {
	quoted := quoteSQLIdentifier(column)
	return quoted + " = VALUES(" + quoted + ")"
}

func keepCreatedAt(column string) string {
	quoted := quoteSQLIdentifier(column)
	return quoted + " = COALESCE(" + quoted + ", VALUES(" + quoted + "))"
}

func buildUpsert(table string, columns []string, updates []string) string {
	return fmt.Sprintf( //nolint:gosec // G201：库表和列名来自白名单/校验后的 schema，业务值使用占位符。
		"INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
		table,
		quotedColumns(columns),
		placeholders(len(columns)),
		strings.Join(updates, ", "),
	)
}

func buildInsert(table string, columns []string) string {
	return fmt.Sprintf( //nolint:gosec // G201：库表和列名来自白名单/校验后的 schema，业务值使用占位符。
		"INSERT INTO %s (%s) VALUES (%s)",
		table,
		quotedColumns(columns),
		placeholders(len(columns)),
	)
}

func scanErr(err error) (bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func isZeroCharacterRoster(value repository.CharacterRoster) bool {
	return value.Header == (repository.CharacterRosterHeader{}) &&
		value.Entry.ByteA == 0 &&
		value.Entry.PackedJobGrow == 0 &&
		value.Entry.ByteC == 0 &&
		value.Entry.Field2CC == 0 &&
		value.Entry.State0 == 0 &&
		value.Entry.TimeA == 0 &&
		value.Entry.TimeB == 0 &&
		len(value.Entry.EquipSummary) == 0 &&
		value.Entry.Value0 == 0 &&
		value.Entry.Value1 == 0 &&
		value.Entry.Value2 == 0 &&
		value.Entry.ReservedA == 0 &&
		value.Entry.ReservedB == 0 &&
		len(value.Entry.LinkedIDBlock) == 0 &&
		value.Entry.Value3 == 0 &&
		value.Entry.ObjectID == 0 &&
		value.Entry.Flag0Eq1 == 0 &&
		value.Entry.SpecialStatusFlag == 0 &&
		value.Entry.Value5 == 0 &&
		value.Entry.DisplayFlags == 0 &&
		value.Entry.ReservedC == 0 &&
		value.Entry.ReservedD == 0 &&
		value.Entry.Value6 == 0 &&
		value.Entry.Flag1Nonzero == 0 &&
		value.Entry.BoolAEq1 == 0 &&
		value.Entry.BoolBEq1 == 0 &&
		value.Entry.BoolCEq1 == 0 &&
		value.Entry.Flag2Nonzero == 0 &&
		value.Entry.Flag3Nonzero == 0 &&
		value.Entry.Flag4Nonzero == 0 &&
		value.Entry.Flag5Nonzero == 0 &&
		value.Entry.Value7 == 0 &&
		value.Entry.Flag6Eq1 == 0 &&
		len(value.Entry.Flags) == 0
}

func firstValue(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
