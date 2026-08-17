package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var (
	errDungeonDropObjectKeyRange     = errors.New("dnf dungeon drop object key range exhausted")
	errDungeonDropSceneSlotRange     = errors.New("dnf dungeon drop scene slot range exhausted")
	errDungeonDropObjectConflict     = errors.New("dnf dungeon drop object key already exists")
	errDungeonDropNotFound           = errors.New("dnf dungeon drop object was not found")
	errDungeonDropRoomMismatch       = errors.New("dnf dungeon drop belongs to another room")
	errDungeonDropActorMismatch      = errors.New("dnf dungeon drop belongs to another actor")
	errDungeonDropCoinBranchUnproved = errors.New("dnf dungeon coin/gold pickup branch is not proved")
)

type runtimeDungeonDropStatus byte

const (
	runtimeDungeonDropAvailable runtimeDungeonDropStatus = iota + 1
	runtimeDungeonDropConsumed
)

type runtimeDungeonDrop struct {
	ObjectKey            uint32
	SceneSlot            uint16
	Item                 dungeonDropItemDefinition
	Amount               uint32
	QualitySeed          uint32
	DiscardOrigin        *runtimeDungeonDiscardOrigin
	OwnerActorObjectKey  uint16
	Room                 runtimeDungeonRoomKey
	Status               runtimeDungeonDropStatus
	DestinationSlot      uint16
	GoldAfter            int64
	PickupResponseBody   []byte
	PickupItemUpdateBody []byte
}

func (drop *runtimeDungeonDrop) isGold() bool {
	return drop != nil && drop.Item.ItemID == 0
}

type runtimeDungeonDropOwner struct {
	nextSceneSlot uint16
	byObjectKey   map[uint32]*runtimeDungeonDrop
}

func newRuntimeDungeonDropOwner() *runtimeDungeonDropOwner {
	return &runtimeDungeonDropOwner{
		nextSceneSlot: 1,
		byObjectKey:   make(map[uint32]*runtimeDungeonDrop),
	}
}

func (owner *runtimeDungeonDropOwner) registerBatch(drops []*runtimeDungeonDrop, nextSceneSlot uint16) error {
	if owner == nil {
		return errDungeonDropNotFound
	}
	if owner.byObjectKey == nil {
		owner.byObjectKey = make(map[uint32]*runtimeDungeonDrop)
	}
	for _, drop := range drops {
		if drop == nil || drop.ObjectKey == 0 {
			return errDungeonDropNotFound
		}
		if _, exists := owner.byObjectKey[drop.ObjectKey]; exists {
			return fmt.Errorf("%w: object_key=%d", errDungeonDropObjectConflict, drop.ObjectKey)
		}
	}
	for _, drop := range drops {
		owner.byObjectKey[drop.ObjectKey] = drop
	}
	owner.nextSceneSlot = nextSceneSlot
	return nil
}

func (owner *runtimeDungeonDropOwner) owned(
	objectKey uint32,
	room runtimeDungeonRoomKey,
	actorObjectKey uint16,
) (*runtimeDungeonDrop, error) {
	if owner == nil || objectKey == 0 {
		return nil, errDungeonDropNotFound
	}
	drop := owner.byObjectKey[objectKey]
	if drop == nil {
		return nil, fmt.Errorf("%w: object_key=%d", errDungeonDropNotFound, objectKey)
	}
	if drop.Room != room {
		return nil, fmt.Errorf(
			"%w: object_key=%d owned=%+v current=%+v",
			errDungeonDropRoomMismatch,
			objectKey,
			drop.Room,
			room,
		)
	}
	if drop.OwnerActorObjectKey != actorObjectKey {
		return nil, fmt.Errorf(
			"%w: object_key=%d owned=%d current=%d",
			errDungeonDropActorMismatch,
			objectKey,
			drop.OwnerActorObjectKey,
			actorObjectKey,
		)
	}
	return drop, nil
}

