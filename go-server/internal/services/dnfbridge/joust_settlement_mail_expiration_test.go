package dnfbridge

import (
	"context"
	"testing"
	"time"

	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestReconcileCurrentJoustSettlementMailAttachmentsAddsPVFDeadline(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Mailbox.Save(ctx, dnfrepo.MailboxRecord{
		CharacterID: "19",
		Mails: map[string]dnfrepo.MailRecord{
			"42": {
				MailID: "42",
				Attachments: []dnfrepo.MailAttachment{{
					ItemID: dnfjoust.PermanentCrystalID,
					Count:  232,
				}},
				Metadata: map[string]string{"source": currentJoustSettlementMailSource},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		pvfItemCatalog:     mustTestJoustCatalog(t),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	changed, err := service.reconcileCurrentJoustSettlementMailAttachments(ctx, 19)
	if err != nil || changed != 1 {
		t.Fatalf("repair changed=%d err=%v", changed, err)
	}
	mailbox, found, err := repositories.Mailbox.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load mailbox found=%t err=%v", found, err)
	}
	want := time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)
	if got := mailbox.Mails["42"].Attachments[0].ExpireAt; !got.Equal(want) {
		t.Fatalf("attachment expiry=%s want=%s", got, want)
	}
	changed, err = service.reconcileCurrentJoustSettlementMailAttachments(ctx, 19)
	if err != nil || changed != 0 {
		t.Fatalf("repeat repair changed=%d err=%v", changed, err)
	}
}
