package dnfbridge

import (
	"context"
	"encoding/binary"

	dnfcharacterdata "longheng.io/server/internal/modules/dnf/characterdata"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) handleChangeCharacterSlot(session *gameSession, body []byte, upper bool) error {
	// Both current op295 writers emit exactly two u32 slot indexes. Reject
	// truncated bodies and any trailing field before touching the repository.
	if len(body) != 8 {
		s.logGameEvent(session, "game-character-slot-change-malformed", "upper", upper, "body_len", len(body), "expected_body_len", 8)
		return s.sendChangeCharacterSlotFailure(session, upper)
	}
	slotA := int(binary.LittleEndian.Uint32(body[:4]))
	slotB := int(binary.LittleEndian.Uint32(body[4:8]))
	repos, ok := s.repositoryGroup()
	if !ok || repos.Character == nil {
		s.logGameEvent(session, "game-character-slot-change-repository-missing", "upper", upper, "slot_a", slotA, "slot_b", slotB)
		return s.sendChangeCharacterSlotFailure(session, upper)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	accountID := s.accountIDForSession(session)
	owner, err := dnfcharacterdata.NewRosterOwner(repos)
	if err != nil {
		s.logGameEvent(session, "game-character-slot-change-owner-missing",
			"upper", upper,
			"account_id", accountID,
			"slot_a", slotA,
			"slot_b", slotB,
			"error", err)
		return s.sendChangeCharacterSlotFailure(session, upper)
	}
	if err := owner.SwapSlots(ctx, accountID, slotA, slotB); err != nil {
		s.logGameEvent(session, "game-character-slot-change-failed",
			"upper", upper,
			"account_id", accountID,
			"slot_a", slotA,
			"slot_b", slotB,
			"error", err)
		return s.sendChangeCharacterSlotFailure(session, upper)
	}
	characters, err := s.listCharacters(ctx, repos, accountID)
	if err != nil {
		s.logGameEvent(session, "game-character-slot-change-refresh-list-failed",
			"upper", upper,
			"account_id", accountID,
			"slot_a", slotA,
			"slot_b", slotB,
			"error", err)
		return s.sendChangeCharacterSlotFailure(session, upper)
	}
	if upper {
		if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketChangeCharacSlot), nil); err != nil {
			return err
		}
	} else if err := s.sendGame(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketChangeCharacSlot), []byte{0x01}); err != nil {
		return err
	}
	if err := s.sendUpperRoster(session, characters); err != nil {
		return err
	}
	s.logGameEvent(session, "game-character-slot-change-accepted",
		"upper", upper,
		"account_id", accountID,
		"slot_a", slotA,
		"slot_b", slotB,
		"count", len(characters))
	return nil
}

func (s *Service) sendChangeCharacterSlotFailure(session *gameSession, upper bool) error {
	if upper {
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketChangeCharacSlot), createCodeServerError)
	}
	return s.sendGame(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketChangeCharacSlot), []byte{0x00, createCodeServerError})
}

// sendCharacterBootstrap 发送角色选择页初始化序列。
// 当前只回角色列表；未选角色前不提前发送 C# 旧端的场景尾包。