func (s *Service) planCurrentDungeonMonsterDrops(
	runtime *runtimeDungeonState,
	monster runtimeDungeonMonster,
	ownerActorObjectKey uint16,
) ([]currentDungeonDeathDropWire, error) {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		return nil, errDungeonWorldMapUnavailable
	}
	if ownerActorObjectKey == 0 {
		return nil, errDungeonDropActorMismatch
	}
	scene, ok := runtime.Session.Scene()
	if !ok {
		return nil, errDungeonWorldMapUnavailable
	}
	roomKey := runtimeDungeonRoomKeyFromScene(scene)
	monsterCatalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return nil, err
	}
	rules, err := currentDungeonMonsterDropRules(monsterCatalog)
	if err != nil {
		return nil, err
	}
	dropCatalog, err := monsterCatalog.DropCatalog()
	if err != nil {
		return nil, err
	}
	pool, err := dropCatalog.MonsterPool(monster.Spawn.MonsterID)
	if err != nil {
		return nil, err
	}
	visit, err := runtime.currentDungeonRoomVisit(scene)
	if err != nil {
		return nil, err
	}
	monsterLevel, err := currentDungeonMonsterLevel(monster.Spawn, runtime.Dungeon.Metadata.BasisLevel)
	if err != nil {
		return nil, err
	}
	monsterType, err := currentDungeonMonsterType(monster.Spawn.Rank)
	if err != nil {
		return nil, err
	}
	rng := newCurrentDungeonDropLCG(visit.DropRNG)
	probability, probabilityFound := rules.Probability(int64(monsterLevel))
	itemReference, itemReferenceFound := rules.ItemReference(int64(monsterLevel))
	difficultyRate := currentDungeonDropDifficultyRate(int(runtime.Request.Difficulty))
	raw := rules.RawTables()

	type plannedDrop struct {
		itemID uint32
		amount uint32
		gold   bool
	}
	selectedDrops := make([]plannedDrop, 0, 8)

	// 86JP computes gold before the ordinary item-rate rolls, and gold is a
	// normal scene drop with template id 0. Older Go intentionally consumed the
	// roll but threw the object away because the coin pickup branch was still
	// being isolated; that made live dungeons never show金币.
	if probabilityFound {
		goldReferences, goldErr := currentDungeonGoldReferences(monsterCatalog)
		if goldErr != nil {
			return nil, goldErr
		}
		goldReference, goldFound := goldReferences[int64(monsterLevel)]
		if !goldFound {
			return nil, fmt.Errorf("%w: monster_level=%d", errCurrentDungeonGoldReferenceInvalid, monsterLevel)
		}
		goldAmount := currentDungeonGoldAmount(rng, goldReference, difficultyRate)
		goldRate := currentDungeonDropRate(probability.Rates[0], raw.MonsterTypeBonus[0][monsterType], difficultyRate)
		goldRoll := rng.Next(currentDungeonDropDenominator)
		goldSelected := goldRate > goldRoll
		s.logGameEvent(nil, "game-dungeon-monster-drop-gold-roll",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"monster_id", monster.Spawn.MonsterID,
			"monster_object_key", monster.ObjectKey,
			"monster_level", monsterLevel,
			"monster_type", monsterType,
			"base_rate", probability.Rates[0],
			"monster_type_bonus", raw.MonsterTypeBonus[0][monsterType],
			"difficulty_bonus", difficultyRate,
			"gold_rate", goldRate,
			"gold_roll", goldRoll,
			"gold_amount", goldAmount,
			"selected", goldSelected,
			"formula_source", "86jp_monster_gold_branch_itemdropinfo_common_and_monseter")
		if goldSelected {
			selectedDrops = append(selectedDrops, plannedDrop{amount: goldAmount, gold: true})
		}
	}

	appendSelected := func(itemID uint32) error {
		if itemID == 0 {
			// Real gold is selected only through the gold-rate branch above,
			// where the amount comes from ItemDropInfo_Common.etc.
			return nil
		}
		if _, err := dropCatalog.ResolveItem(itemID); err != nil {
			return err
		}
		selectedDrops = append(selectedDrops, plannedDrop{itemID: itemID, amount: 1})
		return nil
	}
	if probabilityFound && itemReferenceFound {
		if rate := currentDungeonDropRate(probability.Rates[1], raw.MonsterTypeBonus[1][monsterType], difficultyRate); rate > rng.Next(currentDungeonDropDenominator) {
			rarity := currentDungeonDropRarity(rng, rules)
			itemID, selected, err := dropCatalog.SelectGenericStackable(rng, int(monsterLevel), itemReference.ValueA, itemReference.ValueB, rarity)
			if err != nil {
				return nil, err
			}
			if selected {
				if err := appendSelected(itemID); err != nil {
					return nil, err
				}
			}
		}
		if rate := currentDungeonDropRate(probability.Rates[2], raw.MonsterTypeBonus[2][monsterType], difficultyRate); rate > rng.Next(currentDungeonDropDenominator) {
			rarity := currentDungeonDropRarity(rng, rules)
			itemID, selected, err := dropCatalog.SelectGenericEquipment(rng, int(monsterLevel), itemReference.ValueA, itemReference.ValueB, rarity)
			if err != nil {
				return nil, err
			}
			if selected {
				if err := appendSelected(itemID); err != nil {
					return nil, err
				}
			}
		}
		if rate := currentDungeonDropRate(probability.Rates[3], raw.MonsterTypeBonus[3][monsterType], difficultyRate); rate > rng.Next(currentDungeonDropDenominator) {
			rarity := currentDungeonDropRarity(rng, rules)
			itemID, selected, err := dropCatalog.SelectGenericStackable(rng, int(monsterLevel), itemReference.ValueA, itemReference.ValueB, rarity)
			if err != nil {
				return nil, err
			}
			if selected {
				if err := appendSelected(itemID); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(pool) > 0 && currentDungeonExplicitPoolRate > rng.Next(currentDungeonDropDenominator) {
		poolEntry, selected := selectDungeonMonsterDrop(pool, uint64(rng.NextUint32()))
		if selected {
			if err := appendSelected(poolEntry.ItemID); err != nil {
				return nil, err
			}
		}
	}

	specialDrops, err := currentDungeonSpecialDrops(monsterCatalog)
	if err != nil {
		return nil, err
	}
	dungeonLevel := int(monsterLevel)
	if basisLevel := runtime.Dungeon.Metadata.BasisLevel; basisLevel.Set && basisLevel.Value > 0 {
		dungeonLevel = int(basisLevel.Value)
	}
	growthIndependentDropPercent := int64(0)
	if growthEffect, growthActive := s.currentGrowthContractEffect(
		context.Background(),
		runtime.Character.AccountID,
	); growthActive {
		// This runtime currently admits one locally owned dungeon actor. The
		// PVF's first independent-drop value is therefore authoritative.
		growthIndependentDropPercent = growthEffect.independentDropRatePercent(1)
	}
	independentItems := currentDungeonIndependentDropItems(
		specialDrops,
		monster.Spawn.MonsterID,
		int(runtime.Request.Difficulty),
		dungeonLevel,
		growthIndependentDropPercent,
		rng,
	)
	for _, itemID := range independentItems {
		if err := appendSelected(itemID); err != nil {
			return nil, err
		}
	}
	worldLevel := specialDrops.WorldByLevel[int(monsterLevel)]
	worldItemID, worldSelected := currentDungeonWorldDropItem(specialDrops, int(monsterLevel), rng)
	if worldSelected {
		if err := appendSelected(worldItemID); err != nil {
			return nil, err
		}
	}
	s.logGameEvent(nil, "game-dungeon-monster-special-drop-roll",
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"monster_id", monster.Spawn.MonsterID,
		"monster_level", monsterLevel,
		"dungeon_basis_level", dungeonLevel,
		"difficulty", runtime.Request.Difficulty,
		"independent_entry_count", len(specialDrops.IndependentByMonster[monster.Spawn.MonsterID]),
		"independent_selected_items", independentItems,
		"growth_contract_independent_drop_percent", growthIndependentDropPercent,
		"world_total_weight", worldLevel.TotalWeight,
		"world_candidate_count", len(worldLevel.Items),
		"world_denominator", currentDungeonWorldDropDenominator,
		"world_selected", worldSelected,
		"world_selected_item", worldItemID,
		"source", "runtime_pvf_independent_drop_and_world_drop")

	// The LCG is advanced even when no standard scene item survived the real
	// PVF selection.  This mirrors the reference room stream across kills.
	visit.DropRNG = rng.Seed()
	if len(selectedDrops) == 0 {
		return nil, nil
	}
	owner := runtime.DropOwner
	if owner == nil {
		owner = newRuntimeDungeonDropOwner()
	}
	count := uint32(len(selectedDrops))
	if runtime.NextObjectKey == 0 || runtime.NextObjectKey > math.MaxUint16 ||
		count > math.MaxUint16-runtime.NextObjectKey {
		return nil, fmt.Errorf(
			"%w: next=%d count=%d",
			errDungeonDropObjectKeyRange,
			runtime.NextObjectKey,
			count,
		)
	}
	if owner.nextSceneSlot == 0 || uint32(owner.nextSceneSlot)+count > math.MaxUint16 {
		return nil, fmt.Errorf(
			"%w: next=%d count=%d",
			errDungeonDropSceneSlotRange,
			owner.nextSceneSlot,
			count,
		)
	}

	drops := make([]*runtimeDungeonDrop, 0, count)
	wires := make([]currentDungeonDeathDropWire, 0, count)
	nextObjectKey := runtime.NextObjectKey
	nextSceneSlot := owner.nextSceneSlot
	for _, selected := range selectedDrops {
		definition := dungeonDropItemDefinition{ItemID: 0, Kind: dungeonDropItemStackable}
		if !selected.gold {
			var err error
			definition, err = dropCatalog.ResolveItem(selected.itemID)
			if err != nil {
				return nil, err
			}
		}
		itemState, err := currentDungeonDeathDropItemState(nextSceneSlot, definition, selected.amount)
		if err != nil {
			return nil, err
		}
		qualitySeed := uint32(0)
		if definition.Kind == dungeonDropItemEquipment {
			qualitySeed = binary.LittleEndian.Uint32(itemState.data[6:10])
		}
		drop := &runtimeDungeonDrop{
			ObjectKey:           nextObjectKey,
			SceneSlot:           nextSceneSlot,
			Item:                definition,
			Amount:              selected.amount,
			QualitySeed:         qualitySeed,
			OwnerActorObjectKey: ownerActorObjectKey,
			Room:                roomKey,
			Status:              runtimeDungeonDropAvailable,
		}
		drops = append(drops, drop)
		wires = append(wires, currentDungeonDeathDropWire{
			SceneObjectKey:      drop.ObjectKey,
			Item:                itemState,
			UnknownTailSentinel: math.MaxUint16,
			OwnerActorObjectKey: ownerActorObjectKey,
		})
		nextObjectKey++
		nextSceneSlot++
	}
	if err := owner.registerBatch(drops, nextSceneSlot); err != nil {
		return nil, err
	}
	runtime.DropOwner = owner
	runtime.NextObjectKey = nextObjectKey
	return wires, nil
}
