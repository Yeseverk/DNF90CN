// 本文件集中解析背包类 C2S 请求体。
// 字节顺序按最新 EXE/C# 对齐证据整理；解析器只产生请求计划，不直接修改资产或生成成功响应。
package inventory

import (
	"encoding/binary"
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	listTypeMain          byte = 0
	listTypeAvatar        byte = 1
	listTypePersonalCargo byte = 2
	listTypeEquipment     byte = 3
	listTypePet           byte = 7
	listTypeActorWornAlt  byte = 17
	listTypeAccountCargo  byte = 12
	listTypeGuildMedal    byte = 38
	maxDeleteEntries           = 100
)

// DeleteRequest 描述删除物品请求；Extended=true 时 body 带批量删除 entry。
type DeleteRequest struct {
	Extended  bool
	ListType  byte
	SlotIndex int16
	Count     int16
	Entries   []DeleteEntry
}

// DeleteEntry 是扩展删除格式里的 12 字节 entry。
type DeleteEntry struct {
	OpType      int16
	SlotIndex   int16
	ItemID      int32
	DeleteCount int32
}

// DeleteOrSellRequest 描述删除/出售共用的短格式请求。
type DeleteOrSellRequest struct {
	HasListType bool
	ListType    byte
	SlotIndex   int16
	Count       int16
}

// MoveItemspaceRequest 描述物品移动/拆分请求。
type MoveItemspaceRequest struct {
	SourceListType           byte
	SourceSlotIndex          int16
	SourceInstanceValue      int32
	MoveCount                int32
	DestinationListType      byte
	DestinationSlotIndex     int16
	DestinationInstanceValue int32
	DestinationStack         int32
	ActorIndex               int32
	TrailingState0           byte
	TrailingState1           byte
}

// SortItemRequest 描述整理背包请求。
type SortItemRequest struct {
	ListType  byte
	Category  byte
	Condition byte
}

// BuyItemRequest 描述 NPC 购买请求。
type BuyItemRequest struct {
	ItemTemplateID int32
	Count          int32
}

// RepairEquipmentRequest 描述装备维修请求。
type RepairEquipmentRequest struct {
	InvenType      byte
	SlotIndex      int16
	RepairItemSlot int16
	AutoRepair     bool
	QuickRepair    bool
}

// DisjointItemRequest 描述物品分解请求。
type DisjointItemRequest struct {
	TargetSlotIndex       int16
	ItemSpace             byte
	DisjointItemSlotIndex int16
	ContextValue          int32
}

// UseStackableRequest 描述使用消耗品请求。
type UseStackableRequest struct {
	SlotIndex     int16
	ListType      byte
	InstanceValue int32
	ItemCode      int32
	Reserved      uint32
}

type UseStackableActionRequest struct {
	SourceSlotIndex int16
	ListType        byte
	Reserved0       uint32
	ActionIndex     uint32
	Reserved1       uint32
	Reserved2       uint32
}

type SelectDamageFontRequest struct {
	FontIndex uint16
}

// UpgradeItemRequest 描述强化/增幅请求。
type UpgradeItemRequest struct {
	Mode                    string
	RawMode                 uint16
	TargetSlotIndex         int16
	TargetItemTemplateID    int32
	MaterialSlotIndex       int16
	OptionalTicketSlotIndex int16
	TargetItemName          string
}

// EnchantByBeadRequest 描述宝珠附魔请求。
type EnchantByBeadRequest struct {
	BeadListType    byte
	BeadSlotIndex   int16
	TargetListType  byte
	TargetSlotIndex int16
}

