package dnfbridge

import (
	"encoding/binary"
	"strings"

	"longheng.io/server/internal/modules/dnf/itemquality"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentEquipmentTopQualitySeed         uint32 = itemquality.TopSeed
	currentEquipmentRandomQualitySeedCount uint32 = itemquality.RandomSeedCount
)

func newCurrentEquipmentQualitySeed() (uint32, error) {
	return itemquality.NewRandomSeed()
}

func validCurrentEquipmentQualitySeed(seed uint32) bool {
	return itemquality.Valid(seed)
}

func currentItemStackHasCurrentRaw77(stack dnfrepo.ItemStack) bool {
	return len(stack.RawEntry) == currentItemListEntryWireSize &&
		stack.ItemID > 0 &&
		binary.LittleEndian.Uint32(stack.RawEntry[2:6]) == uint32(stack.ItemID)
}

func currentEquipmentEntryHasCurrentRaw77(entry dnfrepo.EquipmentEntry) bool {
	return len(entry.RawEntry) == currentItemListEntryWireSize &&
		entry.ItemID > 0 &&
		binary.LittleEndian.Uint32(entry.RawEntry[2:6]) == uint32(entry.ItemID)
}

func currentEquipmentQualitySeedFromExtra(extra map[string]string) uint32 {
	return sceneInventoryExtraUint32(extra, "quality_seed", "amount_or_count", "count_or_instance")
}

func currentEquipmentQualitySeedFromStack(stack dnfrepo.ItemStack) uint32 {
	// quality_seed is equipment-specific durable metadata. Accept it directly
	// for legacy inventory rows that predate the item_kind normalization.
	if seed := sceneInventoryExtraUint32(stack.Extra, "quality_seed"); validCurrentEquipmentQualitySeed(seed) {
		return seed
	}
	if !strings.EqualFold(strings.TrimSpace(stack.Extra["item_kind"]), string(dungeonDropItemEquipment)) {
		return 0
	}
	if seed := currentEquipmentQualitySeedFromExtra(stack.Extra); validCurrentEquipmentQualitySeed(seed) {
		return seed
	}
	if currentItemStackHasCurrentRaw77(stack) {
		seed := binary.LittleEndian.Uint32(stack.RawEntry[6:10])
		if validCurrentEquipmentQualitySeed(seed) {
			return seed
		}
	}
	return 0
}

func applyCurrentEquipmentQualitySeed(stack *dnfrepo.ItemStack, seed uint32) error {
	return itemquality.Apply(stack, seed)
}

func currentItemStackValueA(stack dnfrepo.ItemStack) uint32 {
	kind := strings.TrimSpace(stack.Extra["item_kind"])
	explicitQualitySeed := sceneInventoryExtraUint32(stack.Extra, "quality_seed")
	if explicitQualitySeed != 0 ||
		(strings.EqualFold(kind, string(dungeonDropItemEquipment)) && currentEquipmentQualitySeedFromExtra(stack.Extra) != 0) {
		// The quality seed belongs only to raw+0x06. Do not mirror it into the
		// independent raw+0x0E item state field. Legacy amount_or_count is
		// shared by stackables, so it is equipment metadata only when the PVF
		// kind says equipment (or quality_seed is explicitly present).
		return sceneInventoryExtraUint32(stack.Extra, "value_a", "item_uid", "serial")
	}
	if value := sceneInventoryExtraUint32(stack.Extra, "value_a", "instance_value", "item_uid", "serial", "count_or_instance"); value != 0 {
		return value
	}
	// The current EXE's right-click op44 path rejects a stackable row whose
	// raw+0x0E identity is zero before it sends any request. Relational
	// inventory rows imported from the old JSON store legitimately have no
	// historical instance_value. Use their authoritative item id as a stable
	// wire identity instead of leaving the client-side use route disabled.
	if strings.EqualFold(kind, string(dungeonDropItemStackable)) && stack.ItemID > 0 {
		return sceneInventoryUint32FromInt64(stack.ItemID)
	}
	return 0
}
