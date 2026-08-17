package inventory

import (
	"fmt"
	"math"
)

// UnsealRandomOptionRequest is current NoPack class1/op401:
// u16 client collection key + u16 inventory-manager state.
type UnsealRandomOptionRequest struct {
	TargetSlotIndex       int16
	InventoryManagerState uint16
}

// ChangeRandomOptionRequest is current NoPack class1/op437:
// u16 client collection key + literal u16 zero + u8 zero-based option index.
type ChangeRandomOptionRequest struct {
	TargetSlotIndex int16
	Reserved        uint16
	OptionIndex     byte
}

func DecodeUnsealRandomOptionRequest(body []byte) (UnsealRandomOptionRequest, error) {
	if len(body) != 4 {
		return UnsealRandomOptionRequest{}, fmt.Errorf("unseal random option body length %d, want 4", len(body))
	}
	slot := uint16(readI16(body, 0))
	if slot > math.MaxInt16 {
		return UnsealRandomOptionRequest{}, fmt.Errorf("unseal random option collection key %d exceeds main inventory range", slot)
	}
	return UnsealRandomOptionRequest{TargetSlotIndex: int16(slot), InventoryManagerState: uint16(readI16(body, 2))}, nil
}

func DecodeChangeRandomOptionRequest(body []byte) (ChangeRandomOptionRequest, error) {
	if len(body) != 5 {
		return ChangeRandomOptionRequest{}, fmt.Errorf("change random option body length %d, want 5", len(body))
	}
	slot := uint16(readI16(body, 0))
	if slot > math.MaxInt16 {
		return ChangeRandomOptionRequest{}, fmt.Errorf("change random option collection key %d exceeds main inventory range", slot)
	}
	reserved := uint16(readI16(body, 2))
	if reserved != 0 {
		return ChangeRandomOptionRequest{}, fmt.Errorf("change random option reserved value 0x%04X, want 0", reserved)
	}
	return ChangeRandomOptionRequest{TargetSlotIndex: int16(slot), Reserved: reserved, OptionIndex: body[4]}, nil
}
