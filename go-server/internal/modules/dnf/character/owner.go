// 本文件提供角色链 fallback 的只读 owner 预检。
// 登录、选角、USERINFO 和角色列表仍由 dnfbridge 专门链路闭合，这里不写角色也不开放成功 ACK。
package character

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("character owner unavailable")
	ErrCharacterRequired = errors.New("selected character id required")
	ErrCharacterNotFound = errors.New("character record not found")
	ErrAccountMismatch   = errors.New("character account mismatch")
)

// Owner 是角色链 fallback 的只读查询边界。
type Owner struct {
	characters dnfrepo.CharacterRepository
}

// PlanResult 描述角色请求当前能从角色仓储确认到的上下文。
type PlanResult struct {
	AccountID           string
	CharacterID         string
	CharacterKnown      bool
	Name                string
	Slot                int
	Job                 string
	Level               int
	RosterCount         int
	NameTaken           bool
	NameOwnerID         string
	RequestedName       string
	RequestedJob        byte
	RequestedSlot       int
	SelectedOrRequested uint16
}

// NewOwner 创建角色 owner；缺少角色仓储时拒绝预检。
func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Character == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{characters: repos.Character}, nil
}

// Plan 只读取角色名索引、账号角色列表或当前角色快照，不承担真实登录/选角回包。
func (o *Owner) Plan(ctx context.Context, cmd Command) (PlanResult, error) {
	if o == nil || o.characters == nil {
		return PlanResult{}, ErrOwnerUnavailable
	}
	result := PlanResult{
		AccountID:           strings.TrimSpace(cmd.AccountID),
		RequestedName:       strings.TrimSpace(cmd.Name),
		RequestedJob:        cmd.Job,
		RequestedSlot:       int(cmd.Slot),
		SelectedOrRequested: cmd.SlotOrCharacterID,
	}
	if result.AccountID != "" {
		roster, err := o.characters.ListByAccount(ctx, result.AccountID, 32)
		if err != nil {
			return PlanResult{}, err
		}
		result.RosterCount = len(roster)
	}

	switch cmd.Operation {
	case "select_character":
		record, ok, err := o.findSelected(ctx, cmd)
		if err != nil {
			return PlanResult{}, err
		}
		if !ok {
			return PlanResult{}, ErrCharacterNotFound
		}
		fillCharacterResult(&result, record)
	case "return_select_character":
		// 返回角色选择只需要账号角色列表上下文，真实 op2 刷新仍由专门链路生成。
	case "get_userinfo":
		record, ok, err := o.findSelected(ctx, cmd)
		if err != nil {
			return PlanResult{}, err
		}
		if !ok {
			return PlanResult{}, ErrCharacterNotFound
		}
		fillCharacterResult(&result, record)
	case "create_character", "check_double_character_name":
		if result.RequestedName != "" {
			id, taken, err := o.characters.FindIDByName(ctx, result.RequestedName)
			if err != nil {
				return PlanResult{}, err
			}
			result.NameTaken = taken
			result.NameOwnerID = id
		}
	case "delete_character":
		if result.RequestedName != "" {
			id, taken, err := o.characters.FindIDByName(ctx, result.RequestedName)
			if err != nil {
				return PlanResult{}, err
			}
			result.NameTaken = taken
			result.NameOwnerID = id
			if taken {
				record, ok, err := o.characters.Load(ctx, id)
				if err != nil {
					return PlanResult{}, err
				}
				if ok {
					fillCharacterResult(&result, record)
				}
			}
		}
	}
	return result, nil
}

func (o *Owner) findSelected(ctx context.Context, cmd Command) (dnfrepo.CharacterRecord, bool, error) {
	accountID := strings.TrimSpace(cmd.AccountID)
	if cmd.SelectedCharacterID != 0 {
		record, ok, err := o.loadByNumericID(ctx, cmd.SelectedCharacterID)
		if err != nil || !ok {
			return record, ok, err
		}
		if !matchesAccount(record, accountID) {
			return dnfrepo.CharacterRecord{}, false, ErrAccountMismatch
		}
		return record, true, nil
	}
	if cmd.SlotOrCharacterID != 0 {
		if record, ok, err := o.loadByNumericID(ctx, cmd.SlotOrCharacterID); err != nil || (ok && matchesAccount(record, accountID)) {
			return record, ok, err
		}
	}
	if accountID == "" {
		return dnfrepo.CharacterRecord{}, false, ErrCharacterRequired
	}
	roster, err := o.characters.ListByAccount(ctx, accountID, 32)
	if err != nil {
		return dnfrepo.CharacterRecord{}, false, err
	}
	requestedSlot := int(cmd.SlotOrCharacterID)
	for _, record := range roster {
		if record.Slot == requestedSlot {
			return dnfrepo.CloneCharacter(record), true, nil
		}
	}
	return dnfrepo.CharacterRecord{}, false, nil
}

func (o *Owner) loadByNumericID(ctx context.Context, id uint16) (dnfrepo.CharacterRecord, bool, error) {
	key := strconv.FormatUint(uint64(id), 10)
	record, ok, err := o.characters.Load(ctx, key)
	if err != nil || !ok {
		return dnfrepo.CharacterRecord{}, ok, err
	}
	return dnfrepo.CloneCharacter(record), true, nil
}

func matchesAccount(record dnfrepo.CharacterRecord, accountID string) bool {
	return accountID == "" || strings.TrimSpace(record.AccountID) == accountID
}

func fillCharacterResult(result *PlanResult, record dnfrepo.CharacterRecord) {
	record = dnfrepo.CloneCharacter(record)
	result.CharacterID = record.CharacterID
	result.CharacterKnown = true
	result.AccountID = record.AccountID
	result.Name = record.Name
	result.Slot = record.Slot
	result.Job = record.Job
	result.Level = record.Level
}

func planError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", strings.TrimSpace(operation), err)
}
