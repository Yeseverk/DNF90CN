// 本文件定义 DNF 角色仓储接口、记录和字段保存入口。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// DefaultCharacterSlotLimit is the current NoPack character-selector capacity.
// Keep repository fallback reads aligned with the bridge roster/create owner so
// zero-limit callers cannot miss slot conflicts above the old 16-slot boundary.
const DefaultCharacterSlotLimit = 32

var (
	ErrCharacterIDExists            = errors.New("dnf character id already exists")
	ErrCharacterSlotOccupied        = errors.New("dnf character slot is occupied")
	ErrCharacterCreateMissing       = errors.New("dnf character create repository is missing")
	ErrCharacterSlotMissing         = errors.New("dnf character slot is missing")
	ErrCharacterSlotSwapUnavailable = errors.New("dnf character slot swap repository is unavailable")
)

// CharacterRepository 保存角色基础、数值和位置状态。
type CharacterRepository interface {
	db.Store[CharacterRecord]
	ListByAccount(context.Context, string, int) ([]CharacterRecord, error)
	FindIDByName(context.Context, string) (string, bool, error)
	NextNumericID(context.Context) (int, error)
}

// CharacterCreator 提供新建角色的 insert-only 写入语义。
type CharacterCreator interface {
	CreateCharacter(context.Context, CharacterRecord) error
}

// CharacterSlotSwapper persists a character-select screen slot exchange. It is
// optional so existing test doubles and limited repositories can fall back to a
// load/save implementation.
type CharacterSlotSwapper interface {
	SwapCharacterSlots(context.Context, string, int, int) error
}

