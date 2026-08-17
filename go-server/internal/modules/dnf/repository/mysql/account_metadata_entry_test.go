package mysql

import (
	"context"
	"longheng.io/server/internal/modules/dnf/repository"
	"testing"
	"time"
)

func TestMySQLAccountMetadataEntryWritesOnlyTimestampAndOwnedRow(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repositories := newTestMySQLGroup(t, sqlDB)
	now := time.Unix(2_000_000_000, 0).UTC()
	err := repository.SaveAccountMetadataEntry(
		context.Background(),
		repositories.Account,
		repository.AccountRecord{AccountID: "account-1"},
		"selector_adventure_info_slot",
		"4",
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	accountCall := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_accounts` SET updated_at")
	if len(accountCall.Args) != 2 || accountCall.Args[1] != "account-1" {
		t.Fatalf("account update args=%#v", accountCall.Args)
	}
	metadataCall := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_account_metadata`")
	assertContains(t, metadataCall.Query, "ON DUPLICATE KEY UPDATE")
	assertContains(t, metadataCall.Query, "`entry_value` = VALUES(`entry_value`)")
	if len(metadataCall.Args) != 3 ||
		metadataCall.Args[0] != "account-1" ||
		metadataCall.Args[1] != "selector_adventure_info_slot" ||
		metadataCall.Args[2] != "4" {
		t.Fatalf("metadata upsert args=%#v", metadataCall.Args)
	}
	for _, call := range sqlDB.execs {
		assertNotContains(t, call.Query, "DELETE FROM")
	}
}
