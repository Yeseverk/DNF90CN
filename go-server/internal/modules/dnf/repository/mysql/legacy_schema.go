// 本文件把 C# SQLite inventory.db schema 转成 Go MySQL legacy 表。
// legacy 表只作为导入和协议证据镜像，不替代新的 DNF owner 权威表。
package mysql

import (
	"embed"
	"fmt"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

//go:embed csharp_item_schema.sql
var csharpSchemaFS embed.FS

const csharpSchemaFile = "csharp_item_schema.sql"

// CSharpLegacyMySQLSchema 返回 C# inventory.db 对应的 MySQL legacy 表 DDL。
// 它不连接数据库、不执行迁移；调用方只有显式 AutoCreate 时才会写 MySQL。
func CSharpLegacyMySQLSchema(database, tablePrefix string) ([]string, error) {
	tablePrefix, err := normTablePrefix(tablePrefix)
	if err != nil {
		return nil, err
	}
	if _, err := repository.ValidateDatabases([]string{database}); err != nil {
		return nil, err
	}
	return csharpLegacyMySQLSchema(database, tablePrefix)
}

func csharpLegacyMySQLSchema(database, tablePrefix string) ([]string, error) {
	raw, err := csharpSchemaFS.ReadFile(csharpSchemaFile)
	if err != nil {
		return nil, err
	}
	statements := splitSQL(stripSQLComments(string(raw)))
	indexes, err := legacyIndexes(statements)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(statements))
	for _, statement := range statements {
		stmt := strings.TrimSpace(statement)
		if stmt == "" {
			continue
		}
		upper := strings.ToUpper(stmt)
		if !strings.HasPrefix(upper, "CREATE TABLE IF NOT EXISTS ") {
			if strings.HasPrefix(upper, "INSERT OR IGNORE INTO ") {
				insert, err := legacyInsert(database, tablePrefix, stmt)
				if err != nil {
					return nil, err
				}
				out = append(out, insert)
			}
			continue
		}
		tableName := legacyTableName(stmt)
		ddl, err := legacyTableDDL(database, tablePrefix, stmt, indexes[tableName])
		if err != nil {
			return nil, err
		}
		out = append(out, ddl)
		if tableName == "character_items" {
			out = append(out, legacyCharacterItemExtraDDL(database, tablePrefix))
		}
		if tableName == "item_audit_log" {
			out = append(out, legacyItemAuditPayloadDDL(database, tablePrefix))
		}
	}
	return out, nil
}

func legacyTableDDL(database, prefix, stmt string, indexes []string) (string, error) {
	head := "CREATE TABLE IF NOT EXISTS "
	rest := strings.TrimSpace(stmt[len(head):])
	open := strings.Index(rest, "(")
	close := strings.LastIndex(rest, ")")
	if open <= 0 || close <= open {
		return "", fmt.Errorf("%w: malformed legacy table statement", ErrSchemaConfigInvalid)
	}
	name := strings.TrimSpace(rest[:open])
	name = strings.Trim(name, "`")
	if !repository.IsSQLIdentifier(name) {
		return "", fmt.Errorf("%w: legacy table %q is invalid", ErrSchemaConfigInvalid, name)
	}
	body := rest[open+1 : close]
	parts := splitCols(body)
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		if name == "character_items" && legacyColumnName(part) == "extra_json" {
			continue
		}
		if name == "item_audit_log" && legacyColumnName(part) == "payload_json" {
			continue
		}
		converted, ok, err := legacyColumn(part)
		if err != nil {
			return "", err
		}
		if ok {
			columns = append(columns, converted)
		}
	}
	columns = append(columns, indexes...)
	if len(columns) == 0 {
		return "", fmt.Errorf("%w: legacy table %q has no columns", ErrSchemaConfigInvalid, name)
	}
	table := mysqlTable(database, prefix, "legacy_"+name)
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
		table,
		strings.Join(columns, ",\n  "),
	), nil
}

func legacyColumnName(part string) string {
	part = strings.TrimSpace(part)
	name, _, ok := strings.Cut(part, " ")
	if !ok {
		return ""
	}
	return strings.Trim(name, "`")
}

