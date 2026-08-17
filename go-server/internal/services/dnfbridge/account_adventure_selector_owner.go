package dnfbridge

import (
	"context"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
)

func currentSelectorAdventureInfoSlot(metadata map[string]string) (int, bool) {
	if metadata == nil {
		return 0, false
	}
	slot, err := strconv.Atoi(strings.TrimSpace(metadata[currentSelectorAdventureInfoSlotMetadata]))
	if err != nil || slot < 0 || slot >= defaultCharacterSlots {
		return 0, false
	}
	return slot, true
}

// persistCurrentSelectorAdventureInfoSlot records only a slot that the
// SELECT_CHARACTER handler already resolved against the real account roster.
func (s *Service) persistCurrentSelectorAdventureInfoSlot(
	ctx context.Context,
	session *gameSession,
	slot int,
) {
	if s == nil || slot < 0 || slot >= defaultCharacterSlots {
		return
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return
	}
	owner, err := adventuregroup.NewOwner(repositories)
	if err != nil {
		return
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	result, err := owner.RememberSelectorSlot(ctx, adventuregroup.RememberSelectorSlotCommand{
		AccountID: accountID,
		Slot:      slot,
		SlotLimit: defaultCharacterSlots,
	})
	if err != nil {
		s.logGameEvent(session, "game-selector-adventure-slot-persist-failed",
			"account_id", accountID,
			"slot", slot,
			"error", err)
		return
	}
	if result.Changed {
		s.logGameEvent(session, "game-selector-adventure-slot-persisted",
			"account_id", accountID,
			"slot", slot)
	}
}
