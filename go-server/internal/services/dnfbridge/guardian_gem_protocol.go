package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// Current NoPack sub_E9C340/sub_E9C5F0 writes this body for C2S
	// ENUM_CMDPACKET_USE_GEM (829): target medal id, exact list-38 guardian-gem
	// source slot, guardian-gem id, and socket index.
	currentGuardianGemUseRequestWireSize        = 11
	currentGuardianGemSocketCount               = 4
	currentGuardianGemRawSocketOffset           = 101
	currentGuardianGemRawSocketWidth            = 2
	currentGuardianGemItemIDBase         uint32 = 89999
	currentGuardianGemInventoryListType  byte   = 38
	currentGuildMedalActorSlot           uint64 = 32
	currentGuildMedalPageSlotStart       int16  = 0
	currentGuildMedalPageSlotEnd         int16  = 48
	currentGuardianGemPageSlotStart      int16  = 49
	currentGuardianGemPageSlotEnd        int16  = 97
)

var (
	errCurrentGuardianGemCatalogRequired    = errors.New("current guardian gem PVF catalog is required")
	errCurrentGuardianGemNotFlagGem         = errors.New("item is not a PVF [flag gem]")
	errCurrentGuardianGemTargetNotMedal     = errors.New("guardian gem target is not a PVF [flag] guild medal")
	errCurrentGuardianGemGradeInvalid       = errors.New("guardian gem PVF grade is outside 0..3")
	errCurrentGuardianGemEnchantAmbiguous   = errors.New("guardian gem PVF enchant family is missing or ambiguous")
	errCurrentGuardianGemCharacterMissing   = errors.New("guardian gem selected character is missing")
	errCurrentGuardianGemRepositoryMissing  = errors.New("guardian gem inventory/equipment repository is missing")
	errCurrentGuardianGemTransactionMissing = errors.New("guardian gem item transaction is missing")
	errCurrentGuardianGemInventoryMissing   = errors.New("guardian gem inventory is missing")
	errCurrentGuardianGemSourceMissing      = errors.New("guardian gem source stack is missing from the guild-medal inventory")
	errCurrentGuardianGemTargetMissing      = errors.New("guardian gem target medal is missing")
	errCurrentGuardianGemTargetAmbiguous    = errors.New("guardian gem target medal is ambiguous")
	errCurrentGuardianGemTargetRawMissing   = errors.New("guardian gem target medal has no current 0x77 raw entry")
	errCurrentGuardianGemItemIDRange        = errors.New("guardian gem item id cannot be represented by the current raw socket field")
	errCurrentGuardianGemSourceSlotRange    = errors.New("guardian gem source slot is outside the current list-38 guardian-gem page")
)

type currentGuardianGemTargetContainer byte

const (
	currentGuardianGemTargetInventory currentGuardianGemTargetContainer = iota
	currentGuardianGemTargetWarehouse
	currentGuardianGemTargetEquipped
)

type currentGuardianGemTargetLocation struct {
	Container currentGuardianGemTargetContainer
	ListType  byte
	Slot      int16
	Key       string
}

type currentGuardianGemMutationResult struct {
	Target currentGuardianGemTargetLocation
	Source currentSocketChangedSlot
}

// currentGuardianGemUseRequest is the exact current-EXE request grammar.  It
// names the exact list-38 source slot. sub_E9C340 obtains the target medal from
// sub_21D9F30(0), whose category-3/index-0 mapping resolves actor slot 32.
type currentGuardianGemUseRequest struct {
	TargetMedalItemID     uint32
	GuardianGemSourceSlot uint16
	GuardianGemItemID     uint32
	SocketIndex           byte
}

// currentGuardianGemDefinition is a typed, PVF-backed description of a
// guardian gem. EnchantFamily is the sole numeric child section inside
// [enchant], normalized only for stable equality comparisons. It is intentionally not
// inferred from an item-id range or localized display name.

type currentGuardianGemUseSnapshot struct {
	GemMainStackCount      int64
	GemWarehouseStackCount int64
	TargetMainMatches      int
	TargetWarehouseMatches int
	TargetEquippedMatches  int
}

func decodeCurrentGuardianGemUseRequest(body []byte) (currentGuardianGemUseRequest, error) {
	if len(body) != currentGuardianGemUseRequestWireSize {
		return currentGuardianGemUseRequest{}, fmt.Errorf(
			"guardian-gem use body length=%d, want=%d",
			len(body),
			currentGuardianGemUseRequestWireSize,
		)
	}
	request := currentGuardianGemUseRequest{
		TargetMedalItemID:     binary.LittleEndian.Uint32(body[0:4]),
		GuardianGemSourceSlot: binary.LittleEndian.Uint16(body[4:6]),
		GuardianGemItemID:     binary.LittleEndian.Uint32(body[6:10]),
		SocketIndex:           body[10],
	}
	if request.TargetMedalItemID == 0 || request.GuardianGemItemID == 0 {
		return currentGuardianGemUseRequest{}, fmt.Errorf("guardian-gem use item id must be nonzero")
	}
	if request.GuardianGemSourceSlot > 32767 || !currentGuardianGemPageContains(int16(request.GuardianGemSourceSlot)) {
		return currentGuardianGemUseRequest{}, fmt.Errorf("%w: slot=%d", errCurrentGuardianGemSourceSlotRange, request.GuardianGemSourceSlot)
	}
	if request.SocketIndex >= currentGuardianGemSocketCount {
		return currentGuardianGemUseRequest{}, fmt.Errorf(
			"guardian-gem socket index=%d, want 0..%d",
			request.SocketIndex,
			currentGuardianGemSocketCount-1,
		)
	}
	return request, nil
}

// resolveCurrentGuardianGem accepts only a stackable document whose real PVF
// type is [flag gem]. It extracts the client-visible grade and the one
// attribute family the PVF attaches to a medal. This is a read-only resolver:
// it neither decides raw-item encoding nor consumes inventory.

func currentGuardianGemSocketValue(itemID uint32) (uint16, error) {
	delta := int64(itemID) - int64(currentGuardianGemItemIDBase)
	if delta <= 0 || delta > 32767 {
		return 0, fmt.Errorf("%w: item=%d base=%d delta=%d", errCurrentGuardianGemItemIDRange, itemID, currentGuardianGemItemIDBase, delta)
	}
	return uint16(delta), nil
}

func currentGuildMedalPageContains(slot int16) bool {
	return slot >= currentGuildMedalPageSlotStart && slot <= currentGuildMedalPageSlotEnd
}

func currentGuardianGemPageContains(slot int16) bool {
	return slot >= currentGuardianGemPageSlotStart && slot <= currentGuardianGemPageSlotEnd
}
