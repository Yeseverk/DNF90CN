package dnfbridge

const currentItemListEntryWireSize = 0x77
const currentAvatarItemListOptionalBlobCount = 2
const currentEquipmentUpdateEntryWireSize = 0x54
const currentPetRemainSecondsOffset = 0x16
const currentItemListExpireTimeOffset = 0x38
const legacyWrongCurrentItemListExpireTimeOffset = 0x2B
const currentEquipmentUpdateExpireTimeOffset = 0x2B

const currentPetInventoryListType byte = 7
const currentGuildMedalInventoryListType byte = 38

// Explicit runtime refreshes rebuild these character-owned containers. Initial
// selection uses the wider actor-bound bootstrap, which also includes account
// cargo list 12, and scene-ready must not replay this family.
var currentSelectedItemListTypes = []byte{0, 1, 2, currentPetInventoryListType, currentGuildMedalInventoryListType}

type currentItemListEntry struct {
	data             [currentItemListEntryWireSize]byte
	avatarSocketData []byte
	avatarColorData  []byte
}

type currentEquipmentUpdateEntry struct {
	data [currentEquipmentUpdateEntryWireSize]byte
}
