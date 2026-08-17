// 本文件负责修理费用的 86JP 领域公式（EquipmentRepairPriceProvider）。
// PVF 证据由 dnfbridge resolver 提供，这里只做确定性计算。
package equip

import (
	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// repairAllEligibleTypes is the 86JP repair-all type filter: only these PVF
// [equipment type] categories are touched by a repair-all request; charms,
// avatars (except aurora), titles, pet gear and medals are skipped.
var repairAllEligibleTypes = map[string]bool{
	"[weapon]":        true,
	"[coat]":          true,
	"[pants]":         true,
	"[hat]":           true,
	"[shoulder]":      true,
	"[waist]":         true,
	"[shoes]":         true,
	"[amulet]":        true,
	"[wrist]":         true,
	"[ring]":          true,
	"[support]":       true,
	"[aurora avatar]": true,
	"[magic stone]":   true,
}

// RepairAllEligible reports whether one PVF equipment type participates in a
// repair-all scan (86JP ItemMetadataResolver.IsRepairAllEligible).
func RepairAllEligible(equipmentType string) bool {
	return repairAllEligibleTypes[equipmentType]
}

// CalcRepairCost mirrors 86JP EquipmentRepairPriceProvider.CalcRepairCost,
// including its C# evaluation order: the (repairPrice*(grade+5))/10 factor is
// computed with integer arithmetic before the float rate chain, and every
// rate multiply is float32 like the C# float math. The final cost truncates
// to int64. Zero lost durability, non-positive repair price or max durability
// yields zero cost.
func CalcRepairCost(evidence alignedcmd.RepairCostEvidence, currentDurability int64, upgradeLevel int, quickRepair bool) int64 {
	if evidence.MaxDurability <= 0 || evidence.RepairPrice <= 0 || currentDurability >= evidence.MaxDurability || currentDurability < 0 {
		return 0
	}
	lost := evidence.MaxDurability - currentDurability
	scaledPrice := evidence.RepairPrice * (evidence.Grade + 5) / 10
	base := float32(scaledPrice) * float32(evidence.RepairCostRate) / float32(evidence.MaxDurability) * float32(lost)
	cost := base * float32(upgradeRepairRate(evidence.UpgradeRates, upgradeLevel))
	if quickRepair {
		cost *= float32(evidence.QuickRepairRate)
	}
	if cost <= 0 {
		return 0
	}
	return int64(cost)
}

func upgradeRepairRate(rates []float64, upgradeLevel int) float32 {
	if len(rates) == 0 {
		return 1
	}
	if upgradeLevel < 0 {
		upgradeLevel = 0
	}
	if upgradeLevel > len(rates)-1 {
		upgradeLevel = len(rates) - 1
	}
	rate := rates[upgradeLevel]
	if rate <= 0 {
		return 1
	}
	return float32(rate)
}
