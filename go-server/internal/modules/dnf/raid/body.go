// 本文件按 NoPack 0x24F/sub_1D71CA0 的读取顺序构造攻坚队成员刷新体。
package raid

import (
	"errors"
	"unicode/utf16"
)

const (
	// RaidMemberRefreshMsgID 是当前 EXE 的 raid 成员刷新 raw upper id，不套用旧 CmdPacket 591 名字。
	RaidMemberRefreshMsgID uint16 = 0x024f
	RaidMemberRefreshMode3 uint32 = 3
	MaxAttackPartyMembers         = 20

	raidMemberNameMaxBytes = 256
)

// MemberRecord 是 sub_17B6EF0 读取的一条攻坚队成员记录。
type MemberRecord struct {
	CharKey           uint16
	Field4            byte
	Name              string
	RawNameUTF16LE    []byte
	Field40           byte
	Field44           byte
	GroupIndex        byte
	SlotOrder         byte
	Field48           uint32
	Field52           byte
	Field53           byte
	Field56           byte
	Field60           uint32
	Field64           uint16
	Field66BoolSource uint32
}

// MemberRefresh 保存 0x24F mode=3 的成员名单刷新数据。
type MemberRefresh struct {
	RaidKey uint32
	Members []MemberRecord
}

var errRaidMemberTooMany = errors.New("raid member count exceeds 20")
var errRaidMemberNameInvalid = errors.New("raid member name utf16 bytes invalid")
var errRaidMemberNameTooLong = errors.New("raid member name utf16 bytes too long")

// BuildCreateRaidResultBody constructs class1/op664 CREATE_RAID success.
// Current EXE sub_1D04340 consumes the common success byte followed by raid_key:u32.
func BuildCreateRaidResultBody(raidKey uint32) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint32(raidKey)
	return writer.bytes()
}

// BuildLeaveRaidResultBody constructs class1/op665 LEAVE_RAID success.
// Current EXE sub_1D04A00 consumes the common success byte followed by a one-byte
// flag; the client takes its raid-ended notification branch when that flag is set.
func BuildLeaveRaidResultBody(raidEnded bool) []byte {
	var writer packetWriter
	writer.writeByte(1)
	if raidEnded {
		writer.writeByte(1)
	} else {
		writer.writeByte(0)
	}
	return writer.bytes()
}

// BuildMemberRefreshMode3Body 构造 0x24F mode=3：u32 raid_key, u32 mode, u8 count, repeat member。
func BuildMemberRefreshMode3Body(refresh MemberRefresh) ([]byte, error) {
	if len(refresh.Members) > MaxAttackPartyMembers {
		return nil, errRaidMemberTooMany
	}
	var writer packetWriter
	writer.writeUint32(refresh.RaidKey)
	writer.writeUint32(RaidMemberRefreshMode3)
	writer.writeByte(byte(len(refresh.Members)))
	for _, member := range refresh.Members {
		if err := writeMemberRecord(&writer, member); err != nil {
			return nil, err
		}
	}
	return writer.bytes(), nil
}

func writeMemberRecord(writer *packetWriter, member MemberRecord) error {
	nameBytes, err := raidWstrBytes(member)
	if err != nil {
		return err
	}
	writer.writeUint16(member.CharKey)
	writer.writeByte(member.Field4)
	writer.writeRawDstr(nameBytes)
	writer.writeByte(member.Field40)
	writer.writeByte(member.Field44)
	writer.writeByte(member.GroupIndex)
	writer.writeByte(member.SlotOrder)
	writer.writeUint32(member.Field48)
	writer.writeByte(member.Field52)
	writer.writeByte(member.Field53)
	writer.writeByte(member.Field56)
	writer.writeUint32(member.Field60)
	writer.writeUint16(member.Field64)
	writer.writeUint32(member.Field66BoolSource)
	return nil
}

func raidWstrBytes(member MemberRecord) ([]byte, error) {
	if len(member.RawNameUTF16LE) > 0 {
		return validateRaidWstrBytes(member.RawNameUTF16LE)
	}
	units := utf16.Encode([]rune(member.Name))
	if len(units) == 0 {
		return []byte{0, 0}, nil
	}
	out := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return validateRaidWstrBytes(out)
}

func validateRaidWstrBytes(data []byte) ([]byte, error) {
	if len(data)%2 != 0 {
		return nil, errRaidMemberNameInvalid
	}
	if len(data) == 0 {
		data = []byte{0, 0}
	}
	if len(data) >= raidMemberNameMaxBytes {
		return nil, errRaidMemberNameTooLong
	}
	return append([]byte(nil), data...), nil
}