// DecodeDeleteRequest 解析删除物品请求，兼容 C# 里 simple 与 extended 两种格式。
func DecodeDeleteRequest(body []byte) (DeleteRequest, error) {
	// Skill-material consumption appends a one-byte state marker (0x01)
	// after the declared delete protobuf. Keep this exception local to op18.
	if payload, ok := unwrapCurrentDeleteStateTrailer(body); ok {
		return decodeCurrentDeleteRequest(payload)
	}
	if payload, current, err := unwrapCurrentProtobufBody(body); current {
		if err != nil {
			return DeleteRequest{}, err
		}
		return decodeCurrentDeleteRequest(payload)
	}
	if len(body) < 4 {
		return DeleteRequest{}, shortBodyError(len(body), 4)
	}
	if len(body) >= 15 && body[1] >= 1 && body[1] <= maxDeleteEntries {
		count := int(body[1])
		want := 2 + count*12
		if len(body) < want {
			return DeleteRequest{}, shortBodyError(len(body), want)
		}
		entries := make([]DeleteEntry, 0, count)
		for i := 0; i < count; i++ {
			offset := 2 + i*12
			entries = append(entries, DeleteEntry{
				OpType:      readI16(body, offset),
				SlotIndex:   readI16(body, offset+2),
				ItemID:      readI32(body, offset+4),
				DeleteCount: readI32(body, offset+8),
			})
		}
		return DeleteRequest{Extended: true, ListType: body[0], Entries: entries}, nil
	}

	simple, err := DecodeDeleteOrSellRequest(body)
	if err != nil {
		return DeleteRequest{}, err
	}
	return DeleteRequest{
		ListType:  simple.ListType,
		SlotIndex: simple.SlotIndex,
		Count:     simple.Count,
	}, nil
}

// DecodeDeleteOrSellRequest 解析删除/出售短格式；首字节是已知 list type 时使用带 list 格式。
func unwrapCurrentDeleteStateTrailer(body []byte) ([]byte, bool) {
	if len(body) < 5 {
		return nil, false
	}
	declared := uint64(binary.LittleEndian.Uint32(body[:4]))
	available := uint64(len(body) - 4)
	if declared+1 != available || body[len(body)-1] != 1 || declared > uint64(len(body)-5) {
		return nil, false
	}
	return body[4 : 4+int(declared)], true
}

func DecodeDeleteOrSellRequest(body []byte) (DeleteOrSellRequest, error) {
	if len(body) < 4 {
		return DeleteOrSellRequest{}, shortBodyError(len(body), 4)
	}
	if len(body) >= 5 && isKnownListType(body[0]) {
		return DeleteOrSellRequest{
			HasListType: true,
			ListType:    body[0],
			SlotIndex:   readI16(body, 1),
			Count:       readI16(body, 3),
		}, nil
	}
	return DeleteOrSellRequest{
		ListType:  listTypeMain,
		SlotIndex: readI16(body, 0),
		Count:     readI16(body, 2),
	}, nil
}

// DecodeMoveItemspaceRequest decodes the 28-byte layout emitted by the current
// NoPack.exe senders sub_2326C90, sub_232EA00, and sub_1B33B40.
func DecodeMoveItemspaceRequest(body []byte) (MoveItemspaceRequest, error) {
	const currentMoveItemspaceBodySize = 28
	if len(body) != currentMoveItemspaceBodySize {
		return MoveItemspaceRequest{}, fmt.Errorf("move itemspace body length %d, want %d", len(body), currentMoveItemspaceBodySize)
	}
	return MoveItemspaceRequest{
		SourceListType:           body[0],
		SourceSlotIndex:          readI16(body, 1),
		SourceInstanceValue:      readI32(body, 3),
		MoveCount:                readI32(body, 7),
		DestinationListType:      body[11],
		DestinationSlotIndex:     readI16(body, 12),
		DestinationInstanceValue: readI32(body, 14),
		DestinationStack:         readI32(body, 18),
		ActorIndex:               readI32(body, 22),
		TrailingState0:           body[26],
		TrailingState1:           body[27],
	}, nil
}

// DecodeSortItemRequest 解析整理背包请求。
func DecodeSortItemRequest(body []byte) (SortItemRequest, error) {
	if payload, current, err := unwrapCurrentProtobufBody(body); current {
		if err != nil {
			return SortItemRequest{}, err
		}
		return decodeCurrentSortItemRequest(payload)
	}
	if len(body) < 2 {
		return SortItemRequest{}, shortBodyError(len(body), 2)
	}
	req := SortItemRequest{ListType: body[0], Category: body[1]}
	if len(body) > 2 {
		req.Condition = body[2]
	}
	return req, nil
}