func legacyCharacterItemExtraDDL(database, prefix string) string {
	table := mysqlTable(database, prefix, mysqlLegacyItemExtraTable)
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  item_uid BIGINT NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (item_uid, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}

func legacyItemAuditPayloadDDL(database, prefix string) string {
	table := mysqlTable(database, prefix, mysqlLegacyItemAuditPayloadTable)
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  audit_id BIGINT NOT NULL,
  value_path VARCHAR(512) NOT NULL,
  value_type VARCHAR(16) NOT NULL,
  string_value LONGTEXT NULL,
  number_value VARCHAR(128) NULL,
  bool_value TINYINT UNSIGNED NULL,
  PRIMARY KEY (audit_id, value_path)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}

func legacyTableName(stmt string) string {
	head := "CREATE TABLE IF NOT EXISTS "
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), head) {
		return ""
	}
	rest := strings.TrimSpace(stmt[len(head):])
	open := strings.Index(rest, "(")
	if open <= 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(rest[:open]), "`")
}

func legacyIndexes(statements []string) (map[string][]string, error) {
	out := make(map[string][]string)
	for _, statement := range statements {
		stmt := strings.TrimSpace(statement)
		upper := strings.ToUpper(stmt)
		if !strings.HasPrefix(upper, "CREATE INDEX IF NOT EXISTS ") && !strings.HasPrefix(upper, "CREATE UNIQUE INDEX IF NOT EXISTS ") {
			continue
		}
		table, clause, err := legacyIndex(stmt)
		if err != nil {
			return nil, err
		}
		out[table] = append(out[table], clause)
	}
	return out, nil
}

func legacyIndex(stmt string) (string, string, error) {
	unique := false
	prefix := "CREATE INDEX IF NOT EXISTS "
	if strings.HasPrefix(strings.ToUpper(stmt), "CREATE UNIQUE INDEX IF NOT EXISTS ") {
		unique = true
		prefix = "CREATE UNIQUE INDEX IF NOT EXISTS "
	}
	rest := strings.TrimSpace(stmt[len(prefix):])
	onPos := strings.Index(strings.ToUpper(rest), " ON ")
	if onPos <= 0 {
		return "", "", fmt.Errorf("%w: malformed legacy index %q", ErrSchemaConfigInvalid, stmt)
	}
	indexName := strings.Trim(strings.TrimSpace(rest[:onPos]), "`")
	if !repository.IsSQLIdentifier(indexName) {
		return "", "", fmt.Errorf("%w: legacy index %q is invalid", ErrSchemaConfigInvalid, indexName)
	}
	target := strings.TrimSpace(rest[onPos+4:])
	open := strings.Index(target, "(")
	close := strings.LastIndex(target, ")")
	if open <= 0 || close <= open {
		return "", "", fmt.Errorf("%w: malformed legacy index target %q", ErrSchemaConfigInvalid, stmt)
	}
	table := strings.Trim(strings.TrimSpace(target[:open]), "`")
	if !repository.IsSQLIdentifier(table) {
		return "", "", fmt.Errorf("%w: legacy index table %q is invalid", ErrSchemaConfigInvalid, table)
	}
	columns := strings.TrimSpace(target[open : close+1])
	kind := "KEY"
	if unique {
		kind = "UNIQUE KEY"
	}
	return table, fmt.Sprintf("%s %s %s", kind, quoteSQLIdentifier(indexName), columns), nil
}

func legacyColumn(part string) (string, bool, error) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", false, nil
	}
	upper := strings.ToUpper(part)
	if strings.HasPrefix(upper, "FOREIGN KEY") || strings.HasPrefix(upper, "CONSTRAINT") {
		return "", false, nil
	}
	part = stripChecks(part)
	upper = strings.ToUpper(part)
	if strings.HasPrefix(upper, "PRIMARY KEY") || strings.HasPrefix(upper, "UNIQUE") {
		return part, true, nil
	}
	name, rest, ok := strings.Cut(part, " ")
	if !ok {
		return "", false, fmt.Errorf("%w: malformed legacy column %q", ErrSchemaConfigInvalid, part)
	}
	name = strings.Trim(name, "`")
	if !repository.IsSQLIdentifier(name) {
		return "", false, fmt.Errorf("%w: legacy column %q is invalid", ErrSchemaConfigInvalid, name)
	}
	definition := legacyDef(strings.TrimSpace(rest))
	return quoteSQLIdentifier(name) + " " + definition, true, nil
}

