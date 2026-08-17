// Package combatpower derives the damage-affix view used by the local
// combat-power sidecar from the authoritative runtime PVF.
package combatpower

import "errors"

var (
	ErrSourceRequired = errors.New("dnf combat power pvf source is required")
	ErrItemMissing    = errors.New("dnf combat power equipment definition is missing")
)

// Affixes keeps the 90-era damage categories separate. In particular,
// YellowAdditional and CriticalAdditional must never be folded into the
// mutually-exclusive yellow/critical base categories while parsing PVF.
type Affixes struct {
	WhiteDamage        float64
	YellowDamage       float64
	CriticalDamage     float64
	YellowAdditional   float64
	CriticalAdditional float64
	AllAttack          float64
}

// Add combines independent equipment/set contributions. YellowDamage and
// CriticalDamage are unique categories in this PVF and therefore keep only
// the greatest equipped value; the additive categories accumulate.
func (a *Affixes) Add(other Affixes) {
	if a == nil {
		return
	}
	a.WhiteDamage += other.WhiteDamage
	if other.YellowDamage > a.YellowDamage {
		a.YellowDamage = other.YellowDamage
	}
	if other.CriticalDamage > a.CriticalDamage {
		a.CriticalDamage = other.CriticalDamage
	}
	a.YellowAdditional += other.YellowAdditional
	a.CriticalAdditional += other.CriticalAdditional
	a.AllAttack += other.AllAttack
}

type ItemDefinition struct {
	ID             int64
	Path           string
	PartSetID      int64
	Rarity         int
	MinimumLevel   int
	Grade          int
	EquipmentType  string
	EquipmentScore int
	Affixes        Affixes
}

type SetAbility struct {
	RequiredPieces int
	Affixes        Affixes
}

type SetDefinition struct {
	ID        int64
	Path      string
	Abilities []SetAbility
}

type ActiveSet struct {
	ID     int64
	Pieces int
}

type Result struct {
	Affixes           Affixes
	EquippedItems     int
	ScoredItems       int
	Level90EpicItems  int
	PVFEquipmentScore int
	ActiveSets        []ActiveSet
}
