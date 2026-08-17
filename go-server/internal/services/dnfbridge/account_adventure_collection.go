package dnfbridge

import (
	"context"
	"encoding/binary"
	"sort"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentAdventureRepresentRequestLength = 2
	currentAdventureRepresentGrowMask      = byte(0x0f)
	currentAdventureRepresentTypeMask      = byte(0x70)
	currentAdventureRepresentType          = byte(0x20)
	currentAdventureRepresentEntrySize     = 52
	currentAdventureRepresentGroupOffset   = 0
	currentAdventureRepresentStyleOffset   = 4
	currentAdventureRepresentLevelOffset   = 8
	currentAdventureRepresentIDOffset      = 12
	currentAdventureRepresentNameOffset    = 20
	currentAdventureRepresentNameSize      = 30
)

// handleCurrentAdventureRepresentCharacters follows current NoPack
// sub_C79FE0/sub_C7CC60. The request is u8 job plus the packed grow byte
// produced by sub_26900E0(grow, 2). The success body groups matching account
// characters by server-group byte. Its 52-byte row is specific to op1467 and
// must not reuse the differently laid out op1340/op1403 roster record.
func (s *Service) handleCurrentAdventureRepresentCharacters(
	session *gameSession,
	request []byte,
) error {
	opcode := uint16(dnfenum.CmdPacketGetRepresentCharacJob)
	if len(request) != currentAdventureRepresentRequestLength ||
		request[1]&currentAdventureRepresentTypeMask != currentAdventureRepresentType {
		return s.sendGameUpperFailure(session, opcode, 3)
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	state, err := s.currentAdventureSelectedState(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-adventure-represent-characters-rejected",
			"job", request[0],
			"grow", request[1]&currentAdventureRepresentGrowMask,
			"error", err)
		return s.sendGameUpperFailure(session, opcode, currentAdventureGenericFailureCode)
	}

	job := request[0]
	grow := request[1] & currentAdventureRepresentGrowMask
	body := buildCurrentAdventureRepresentCharactersBody(job, grow, state.Characters)
	s.logGameEvent(session, "game-adventure-represent-characters-send",
		"job", job,
		"grow", grow,
		"roster_count", countCurrentAdventureRepresentCharacters(job, grow, state.Characters),
		"body_len", len(body))
	return s.sendGameUpperSuccess(session, opcode, body)
}

func buildCurrentAdventureRepresentCharactersBody(
	job byte,
	grow byte,
	characters []dnfrepo.CharacterRecord,
) []byte {
	grouped := make(map[byte][]dnfrepo.CharacterRecord)
	for _, character := range characters {
		if byte(numericCharacterStat(character.Job)) != job ||
			byte(numericCharacterStatValue(character, "grow_type")) != grow {
			continue
		}
		group := currentAdventureCharacterServerGroup(character)
		grouped[group] = append(grouped[group], character)
	}

	groups := make([]int, 0, len(grouped))
	for group := range grouped {
		groups = append(groups, int(group))
	}
	sort.Ints(groups)
	if len(groups) > 255 {
		groups = groups[:255]
	}

	var writer packetWriter
	writer.writeByte(byte(len(groups)))
	for _, groupValue := range groups {
		group := byte(groupValue)
		rows := grouped[group]
		sort.SliceStable(rows, func(left, right int) bool {
			if rows[left].Slot != rows[right].Slot {
				return rows[left].Slot < rows[right].Slot
			}
			return numericCharacterID(rows[left]) < numericCharacterID(rows[right])
		})
		if len(rows) > 255 {
			rows = rows[:255]
		}
		writer.writeByte(group)
		writer.writeByte(byte(len(rows)))
		for _, character := range rows {
			entry := make([]byte, currentAdventureRepresentEntrySize)
			writeCurrentAdventureRepresentEntry(entry, group, character)
			writer.writeBytes(entry)
		}
	}
	return writer.bytes()
}

func writeCurrentAdventureRepresentEntry(
	entry []byte,
	group byte,
	character dnfrepo.CharacterRecord,
) {
	if len(entry) < currentAdventureRepresentEntrySize {
		return
	}
	entry = entry[:currentAdventureRepresentEntrySize]
	clear(entry)

	// Current NoPack sub_9F2810/sub_9F3F70 read the row directly:
	//   +0  u8  server group used by each list card
	//   +4  u32 presentation tier/style (zero selects the default style)
	//   +8  u32 character level rendered by the "Lv%d" label
	//   +12 u32 character ID sent back by op1468
	//   +20 char[30] GB18030 character name
	entry[currentAdventureRepresentGroupOffset] = group
	binary.LittleEndian.PutUint32(
		entry[currentAdventureRepresentStyleOffset:currentAdventureRepresentStyleOffset+4],
		0,
	)
	binary.LittleEndian.PutUint32(
		entry[currentAdventureRepresentLevelOffset:currentAdventureRepresentLevelOffset+4],
		uint32(rosterLevel(character)),
	)
	binary.LittleEndian.PutUint32(
		entry[currentAdventureRepresentIDOffset:currentAdventureRepresentIDOffset+4],
		uint32(numericCharacterID(character)),
	)
	name := currentAdventureInfoRosterNameBytes(character.Name)
	copy(
		entry[currentAdventureRepresentNameOffset:currentAdventureRepresentNameOffset+currentAdventureRepresentNameSize],
		name,
	)
}

func countCurrentAdventureRepresentCharacters(
	job byte,
	grow byte,
	characters []dnfrepo.CharacterRecord,
) int {
	count := 0
	for _, character := range characters {
		if byte(numericCharacterStat(character.Job)) == job &&
			byte(numericCharacterStatValue(character, "grow_type")) == grow {
			count++
		}
	}
	return count
}

func currentAdventureCharacterServerGroup(character dnfrepo.CharacterRecord) byte {
	value := numericCharacterStatValue(character, "server_group_id")
	if value < 0 || value > 255 {
		return 0
	}
	return byte(value)
}