// unwrapCurrentProtobufBody recognizes the current u32-length-prefixed body.
// One zero byte after the protobuf is accepted because current delete senders
// append that terminator. Legacy S4A12 bodies are left to the old decoders.
func unwrapCurrentProtobufBody(body []byte) ([]byte, bool, error) {
	if len(body) < 4 {
		return nil, false, nil
	}
	declared := uint64(binary.LittleEndian.Uint32(body[:4]))
	available := uint64(len(body) - 4)
	if declared == available {
		return body[4:], true, nil
	}
	if declared+1 == available {
		if body[len(body)-1] != 0 {
			return nil, true, fmt.Errorf("protobuf body has non-zero terminator 0x%02X", body[len(body)-1])
		}
		return body[4 : len(body)-1], true, nil
	}
	// Current inventory protobufs are small and therefore have a zero upper
	// length prefix. Treat a mismatch as truncation instead of unsafe fallback.
	if body[1] == 0 && body[2] == 0 && body[3] == 0 {
		return nil, true, fmt.Errorf("protobuf body length %d, want %d or %d with zero terminator", available, declared, declared+1)
	}
	return nil, false, nil
}

func decodeCurrentSortItemRequest(payload []byte) (SortItemRequest, error) {
	var req SortItemRequest
	var seen uint8
	for len(payload) > 0 {
		number, wireType, n := protowire.ConsumeTag(payload)
		if n < 0 {
			return SortItemRequest{}, fmt.Errorf("sort protobuf tag: %w", protowire.ParseError(n))
		}
		payload = payload[n:]
		if wireType != protowire.VarintType || number < 2 || number > 4 {
			return SortItemRequest{}, fmt.Errorf("sort protobuf field %d has unsupported wire type %d", number, wireType)
		}
		value, n := protowire.ConsumeVarint(payload)
		if n < 0 {
			return SortItemRequest{}, fmt.Errorf("sort protobuf field %d: %w", number, protowire.ParseError(n))
		}
		payload = payload[n:]
		if value > math.MaxUint8 {
			return SortItemRequest{}, fmt.Errorf("sort protobuf field %d value %d overflows byte", number, value)
		}
		bit := uint8(1 << (number - 2))
		if seen&bit != 0 {
			return SortItemRequest{}, fmt.Errorf("sort protobuf field %d is duplicated", number)
		}
		seen |= bit
		switch number {
		case 2:
			req.ListType = byte(value)
		case 3:
			req.Category = byte(value)
		case 4:
			req.Condition = byte(value)
		}
	}
	if seen != 0x07 {
		return SortItemRequest{}, fmt.Errorf("sort protobuf required fields missing: mask=0x%02X", seen)
	}
	return req, nil
}

func decodeCurrentDeleteRequest(payload []byte) (DeleteRequest, error) {
	var req DeleteRequest
	req.Extended = true
	var seenListType, seenCondition bool
	for len(payload) > 0 {
		number, wireType, n := protowire.ConsumeTag(payload)
		if n < 0 {
			return DeleteRequest{}, fmt.Errorf("delete protobuf tag: %w", protowire.ParseError(n))
		}
		payload = payload[n:]
		switch number {
		case 2, 4:
			if wireType != protowire.VarintType {
				return DeleteRequest{}, fmt.Errorf("delete protobuf field %d has wire type %d, want varint", number, wireType)
			}
			value, consumed := protowire.ConsumeVarint(payload)
			if consumed < 0 {
				return DeleteRequest{}, fmt.Errorf("delete protobuf field %d: %w", number, protowire.ParseError(consumed))
			}
			payload = payload[consumed:]
			if number == 2 {
				if seenListType || value > math.MaxUint8 {
					return DeleteRequest{}, fmt.Errorf("delete protobuf list type is duplicated or invalid: %d", value)
				}
				req.ListType = byte(value)
				seenListType = true
			} else {
				if seenCondition || value != 0 {
					return DeleteRequest{}, fmt.Errorf("delete protobuf condition is duplicated or non-zero: %d", value)
				}
				seenCondition = true
			}
		case 3:
			if wireType != protowire.BytesType {
				return DeleteRequest{}, fmt.Errorf("delete protobuf field 3 has wire type %d, want bytes", wireType)
			}
			if len(req.Entries) >= maxDeleteEntries {
				return DeleteRequest{}, fmt.Errorf("delete protobuf has more than %d entries", maxDeleteEntries)
			}
			nested, consumed := protowire.ConsumeBytes(payload)
			if consumed < 0 {
				return DeleteRequest{}, fmt.Errorf("delete protobuf entry: %w", protowire.ParseError(consumed))
			}
			payload = payload[consumed:]
			entry, err := decodeCurrentDeleteEntry(nested)
			if err != nil {
				return DeleteRequest{}, err
			}
			req.Entries = append(req.Entries, entry)
		default:
			return DeleteRequest{}, fmt.Errorf("delete protobuf field %d is unsupported", number)
		}
	}
	if !seenListType || !seenCondition || len(req.Entries) == 0 {
		return DeleteRequest{}, fmt.Errorf("delete protobuf required fields missing: list=%t entries=%d condition=%t", seenListType, len(req.Entries), seenCondition)
	}
	return req, nil
}

