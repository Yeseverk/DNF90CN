// 本文件解析时装和称号类 C2S 请求体。
// 字段顺序来自当前已整理的 C# 旧实现与 Go 侧现有协议号对齐结果，成功回包仍需 EXE/MCP 闭合。
package avatartitle

import (
	"encoding/binary"
	"fmt"
)

const listTypeAvatar byte = 1

// CompoundAvatarRequest 描述时装合成请求。
type CompoundAvatarRequest struct {
	ConsumeSlot   int16
	Slot1         int16
	Slot2         int16
	RequestItemID int32
}

// AvatarSocketRequest 描述时装开孔请求。
type AvatarSocketRequest struct {
	TargetSlot   int16
	TargetItemID int32
	MaterialSlot int16
}

// AvatarEmblemRequest 描述徽章镶嵌请求。
type AvatarEmblemRequest struct {
	TargetSlot   int16
	TargetItemID int32
	Emblems      []EmblemApply
}

// EmblemApply 描述单个徽章应用记录。
type EmblemApply struct {
	EmblemSlot   int16
	EmblemItemID int32
	SocketIndex  byte
}

// TitleBookRequest 描述称号簿放入或取出请求。
type TitleBookRequest struct {
	ItemSpaceRaw int32
	Slot         int16
	ItemID       int32
	Category     int32
	Index        int32
}

// DecodeCompoundAvatarRequest 解析时装合成请求。
func DecodeCompoundAvatarRequest(body []byte) (CompoundAvatarRequest, error) {
	if len(body) < 22 {
		return CompoundAvatarRequest{}, fmt.Errorf("body too short: got %d want >= 22", len(body))
	}
	return CompoundAvatarRequest{
		ConsumeSlot:   readI16(body, 0),
		Slot1:         readI16(body, 2),
		Slot2:         readI16(body, 8),
		RequestItemID: readI32(body, 14),
	}, nil
}

// DecodeAvatarSocketRequest 解析时装开孔请求。
func DecodeAvatarSocketRequest(body []byte) (AvatarSocketRequest, error) {
	if len(body) < 8 {
		return AvatarSocketRequest{}, fmt.Errorf("body too short: got %d want >= 8", len(body))
	}
	return AvatarSocketRequest{
		TargetSlot:   readI16(body, 0),
		TargetItemID: readI32(body, 2),
		MaterialSlot: readI16(body, 6),
	}, nil
}

// DecodeAvatarEmblemRequest 解析时装徽章请求，兼容首字节为 Avatar list type 的包体。
func DecodeAvatarEmblemRequest(body []byte) (AvatarEmblemRequest, error) {
	if len(body) >= 8 && body[0] == listTypeAvatar {
		return decodeEmblemAt(body, 1)
	}
	return decodeEmblemAt(body, 0)
}

// DecodeTitleBookRequest 解析称号簿请求。
func DecodeTitleBookRequest(body []byte) (TitleBookRequest, error) {
	if len(body) < 20 {
		return TitleBookRequest{}, fmt.Errorf("body too short: got %d want >= 20", len(body))
	}
	return TitleBookRequest{
		ItemSpaceRaw: readI32(body, 0),
		Slot:         int16(readI32(body, 4)),
		ItemID:       readI32(body, 8),
		Category:     readI32(body, 12),
		Index:        readI32(body, 16),
	}, nil
}

func decodeEmblemAt(body []byte, offset int) (AvatarEmblemRequest, error) {
	if len(body) < offset+7 {
		return AvatarEmblemRequest{}, fmt.Errorf("body too short: got %d want >= %d", len(body), offset+7)
	}
	count := int(body[offset+6])
	req := AvatarEmblemRequest{
		TargetSlot:   readI16(body, offset),
		TargetItemID: readI32(body, offset+2),
		Emblems:      make([]EmblemApply, 0, count),
	}
	entryOffset := offset + 7
	for i := 0; i < count; i++ {
		if entryOffset+7 > len(body) {
			return AvatarEmblemRequest{}, fmt.Errorf("emblem entry too short: index %d", i)
		}
		req.Emblems = append(req.Emblems, EmblemApply{
			EmblemSlot:   readI16(body, entryOffset),
			EmblemItemID: readI32(body, entryOffset+2),
			SocketIndex:  body[entryOffset+6],
		})
		entryOffset += 7
	}
	return req, nil
}

func (r CompoundAvatarRequest) String() string {
	return fmt.Sprintf("consume=%d slot1=%d slot2=%d requestItem=%d", r.ConsumeSlot, r.Slot1, r.Slot2, r.RequestItemID)
}

func (r AvatarSocketRequest) String() string {
	return fmt.Sprintf("target=(%d,%d) material=%d", r.TargetSlot, r.TargetItemID, r.MaterialSlot)
}

func (r AvatarEmblemRequest) String() string {
	return fmt.Sprintf("target=(%d,%d) emblems=%d", r.TargetSlot, r.TargetItemID, len(r.Emblems))
}

func (r TitleBookRequest) String() string {
	return fmt.Sprintf("space=%d slot=%d item=%d category=%d index=%d", r.ItemSpaceRaw, r.Slot, r.ItemID, r.Category, r.Index)
}

func readI16(body []byte, offset int) int16 {
	return int16(binary.LittleEndian.Uint16(body[offset:]))
}

func readI32(body []byte, offset int) int32 {
	return int32(binary.LittleEndian.Uint32(body[offset:]))
}
