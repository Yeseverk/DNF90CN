package joust

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestOwnerSettlementPaysWinnerAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repos := betRepositories(t, 10)
	character, _, _ := repos.Character.Load(ctx, "19")
	character.Stats = map[string]int64{RoundStat: 7, KnightStat: 1, AmountStat: 3, PendingStat: 1}
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repos)
	deliver := func(mailbox dnfrepo.MailboxRepository, itemID int64, count uint32) (string, error) {
		return dnfrepo.AppendSystemMail(ctx, mailbox, dnfrepo.SystemMailDelivery{
			RecipientCharacterID: "19",
			Title:                "test",
			Source:               "joust-test",
			Attachments:          []dnfrepo.MailAttachment{{ItemID: itemID, Count: int64(count)}},
		})
	}
	result, err := owner.Settle(ctx, SettlementCommand{SelectedCharacterID: 19, Round: 7, Winner: 1, Multiplier: 2.5, DeliverReward: deliver})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Settled || !result.Won || result.Payout != 7 {
		t.Fatalf("result=%+v", result)
	}
	second, err := owner.Settle(ctx, SettlementCommand{SelectedCharacterID: 19, Round: 7, Winner: 1, Multiplier: 2.5, DeliverReward: deliver})
	if err != nil || second.Settled {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	mailbox, _, _ := repos.Mailbox.Load(ctx, "19")
	mail := mailbox.Mails[result.MailID]
	if result.MailID == "" || result.RewardItemID != PermanentCrystalID || len(mail.Attachments) != 1 ||
		mail.Attachments[0].ItemID != PermanentCrystalID || mail.Attachments[0].Count != 7 {
		t.Fatalf("missing or duplicate payout mail result=%+v mailbox=%+v", result, mailbox)
	}
}

func TestOwnerSettlementPlacementFailureKeepsPendingClaim(t *testing.T) {
	ctx := context.Background()
	repos := betRepositories(t, 10)
	character, _, _ := repos.Character.Load(ctx, "19")
	character.Stats = map[string]int64{RoundStat: 7, KnightStat: 1, AmountStat: 3, PendingStat: 1}
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repos)
	_, err := owner.Settle(ctx, SettlementCommand{
		SelectedCharacterID: 19,
		Round:               7,
		Winner:              1,
		Multiplier:          2.5,
		DeliverReward: func(dnfrepo.MailboxRepository, int64, uint32) (string, error) {
			return "", errors.New("mail unavailable")
		},
	})
	if !errors.Is(err, ErrRewardPlacement) {
		t.Fatalf("error=%v", err)
	}
	character, _, _ = repos.Character.Load(ctx, "19")
	if character.Stats[PendingStat] != 1 {
		t.Fatalf("claim was lost stats=%v", character.Stats)
	}
}

func TestOwnerSettlementPaysOnlyTheWinningPortionOfSplitSupport(t *testing.T) {
	ctx := context.Background()
	repos := betRepositories(t, 10)
	character, _, _ := repos.Character.Load(ctx, "19")
	character.Stats = map[string]int64{
		RoundStat:           7,
		KnightStat:          2,
		AmountStat:          10,
		KnightAmountStat(1): 3,
		KnightAmountStat(2): 7,
		PendingStat:         1,
	}
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repos)
	result, err := owner.Settle(ctx, SettlementCommand{
		SelectedCharacterID: 19,
		Round:               7,
		Winner:              1,
		Multiplier:          2.5,
		DeliverReward: func(mailbox dnfrepo.MailboxRepository, itemID int64, count uint32) (string, error) {
			return dnfrepo.AppendSystemMail(ctx, mailbox, dnfrepo.SystemMailDelivery{
				RecipientCharacterID: "19",
				Title:                "test",
				Source:               "joust-test",
				Attachments:          []dnfrepo.MailAttachment{{ItemID: itemID, Count: int64(count)}},
			})
		},
	})
	if err != nil || !result.Settled || !result.Won || result.Amount != 10 || result.Payout != 7 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestOwnerSettlementReturnsTheActualStakedMaterialByMail(t *testing.T) {
	ctx := context.Background()
	repos := betRepositories(t, 10)
	character, _, _ := repos.Character.Load(ctx, "19")
	character.Stats = map[string]int64{
		RoundStat:        7,
		KnightStat:       1,
		AmountStat:       3,
		SourceItemIDStat: EventCrystalID,
		PendingStat:      1,
	}
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repos)
	result, err := owner.Settle(ctx, SettlementCommand{
		SelectedCharacterID: 19,
		Round:               7,
		Winner:              1,
		Multiplier:          2.5,
		DeliverReward: func(mailbox dnfrepo.MailboxRepository, itemID int64, count uint32) (string, error) {
			return dnfrepo.AppendSystemMail(ctx, mailbox, dnfrepo.SystemMailDelivery{
				RecipientCharacterID: "19",
				Title:                "test",
				Source:               "joust-test",
				Attachments:          []dnfrepo.MailAttachment{{ItemID: itemID, Count: int64(count)}},
			})
		},
	})
	if err != nil || !result.Settled || result.RewardItemID != EventCrystalID || result.Payout != 7 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	mailbox, _, _ := repos.Mailbox.Load(ctx, "19")
	mail := mailbox.Mails[result.MailID]
	if len(mail.Attachments) != 1 || mail.Attachments[0].ItemID != EventCrystalID || mail.Attachments[0].Count != 7 {
		t.Fatalf("mail=%+v", mail)
	}
}