func decodeCurrentDeleteEntry(payload []byte) (DeleteEntry, error) {
	var entry DeleteEntry
	var seen uint8
	for len(payload) > 0 {
		number, wireType, n := protowire.ConsumeTag(payload)
		if n < 0 {
			return DeleteEntry{}, fmt.Errorf("delete entry protobuf tag: %w", protowire.ParseError(n))
		}
		payload = payload[n:]
		if wireType != protowire.VarintType || number < 1 || number > 4 {
			return DeleteEntry{}, fmt.Errorf("delete entry protobuf field %d has unsupported wire type %d", number, wireType)
		}
		value, consumed := protowire.ConsumeVarint(payload)
		if consumed < 0 {
			return DeleteEntry{}, fmt.Errorf("delete entry protobuf field %d: %w", number, protowire.ParseError(consumed))
		}
		payload = payload[consumed:]
		bit := uint8(1 << (number - 1))
		if seen&bit != 0 {
			return DeleteEntry{}, fmt.Errorf("delete entry protobuf field %d is duplicated", number)
		}
		seen |= bit
		switch number {
		case 1:
			if value > math.MaxInt16 {
				return DeleteEntry{}, fmt.Errorf("delete entry op type %d overflows int16", value)
			}
			entry.OpType = int16(value)
		case 2:
			if value > math.MaxInt16 {
				return DeleteEntry{}, fmt.Errorf("delete entry slot %d overflows int16", value)
			}
			entry.SlotIndex = int16(value)
		case 3:
			if value == 0 || value > math.MaxInt32 {
				return DeleteEntry{}, fmt.Errorf("delete entry item id %d is invalid", value)
			}
			entry.ItemID = int32(value)
		case 4:
			if value == 0 || value > math.MaxInt32 {
				return DeleteEntry{}, fmt.Errorf("delete entry count %d is invalid", value)
			}
			entry.DeleteCount = int32(value)
		}
	}
	if seen != 0x0F {
		return DeleteEntry{}, fmt.Errorf("delete entry protobuf required fields missing: mask=0x%02X", seen)
	}
	return entry, nil
}

// DecodeBuyItemRequest 解析 NPC 购买请求。
func DecodeBuyItemRequest(body []byte) (BuyItemRequest, error) {
	if len(body) < 4 {
		return BuyItemRequest{}, shortBodyError(len(body), 4)
	}
	req := BuyItemRequest{ItemTemplateID: readI32(body, 0), Count: 1}
	if len(body) >= 8 {
		req.Count = readI32(body, 4)
		if req.Count <= 0 {
			req.Count = 1
		}
	}
	return req, nil
}

// DecodeRepairEquipmentRequest 解析装备维修请求。
func DecodeRepairEquipmentRequest(body []byte) (RepairEquipmentRequest, error) {
	if len(body) < 5 {
		return RepairEquipmentRequest{}, shortBodyError(len(body), 5)
	}
	return RepairEquipmentRequest{
		InvenType:      body[0],
		SlotIndex:      readI16(body, 1),
		RepairItemSlot: readI16(body, 3),
		AutoRepair:     len(body) >= 6 && body[5] == 1,
		QuickRepair:    len(body) >= 8 && body[7] == 1,
	}, nil
}

// DecodeDisjointItemRequest 解析物品分解请求。
func DecodeDisjointItemRequest(body []byte) (DisjointItemRequest, error) {
	if len(body) < 5 {
		return DisjointItemRequest{}, shortBodyError(len(body), 5)
	}
	req := DisjointItemRequest{
		TargetSlotIndex:       readI16(body, 0),
		ItemSpace:             body[2],
		DisjointItemSlotIndex: readI16(body, 3),
	}
	if len(body) >= 9 {
		req.ContextValue = readI32(body, 5)
	}
	if req.TargetSlotIndex < 0 {
		return DisjointItemRequest{}, fmt.Errorf("target slot %d invalid", req.TargetSlotIndex)
	}
	return req, nil
}

