package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/adventuregroup"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	currentAdventureInfoPushMsgID             = uint16(dnfenum.CmdPacketCustomAbilityEquipOption)
	currentAdventureActorRefreshMsgID         = uint16(dnfenum.CmdPacketAgitWarMissionReward)
	currentAdventureInfoRequestWireLength     = 16
	currentAdventureInfoSceneRequestLength    = 2
	currentAdventureInfoRawLength             = 7420
	currentAdventureActorRefreshRawLength     = 16
	currentAdventureInfoManageLevelOffset     = 0
	currentAdventureInfoCurrentPointOffset    = 8
	currentAdventureInfoLoginDaysOffset       = 20
	currentAdventureInfoRosterOffset          = 60
	currentAdventureInfoRosterCount           = 100
	currentAdventureInfoRosterEntrySize       = 52
	currentAdventureInfoRosterIDOffset        = 4
	currentAdventureInfoRosterNameOffset      = 12
	currentAdventureInfoRosterNameSize        = 30
	currentAdventureInfoRosterLevelOffset     = 42
	currentAdventureInfoRosterCardLevelOffset = 48
	currentAdventureInfoCharacterCountOffset  = 56
	currentAdventureInfoShopPointOffset       = 5260
	currentAdventureInfoShopPointCount        = 5
	currentAdventureInfoShopPointEntrySize    = 8
	currentAdventureInfoPurchaseOffset        = 5300
	currentAdventureInfoPurchaseCount         = 256
	currentAdventureInfoPurchaseEntrySize     = 8
	currentAdventureInfoTripleOffset          = 7348
	currentAdventureInfoTripleCount           = 20
	currentAdventureInfoTripleSize            = 3
	currentAdventureInfoTripleCountOffset     = 0
	currentAdventureInfoTripleTypeOffset      = 2
	currentAdventureInfoGrowthCapsuleOffset   = 7408
	currentAdventureInfoTailUint32Offset      = 7416
	currentSelectorAdventureInfoSlotMetadata  = adventuregroup.SelectorSlotMetadataKey
)
