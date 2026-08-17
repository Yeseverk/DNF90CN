package dnfbridge

import (
	"context"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfequip "longheng.io/server/internal/modules/dnf/equip"
)

func (s *Service) sendSelectCharacterState(session *gameSession, body []byte) error {
	if len(body) < 11 {
		s.logPacketEvent("game-select-character-body-short",
			"conn_id", session.connID,
			"body_len", len(body))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, _, _, hasCharacter, slot := s.resolveSelectedCharacter(ctx, session, body)
	s.bindGameSessionCharacter(session, charID)
	if hasCharacter {
		s.persistCurrentSelectorAdventureInfoSlot(ctx, session, slot)
	}
	s.cleanupExpiredNameTagOnSelect(ctx, session, charID)
	s.logGameEvent(session, "game-select-character-state-selected", "slot", slot, "char_id", charID)
	// NoPack.exe 0x2FBC9B0 type=3 S2C reads u32,u8,u8,u32,u8.
	return s.sendGame(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.GameTypeSelectCharacter),
		append([]byte(nil), body[:11]...),
	)
}

func (s *Service) cleanupExpiredNameTagOnSelect(
	ctx context.Context,
	session *gameSession,
	characterID uint16,
) {
	if characterID == 0 {
		return
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return
	}
	owner, err := dnfequip.NewOwner(repositories)
	if err != nil {
		s.logGameEvent(session, "game-name-tag-card-expired-cleanup-failed",
			"char_id", characterID,
			"error", err)
		return
	}
	result, err := owner.CleanupExpiredNameTag(ctx, dnfequip.CleanupExpiredNameTagCommand{
		AccountID:   s.accountIDForSession(session),
		CharacterID: strconv.FormatUint(uint64(characterID), 10),
		SlotIndex:   currentNameTagEquipmentSlot,
	})
	if err != nil {
		s.logGameEvent(session, "game-name-tag-card-expired-cleanup-failed",
			"char_id", characterID,
			"error", err)
		return
	}
	if result.Changed {
		s.logGameEvent(session, "game-name-tag-card-expired-cleaned",
			"char_id", characterID,
			"item_id", result.ItemID,
			"expired_at", result.ExpiredAt,
			"equipment_removed", result.EquipmentRemoved)
	}
}
