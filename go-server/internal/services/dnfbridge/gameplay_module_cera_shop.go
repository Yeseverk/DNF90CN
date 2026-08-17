package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func ceraShopGameplayModule() gameplayModuleDefinition {
	purchaseOpcode := uint16(dnfenum.CmdPacketBuyCerashopItem)
	return gameplayModuleDefinition{
		Name: "cera-shop",
		LegacyHandlers: map[uint16]gameplayHandler{
			// Current-client evidence proves op64 can arrive on the legacy
			// decoder with the same current cart body as the upper transport.
			purchaseOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentCeraShopPurchase(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			purchaseOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-cera-shop-purchase-blocked", "current_exe_op64_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentCeraShopPurchase(session, request.Body)
			},
		},
	}
}
