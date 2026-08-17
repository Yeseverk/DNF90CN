package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) sendCharacterBootstrap(session *gameSession, source string) error {
	repos, ok := s.repositoryGroup()
	if !ok || repos.Character == nil {
		s.logGameEvent(session, "game-character-bootstrap-repository-missing", "source", source)
		return errCharacterRepositoryMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	accountID := s.accountIDForSession(session)
	characters, err := s.listCharacters(ctx, repos, accountID)
	if err != nil {
		s.logGameEvent(session, "game-character-bootstrap-list-failed", "source", source, "account_id", accountID, "error", err)
		return err
	}
	if err := s.sendCurrentCharacterSelectRoster(session, characters); err != nil {
		return err
	}
	s.logGameEvent(session, "game-character-bootstrap-sent", "source", source, "account_id", accountID, "count", len(characters))
	return nil
}

// createCharacter 执行 latest 创建角色的最小写路经。
// 当前 bridge 只落账号、角色、空背包和空技能记录；后续 starter 装备、技能和经济初始化要迁入 DNF 角色 owner。

func (s *Service) sendUpperRoster(session *gameSession, characters []dnfrepo.CharacterRecord) error {
	return s.sendGameUpperCharacterRosterRaw(session, buildCSharpRosterBody(characters))
}

func (s *Service) sendCharacterList(session *gameSession, characters []dnfrepo.CharacterRecord) error {
	// 86 级 C# inventory.db 的 get_userinfo_template 证明 0x0002 必须带 route=1。
	return s.sendGameWithTransport(
		session,
		byte(dnfenum.GameCmdNotice),
		uint16(dnfenum.GameTypeCharacterList),
		buildCharacterListBody(characters),
		dnfproto.TransportOptions{Route: latestCharacterStateRoute},
	)
}

// sendCurrentCharacterSelectRoster is the current NoPack mode-2 character
// select snapshot.  It is intentionally distinct from the old compact
// ID/slot notification: the current reader dispatches type 2 to
// sub_200BEA0(mode=2), which consumes the complete roster rows.
func (s *Service) sendCurrentCharacterSelectRoster(session *gameSession, characters []dnfrepo.CharacterRecord) error {
	body := buildNoPackRosterBody(characters)
	if len(body) < 15 {
		return fmt.Errorf("current character roster is shorter than its 15-byte prefix: %d", len(body))
	}
	s.logGameEvent(session, "game-character-roster-prefix",
		"repository_count", len(characters),
		"display_count", body[1],
		"selected_page", binary.LittleEndian.Uint16(body[7:9]),
		"entry_count", binary.LittleEndian.Uint16(body[13:15]),
		"prefix_len", 15,
	)
	if err := s.sendGameFixed15Route(
		session,
		byte(dnfenum.GameCmdNotice),
		uint16(dnfenum.GameTypeCharacterList),
		// Current NoPack sub_200BEA0 mode=2 consumes two leading bytes,
		// three u16 values, one u32 value, then the final u16 entry count.
		// Do not substitute the different C# account-header grammar here.
		body,
		latestCharacterStateRoute,
	); err != nil {
		return err
	}
	noteEmptyRosterSlotProbe(session, body)
	return nil
}

func buildCharacterListBody(characters []dnfrepo.CharacterRecord) []byte {
	var writer packetWriter
	count := len(characters)
	if count > 255 {
		count = 255
	}
	writer.writeByte(byte(count))
	writer.writeByte(latestCharacterStateActive)
	writer.writeByte(0)
	writer.writeByte(latestCharacterCreateEnabled)
	for idx, character := range characters {
		if idx >= count {
			break
		}
		writer.writeUint32(uint32(numericCharacterID(character)))
		writer.writeUint16(uint16(character.Slot))
	}
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	return writer.bytes()
}

func numericCharacterID(record dnfrepo.CharacterRecord) int {
	value, err := strconv.Atoi(strings.TrimSpace(record.CharacterID))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func numericCharacterStat(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func numericCharacterStatValue(record dnfrepo.CharacterRecord, key string) int64 {
	if record.Stats == nil {
		return 0
	}
	value := record.Stats[key]
	if value < 0 {
		return 0
	}
	return value
}
