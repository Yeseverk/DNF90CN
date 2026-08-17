package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// currentQuestDropCandidate is one rolled quest-item drop from a PVF
// [monster reward item] or [enemy reward item] entry.
type currentQuestDropCandidate struct {
	QuestID  int64
	ItemID   uint32
	Count    int64
	DropRate int64
	MaxStack int64
}

// checkCurrentDungeonQuestMonsterDrops implements the 86JP
// QuestDropService.CheckMonsterDrop path: after a monster dies, every
// active quest's PVF [monster reward item] table is checked for the
// killed monster code. Matched entries are rolled (per-attempt
// percentage) and granted directly to the character backpack, followed
// by an op14 incremental refresh. Quest progress sync for "get item" /
// "seeking" quests is left to the existing pickup/progress owners.
func (s *Service) checkCurrentDungeonQuestMonsterDrops(
	session *gameSession,
	runtime *runtimeDungeonState,
	monsterID int64,
) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || runtime == nil || monsterID <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil || catalog == nil {
		return
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Quest == nil || repositories.CharacterItems == nil {
		return
	}
	questHelperActive := false
	growthQuestDropPercent := int64(0)
	if repositories.Account != nil {
		account, accountFound, accountErr := repositories.Account.Load(ctx, s.accountIDForSession(session))
		questHelperActive = accountErr == nil &&
			accountFound &&
			premium.Active(account, premium.DevilSlotType(premium.DevilSlotQuestHelper), time.Now().UTC())
	}
	if growthEffect, growthActive := s.currentGrowthContractEffect(ctx, s.accountIDForSession(session)); growthActive {
		growthQuestDropPercent = growthEffect.QuestItemDropRatePercent
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	questRecord, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil || !found {
		return
	}
	activeIDs := currentQuestDropActiveIDs(questRecord)
	if len(activeIDs) == 0 {
		return
	}
	dungeonID := int64(runtime.Dungeon.ID)
	difficulty := int64(runtime.Request.Difficulty)
	candidates := currentQuestDropMatchMonster(catalog, activeIDs, dungeonID, difficulty, monsterID)
	if len(candidates) == 0 {
		return
	}
	owner, err := dnfdungeon.NewOwner(repositories)
	if err != nil {
		return
	}
	dropCatalog, catalogErr := s.currentPVFItemCatalog()
	rng := newCurrentDungeonDropLCG(uint32(time.Now().UnixNano()))
	var grantedSlots []int16
	mutation, err := owner.MutateInventory(ctx, dnfdungeon.InventoryMutationCommand{
		CharacterID: characterID,
		UpdatedAt:   time.Now().UTC(),
		Apply: func(inventory *dnfrepo.InventoryRecord) (bool, error) {
			granted := false
			for _, candidate := range candidates {
				held := currentQuestDropHeldCount(*inventory, int64(candidate.ItemID))
				if candidate.MaxStack > 0 && held >= candidate.MaxStack {
					s.logGameEvent(session, "game-dungeon-quest-drop-skipped-max-stack",
						"quest_id", candidate.QuestID,
						"item_id", candidate.ItemID,
						"held", held,
						"max_stack", candidate.MaxStack)
					continue
				}
				dropRate := currentQuestItemDropRate(candidate.DropRate, growthQuestDropPercent, questHelperActive)
				dropCount := currentQuestDropRoll(int(rng.NextUint32()), candidate.Count, dropRate, candidate.MaxStack, held)
				if dropCount <= 0 {
					continue
				}
				if catalogErr != nil || dropCatalog == nil {
					s.logGameEvent(session, "game-dungeon-quest-drop-skipped-catalog",
						"quest_id", candidate.QuestID,
						"item_id", candidate.ItemID,
						"error", catalogErr)
					continue
				}
				definition, resolveErr := dropCatalog.ResolveItem(candidate.ItemID)
				if resolveErr != nil {
					s.logGameEvent(session, "game-dungeon-quest-drop-skipped-resolve",
						"quest_id", candidate.QuestID,
						"item_id", candidate.ItemID,
						"error", resolveErr)
					continue
				}
				slot, grantErr := currentQuestDropGrantStack(inventory, definition, uint32(dropCount))
				if grantErr != nil {
					s.logGameEvent(session, "game-dungeon-quest-drop-skipped-grant",
						"quest_id", candidate.QuestID,
						"item_id", candidate.ItemID,
						"count", dropCount,
						"error", grantErr)
					continue
				}
				key := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)
				stack := inventory.Slots[key]
				entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
				stack.RawEntry = append([]byte(nil), entry.data[:]...)
				inventory.Slots[key] = stack
				grantedSlots = append(grantedSlots, slot)
				granted = true
				s.logGameEvent(session, "game-dungeon-quest-drop-granted",
					"quest_id", candidate.QuestID,
					"monster_id", monsterID,
					"item_id", candidate.ItemID,
					"count", dropCount,
					"base_drop_rate", candidate.DropRate,
					"effective_drop_rate", dropRate,
					"growth_contract_drop_percent", growthQuestDropPercent,
					"quest_helper", questHelperActive,
					"slot", slot,
					"new_total", stack.Count)
			}
			return granted, nil
		},
	})
	if err != nil {
		s.logGameEvent(session, "game-dungeon-quest-drop-save-failed",
			"monster_id", monsterID,
			"error", err)
		return
	}
	if !mutation.Changed {
		return
	}
	inventory := mutation.Inventory
	updates := make([]currentItemListEntry, 0, len(grantedSlots))
	for _, slot := range grantedSlots {
		key := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)
		if stack, stackFound := inventory.Slots[key]; stackFound {
			updates = append(updates, currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack))
		}
	}
	if len(updates) == 0 {
		return
	}
	sortCurrentItemListEntries(updates)
	body := buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, updates)
	if sendErr := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0); sendErr != nil {
		s.logGameEvent(session, "game-dungeon-quest-drop-refresh-failed",
			"monster_id", monsterID,
			"error", sendErr)
		return
	}
	s.logGameEvent(session, "game-dungeon-quest-drop-refresh-send",
		"monster_id", monsterID,
		"entries", len(updates),
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember))
}

