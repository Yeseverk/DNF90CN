// 本文件由 transaction_test.go 按后端拆分而来。
package mysql

import (
	"testing"
)

func TestMySQLTransactionRouterLocksAggregateReads(t *testing.T) {
	query := "SELECT character_id FROM dnf_inventory WHERE character_id = ?"
	if got := (mysqlRouter{}).selectQuery(query); got != query {
		t.Fatalf("ordinary select = %q", got)
	}
	if got := (mysqlRouter{lockReads: true}).selectQuery(query); got != query+" FOR UPDATE" {
		t.Fatalf("transaction select = %q", got)
	}
}
