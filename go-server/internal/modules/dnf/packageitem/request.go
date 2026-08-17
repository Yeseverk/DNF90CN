// 本文件解析礼包和随机盒子 C2S 请求体。
// 可选礼包按 C# SelectablePackageOpenRequest，随机盒子单开按 MagicBoxOpenRequest.TryParseSingle。
package packageitem

import (
	"encoding/binary"
	"fmt"
)

const (
	listTypeMain       byte = 0
	clientMainListType byte = 4
)

// SelectablePackageRequest 描述可选礼包开启请求。
type SelectablePackageRequest struct {
	SlotIndex              int16
	SelectionContext       int16
	SelectedItemTemplateID int32
	SelectionFlag          byte
	AvatarChoiceCount      int
}

// MagicBoxSingleRequest 描述随机盒子单开请求。
type MagicBoxSingleRequest struct {
	RawListType       byte
	ListType          byte
	SlotIndex         int16
	MaterialSlotIndex int16
}

// MagicBoxExpandRequest 描述 0x0468 随机盒子连开（全部开启）请求。
// 布局来自当前 NoPack 客户端实测抓包（2026-07-25，开 100 个泰迪礼盒）：
// u8 listType, u16 boxSlot, u32 boxItemID, u16 materialSlot,
// u32 materialItemID, u16 openCount，共 15 字节。
type MagicBoxExpandRequest struct {
	RawListType       byte
	ListType          byte
	SlotIndex         int16
	BoxItemID         uint32
	MaterialSlotIndex int16
	MaterialItemID    uint32
	OpenCount         uint16
}

// DecodeSelectablePackageRequest 解析 0x00A0 可选礼包请求。
func DecodeSelectablePackageRequest(body []byte) (SelectablePackageRequest, error) {
	if len(body) < 9 {
		return SelectablePackageRequest{}, fmt.Errorf("body too short: got %d want >= 9", len(body))
	}
	req := SelectablePackageRequest{
		SlotIndex:              int16(binary.LittleEndian.Uint16(body)),
		SelectionContext:       int16(binary.LittleEndian.Uint16(body[2:])),
		SelectedItemTemplateID: int32(binary.LittleEndian.Uint32(body[4:])),
		SelectionFlag:          body[8],
	}
	if req.SelectedItemTemplateID <= 0 {
		return SelectablePackageRequest{}, fmt.Errorf("selected item %d invalid", req.SelectedItemTemplateID)
	}
	req.AvatarChoiceCount = countAvatarChoices(body)
	return req, nil
}

// DecodeMagicBoxSingleRequest 解析 0x00D0 随机盒子单开请求。
func DecodeMagicBoxSingleRequest(body []byte) (MagicBoxSingleRequest, error) {
	if len(body) < 3 {
		return MagicBoxSingleRequest{}, fmt.Errorf("body too short: got %d want >= 3", len(body))
	}
	listType, ok := mapMagicBoxListType(body[0])
	if !ok {
		return MagicBoxSingleRequest{}, fmt.Errorf("list type %d invalid", body[0])
	}
	req := MagicBoxSingleRequest{
		RawListType:       body[0],
		ListType:          listType,
		SlotIndex:         int16(binary.LittleEndian.Uint16(body[1:])),
		MaterialSlotIndex: -1,
	}
	if req.SlotIndex < 0 {
		return MagicBoxSingleRequest{}, fmt.Errorf("slot %d invalid", req.SlotIndex)
	}
	if len(body) >= 5 {
		req.MaterialSlotIndex = int16(binary.LittleEndian.Uint16(body[3:]))
		if req.MaterialSlotIndex < 0 {
			req.MaterialSlotIndex = -1
		}
	}
	return req, nil
}

// DecodeMagicBoxExpandRequest 解析 0x0468 随机盒子连开请求。
func DecodeMagicBoxExpandRequest(body []byte) (MagicBoxExpandRequest, error) {
	if len(body) < 15 {
		return MagicBoxExpandRequest{}, fmt.Errorf("body too short: got %d want >= 15", len(body))
	}
	listType, ok := mapMagicBoxListType(body[0])
	if !ok {
		return MagicBoxExpandRequest{}, fmt.Errorf("list type %d invalid", body[0])
	}
	req := MagicBoxExpandRequest{
		RawListType:       body[0],
		ListType:          listType,
		SlotIndex:         int16(binary.LittleEndian.Uint16(body[1:])),
		BoxItemID:         binary.LittleEndian.Uint32(body[3:]),
		MaterialSlotIndex: int16(binary.LittleEndian.Uint16(body[7:])),
		MaterialItemID:    binary.LittleEndian.Uint32(body[9:]),
		OpenCount:         binary.LittleEndian.Uint16(body[13:]),
	}
	if req.SlotIndex < 0 {
		return MagicBoxExpandRequest{}, fmt.Errorf("slot %d invalid", req.SlotIndex)
	}
	if req.BoxItemID == 0 {
		return MagicBoxExpandRequest{}, fmt.Errorf("box item %d invalid", req.BoxItemID)
	}
	if req.MaterialSlotIndex < 0 {
		req.MaterialSlotIndex = -1
	}
	if req.OpenCount == 0 {
		return MagicBoxExpandRequest{}, fmt.Errorf("open count %d invalid", req.OpenCount)
	}
	return req, nil
}

func countAvatarChoices(body []byte) int {
	for count := 1; count <= 32; count++ {
		countOffset := 4 + count*4
		expectedLength := countOffset + 1 + count*5
		if expectedLength == len(body) && body[countOffset] == byte(count) {
			return count
		}
	}
	return 0
}

func mapMagicBoxListType(raw byte) (byte, bool) {
	if raw == listTypeMain || raw == clientMainListType {
		return listTypeMain, true
	}
	return raw, false
}

func (r SelectablePackageRequest) String() string {
	return fmt.Sprintf("slot=%d context=%d selected=%d flag=%d avatarChoices=%d", r.SlotIndex, r.SelectionContext, r.SelectedItemTemplateID, r.SelectionFlag, r.AvatarChoiceCount)
}

func (r MagicBoxSingleRequest) String() string {
	return fmt.Sprintf("list=%d slot=%d materialSlot=%d", r.ListType, r.SlotIndex, r.MaterialSlotIndex)
}

func (r MagicBoxExpandRequest) String() string {
	return fmt.Sprintf("list=%d slot=%d box=%d materialSlot=%d material=%d count=%d", r.ListType, r.SlotIndex, r.BoxItemID, r.MaterialSlotIndex, r.MaterialItemID, r.OpenCount)
}
