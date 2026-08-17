package dnfbridge

import (
	"errors"
	"fmt"
	"math"
)

const (
	currentDungeonPlayResultNoticeMsgID         = uint16(34)
	currentDungeonClearRewardMsgID              = uint16(35)
	currentDungeonPlayResultMaximumParticipants = 8
	// The all-empty dynamic form consumed by current NoPack sub_1D4D380 is
	// 210 bytes. After both eight-slot auxiliary tables the reader consumes
	// one standalone u32 (v67), followed by the 22-byte result-window tail:
	// u32(v50), u8(v101), u8(v97), and four u32 values. Omitting v67 shifts
	// the result-window flag by four bytes and makes the client read it as 0.
	currentDungeonClearRewardMinimumBodySize    = 210
	currentDungeonClearRewardGroupCount         = 8
	currentDungeonClearRewardBonusFieldCount    = 22
	currentDungeonClearRewardPostBaseFieldCount = 7
	currentDungeonClearRewardScoreFieldCount    = 4
	currentDungeonClearRewardTailSize           = 22
)

const (
	currentDungeonClearRewardBaseClearExperienceIndex = iota
	currentDungeonClearRewardBaseScoreBonusIndex
	currentDungeonClearRewardBaseDetailCategory3Index
	currentDungeonClearRewardBaseDetailCategory0Index
)

const (
	currentDungeonClearRewardBonusClearEventIndex = iota
	currentDungeonClearRewardBonusClearAncientElixirIndex
	currentDungeonClearRewardBonusClearBlackDiamondIndex
	currentDungeonClearRewardBonusClearInternetCafeIndex
	currentDungeonClearRewardBonusUnknown4Index
	currentDungeonClearRewardBonusClearPetIndex
	currentDungeonClearRewardBonusClearDetailCategory6Index
	currentDungeonClearRewardBonusMonsterReturnedHeroPart1Index
	currentDungeonClearRewardBonusClearGrowthContractIndex
	currentDungeonClearRewardBonusClearFatigueBurnIndex
	currentDungeonClearRewardBonusUnknown10Index
	currentDungeonClearRewardBonusClearGuildIndex
	currentDungeonClearRewardBonusMonsterPartyIndex
	currentDungeonClearRewardBonusMonsterLuckyCharacterPartyIndex
	currentDungeonClearRewardBonusMonsterReturnedHeroPart2Index
	currentDungeonClearRewardBonusMonsterFatigueBurnIndex
	currentDungeonClearRewardBonusMonsterFatigueBuffIndex
	currentDungeonClearRewardBonusMonsterGrowthContractIndex
	currentDungeonClearRewardBonusMonsterWeekendIndex
	currentDungeonClearRewardBonusMonsterAvatarIndex
	currentDungeonClearRewardBonusMonsterGuildIndex
	currentDungeonClearRewardBonusUnknown21Index
)

var (
	errCurrentDungeonPlayResultNoticeShape  = errors.New("current dungeon play-result notice shape is invalid")
	errCurrentDungeonClearRewardShape       = errors.New("current dungeon clear-reward shape is invalid")
	errCurrentDungeonClearRewardUncommitted = errors.New("current dungeon clear-reward snapshot is not committed")
)

// currentDungeonPlayResultParticipant is the exact repeated row consumed by
// current NoPack sub_1D3BAE0: u16 + u32 + u8. The u32 is kept as the same
// clear-time value as the fixed header because that is the only current-reader
// compatible domain ordering corroborated by the reference writer.
type currentDungeonPlayResultParticipant struct {
	ObjectKey   uint16
	ClearTimeMS uint32
	NewBest     bool
}

// currentDungeonPlayResultNotice is the exact current class0/op34 grammar.
// RankPoint is copied from the proved byte at current C2S op46 offset 10; it is
// not trusted as a reward source.
type currentDungeonPlayResultNotice struct {
	RankGrade       byte
	ClearTimeMS     uint32
	TimeBonusPoint  byte
	RankPoint       byte
	AllVisitedClear bool
	Participants    []currentDungeonPlayResultParticipant
}

