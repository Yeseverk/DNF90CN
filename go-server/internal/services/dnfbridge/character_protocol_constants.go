package dnfbridge

import dnfrepo "longheng.io/server/internal/modules/dnf/repository"

const (
	defaultCharacterSlots        = dnfrepo.DefaultCharacterSlotLimit
	noPackRosterWireSlotLimit    = defaultCharacterSlots
	latestRosterRouteNormal      = 0
	latestRosterContextNormal    = 0
	currentSceneObjectRoute      = 0
	currentSceneObjectContext    = 0
	upperMaxCharacter            = 0xffff
	latestCharacterStateRoute    = 1
	latestCharacterCreateEnabled = 1
	latestCharacterStateActive   = 1
	latestCharacterStateNormal   = 0
	rosterNameMaxBytes           = 30
	rosterWideNameUnits          = 31
	noPackRosterPreJobBytes      = 2
	noPackRosterPostGrowBytes    = 2
	rosterLinkedIDBlockSize      = 8
	// Current sub_20026C0 consumes a mandatory type-44 raw block after every
	// equipment item ID. Even an empty block has its u32 length prefix.
	noPackRosterEquipRowBytes  = 19
	noPackRosterPostEquipBytes = 38
	noPackRosterEntryTailBytes = noPackRosterPreJobBytes + 1 + 1 + noPackRosterPostGrowBytes + 1 + 4 + 4 + 1 + noPackRosterPostEquipBytes
	rosterDefaultPageCount     = 1
	createCodeDuplicated       = 0x00
	createCodeSlotFull         = 0x04
	createCodeServerError      = 0x14
)

func currentConnectionTownActorOwnerContext(session *gameSession) byte {
	if session == nil {
		return currentSceneObjectContext
	}
	return session.connectionTownActorOwnerChannel
}

func currentTownActorOwnerContext(session *gameSession) byte {
	if session == nil {
		return currentSceneObjectContext
	}
	return session.townActorOwnerChannel
}