// currentQuestHelperDropRate applies the original task-assistant behavior:
// quest-item probability is raised by one fifth, capped at certainty. The
// current EXE has no slot-2 local call site, so this belongs on the server
// roll that owns quest-item drops.
func currentQuestHelperDropRate(base int64, active bool) int64 {
	return currentQuestItemDropRate(base, 0, active)
}

func currentQuestItemDropRate(base int64, growthContractPercent int64, questHelperActive bool) int64 {
	const questHelperPercent int64 = 20
	if base <= 0 {
		return base
	}
	bonusPercent := growthContractPercent
	if bonusPercent < 0 {
		bonusPercent = 0
	}
	if questHelperActive {
		bonusPercent += questHelperPercent
	}
	if bonusPercent <= 0 {
		return base
	}
	if bonusPercent >= 10000 || base > math.MaxInt64/(100+bonusPercent) {
		return 100
	}
	boosted := (base*(100+bonusPercent) + 99) / 100
	if boosted > 100 {
		return 100
	}
	return boosted
}

// currentQuestDropActiveIDs extracts quest IDs whose status is "active"
// from the persisted quest record.
func currentQuestDropActiveIDs(record dnfrepo.QuestRecord) []int64 {
	if len(record.States) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(record.States))
	for questID, state := range record.States {
		if strings.EqualFold(strings.TrimSpace(state.Status), "active") {
			ids = append(ids, questID)
		}
	}
	return ids
}

// currentQuestDropMatchMonster checks every active quest's PVF
// [monster reward item] entries for the killed monster, applying the
// 86JP dungeon/difficulty wildcard rules.
func currentQuestDropMatchMonster(
	catalog *dnfquest.Catalog,
	activeIDs []int64,
	dungeonID int64,
	difficulty int64,
	monsterID int64,
) []currentQuestDropCandidate {
	var candidates []currentQuestDropCandidate
	for _, questID := range activeIDs {
		definition, found := catalog.Find(questID)
		if !found {
			continue
		}
		for _, entry := range definition.MonsterRewardItems {
			if entry.MonsterCode != monsterID {
				continue
			}
			if entry.DungeonID != -1 && entry.DungeonID != dungeonID {
				continue
			}
			if entry.Difficulty >= 0 && entry.Difficulty != difficulty {
				continue
			}
			if entry.ItemID <= 0 || entry.ItemID > math.MaxUint32 || entry.Count <= 0 || entry.DropRate <= 0 {
				continue
			}
			candidates = append(candidates, currentQuestDropCandidate{
				QuestID:  questID,
				ItemID:   uint32(entry.ItemID),
				Count:    entry.Count,
				DropRate: entry.DropRate,
				MaxStack: entry.MaxStack,
			})
		}
		for _, entry := range definition.EnemyRewardItems {
			if entry.EnemyType != 1 || entry.EnemyCode != monsterID {
				continue
			}
			if entry.DungeonID != -1 && entry.DungeonID != dungeonID {
				continue
			}
			if entry.Difficulty >= 0 && entry.Difficulty != difficulty {
				continue
			}
			if entry.ItemID <= 0 || entry.ItemID > math.MaxUint32 || entry.Count <= 0 || entry.DropRate <= 0 {
				continue
			}
			candidates = append(candidates, currentQuestDropCandidate{
				QuestID:  questID,
				ItemID:   uint32(entry.ItemID),
				Count:    entry.Count,
				DropRate: entry.DropRate,
				MaxStack: entry.MaxStack,
			})
		}
	}
	return candidates
}

