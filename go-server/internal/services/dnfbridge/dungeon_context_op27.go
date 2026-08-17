package dnfbridge

import (
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

var (
	errDungeonContextListCount     = errors.New("dnf dungeon-context list count exceeds current EXE range")
	errDungeonContextGroupCount    = errors.New("dnf dungeon-context group count exceeds current EXE range")
	errDungeonContextOptionalCount = errors.New("dnf dungeon-context optional-table count exceeds current EXE range")
	errDungeonContextPairCount     = errors.New("dnf dungeon-context pair count exceeds current EXE range")
	errDungeonContextBoundedMode   = errors.New("dnf dungeon-context bounded mode exceeds current EXE range")
	errDungeonContextCappedValue   = errors.New("dnf dungeon-context capped value exceeds current EXE range")
)

const currentDungeonContextMsgID = uint16(dnfenum.CmdPacketUseLotteryItem)

type currentDungeonContextGroupRow struct {
	ValueA uint32
	ValueB uint32
	ValueC uint32
}

type currentDungeonContextGroup struct {
	ObjectOrActorKey uint16
	Rows             []currentDungeonContextGroupRow
}

type currentDungeonContextOptionalRow struct {
	Key    uint32
	Value0 uint16
	Value1 uint16
}

type currentDungeonContextPair struct {
	Key   uint32
	Value uint32
}

type currentDungeonContext struct {
	Value0               uint32
	Value1               uint32
	List0                []uint16
	List1                []uint16
	Groups               []currentDungeonContextGroup
	ContextValue0        uint32
	ContextValue1        uint16
	ContextValue2        uint16
	ContextValue3        uint32
	OptionalTablePresent bool
	OptionalRows         []currentDungeonContextOptionalRow
	BooleanLikeValue     byte
	LargeContextValue0   uint32
	LargeContextValue1   uint32
	BoundedMode          byte
	CappedValue          uint16
	Pairs                []currentDungeonContextPair
}

func (packet currentDungeonContext) Build() ([]byte, error) {
	if len(packet.List0) > 8 || len(packet.List1) > 8 {
		return nil, fmt.Errorf("%w: list0=%d list1=%d", errDungeonContextListCount, len(packet.List0), len(packet.List1))
	}
	if len(packet.Groups) > int(^byte(0)) {
		return nil, fmt.Errorf("%w: count=%d", errDungeonContextGroupCount, len(packet.Groups))
	}
	for groupIndex, group := range packet.Groups {
		if len(group.Rows) > int(^byte(0)) {
			return nil, fmt.Errorf("%w: group=%d rows=%d", errDungeonContextGroupCount, groupIndex, len(group.Rows))
		}
	}
	if len(packet.OptionalRows) > int(^byte(0)) {
		return nil, fmt.Errorf("%w: count=%d", errDungeonContextOptionalCount, len(packet.OptionalRows))
	}
	if !packet.OptionalTablePresent && len(packet.OptionalRows) != 0 {
		return nil, fmt.Errorf("%w: rows=%d without table-present flag", errDungeonContextOptionalCount, len(packet.OptionalRows))
	}
	if len(packet.Pairs) > int(^byte(0)) {
		return nil, fmt.Errorf("%w: count=%d", errDungeonContextPairCount, len(packet.Pairs))
	}
	if packet.BoundedMode >= 8 {
		return nil, fmt.Errorf("%w: value=%d", errDungeonContextBoundedMode, packet.BoundedMode)
	}
	if packet.CappedValue > 100 {
		return nil, fmt.Errorf("%w: value=%d", errDungeonContextCappedValue, packet.CappedValue)
	}

	var writer packetWriter
	writer.writeUint32(packet.Value0)
	writer.writeUint32(packet.Value1)
	writer.writeByte(byte(len(packet.List0)))
	for _, value := range packet.List0 {
		writer.writeUint16(value)
	}
	writer.writeByte(byte(len(packet.List1)))
	for _, value := range packet.List1 {
		writer.writeUint16(value)
	}
	writer.writeByte(byte(len(packet.Groups)))
	for _, group := range packet.Groups {
		writer.writeUint16(group.ObjectOrActorKey)
		writer.writeByte(byte(len(group.Rows)))
		for _, row := range group.Rows {
			writer.writeUint32(row.ValueA)
			writer.writeUint32(row.ValueB)
			writer.writeUint32(row.ValueC)
		}
	}
	writer.writeUint32(packet.ContextValue0)
	writer.writeUint16(packet.ContextValue1)
	writer.writeUint16(packet.ContextValue2)
	writer.writeUint32(packet.ContextValue3)
	if packet.OptionalTablePresent {
		writer.writeByte(1)
		writer.writeByte(byte(len(packet.OptionalRows)))
		for _, row := range packet.OptionalRows {
			writer.writeUint32(row.Key)
			writer.writeUint16(row.Value0)
			writer.writeUint16(row.Value1)
		}
	} else {
		writer.writeByte(0)
	}
	writer.writeByte(packet.BooleanLikeValue)
	writer.writeUint32(packet.LargeContextValue0)
	writer.writeUint32(packet.LargeContextValue1)
	writer.writeByte(packet.BoundedMode)
	writer.writeUint16(packet.CappedValue)
	writer.writeByte(byte(len(packet.Pairs)))
	for _, pair := range packet.Pairs {
		writer.writeUint32(pair.Key)
		writer.writeUint32(pair.Value)
	}
	return writer.bytes(), nil
}

func (s *Service) sendCurrentDungeonContextOp27(session *gameSession, source string) error {
	packet := currentDungeonContext{}
	body, err := packet.Build()
	if err != nil {
		return err
	}
	s.logGameEvent(session, "game-upper-current-dungeon-context-op27-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", currentDungeonContextMsgID,
		"classification", 0,
		"body_len", len(body),
		"list0_count", 0,
		"list1_count", 0,
		"group_count", 0,
		"optional_table_present", false,
		"pair_count", 0,
		"body_source", "current_exe_sub_1D868B0_typed_minimum",
		"context_source", "no_confirmed_current_context_owner")
	return s.sendGameUpperRawClass(session, currentDungeonContextMsgID, body, 0)
}
