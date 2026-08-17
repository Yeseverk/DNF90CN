package dungeon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const tutorialRewardMarkerValue = int64(1)

var (
	ErrTutorialRewardInvalid = errors.New("dungeon tutorial reward is invalid")
	ErrTutorialInventoryFull = errors.New("dungeon tutorial reward inventory is full")
	ErrTutorialAssetsMissing = errors.New("dungeon tutorial reward assets are unavailable")
)

type TutorialItemReward struct {
	Progress      uint32
	ItemID        uint32
	Count         uint32
	Consumable    bool
	StackLimit    int64
	SlotStart     int16
	SlotEnd       int16
	ExpireAt      time.Time
	PVFPath       string
	StackableType string
}

type TutorialRewardCommand struct {
	CharacterID string
	Progress    uint32
	Rewards     []TutorialItemReward
	UpdatedAt   time.Time
	Project     StackProjector
}

type TutorialRewardRow struct {
	Slot   uint16
	ItemID uint32
	Count  uint32
}

type TutorialRewardResult struct {
	Granted bool
	Rows    []TutorialRewardRow
}

func (o *Owner) GrantTutorialReward(ctx context.Context, cmd TutorialRewardCommand) (TutorialRewardResult, error) {
	if o == nil || o.assets == nil || strings.TrimSpace(cmd.CharacterID) == "" ||
		cmd.Progress == 0 || len(cmd.Rewards) == 0 {
		return TutorialRewardResult{}, ErrTutorialAssetsMissing
	}
	ctx = contextOrBackground(ctx)
	now := updatedAtOrNow(cmd.UpdatedAt)
	marker := TutorialRewardMarker(cmd.Progress)

	var result TutorialRewardResult
	err := o.assets.WithinCharacterAssets(ctx, cmd.CharacterID, func(
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		character, found, err := characters.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrTutorialAssetsMissing
		}
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64)
		}
		if character.Stats[marker] == tutorialRewardMarkerValue {
			return nil
		}

		inventory, found, err := inventories.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			inventory = dnfrepo.InventoryRecord{CharacterID: cmd.CharacterID}
		}
		inventory = dnfrepo.CloneInventory(inventory)
		if inventory.Slots == nil {
			inventory.Slots = make(map[string]dnfrepo.ItemStack)
		}
		rows := make([]TutorialRewardRow, 0, len(cmd.Rewards))
		for _, reward := range cmd.Rewards {
			if reward.Progress != cmd.Progress {
				return fmt.Errorf(
					"%w: requested_progress=%d row_progress=%d",
					ErrTutorialRewardInvalid,
					cmd.Progress,
					reward.Progress,
				)
			}
			slot, err := PlaceTutorialReward(&inventory, cmd.Progress, reward, cmd.Project)
			if err != nil {
				return err
			}
			rows = append(rows, TutorialRewardRow{
				Slot: slot, ItemID: reward.ItemID, Count: reward.Count,
			})
		}
		character.Stats[marker] = tutorialRewardMarkerValue
		character.UpdatedAt = now
		inventory.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		result.Granted = true
		result.Rows = rows
		return nil
	})
	if err != nil {
		return TutorialRewardResult{}, err
	}
	return result, nil
}

func TutorialRewardMarker(progress uint32) string {
	return "tutorial_reward_progress_" + strconv.FormatUint(uint64(progress), 10)
}

func PlaceTutorialReward(
	record *dnfrepo.InventoryRecord,
	progress uint32,
	reward TutorialItemReward,
	project StackProjector,
) (uint16, error) {
	if record == nil || reward.Progress != progress || reward.ItemID == 0 || reward.Count == 0 ||
		!reward.Consumable || reward.SlotStart < 0 || reward.SlotEnd < reward.SlotStart {
		return 0, ErrTutorialRewardInvalid
	}
	if reward.StackLimit > 0 && uint64(reward.Count) > uint64(reward.StackLimit) {
		return 0, ErrTutorialRewardInvalid
	}
	if !reward.ExpireAt.IsZero() && project == nil {
		return 0, ErrStackProjectorRequired
	}
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if slot, ok, err := stackTutorialRewardInRange(record, reward, project, 3, 8); err != nil {
		return 0, err
	} else if ok {
		return uint16(slot), nil
	}
	if slot, ok := firstEmptyMainSlot(record, 3, 8); ok {
		return insertTutorialReward(record, slot, progress, reward, project)
	}
	if slot, ok, err := stackTutorialRewardInRange(
		record,
		reward,
		project,
		reward.SlotStart,
		reward.SlotEnd,
	); err != nil {
		return 0, err
	} else if ok {
		return uint16(slot), nil
	}
	if slot, ok := firstEmptyMainSlot(record, reward.SlotStart, reward.SlotEnd); ok {
		return insertTutorialReward(record, slot, progress, reward, project)
	}
	return 0, fmt.Errorf(
		"%w: progress=%d item=%d quick=3..8 fallback=%d..%d",
		ErrTutorialInventoryFull,
		progress,
		reward.ItemID,
		reward.SlotStart,
		reward.SlotEnd,
	)
}

func stackTutorialRewardInRange(
	record *dnfrepo.InventoryRecord,
	reward TutorialItemReward,
	project StackProjector,
	start int16,
	end int16,
) (int16, bool, error) {
	for slot := start; slot <= end; slot++ {
		key := mainSlotKey(uint16(slot))
		stack, ok := record.Slots[key]
		if !ok || !canStackTutorialReward(stack, reward) {
			continue
		}
		if !reward.ExpireAt.IsZero() {
			var err error
			stack, err = project(stack, reward.ExpireAt)
			if err != nil {
				return 0, false, err
			}
		}
		stack.Count += int64(reward.Count)
		record.Slots[key] = stack
		return slot, true, nil
	}
	return 0, false, nil
}

func canStackTutorialReward(stack dnfrepo.ItemStack, reward TutorialItemReward) bool {
	if stack.ItemID != int64(reward.ItemID) || stack.Bind || stack.Count < 0 {
		return false
	}
	if reward.ExpireAt.IsZero() && !stack.ExpireAt.IsZero() {
		return false
	}
	amount := int64(reward.Count)
	if stack.Count > math.MaxInt64-amount {
		return false
	}
	return reward.StackLimit <= 0 || stack.Count <= reward.StackLimit-amount
}

func insertTutorialReward(
	record *dnfrepo.InventoryRecord,
	slot int16,
	progress uint32,
	reward TutorialItemReward,
	project StackProjector,
) (uint16, error) {
	if slot <= 0 {
		return 0, ErrTutorialRewardInvalid
	}
	extra := map[string]string{
		"source":          "tutorial_pvf_reward",
		"reward_progress": strconv.FormatUint(uint64(progress), 10),
		"item_kind":       "stackable",
		"pvf_path":        reward.PVFPath,
		"stackable_type":  reward.StackableType,
	}
	if reward.StackLimit > 0 {
		extra["stack_limit"] = strconv.FormatInt(reward.StackLimit, 10)
	}
	stack := dnfrepo.ItemStack{
		ItemID: int64(reward.ItemID),
		Count:  int64(reward.Count),
		Extra:  extra,
	}
	if !reward.ExpireAt.IsZero() {
		var err error
		stack, err = project(stack, reward.ExpireAt)
		if err != nil {
			return 0, err
		}
	}
	record.Slots[mainSlotKey(uint16(slot))] = stack
	return uint16(slot), nil
}