func buildCurrentDungeonPlayResultNoticeBody(notice currentDungeonPlayResultNotice) ([]byte, error) {
	if len(notice.Participants) == 0 || len(notice.Participants) > currentDungeonPlayResultMaximumParticipants {
		return nil, fmt.Errorf("%w: participants=%d", errCurrentDungeonPlayResultNoticeShape, len(notice.Participants))
	}
	var writer packetWriter
	writer.writeByte(notice.RankGrade)
	writer.writeUint32(notice.ClearTimeMS)
	writer.writeByte(notice.TimeBonusPoint)
	writer.writeByte(notice.RankPoint)
	writer.writeByte(boolByte(notice.AllVisitedClear))
	writer.writeByte(byte(len(notice.Participants)))
	for _, participant := range notice.Participants {
		if participant.ObjectKey == 0 {
			return nil, fmt.Errorf("%w: participant object key is zero", errCurrentDungeonPlayResultNoticeShape)
		}
		writer.writeUint16(participant.ObjectKey)
		writer.writeUint32(participant.ClearTimeMS)
		writer.writeByte(boolByte(participant.NewBest))
	}
	return writer.bytes(), nil
}

type currentDungeonClearRewardPair struct {
	Key   uint32
	Value uint32
}

// currentDungeonClearRewardDrop is the exact 15-byte row consumed by current
// NoPack sub_1D4D380. Business names are limited to fields whose consumers are
// structurally proved; the two trailing u16 values remain opaque.
type currentDungeonClearRewardDrop struct {
	ObjectKey  uint16
	TemplateID uint32
	StackCount uint32
	Value16A   uint16
	Value8     byte
	Value16B   uint16
}

type currentDungeonClearRewardTail struct {
	// Current NoPack sub_1D4D380 reads this exact final sequence:
	// u32(v50), u8(v101), u8(v97), u32(v65), u32(v51), u32(v53), u32(v49).
	// ShowResult gates the normal result window. Current-EXE data flow proves
	// the first u32 after the flags is the complete monster base EXP total.
	Value0          uint32
	ShowResult      bool
	Flag0           bool
	MonsterTotalExp uint32
	Value2          uint32
	Value3          uint32
	Value4          uint32
}