// DecodeUseStackableRequest 解析使用消耗品请求。
func DecodeUseStackableRequest(body []byte) (UseStackableRequest, error) {
	const currentUseStackableBodySize = 15
	if len(body) != currentUseStackableBodySize {
		return UseStackableRequest{}, fmt.Errorf("use stackable body length %d, want %d", len(body), currentUseStackableBodySize)
	}
	req := UseStackableRequest{
		SlotIndex:     readI16(body, 0),
		ListType:      body[2],
		InstanceValue: readI32(body, 3),
		ItemCode:      readI32(body, 7),
		Reserved:      readU32(body, 11),
	}
	if req.SlotIndex < 0 {
		return UseStackableRequest{}, fmt.Errorf("use stackable slot %d invalid", req.SlotIndex)
	}
	if req.InstanceValue == 0 || req.ItemCode <= 0 {
		return UseStackableRequest{}, fmt.Errorf("use stackable identity invalid: instance=0x%08X item=%d", uint32(req.InstanceValue), req.ItemCode)
	}
	if req.Reserved != 0 {
		return UseStackableRequest{}, fmt.Errorf("use stackable reserved value 0x%08X, want 0", req.Reserved)
	}
	return req, nil
}

func DecodeUseStackableActionRequest(body []byte) (UseStackableActionRequest, error) {
	const bodySize = 19
	if len(body) != bodySize {
		return UseStackableActionRequest{}, fmt.Errorf("use stackable action body length %d, want %d", len(body), bodySize)
	}
	req := UseStackableActionRequest{
		SourceSlotIndex: readI16(body, 0),
		ListType:        body[2],
		Reserved0:       readU32(body, 3),
		ActionIndex:     readU32(body, 7),
		Reserved1:       readU32(body, 11),
		Reserved2:       readU32(body, 15),
	}
	if req.SourceSlotIndex < 0 {
		return UseStackableActionRequest{}, fmt.Errorf("use stackable action slot %d invalid", req.SourceSlotIndex)
	}
	if req.ListType != listTypeMain {
		return UseStackableActionRequest{}, fmt.Errorf("use stackable action list %d, want %d", req.ListType, listTypeMain)
	}
	if req.ActionIndex != damageFontActionIndex {
		return UseStackableActionRequest{}, fmt.Errorf("use stackable action index %d unsupported", req.ActionIndex)
	}
	if req.Reserved0 != 0 || req.Reserved1 != 0 || req.Reserved2 != 0 {
		return UseStackableActionRequest{}, fmt.Errorf("use stackable action reserved values (%d,%d,%d), want all zero", req.Reserved0, req.Reserved1, req.Reserved2)
	}
	return req, nil
}

func DecodeSelectDamageFontRequest(body []byte) (SelectDamageFontRequest, error) {
	if len(body) != 2 {
		return SelectDamageFontRequest{}, fmt.Errorf("select damage font body length %d, want 2", len(body))
	}
	return SelectDamageFontRequest{FontIndex: readU16(body, 0)}, nil
}

// DecodeUpgradeItemRequest 解析强化/增幅请求。
func DecodeUpgradeItemRequest(body []byte) (UpgradeItemRequest, error) {
	if len(body) < 16 {
		return UpgradeItemRequest{}, shortBodyError(len(body), 16)
	}
	rawMode := readU16(body, 0)
	if rawMode > 1 {
		return UpgradeItemRequest{}, fmt.Errorf("raw mode %d invalid", rawMode)
	}
	nameLength := readI32(body, 12)
	if nameLength < 0 {
		return UpgradeItemRequest{}, fmt.Errorf("name length %d invalid", nameLength)
	}
	end := 16 + int(nameLength)
	if len(body) < end {
		return UpgradeItemRequest{}, shortBodyError(len(body), end)
	}
	mode := "reinforce"
	if rawMode == 1 {
		mode = "amplify"
	}
	return UpgradeItemRequest{
		Mode:                    mode,
		RawMode:                 rawMode,
		TargetSlotIndex:         readI16(body, 2),
		TargetItemTemplateID:    readI32(body, 4),
		MaterialSlotIndex:       readI16(body, 8),
		OptionalTicketSlotIndex: readI16(body, 10),
		TargetItemName:          string(body[16:end]),
	}, nil
}

