package dnfbridge

import (
	"context"
	"testing"
	"time"

	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentJoustSettlementPersistsHistoryPaysOnceAndClearsPending(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	catalog := mustTestJoustEventCatalog(t)
	round := uint16(4320)
	tournament, err := catalog.TournamentFor(round)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       90,
		Stats: map[string]int64{
			dnfjoust.RoundStat:   int64(round),
			dnfjoust.KnightStat:  int64(tournament.Champion()),
			dnfjoust.AmountStat:  3,
			dnfjoust.PendingStat: 1,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:7": {ItemID: dnfjoust.PermanentCrystalID, Count: 10},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		pvfItemCatalog:     mustTestJoustCatalog(t),
		joustCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	now := time.Unix(7200*4321, 0).UTC().Add(10 * time.Minute)
	results, err := service.settleCurrentJoustAccount(ctx, "account-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Won || results[0].Payout == 0 {
		t.Fatalf("results=%+v", results)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	if character.Stats[dnfjoust.PendingStat] != 0 || character.Stats[dnfjoust.WinnerStat] != int64(tournament.Champion()) {
		t.Fatalf("stats=%v", character.Stats)
	}
	mailbox, _, _ := repositories.Mailbox.Load(ctx, "19")
	mail := mailbox.Mails[results[0].MailID]
	wantExpiry := time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)
	if results[0].MailID == "" || results[0].RewardItemID != dnfjoust.PermanentCrystalID ||
		len(mail.Attachments) != 1 || mail.Attachments[0].ItemID != dnfjoust.PermanentCrystalID ||
		mail.Attachments[0].Count != int64(results[0].Payout) || !mail.Attachments[0].ExpireAt.Equal(wantExpiry) {
		t.Fatalf("mailbox=%+v payout=%d result=%+v", mailbox, results[0].Payout, results[0])
	}
	second, err := service.settleCurrentJoustAccount(ctx, "account-1", now)
	if err != nil || len(second) != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	after, _, _ := repositories.Mailbox.Load(ctx, "19")
	if len(after.Mails) != len(mailbox.Mails) {
		t.Fatalf("payout duplicated before=%v after=%v", mailbox.Mails, after.Mails)
	}
	overrides, err := service.loadCurrentJoustHistoryOverrides(ctx, "account-1")
	if err != nil || overrides[round].Winner != tournament.Champion() || overrides[round].Multiplier <= 0 {
		t.Fatalf("history=%+v err=%v", overrides, err)
	}
}
