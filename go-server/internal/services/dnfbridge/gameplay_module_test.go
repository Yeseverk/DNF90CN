package dnfbridge

import (
	"errors"
	"reflect"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestGameplayModuleRegistryRejectsDuplicateOwnership(t *testing.T) {
	handler := func(*Service, *gameSession, gameplayRequest) error { return nil }
	normalizer := func(body []byte) []byte { return body }
	tests := []struct {
		name    string
		modules []gameplayModuleDefinition
	}{
		{
			name: "module name",
			modules: []gameplayModuleDefinition{
				{Name: "same"},
				{Name: "same"},
			},
		},
		{
			name: "legacy opcode",
			modules: []gameplayModuleDefinition{
				{Name: "one", LegacyHandlers: map[uint16]gameplayHandler{21: handler}},
				{Name: "two", LegacyHandlers: map[uint16]gameplayHandler{21: handler}},
			},
		},
		{
			name: "upper opcode",
			modules: []gameplayModuleDefinition{
				{Name: "one", UpperHandlers: map[uint16]gameplayHandler{21: handler}},
				{Name: "two", UpperHandlers: map[uint16]gameplayHandler{21: handler}},
			},
		},
		{
			name: "legacy normalizer",
			modules: []gameplayModuleDefinition{
				{Name: "one", LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{21: normalizer}},
				{Name: "two", LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{21: normalizer}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newGameplayModuleRegistry(test.modules...); !errors.Is(err, errGameplayModuleDuplicate) {
				t.Fatalf("error=%v want=%v", err, errGameplayModuleDuplicate)
			}
		})
	}
}

func TestCurrentGameplayModulesHaveIndependentRegisteredOwners(t *testing.T) {
	wantNames := []string{
		"npc-shop",
		"cera-shop",
		"cera-package",
		"premium-service",
		"inventory",
		"skill",
		"equipment-disjoint",
		"avatar-disjoint",
		"emblem-compound",
		"expert-job",
		"booster-item",
		"equipment-rental",
		"epic-production",
		"guardian-gem",
		"equipment-socket",
		"equipment-emblem-control",
		"avatar-socket",
		"avatar-emblem",
		"item-grade-adjust",
		"equipment-effect",
		"aura-skin-slot",
		"quest",
		"dungeon-navigation",
		"dungeon-combat-drop",
		"dungeon-death-revive",
		"dungeon-settlement-card",
		"dungeon-tutorial-story",
		"death-tower",
		"town",
		"adventure-info",
		"title-book",
		"clone-title",
		"achievement",
		"emotion",
		"joust",
		"collectbox",
	}
	if !reflect.DeepEqual(currentGameplayModules.names, wantNames) {
		t.Fatalf("module names=%v want=%v", currentGameplayModules.names, wantNames)
	}
	ownedRoutes := make(map[string]int, len(wantNames))
	for _, route := range currentGameplayModules.legacyHandlers {
		ownedRoutes[route.module]++
	}
	for _, route := range currentGameplayModules.upperHandlers {
		ownedRoutes[route.module]++
	}
	for _, route := range currentGameplayModules.legacyNormalizers {
		ownedRoutes[route.module]++
	}
	for _, module := range wantNames {
		if ownedRoutes[module] == 0 {
			t.Errorf("gameplay module %q owns no handler or normalizer route", module)
		}
	}

	legacyOwners := map[uint16]string{
		uint16(dnfenum.CmdPacketBuyItem):                        "npc-shop",
		uint16(dnfenum.CmdPacketShopPurchaseCount):              "npc-shop",
		uint16(dnfenum.CmdPacketBuyCerashopItem):                "cera-shop",
		uint16(dnfenum.CmdPacketOpenCerapackage):                "cera-package",
		uint16(dnfenum.CmdPacketPremiumService):                 "premium-service",
		uint16(dnfenum.CmdPacketDisjointItem):                   "equipment-disjoint",
		uint16(dnfenum.CmdPacketDisjointAvatar):                 "avatar-disjoint",
		uint16(dnfenum.CmdPacketCompoundEmblem):                 "emblem-compound",
		uint16(dnfenum.CmdPacketUseBoosterItem):                 "booster-item",
		uint16(dnfenum.CmdPacketUseLotteryItem):                 "booster-item",
		uint16(dnfenum.CmdPacketOverflowInfo):                   "booster-item",
		uint16(dnfenum.CmdPacketRentEquipmentItem):              "equipment-rental",
		uint16(dnfenum.CmdPacketEpicProductionStartFinish):      "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionChangeItem):       "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionProcess):          "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionMaterialCompound): "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionAbilityOption):    "epic-production",
		uint16(dnfenum.CmdPacketUseGem):                         "guardian-gem",
		uint16(currentEquipmentSocketOpenOpcode):                "equipment-socket",
		uint16(currentNoBody796Opcode):                          "equipment-emblem-control",
		uint16(currentEquipmentEmblemAttachOpcode):              "equipment-emblem-control",
		uint16(currentAvatarSocketOpenOpcode):                   "avatar-socket",
		uint16(currentAvatarEmblemAttachOpcode):                 "avatar-emblem",
		uint16(dnfenum.CmdPacketResetItemAttr):                  "item-grade-adjust",
		uint16(dnfenum.CmdPacketAddEquipmentEffect):             "equipment-effect",
		uint16(dnfenum.CmdPacketOpenAuraSkinSlot):               "aura-skin-slot",
		uint16(dnfenum.CmdPacketSetQuestTrigger):                "quest",
		uint16(dnfenum.UpperMsgSelectEnter):                     "dungeon-navigation",
		uint16(dnfenum.CmdPacketMoveMap):                        "dungeon-navigation",
		uint16(dnfenum.CmdPacketPrevVillage):                    "dungeon-navigation",
		uint16(dnfenum.CmdPacketDropItem):                       "dungeon-combat-drop",
		uint16(dnfenum.CmdPacketDieMonster):                     "dungeon-combat-drop",
		uint16(dnfenum.CmdPacketGetItem):                        "dungeon-combat-drop",
		uint16(dnfenum.CmdPacketDieCharacter):                   "dungeon-death-revive",
		uint16(dnfenum.CmdPacketScoreScrollState):               "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketCardSelectRightState):           "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketSelectCard):                     "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketEplpCommand):                    "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketDungeonEventStoryPause):         "dungeon-tutorial-story",
		uint16(dnfenum.CmdPacketSetUserPosition):                "town",
		uint16(dnfenum.CmdPacketSoloTelepoart):                  "town",
		uint16(dnfenum.CmdPacketRequestAdventureInfo):           "adventure-info",
		uint16(dnfenum.CmdPacketTitleBookPut):                   "title-book",
		uint16(currentSetCloneTitleMsgID):                       "clone-title",
		uint16(dnfenum.CmdPacketAchievementTrigger):             "achievement",
		uint16(dnfenum.CmdPacketChangeEmotion):                  "emotion",
		uint16(dnfenum.CmdPacketJoustInfo):                      "joust",
		uint16(dnfenum.CmdPacketJoustBetting):                   "joust",
		uint16(dnfenum.CmdPacketJoustMatchHistory):              "joust",
		uint16(dnfenum.CmdPacketSelectCollectbox):               "collectbox",
	}
	for opcode, want := range legacyOwners {
		if route, ok := currentGameplayModules.legacyHandlers[opcode]; !ok || route.module != want {
			t.Errorf("legacy opcode=%d owner=%q found=%t want=%q", opcode, route.module, ok, want)
		}
	}

	upperOwners := map[uint16]string{
		uint16(dnfenum.CmdPacketShopPurchaseCount):              "npc-shop",
		uint16(dnfenum.CmdPacketRecoverStamina):                 "premium-service",
		uint16(dnfenum.CmdPacketPremiumService):                 "premium-service",
		uint16(dnfenum.CmdPacketUseLotteryItem):                 "booster-item",
		uint16(dnfenum.CmdPacketEpicProductionStartFinish):      "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionChangeItem):       "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionProcess):          "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionMaterialCompound): "epic-production",
		uint16(dnfenum.CmdPacketEpicProductionAbilityOption):    "epic-production",
		uint16(dnfenum.UpperMsgSelectEnter):                     "dungeon-navigation",
		uint16(dnfenum.CmdPacketMoveMap):                        "dungeon-navigation",
		uint16(dnfenum.CmdPacketGiveupGame):                     "dungeon-navigation",
		uint16(dnfenum.CmdPacketBack2Village):                   "dungeon-navigation",
		uint16(dnfenum.CmdPacketDropItem):                       "dungeon-combat-drop",
		uint16(dnfenum.CmdPacketDieMonster):                     "dungeon-combat-drop",
		uint16(dnfenum.CmdPacketGetItem):                        "dungeon-combat-drop",
		uint16(dnfenum.CmdPacketBossDieCheck):                   "dungeon-combat-drop",
		uint16(dnfenum.CmdPacketDieCharacter):                   "dungeon-death-revive",
		uint16(dnfenum.CmdPacketUseCoin):                        "dungeon-death-revive",
		uint16(dnfenum.CmdPacketSetPlayResult):                  "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketScoreScrollState):               "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketCardSelectRightState):           "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketSelectCard):                     "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketEplpCommand):                    "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketCharacterStatistic):             "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketChangeTutorialFlag):             "dungeon-tutorial-story",
		uint16(dnfenum.CmdPacketDungeonMissionCheckSuccess):     "dungeon-tutorial-story",
		uint16(dnfenum.CmdPacketDeathTowerStageCmd):             "death-tower",
		uint16(dnfenum.CmdPacketSoloTelepoart):                  "town",
		uint16(dnfenum.CmdPacketTitleBookGet):                   "title-book",
		uint16(currentSetCloneTitleMsgID):                       "clone-title",
		uint16(dnfenum.CmdPacketJoustInfo):                      "joust",
		uint16(dnfenum.CmdPacketJoustBetting):                   "joust",
		uint16(dnfenum.CmdPacketJoustMatchHistory):              "joust",
	}
	for opcode, want := range upperOwners {
		if route, ok := currentGameplayModules.upperHandlers[opcode]; !ok || route.module != want {
			t.Errorf("upper opcode=%d owner=%q found=%t want=%q", opcode, route.module, ok, want)
		}
	}

	normalizerOwners := map[uint16]string{
		uint16(dnfenum.CmdPacketShopPurchaseCount):      "npc-shop",
		uint16(dnfenum.CmdPacketMoveItemspace):          "inventory",
		uint16(dnfenum.CmdPacketChangeSkillslot):        "skill",
		uint16(dnfenum.CmdPacketSetQuestTrigger):        "quest",
		uint16(dnfenum.CmdPacketDieCharacter):           "dungeon-death-revive",
		uint16(dnfenum.CmdPacketSelectCard):             "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketEplpCommand):            "dungeon-settlement-card",
		uint16(dnfenum.CmdPacketDungeonEventStoryPause): "dungeon-tutorial-story",
		uint16(dnfenum.CmdPacketUseGem):                 "guardian-gem",
		uint16(dnfenum.CmdPacketDisjointItem):           "equipment-disjoint",
		uint16(dnfenum.CmdPacketDisjointAvatar):         "avatar-disjoint",
		uint16(dnfenum.CmdPacketCompoundEmblem):         "emblem-compound",
		uint16(dnfenum.CmdPacketJoustInfo):              "joust",
		uint16(dnfenum.CmdPacketJoustBetting):           "joust",
		uint16(dnfenum.CmdPacketJoustMatchHistory):      "joust",
	}
	for opcode, want := range normalizerOwners {
		if route, ok := currentGameplayModules.legacyNormalizers[opcode]; !ok || route.module != want {
			t.Errorf("normalizer opcode=%d owner=%q found=%t want=%q", opcode, route.module, ok, want)
		}
	}
}

