package dnfbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentJoustSettlementMailSource = "joust_settlement_multiplier_return"

// reconcileCurrentJoustSettlementMailAttachments repairs the previously
// created, still-claimable joust reward mails that had no item-instance
// expiration. The current client treats that missing timestamp as the retired
// event date even though the active PVF supplies a future deadline.
func (s *Service) reconcileCurrentJoustSettlementMailAttachments(
	ctx context.Context,
	characterID uint16,
) (int, error) {
	if s == nil || characterID == 0 {
		return 0, nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.MailboxAssets == nil {
		return 0, dnfrepo.ErrRepoMissing
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return 0, err
	}
	characterKey := strconv.FormatUint(uint64(characterID), 10)
	now := s.gameplayNow().UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	changed := 0
	err = repositories.MailboxAssets.WithinMailboxAssets(ctx, characterKey, characterKey, func(
		_ dnfrepo.CharacterRepository,
		_ dnfrepo.InventoryRepository,
		mailboxes dnfrepo.MailboxRepository,
	) error {
		mailbox, found, loadErr := mailboxes.Load(ctx, characterKey)
		if loadErr != nil || !found {
			return loadErr
		}
		mailbox = dnfrepo.CloneMailbox(mailbox)
		for mailID, mail := range mailbox.Mails {
			if mail.Claimed || mail.Deleted ||
				!strings.EqualFold(strings.TrimSpace(mail.Metadata["source"]), currentJoustSettlementMailSource) {
				continue
			}
			mailChanged := false
			for index := range mail.Attachments {
				attachment := &mail.Attachments[index]
				if attachment.ItemID <= 0 || attachment.Count <= 0 || attachment.ItemID > int64(^uint32(0)) {
					return fmt.Errorf("joust settlement mail=%s attachment=%d is invalid", mailID, index)
				}
				definition, resolveErr := catalog.ResolveItem(uint32(attachment.ItemID))
				if resolveErr != nil {
					return fmt.Errorf("resolve joust settlement mail=%s attachment=%d item=%d: %w", mailID, index, attachment.ItemID, resolveErr)
				}
				if definition.Kind != dungeonDropItemStackable ||
					(!definition.ExpirationDate.IsZero() && !definition.ExpirationDate.After(now)) {
					return fmt.Errorf("joust settlement mail=%s attachment=%d item=%d is not a live stackable", mailID, index, attachment.ItemID)
				}
				if definition.ExpirationDate.IsZero() || attachment.ExpireAt.Equal(definition.ExpirationDate) {
					continue
				}
				attachment.ExpireAt = definition.ExpirationDate
				mailChanged = true
				changed++
			}
			if mailChanged {
				mailbox.Mails[mailID] = mail
			}
		}
		if changed == 0 {
			return nil
		}
		mailbox.UpdatedAt = now
		return dnfrepo.SaveMailboxFields(ctx, mailboxes, mailbox, dnfrepo.MailboxFieldMails)
	})
	return changed, err
}

func (s *Service) reconcileCurrentJoustSettlementMailBeforeOpen(session *gameSession) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	changed, err := s.reconcileCurrentJoustSettlementMailAttachments(ctx, session.selectedCharacterID)
	if err != nil {
		s.logGameEvent(session, "game-joust-settlement-mail-expiration-repair-deferred", "error", err)
		return
	}
	if changed > 0 {
		s.logGameEvent(session, "game-joust-settlement-mail-expiration-repaired", "attachments", changed)
	}
}
