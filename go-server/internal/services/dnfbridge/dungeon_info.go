package dnfbridge

import (
	"errors"
	"fmt"
)

var (
	errDungeonInfoPairGroupCount = errors.New("dnf dungeon-info pair group count exceeds current EXE range")
	errDungeonInfoOpaqueCount    = errors.New("dnf dungeon-info opaque entry count exceeds current EXE range")
)

type currentDungeonInfoPair struct {
	First  byte
	Second byte
}

// currentDungeonInfoOpaqueEntry preserves the current EXE's trailing
// u32/u8/u8 records without assigning semantics that PVF/EXE evidence has not
// established yet.
type currentDungeonInfoOpaqueEntry struct {
	Value  uint32
	ParamA byte
	ParamB byte
}

type currentDungeonInfo struct {
	DungeonID      uint32
	Difficulty     byte
	EntryOption    uint16
	MazeIndex      byte
	BossX          byte
	BossY          byte
	HellPartyRoomX byte
	HellPartyRoomY byte
	DungeonMode    byte
	PairGroups     [][]currentDungeonInfoPair
	HellPartyValue uint16
	DungeonValue   uint16
	Value2         byte
	FlagA          byte
	PacketSeed     uint32
	ParamA         byte
	ParamB         byte
	ParamC         byte
	TailFlag0      byte
	TailFlag1      byte
	TailFlag2      byte
	OpaqueEntries  []currentDungeonInfoOpaqueEntry
	TailMode       byte
	TailValue0     uint16
	TailValue1     uint16
}

func (packet currentDungeonInfo) Build() ([]byte, error) {
	if len(packet.PairGroups) > 0xff {
		return nil, fmt.Errorf("%w: count=%d", errDungeonInfoPairGroupCount, len(packet.PairGroups))
	}
	for groupIndex, group := range packet.PairGroups {
		if len(group) > 0xff {
			return nil, fmt.Errorf("%w: group=%d count=%d", errDungeonInfoPairGroupCount, groupIndex, len(group))
		}
	}
	if len(packet.OpaqueEntries) > 0xff {
		return nil, fmt.Errorf("%w: count=%d", errDungeonInfoOpaqueCount, len(packet.OpaqueEntries))
	}

	var writer packetWriter
	writer.writeUint32(packet.DungeonID)
	writer.writeByte(packet.Difficulty)
	writer.writeUint16(packet.EntryOption)
	writer.writeByte(packet.MazeIndex)
	writer.writeByte(packet.BossX)
	writer.writeByte(packet.BossY)
	writer.writeByte(packet.HellPartyRoomX)
	writer.writeByte(packet.HellPartyRoomY)
	writer.writeByte(packet.DungeonMode)
	writer.writeByte(byte(len(packet.PairGroups)))
	for _, group := range packet.PairGroups {
		writer.writeByte(byte(len(group)))
		for _, pair := range group {
			writer.writeByte(pair.First)
			writer.writeByte(pair.Second)
		}
	}
	writer.writeUint16(packet.HellPartyValue)
	writer.writeUint16(packet.DungeonValue)
	writer.writeByte(packet.Value2)
	writer.writeByte(packet.FlagA)
	writer.writeUint32(packet.PacketSeed)
	writer.writeByte(packet.ParamA)
	writer.writeByte(packet.ParamB)
	writer.writeByte(packet.ParamC)
	writer.writeByte(packet.TailFlag0)
	writer.writeByte(packet.TailFlag1)
	writer.writeByte(packet.TailFlag2)
	writer.writeByte(byte(len(packet.OpaqueEntries)))
	for _, entry := range packet.OpaqueEntries {
		writer.writeUint32(entry.Value)
		writer.writeByte(entry.ParamA)
		writer.writeByte(entry.ParamB)
	}
	writer.writeByte(packet.TailMode)
	writer.writeUint16(packet.TailValue0)
	writer.writeUint16(packet.TailValue1)
	return writer.bytes(), nil
}