func TestCloneTitleModuleUsesCurrentNoPackOpcode568(t *testing.T) {
	if currentSetCloneTitleMsgID != uint16(dnfenum.CmdPacketSetCloneTitle) {
		t.Fatalf(
			"clone title opcode=%d enum=%d",
			currentSetCloneTitleMsgID,
			dnfenum.CmdPacketSetCloneTitle,
		)
	}
	if currentSetCloneTitleMsgID != 568 {
		t.Fatalf("clone title opcode=%d want=568/0x0238", currentSetCloneTitleMsgID)
	}
	if _, ok := currentGameplayModules.legacyHandlers[569]; ok {
		t.Fatal("legacy opcode 569/0x0239 must not be registered as clone title")
	}
}

func TestGameplayModuleOwnsLegacyNormalizationBoundary(t *testing.T) {
	body := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	got, ok := currentGameplayModules.NormalizeLegacy(uint16(dnfenum.CmdPacketChangeSkillslot), body)
	if !ok || !reflect.DeepEqual(got, body[:8]) {
		t.Fatalf("normalized=%x found=%t want=%x", got, ok, body[:8])
	}
	if got, ok := currentGameplayModules.NormalizeLegacy(0xffff, body); ok || !reflect.DeepEqual(got, body) {
		t.Fatalf("unknown normalization=%x found=%t want unchanged", got, ok)
	}
}