// currentQuestDropRoll implements the 86JP QuestDropProvider.RollDrop:
// for k in 0..count-1, if rand(100) < dropRate then actual++.
// The result is clamped to maxStack - held.
func currentQuestDropRoll(seed int, count int64, dropRate int64, maxStack int64, held int64) int64 {
	if count <= 0 || dropRate <= 0 {
		return 0
	}
	if maxStack > 0 && held >= maxStack {
		return 0
	}
	var actual int64
	rng := newCurrentDungeonDropLCG(uint32(seed))
	for k := int64(0); k < count; k++ {
		if int64(rng.Next(100)) < dropRate {
			actual++
		}
	}
	if actual <= 0 {
		return 0
	}
	if actual > 999 {
		actual = 999
	}
	if maxStack > 0 && held+actual > maxStack {
		actual = maxStack - held
	}
	if actual < 0 {
		return 0
	}
	return actual
}

// currentQuestDropHeldCount counts how many of itemID the character
// currently holds across all backpack slots.
func currentQuestDropHeldCount(inventory dnfrepo.InventoryRecord, itemID int64) int64 {
	var total int64
	for _, stack := range inventory.Slots {
		if stack.ItemID == itemID {
			total += stack.Count
		}
	}
	return total
}

// currentQuestDropGrantStack adds count of the item to the character
// backpack, merging with an existing stack when possible (86JP
// QuestDropService.TryPickupItemToInventory). It returns the slot
// that received the items.
func currentQuestDropGrantStack(inventory *dnfrepo.InventoryRecord, definition dungeonDropItemDefinition, count uint32) (int16, error) {
	if inventory == nil || count == 0 || definition.ItemID == 0 {
		return 0, errDungeonPickupItemInvalid
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	itemID := int64(definition.ItemID)
	stackLimit := int64(definition.StackLimit)
	if definition.Kind == dungeonDropItemEquipment {
		stackLimit = 1
	}
	// Quest stackables often have no [stack limit] in PVF; default to 999
	// so they merge instead of occupying one slot per drop.
	if definition.Kind == dungeonDropItemStackable && stackLimit <= 0 {
		stackLimit = 999
	}
	// Use the PVF-defined slot range for the item type. Quest items
	// ([quest] stackable) land in slots 177..232 (the quest backpack
	// page), materials in 121..176, etc.
	slotStart := definition.SlotStart
	slotEnd := definition.SlotEnd
	if slotStart <= 0 || slotEnd < slotStart {
		slotStart, slotEnd = 0, dnfrepo.CrystalWarehouseFirstSlot-1
	}
	// Try to merge with an existing stack in the item's slot range.
	if definition.Kind == dungeonDropItemStackable {
		for slot := slotStart; slot <= slotEnd; slot++ {
			key := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)
			stack, exists := inventory.Slots[key]
			if !exists || stack.ItemID != itemID {
				continue
			}
			room := stackLimit - stack.Count
			if room <= 0 {
				continue
			}
			add := int64(count)
			if add > room {
				add = room
			}
			stack.Count += add
			inventory.Slots[key] = stack
			return slot, nil
		}
	}
	// Find the first empty slot in the item's range.
	for slot := slotStart; slot <= slotEnd; slot++ {
		key := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)
		if _, occupied := inventory.Slots[key]; occupied {
			continue
		}
		if _, err := insertCurrentDungeonPickup(inventory, definition, count, slot); err != nil {
			return 0, err
		}
		stack := inventory.Slots[key]
		stack.Extra["source"] = "quest_monster_reward"
		inventory.Slots[key] = stack
		return slot, nil
	}
	return 0, fmt.Errorf("%w: item=%d", errDungeonPickupInventoryFull, definition.ItemID)
}