// currentDungeonClearRewardSnapshot is an already-committed, immutable
// settlement delta. This type deliberately does not calculate rewards. A
// caller must freeze it from authoritative runtime/DB/PVF owners before op35
// can be emitted; an empty source or an entirely zero reward is rejected so a
// fabricated current-reader zero body cannot silently enter production.
type currentDungeonClearRewardSnapshot struct {
	CharacterID   uint16
	CompletionKey string
	Source        string
	Committed     bool
	// These fields are transaction receipts only and are not serialized into
	// an op35 slot whose current-EXE meaning is still unknown.
	CommittedExperienceGain uint32
	CommittedSPGain         int
	CommittedTPGain         int

	Base         [4]uint32
	BaseFlag     byte
	Bonus        [currentDungeonClearRewardBonusFieldCount]uint32
	BonusGroupsA []currentDungeonClearRewardPair
	BonusGroupsB []currentDungeonClearRewardPair
	PostBase     [currentDungeonClearRewardPostBaseFieldCount]uint32
	Score        [currentDungeonClearRewardScoreFieldCount]uint32
	Quest        []currentDungeonClearRewardPair
	Drops        []currentDungeonClearRewardDrop
	CardSlots    [currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair
	TotalReward  uint32
	AuxGroupsA   [currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair
	AuxGroupsB   [currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair
	// Current NoPack sub_1D4D380 reads this standalone u32 as v67 before the
	// final result-window tail. Its business meaning is not proved, so the
	// settlement owner leaves it neutral instead of fabricating a value.
	PreTailValue uint32
	Tail         currentDungeonClearRewardTail
}

func buildCurrentDungeonClearRewardBody(snapshot currentDungeonClearRewardSnapshot) ([]byte, error) {
	if err := validateCurrentDungeonClearRewardSnapshot(snapshot); err != nil {
		return nil, err
	}
	var writer packetWriter
	for _, value := range snapshot.Base {
		writer.writeUint32(value)
	}
	writer.writeByte(snapshot.BaseFlag)
	for _, value := range snapshot.Bonus {
		writer.writeUint32(value)
	}
	writeCurrentDungeonRewardU8Pairs(&writer, snapshot.BonusGroupsA)
	writeCurrentDungeonRewardU8Pairs(&writer, snapshot.BonusGroupsB)
	for _, value := range snapshot.PostBase {
		writer.writeUint32(value)
	}
	for _, value := range snapshot.Score {
		writer.writeUint32(value)
	}
	writer.writeUint32(uint32(len(snapshot.Quest)))
	writeCurrentDungeonRewardPairs(&writer, snapshot.Quest)
	writer.writeByte(byte(len(snapshot.Drops)))
	for _, drop := range snapshot.Drops {
		writer.writeUint16(drop.ObjectKey)
		writer.writeUint32(drop.TemplateID)
		writer.writeUint32(drop.StackCount)
		writer.writeUint16(drop.Value16A)
		writer.writeByte(drop.Value8)
		writer.writeUint16(drop.Value16B)
	}
	for _, slot := range snapshot.CardSlots {
		writeCurrentDungeonRewardU8Pairs(&writer, slot)
	}
	writer.writeUint32(snapshot.TotalReward)
	for _, group := range snapshot.AuxGroupsA {
		writeCurrentDungeonRewardU8Pairs(&writer, group)
	}
	for _, group := range snapshot.AuxGroupsB {
		writeCurrentDungeonRewardU8Pairs(&writer, group)
	}
	writer.writeUint32(snapshot.PreTailValue)
	writer.writeUint32(snapshot.Tail.Value0)
	writer.writeByte(boolByte(snapshot.Tail.ShowResult))
	writer.writeByte(boolByte(snapshot.Tail.Flag0))
	writer.writeUint32(snapshot.Tail.MonsterTotalExp)
	writer.writeUint32(snapshot.Tail.Value2)
	writer.writeUint32(snapshot.Tail.Value3)
	writer.writeUint32(snapshot.Tail.Value4)
	body := writer.bytes()
	if len(body) < currentDungeonClearRewardMinimumBodySize {
		return nil, fmt.Errorf("%w: body=%d minimum=%d", errCurrentDungeonClearRewardShape, len(body), currentDungeonClearRewardMinimumBodySize)
	}
	return body, nil
}

func validateCurrentDungeonClearRewardSnapshot(snapshot currentDungeonClearRewardSnapshot) error {
	if !snapshot.Committed || snapshot.CharacterID == 0 || snapshot.CompletionKey == "" || snapshot.Source == "" {
		return fmt.Errorf("%w: character=%d key=%q source=%q", errCurrentDungeonClearRewardUncommitted, snapshot.CharacterID, snapshot.CompletionKey, snapshot.Source)
	}
	if len(snapshot.BonusGroupsA) > math.MaxUint8 || len(snapshot.BonusGroupsB) > math.MaxUint8 ||
		len(snapshot.Drops) > math.MaxUint8 || uint64(len(snapshot.Quest)) > math.MaxUint32 {
		return fmt.Errorf("%w: top-level dynamic count overflow", errCurrentDungeonClearRewardShape)
	}
	for _, groups := range [][currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair{snapshot.CardSlots, snapshot.AuxGroupsA, snapshot.AuxGroupsB} {
		for _, group := range groups {
			if len(group) > math.MaxUint8 {
				return fmt.Errorf("%w: group count=%d", errCurrentDungeonClearRewardShape, len(group))
			}
		}
	}
	for _, drop := range snapshot.Drops {
		if drop.ObjectKey == 0 || drop.TemplateID == 0 || drop.StackCount == 0 {
			return fmt.Errorf("%w: invalid drop=%+v", errCurrentDungeonClearRewardShape, drop)
		}
	}
	if !currentDungeonClearRewardHasCommittedValue(snapshot) {
		return fmt.Errorf("%w: committed snapshot contains no reward value", errCurrentDungeonClearRewardShape)
	}
	return nil
}

func currentDungeonClearRewardHasCommittedValue(snapshot currentDungeonClearRewardSnapshot) bool {
	if snapshot.CommittedExperienceGain != 0 || snapshot.CommittedSPGain != 0 || snapshot.CommittedTPGain != 0 ||
		snapshot.BaseFlag != 0 || snapshot.TotalReward != 0 ||
		len(snapshot.BonusGroupsA) != 0 || len(snapshot.BonusGroupsB) != 0 || len(snapshot.Quest) != 0 || len(snapshot.Drops) != 0 {
		return true
	}
	for _, value := range snapshot.Base {
		if value != 0 {
			return true
		}
	}
	for _, value := range snapshot.Bonus {
		if value != 0 {
			return true
		}
	}
	for _, value := range snapshot.PostBase {
		if value != 0 {
			return true
		}
	}
	for _, value := range snapshot.Score {
		if value != 0 {
			return true
		}
	}
	for _, groups := range [][currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair{snapshot.CardSlots, snapshot.AuxGroupsA, snapshot.AuxGroupsB} {
		for _, group := range groups {
			if len(group) != 0 {
				return true
			}
		}
	}
	return false
}

func writeCurrentDungeonRewardU8Pairs(writer *packetWriter, pairs []currentDungeonClearRewardPair) {
	writer.writeByte(byte(len(pairs)))
	writeCurrentDungeonRewardPairs(writer, pairs)
}

func writeCurrentDungeonRewardPairs(writer *packetWriter, pairs []currentDungeonClearRewardPair) {
	for _, pair := range pairs {
		writer.writeUint32(pair.Key)
		writer.writeUint32(pair.Value)
	}
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