func legacyDef(definition string) string {
	upper := strings.ToUpper(definition)
	if strings.Contains(upper, "INTEGER PRIMARY KEY AUTOINCREMENT") {
		return strings.TrimSpace(strings.Replace(upper, "INTEGER PRIMARY KEY AUTOINCREMENT", "BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY", 1))
	}
	if strings.Contains(upper, "INTEGER PRIMARY KEY") {
		definition = replaceFirstFold(definition, "INTEGER PRIMARY KEY", "BIGINT NOT NULL PRIMARY KEY")
	} else if strings.HasPrefix(upper, "INTEGER") {
		definition = replaceFirstFold(definition, "INTEGER", "BIGINT")
	} else if strings.HasPrefix(upper, "BLOB") {
		definition = replaceFirstFold(definition, "BLOB", "LONGBLOB")
	} else if strings.HasPrefix(upper, "TEXT") {
		if strings.Contains(upper, "CURRENT_TIMESTAMP") {
			definition = replaceFirstFold(definition, "TEXT", "DATETIME(6)")
			definition = strings.ReplaceAll(definition, "CURRENT_TIMESTAMP", "CURRENT_TIMESTAMP(6)")
		} else {
			definition = replaceFirstFold(definition, "TEXT", "VARCHAR(255)")
		}
	}
	definition = stripMySQLUnsupportedDefault(definition)
	return strings.Join(strings.Fields(definition), " ")
}

func stripMySQLUnsupportedDefault(definition string) string {
	fields := strings.Fields(definition)
	if len(fields) == 0 {
		return definition
	}
	switch strings.ToUpper(fields[0]) {
	case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT", "JSON":
	default:
		return definition
	}
	idx := indexSQLKeyword(definition, "DEFAULT")
	if idx < 0 {
		return definition
	}
	return strings.TrimSpace(definition[:idx])
}

func indexSQLKeyword(value, keyword string) int {
	upperKeyword := strings.ToUpper(keyword)
	inSingleQuote := false
	for idx := 0; idx < len(value); idx++ {
		if value[idx] == '\'' {
			inSingleQuote = !inSingleQuote
			continue
		}
		if inSingleQuote {
			continue
		}
		if idx+len(keyword) > len(value) {
			continue
		}
		if strings.ToUpper(value[idx:idx+len(keyword)]) != upperKeyword {
			continue
		}
		beforeOK := idx == 0 || isSQLKeywordBoundary(value[idx-1])
		after := idx + len(keyword)
		afterOK := after == len(value) || isSQLKeywordBoundary(value[after])
		if beforeOK && afterOK {
			return idx
		}
	}
	return -1
}

func isSQLKeywordBoundary(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '(' || ch == ')'
}

func legacyInsert(database, prefix, stmt string) (string, error) {
	head := "INSERT OR IGNORE INTO "
	rest := strings.TrimSpace(stmt[len(head):])
	open := strings.Index(rest, "(")
	if open <= 0 {
		return "", fmt.Errorf("%w: malformed legacy insert", ErrSchemaConfigInvalid)
	}
	table := strings.Trim(strings.TrimSpace(rest[:open]), "`")
	if !repository.IsSQLIdentifier(table) {
		return "", fmt.Errorf("%w: legacy insert table %q is invalid", ErrSchemaConfigInvalid, table)
	}
	return "INSERT IGNORE INTO " + mysqlTable(database, prefix, "legacy_"+table) + " " + strings.TrimSpace(rest[open:]), nil
}

func stripSQLComments(value string) string {
	lines := strings.Split(value, "\n")
	for idx, line := range lines {
		if pos := strings.Index(line, "--"); pos >= 0 {
			line = line[:pos]
		}
		lines[idx] = line
	}
	return strings.Join(lines, "\n")
}

func splitSQL(value string) []string {
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func splitCols(body string) []string {
	out := make([]string, 0)
	start := 0
	depth := 0
	for idx, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, body[start:idx])
				start = idx + 1
			}
		}
	}
	out = append(out, body[start:])
	return out
}

func stripChecks(value string) string {
	for {
		idx := strings.Index(strings.ToUpper(value), "CHECK")
		if idx < 0 {
			return strings.TrimSpace(value)
		}
		open := strings.Index(value[idx:], "(")
		if open < 0 {
			return strings.TrimSpace(value[:idx])
		}
		open += idx
		depth := 0
		end := -1
	scanGroup:
		for pos := open; pos < len(value); pos++ {
			switch value[pos] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = pos + 1
					break scanGroup
				}
			}
		}
		if end < 0 {
			return strings.TrimSpace(value[:idx])
		}
		value = strings.TrimSpace(value[:idx] + value[end:])
	}
}

func replaceFirstFold(value, old, new string) string {
	idx := strings.Index(strings.ToUpper(value), strings.ToUpper(old))
	if idx < 0 {
		return value
	}
	return value[:idx] + new + value[idx+len(old):]
}
