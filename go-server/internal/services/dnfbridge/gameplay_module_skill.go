package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func skillGameplayModule() gameplayModuleDefinition {
	layoutOpcode := uint16(dnfenum.CmdPacketChangeSkillslot)
	buyOpcode := uint16(dnfenum.CmdPacketBuySkill)
	resetOpcode := uint16(dnfenum.CmdPacketSkillInit)
	return gameplayModuleDefinition{
		Name: "skill",
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			layoutOpcode: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, 8)
			},
			buyOpcode:   stripLegacyBuySkillTransportTrailer,
			resetOpcode: func(body []byte) []byte { return stripLegacyTransportTrailer(body, 2) },
		},
	}
}