// CharacterRecord 是 DNF 角色仓储记录。
type CharacterRecord struct {
	CharacterID string            `json:"character_id"`
	AccountID   string            `json:"account_id"`
	Slot        int               `json:"slot,omitempty"`
	Name        string            `json:"name,omitempty"`
	Job         string            `json:"job,omitempty"`
	Level       int               `json:"level,omitempty"`
	Stats       map[string]int64  `json:"stats,omitempty"`
	Location    CharacterLocation `json:"location,omitempty"`
	Roster      CharacterRoster   `json:"roster,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
}

// CharacterLocation 描述角色下线或当前所在的城镇、频道和副本位置。
type CharacterLocation struct {
	ChannelID int    `json:"channel_id,omitempty"`
	TownID    int64  `json:"town_id,omitempty"`
	DungeonID int64  `json:"dungeon_id,omitempty"`
	RoomID    string `json:"room_id,omitempty"`
}

// CharacterRoster 保存最新 EXE 角色列表 class0/op2 已确认的头部和单角色入口字段。
type CharacterRoster struct {
	Header CharacterRosterHeader `json:"header,omitempty"`
	Entry  CharacterRosterEntry  `json:"entry,omitempty"`
}

// CharacterRosterHeader 对应 sub_200BEA0 mode=2 在角色入口前读取的字段。
type CharacterRosterHeader struct {
	UnkA             int64 `json:"unk_a,omitempty"`
	UnkB             int64 `json:"unk_b,omitempty"`
	TotalOrSlotLimit int64 `json:"total_or_slot_limit,omitempty"`
	UsedOrRemain     int64 `json:"used_or_remain,omitempty"`
	SelectedOrPage   int64 `json:"selected_or_page,omitempty"`
	RosterState      int64 `json:"roster_state,omitempty"`
	PageCount        int64 `json:"page_count,omitempty"`
	RosterFlag       int64 `json:"roster_flag,omitempty"`
	RosterValueA     int64 `json:"roster_value_a,omitempty"`
	RosterValueB     int64 `json:"roster_value_b,omitempty"`
}

// CharacterRosterEntry 对应 sub_200B250 的单角色读取序列。
type CharacterRosterEntry struct {
	ByteA             int64                         `json:"byte_a,omitempty"`
	PackedJobGrow     int64                         `json:"packed_job_grow,omitempty"`
	ByteC             int64                         `json:"byte_c,omitempty"`
	Field2CC          int64                         `json:"field_0x2cc,omitempty"`
	State0            int64                         `json:"state0,omitempty"`
	TimeA             int64                         `json:"time_a,omitempty"`
	TimeB             int64                         `json:"time_b,omitempty"`
	EquipSummary      []CharacterRosterEquipSummary `json:"equip_summary,omitempty"`
	Value0            int64                         `json:"value0,omitempty"`
	Value1            int64                         `json:"value1,omitempty"`
	Value2            int64                         `json:"value2,omitempty"`
	ReservedA         int64                         `json:"reserved_a,omitempty"`
	ReservedB         int64                         `json:"reserved_b,omitempty"`
	LinkedIDBlock     []int64                       `json:"linked_id_block,omitempty"`
	Value3            int64                         `json:"value3,omitempty"`
	ObjectID          int64                         `json:"object_id,omitempty"`
	Flag0Eq1          int64                         `json:"flag0_eq_1,omitempty"`
	SpecialStatusFlag int64                         `json:"special_status_flag,omitempty"`
	Value5            int64                         `json:"value5,omitempty"`
	DisplayFlags      int64                         `json:"display_flags,omitempty"`
	ReservedC         int64                         `json:"reserved_c,omitempty"`
	ReservedD         int64                         `json:"reserved_d,omitempty"`
	Value6            int64                         `json:"value6,omitempty"`
	Flag1Nonzero      int64                         `json:"flag1_nonzero,omitempty"`
	BoolAEq1          int64                         `json:"bool_a_eq_1,omitempty"`
	BoolBEq1          int64                         `json:"bool_b_eq_1,omitempty"`
	BoolCEq1          int64                         `json:"bool_c_eq_1,omitempty"`
	Flag2Nonzero      int64                         `json:"flag2_nonzero,omitempty"`
	Flag3Nonzero      int64                         `json:"flag3_nonzero,omitempty"`
	Flag4Nonzero      int64                         `json:"flag4_nonzero,omitempty"`
	Flag5Nonzero      int64                         `json:"flag5_nonzero,omitempty"`
	Value7            int64                         `json:"value7,omitempty"`
	Flag6Eq1          int64                         `json:"flag6_eq_1,omitempty"`
	Flags             []int64                       `json:"flags,omitempty"`
}

// CharacterRosterEquipSummary 对应 sub_20026C0 读取的角色卡片装备摘要行。
type CharacterRosterEquipSummary struct {
	Slot               int64  `json:"slot,omitempty"`
	ItemIDOrIcon       int64  `json:"item_id_or_icon,omitempty"`
	RawEntry           []byte `json:"raw_entry,omitempty"`
	PackedFlags        int64  `json:"packed_flags,omitempty"`
	OptionalIDOrExpire int64  `json:"optional_id_or_expire,omitempty"`
	AuxValue           int64  `json:"aux_value,omitempty"`
	AuxFlag            int64  `json:"aux_flag,omitempty"`
}

// CharacterField 表示角色记录可局部保存的字段。
type CharacterField string

const (
	CharacterFieldBase     CharacterField = "base"
	CharacterFieldStats    CharacterField = "stats"
	CharacterFieldLocation CharacterField = "location"
	CharacterFieldRoster   CharacterField = "roster"
)

// SaveCharacterFields 保存角色指定字段；底层不支持局部保存时退化为整条保存。
func SaveCharacterFields(ctx context.Context, repo CharacterRepository, record CharacterRecord, fields ...CharacterField) error {
	return db.SaveFields(ctx, repo, record, CharacterFields.Normalize, fields...)
}

// CreateCharacter 只用于创建新角色；实现支持时必须避免唯一槽位冲突时覆盖旧角色。
func CreateCharacter(ctx context.Context, repo CharacterRepository, record CharacterRecord) error {
	if repo == nil {
		return ErrCharacterCreateMissing
	}
	if creator, ok := repo.(CharacterCreator); ok {
		return creator.CreateCharacter(ctx, record)
	}
	return ErrCharacterCreateMissing
}

// SwapCharacterSlots exchanges two visible roster slots for one account.
func SwapCharacterSlots(ctx context.Context, repo CharacterRepository, accountID string, slotA, slotB int) error {
	if repo == nil {
		return ErrCharacterCreateMissing
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return db.ErrRecordKeyRequired
	}
	if slotA == slotB {
		return nil
	}
	if slotA < 0 || slotA >= DefaultCharacterSlotLimit || slotB < 0 || slotB >= DefaultCharacterSlotLimit {
		return ErrCharacterSlotMissing
	}
	if swapper, ok := repo.(CharacterSlotSwapper); ok {
		return swapper.SwapCharacterSlots(ctx, accountID, slotA, slotB)
	}
	records, err := repo.ListByAccount(ctx, accountID, DefaultCharacterSlotLimit)
	if err != nil {
		return err
	}
	var left, right *CharacterRecord
	for idx := range records {
		if records[idx].Slot == slotA {
			left = &records[idx]
		}
		if records[idx].Slot == slotB {
			right = &records[idx]
		}
	}
	if left == nil || right == nil {
		return nil
	}
	left.Slot, right.Slot = slotB, slotA
	if err := SaveCharacterFields(ctx, repo, *left, CharacterFieldBase); err != nil {
		return err
	}
	if err := SaveCharacterFields(ctx, repo, *right, CharacterFieldBase); err != nil {
		left.Slot = slotA
		_ = SaveCharacterFields(context.Background(), repo, *left, CharacterFieldBase)
		return err
	}
	return nil
}

// CloneCharacter 拷贝角色记录，避免 Stats map 污染在线角色快照或缓存。
func CloneCharacter(record CharacterRecord) CharacterRecord {
	record.Stats = cloneInt64Map(record.Stats)
	record.Roster = cloneCharacterRoster(record.Roster)
	return record
}

func CharacterKey(record CharacterRecord) string {
	return strings.TrimSpace(record.CharacterID)
}