// DecodeEnchantByBeadRequest 解析宝珠附魔请求。
func DecodeEnchantByBeadRequest(body []byte) (EnchantByBeadRequest, error) {
	if len(body) < 6 {
		return EnchantByBeadRequest{}, shortBodyError(len(body), 6)
	}
	return EnchantByBeadRequest{
		BeadListType:    body[0],
		BeadSlotIndex:   readI16(body, 1),
		TargetListType:  body[3],
		TargetSlotIndex: readI16(body, 4),
	}, nil
}

func (r DeleteRequest) String() string {
	if r.Extended {
		return fmt.Sprintf("extended list=%d entries=%d", r.ListType, len(r.Entries))
	}
	return fmt.Sprintf("list=%d slot=%d count=%d", r.ListType, r.SlotIndex, r.Count)
}

func (r DeleteOrSellRequest) String() string {
	return fmt.Sprintf("hasList=%t list=%d slot=%d count=%d", r.HasListType, r.ListType, r.SlotIndex, r.Count)
}

func (r MoveItemspaceRequest) String() string {
	return fmt.Sprintf("src=(%d,%d,0x%08X) count=%d dst=(%d,%d,0x%08X) dstStack=%d actor=%d tail=(%d,%d)", r.SourceListType, r.SourceSlotIndex, uint32(r.SourceInstanceValue), r.MoveCount, r.DestinationListType, r.DestinationSlotIndex, uint32(r.DestinationInstanceValue), r.DestinationStack, r.ActorIndex, r.TrailingState0, r.TrailingState1)
}

func (r SortItemRequest) String() string {
	return fmt.Sprintf("list=%d category=%d condition=%d", r.ListType, r.Category, r.Condition)
}

func (r BuyItemRequest) String() string {
	return fmt.Sprintf("item=%d count=%d", r.ItemTemplateID, r.Count)
}

func (r RepairEquipmentRequest) String() string {
	return fmt.Sprintf("inven=%d slot=%d repairSlot=%d auto=%t quick=%t", r.InvenType, r.SlotIndex, r.RepairItemSlot, r.AutoRepair, r.QuickRepair)
}

func (r DisjointItemRequest) String() string {
	return fmt.Sprintf("targetSlot=%d itemSpace=%d disjointSlot=%d ctx=0x%08X", r.TargetSlotIndex, r.ItemSpace, r.DisjointItemSlotIndex, uint32(r.ContextValue))
}

func (r UseStackableRequest) String() string {
	return fmt.Sprintf("slot=%d list=%d instance=0x%08X item=0x%08X reserved=0x%08X", r.SlotIndex, r.ListType, uint32(r.InstanceValue), uint32(r.ItemCode), r.Reserved)
}

func (r UseStackableActionRequest) String() string {
	return fmt.Sprintf("slot=%d list=%d action=%d reserved=(%d,%d,%d)", r.SourceSlotIndex, r.ListType, r.ActionIndex, r.Reserved0, r.Reserved1, r.Reserved2)
}

func (r SelectDamageFontRequest) String() string {
	return fmt.Sprintf("font=%d", r.FontIndex)
}

func (r UpgradeItemRequest) String() string {
	return fmt.Sprintf("mode=%s target=(%d,%d) material=%d ticket=%d name=%q", r.Mode, r.TargetSlotIndex, r.TargetItemTemplateID, r.MaterialSlotIndex, r.OptionalTicketSlotIndex, r.TargetItemName)
}

func (r EnchantByBeadRequest) String() string {
	return fmt.Sprintf("bead=(%d,%d) target=(%d,%d)", r.BeadListType, r.BeadSlotIndex, r.TargetListType, r.TargetSlotIndex)
}

func isKnownListType(v byte) bool {
	switch v {
	case listTypeMain, listTypeAvatar, listTypePersonalCargo, listTypeEquipment, listTypePet, listTypeAccountCargo, listTypeGuildMedal:
		return true
	default:
		return false
	}
}

func shortBodyError(got int, want int) error {
	return fmt.Errorf("body too short: got %d want >= %d", got, want)
}

func readI16(body []byte, offset int) int16 {
	return int16(binary.LittleEndian.Uint16(body[offset:]))
}

func readU16(body []byte, offset int) uint16 {
	return binary.LittleEndian.Uint16(body[offset:])
}

func readI32(body []byte, offset int) int32 {
	return int32(binary.LittleEndian.Uint32(body[offset:]))
}

func readU32(body []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(body[offset:])
}
