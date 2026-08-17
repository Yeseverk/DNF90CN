// 本文件提供副本命令的只读 owner 预检。
// 当前只读取角色和背包快照，房间、掉落、结算和进场门闸闭合前不会写库或开放成功 ACK。
package dungeoncmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable       = errors.New("dungeon owner unavailable")
	ErrCharacterRequired      = errors.New("selected character id required")
	ErrCharacterNotFound      = errors.New("character record not found")
	ErrInventoryNotFound      = errors.New("inventory record not found")
	ErrRuntimeSessionRequired = errors.New("active dungeon runtime session required")
	ErrCombatAuthorityNeeded  = errors.New("server combat authority required")
)

// Owner 是副本命令进入真实场景 owner 前的只读校验边界。
type Owner struct {
	characters dnfrepo.CharacterRepository
	inventory  dnfrepo.InventoryRepository
}

// PlanResult 描述副本命令当前能从玩家读模型确认到的上下文。
type PlanResult struct {
	AccountID          string
	CharacterID        string
	Operation          string
	Level              int
	Fatigue            int64
	TownID             int64
	DungeonID          int64
	RoomID             string
	InventoryKnown     bool
	InventorySlotCount int
	WarehouseSlotCount int
	RequestedDungeonID uint32
	Difficulty         byte
	DropObjectKey      uint32
	NextX              byte
	NextY              byte
	TutorialProgress   uint32
	TutorialCommit     byte
	RawLen             int
}

// NewOwner 创建副本命令 owner；缺少角色或背包仓储时拒绝预检。
func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Character == nil || repos.Inventory == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{characters: repos.Character, inventory: repos.Inventory}, nil
}

// Plan 只读取角色、位置和背包快照；真实房间状态、掉落和结算写路径仍等待后续 owner。
func (o *Owner) Plan(ctx context.Context, cmd Command) (PlanResult, error) {
	if o == nil || o.characters == nil || o.inventory == nil {
		return PlanResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return PlanResult{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	character, ok, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return PlanResult{}, err
	}
	if !ok {
		return PlanResult{}, ErrCharacterNotFound
	}
	character = dnfrepo.CloneCharacter(character)

	// The repository owner cannot prove live room ownership. Keep these paths
	// blocked before inventory/reward reads or any future mutation is attempted.
	switch cmd.Operation {
	case "move_map":
		return PlanResult{}, fmt.Errorf(
			"%w: current op45 success reads two coordinates, but the target-room actor/object packet chain is not proven",
			ErrRuntimeSessionRequired,
		)
	case "die_monster":
		return PlanResult{}, fmt.Errorf(
			"%w: a client op39 report is not a server-owned defeat or reward decision",
			ErrCombatAuthorityNeeded,
		)
	}

	inventory, inventoryKnown, err := o.inventory.Load(ctx, characterID)
	if err != nil {
		return PlanResult{}, err
	}
	if cmd.Operation == "get_item" && !inventoryKnown {
		return PlanResult{}, ErrInventoryNotFound
	}
	inventory = dnfrepo.CloneInventory(inventory)

	return PlanResult{
		AccountID:          character.AccountID,
		CharacterID:        characterID,
		Operation:          cmd.Operation,
		Level:              character.Level,
		Fatigue:            statValue(character.Stats, "fatigue", "fp", "疲劳"),
		TownID:             character.Location.TownID,
		DungeonID:          character.Location.DungeonID,
		RoomID:             character.Location.RoomID,
		InventoryKnown:     inventoryKnown,
		InventorySlotCount: len(inventory.Slots),
		WarehouseSlotCount: len(inventory.Warehouse),
		RequestedDungeonID: cmd.DungeonID,
		Difficulty:         cmd.Difficulty,
		DropObjectKey:      cmd.DropObjectKey,
		NextX:              cmd.NextX,
		NextY:              cmd.NextY,
		TutorialProgress:   cmd.TutorialProgress,
		TutorialCommit:     cmd.TutorialCommit,
		RawLen:             cmd.RawLen,
	}, nil
}

func statValue(stats map[string]int64, names ...string) int64 {
	for _, name := range names {
		if value, ok := stats[name]; ok {
			return value
		}
	}
	return 0
}

func planError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", strings.TrimSpace(operation), err)
}
