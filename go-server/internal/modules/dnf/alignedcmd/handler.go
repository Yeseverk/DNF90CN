// 本文件定义 DNF 已对齐命令的模块化分流结果。
// 这里只承载协议分流和回包描述，不持有具体玩法状态。
package alignedcmd

import (
	"context"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
)

// Request 是 dnfbridge 交给 DNF 业务模块的最小上下文。
// Body 是当前包体副本，模块不得在原地修改调用方缓冲区。
type Request struct {
	Command                    byte
	CommandKnown               bool
	Opcode                     uint16
	Body                       []byte
	AccountID                  string
	SelectedCharacterID        uint16
	Repositories               dnfrepo.Group
	EquipmentPlacement         EquipmentPlacementValidator
	NameTagChecker             func(itemID uint32) bool
	PetHatchResolver           PetHatchResolver
	EnchantBeadResolver        EnchantBeadResolver
	AmplifyItemResolver        AmplifyItemResolver
	RandomOptionResolver       RandomOptionResolver
	UpgradeTicketResolver      UpgradeTicketResolver
	UpgradePolicyResolver      UpgradePolicyResolver
	MagicBoxResolver           MagicBoxResolver
	MagicBoxRewardItemResolver MagicBoxRewardItemResolver
	RandomRewardItemResolver   RandomRewardItemResolver
	PremiumContractResolver    PremiumContractResolver
	DamageFontResolver         DamageFontResolver
	DamageFontNow              time.Time
	RepairCostResolver         RepairCostResolver
	MailboxItemResolver        MailboxItemResolver
	SkillCatalog               *dnfskill.Table
	InitialSkillLevels         map[uint16]int
	SkillPointBaseline         *dnfrepo.SkillPointState
	Party                      *PartyState
}

// EquipmentPlacementRequest is the dependency-neutral bridge contract used
// by the inventory module before an item enters a worn equipment slot.
type EquipmentPlacementRequest struct {
	CharacterID     string
	ItemID          int64
	SourceListType  byte
	SourceSlotIndex int16
	TargetSlotIndex int16
}

// EquipmentPlacementValidator keeps current-PVF access in dnfbridge while
// allowing the equipment owner to enforce the check inside its item UOW.
type EquipmentPlacementValidator func(context.Context, EquipmentPlacementRequest) error

// PetHatchResolution is the dependency-neutral runtime-PVF result passed from
// dnfbridge into the pet domain. Keeping this DTO here avoids an alignedcmd ->
// pet import cycle while still preventing request-body or inventory-Extra
// values from becoming authoritative hatch mappings.
type PetHatchResolution struct {
	EggItemID      int64
	HatchedItemID  int64
	EggPVFPath     string
	HatchedPVFPath string
	MinimumLevel   int
}

// PetHatchResolver resolves one real inventory egg item through the active
// runtime PVF. A nil resolver means that the pet mutation must fail closed.
type PetHatchResolver func(eggItemID int64) (PetHatchResolution, error)

// EnchantBeadResolution is the dependency-neutral runtime-PVF result passed
// from dnfbridge into the inventory domain for one enchant-by-bead attempt.
// CardItemID is the monster-card/enchant item the bead carries; zero means the
// bead template carries no enchant card and the request must be rejected as an
// invalid bead. UpgradeCounts is the card's allowed enchant-upgrade table; an
// empty table means only upgrade count zero is accepted.
type EnchantBeadResolution struct {
	CardItemID            int64
	CardPVFPath           string
	TargetWhitelist       []int64
	AllowedEquipmentTypes []string
	UpgradeCounts         []int64
	TargetEquipmentType   string
	TargetKind            string
}

// EnchantBeadResolver resolves the bead and target templates through the
// active runtime PVF. A nil resolver means the enchant mutation must fail
// closed instead of trusting request or inventory-Extra metadata.
type EnchantBeadResolver func(beadItemID int64, targetItemID int64) (EnchantBeadResolution, error)

// AmplifyWeightedLevel is one runtime-PVF [amplification random value]
// level/weight pair carried by a specific Pure Gold material.
type AmplifyWeightedLevel struct {
	Level  byte
	Weight int64
}

// AmplifyItemResolution is the dependency-neutral runtime-PVF result used by
// class1/op204 (purify/clear) and class1/op205 (invest/twist/Pure Gold).
// Option values follow etc/amplifyitem.etc: 1..4 are the four concrete
// amplification attributes and 5 means the client-selected [all] family.
// InitialValues is keyed by the final client amplification attribute 1..4.
type AmplifyItemResolution struct {
	TargetKind            string
	TargetPVFPath         string
	TargetMinimumLevel    int64
	TargetRarity          int64
	EquipLevelConst       int64
	PurifyMaterialCount   int64
	ClearMaterialCount    int64
	InvestOption          byte
	InvestMaterialCount   int64
	ReinvestOption        byte
	ReinvestMaterialCount int64
	PureGoldOption        byte
	PureGoldMaterialCount int64
	InitialValues         map[byte]uint16
	PureGoldLevels        []AmplifyWeightedLevel
	MaterialPVFPath       string
}

// AmplifyItemResolver resolves the real material and target templates through
// the active runtime PVF. A nil resolver means the mutation must fail closed.
type AmplifyItemResolver func(materialItemID int64, targetItemID int64) (AmplifyItemResolution, error)

// RandomOptionWeightedQuantity is one runtime-PVF random-option count roll.
type RandomOptionWeightedQuantity struct {
	Quantity byte
	Weight   int64
}

// RandomOptionCandidate is one weighted, level-resolved magic-seal option.
// Type/Value1/Value2 map directly to the current NoPack raw item fields.
type RandomOptionCandidate struct {
	Type   byte
	Value1 byte
	Value2 byte
	Weight int64
}

// RandomOptionResolution is the complete runtime-PVF policy for one target.
// InitialGroups and ModifiedGroups contain the three equipment-specific option
// pools selected by optiongroupselection.etc.
type RandomOptionResolution struct {
	TargetKind           string
	TargetPVFPath        string
	TargetEquipmentKey   string
	TargetMinimumLevel   int64
	TargetRarity         int64
	Eligible             bool
	QuantityWeights      []RandomOptionWeightedQuantity
	InitialGroups        [][]RandomOptionCandidate
	ModifiedGroups       [][]RandomOptionCandidate
	BreakSealGoldCost    int64
	ModificationGoldCost int64
}

// RandomOptionResolver resolves the real target template through the active
// runtime PVF. Nil or incomplete results make the mutation fail closed.
type RandomOptionResolver func(targetItemID int64) (RandomOptionResolution, error)

// UpgradeTicketResolution is the dependency-neutral runtime-PVF result passed
// from dnfbridge into the inventory domain for one op50 ticket-scene upgrade
// attempt. TicketMode is "reinforce" or "amplify" when the material template
// carries [equipment (amplify )?reinforcement ticket]; an empty TicketMode
// means the material is not an upgrade ticket and the request must stay on
// the pending normal-reinforcement path. SuccessWeight is the ticket's
// success chance out of 100000 (percent*1000). TicketRandom marks the
// [enchant random] multi-candidate family, currently unsupported.
type UpgradeTicketResolution struct {
	TicketMode             string
	TicketRandom           bool
	TargetLevel            int64
	SuccessWeight          int64
	TargetUpgradeForbidden bool
	TargetEquipmentType    string
	TargetKind             string
	TicketPVFPath          string
}

// UpgradeTicketResolver resolves the material and target templates through
// the active runtime PVF. A nil resolver means the ticket mutation must fail
// closed instead of trusting request or inventory-Extra metadata.
type UpgradeTicketResolver func(materialItemID int64, targetItemID int64) (UpgradeTicketResolution, error)

// UpgradePolicyResolution carries the PVF-derived success/penalty parameters
// for one normal NPC upgrade attempt (non-ticket scene).
type UpgradePolicyResolution struct {
	SuccessWeight      int // out of 100000
	PenaltyType        int // 0=no change, 1=downgrade, 3=destroy
	MaterialItemID     int
	MaterialCount      int
	DestroyBonusItemID int
	DestroyBonusCount  int
	NoticeLevel        int // announcement threshold (-1 = disabled)
}

// UpgradePolicyResolver resolves the PVF upgrade table row for the given mode
// and current upgrade level. A nil resolver means guaranteed success (backward
// compatible with pre-PVF-table behavior).
type UpgradePolicyResolver func(mode string, currentLevel int) (UpgradePolicyResolution, error)

// MagicBoxRewardEntry is one weighted candidate inside a reward group.
type MagicBoxRewardEntry struct {
	ItemID int64
	Weight int64
	Count  int64
}

// MagicBoxRewardGroup is one independently-drawn reward pool; each group
// yields DrawCount weighted picks per open (86JP RollBoosterRewards).
type MagicBoxRewardGroup struct {
	DrawCount int64
	Entries   []MagicBoxRewardEntry
}

// MagicBoxResolution is the dependency-neutral runtime-PVF result passed from
// dnfbridge into the package domain for one magic-box open. Kind is "random"
// for weighted pools ([random upgradable legacy] RANDOMBOX groups and the
// [booster]/[cera booster]/[booster random] booster-info family), "package"
// for grant-all [cera package], "unsupported" for client-selected families,
// and "" when the template is not an openable box. MaterialItemID is the
// per-open material requirement (0 when the box opens free); the box itself
// is never reported as a material.
type MagicBoxResolution struct {
	Kind                string
	Groups              []MagicBoxRewardGroup
	PackageItems        []MagicBoxRewardEntry
	MaterialItemID      int64
	MaterialCountPerUse int64
	BoxPVFPath          string
}

// MagicBoxResolver resolves one box template through the active runtime PVF.
// A nil resolver means the open must fail closed instead of trusting request
// or inventory-Extra metadata.
type MagicBoxResolver func(boxItemID int64) (MagicBoxResolution, error)

// MagicBoxRewardItem carries the grant metadata of one resolved reward
// template: inventory kind, stack limit (0 = unlimited), slot range, the seal
// flag derived from its PVF [attach type], its document path, and the absolute
// expiration resolved once from a relative PVF [usable period] at grant time.
type MagicBoxRewardItem struct {
	ItemID           int64
	Kind             string
	TargetListType   byte
	EquipmentType    string
	StackLimit       int64
	SlotStart        int16
	SlotEnd          int16
	Seal             bool
	PVFPath          string
	ExpireAt         time.Time
	UsablePeriodDays int64
	Durability       uint16
}

// MagicBoxRewardItemResolver resolves one reward template through the active
// runtime PVF for the durable grant step.
type MagicBoxRewardItemResolver func(itemID int64) (MagicBoxRewardItem, error)

// RandomRewardItemOutcome is one weighted [chn random image percent] result.
// A zero Reward.ItemID is the PVF's no-item visual-effect outcome.
type RandomRewardItemOutcome struct {
	Weight int64
	Reward MagicBoxRewardItem
}

// RandomRewardItemResolution is the runtime-PVF authorization for a normal
// op44 consumable with [stackable type] [random reward item]. The source is
// always consumed; one weighted outcome may add one repository-backed item.
type RandomRewardItemResolution struct {
	SourceItemID  int64
	SourcePVFPath string
	StackableType string
	Outcomes      []RandomRewardItemOutcome
}

// RandomRewardItemResolver keeps the random-reward table and reward template
// metadata in dnfbridge, rather than trusting mutable inventory Extra fields.
// A zero SourceItemID means the candidate is not this consumable family.
type RandomRewardItemResolver func(itemID int64) (RandomRewardItemResolution, error)

// PremiumContractResolution is the dependency-neutral runtime-PVF result for
// one contract item activation: the premium type and its duration in seconds,
// read from premiumlist_new.etc [item]/[term] entries only (its [target item]
// metadata is never an activation key).
type PremiumContractResolution struct {
	ItemID          int64
	PremiumType     int64
	DurationSeconds int64
}

// PremiumContractResolver resolves one candidate item through the active
// runtime PVF. A zero PremiumType means the item is not a contract item and
// the caller must fall back to the ordinary use-stackable path.
type PremiumContractResolver func(itemID int64) (PremiumContractResolution, error)

// DamageFontExpirationMode describes the expiration contract carried by a
// runtime-PVF [add damage font skin] item.
type DamageFontExpirationMode byte

const (
	DamageFontExpirationUnknown DamageFontExpirationMode = iota
	DamageFontExpirationPeriod
	DamageFontExpirationFixed
	DamageFontExpirationUnlimited
)

// DamageFontResolution is the runtime-PVF authority for one damage-font item.
// A zero FontIndex or invalid resolution must not consume the source item.
type DamageFontResolution struct {
	Valid           bool
	PVFPath         string
	ActionType      string
	FontIndex       uint16
	ExpirationMode  DamageFontExpirationMode
	PeriodDays      int64
	FixedExpiration time.Time
}

// DamageFontResolver resolves one inventory source through the active PVF.
type DamageFontResolver func(itemID int64) (DamageFontResolution, error)

// RepairCostEvidence is the dependency-neutral runtime-PVF repair evidence
// for one equipment item, matching the 86JP EquipmentRepairPriceProvider
// inputs. MaxDurability <= 0 means the item is not repairable. The rates come
// from equipment/pricetable.tbl ([repair cost], [quick repair cost rate]) and
// etc/upgrade.etc [repair cost rate by upgrade level]; EquipmentType is the
// normalized PVF [equipment type] token used for the repair-all eligibility
// filter.
type RepairCostEvidence struct {
	EquipmentType   string
	MaxDurability   int64
	RepairPrice     int64
	Grade           int64
	RepairCostRate  float64
	QuickRepairRate float64
	UpgradeRates    []float64
}

// RepairCostResolver resolves one candidate equipment item through the active
// runtime PVF. A nil resolver means the repair cost mutation must fail closed
// instead of trusting request or inventory-Extra metadata.
type RepairCostResolver func(itemID int64) (RepairCostEvidence, error)

// MailboxItemResolution is the current-PVF evidence used by the exact
// NoPack mailbox flow. Price is the item's [price] value; Kind and
// EquipmentType preserve the PVF type needed to frame a mailbox attachment
// without treating mutable inventory Extra data as the type authority.
// SlotStart/SlotEnd and StackLimit are used when an attachment is claimed:
// they put the item in its real main-inventory tab rather than treating the
// op13 main-list expansion marker as a physical slot count.
type MailboxItemResolution struct {
	Price         int64
	PVFPath       string
	Kind          string
	EquipmentType string
	SlotStart     int16
	SlotEnd       int16
	StackLimit    int64
}

// MailboxItemResolver resolves each durable attachment template through the
// active runtime PVF. A nil resolver makes attachment sends fail closed.
type MailboxItemResolver func(itemID uint32) (MailboxItemResolution, error)

// PartyState 保存当前已经验证的单人队伍会话字段。
// 它只属于 game 连接临时态，真正多人队伍 owner 接入前不做持久化。
type PartyState struct {
	PartyID          int
	IsLeader         bool
	UserID           uint16
	UserState        byte
	Members          []PartyMemberState
	RequestPrefix0   byte
	RequestPrefix1   byte
	NameBytes        []byte
	MemberSelectCode byte
	MaxMembers       byte
	SelectionID      uint32
	SelectionCode    byte
	SelectionValue   uint16
	RecruitFlag      byte
	TargetMode       byte
	TargetDungeonID  uint16
	ReserveLeaveFlag byte
}

// PartyMemberState 是已验证队伍包里能安全写出的队员槽位状态。
type PartyMemberState struct {
	UserID    uint16
	UserState byte
	HPPercent byte
	MPPercent byte
}

// Result 表示模块化分流后的处理结果。
// ResponseAllowed=false 表示证据不足或被阻断，bridge 不能伪造成功 ACK。
type Result struct {
	Decision        Decision
	Handled         bool
	ResponseAllowed bool
	Operation       string
	Reason          string
	UpperResponses  []UpperResponse
	// MailboxAlarmRecipientID asks the bridge to deliver the exact class0/0x63
	// mailbox alarm to this already-online character after the sender's ACK and
	// its local post-actions are fully written. Zero means no cross-session send.
	MailboxAlarmRecipientID uint16
	// ItemSlotRefreshes names only the durable item rows changed by this
	// command. The bridge reloads and projects these slots through the current
	// EXE's incremental item-update protocol after every ACK succeeds.
	ItemSlotRefreshes []ItemSlotRefresh
	PostActions       []PostAction
}

type ItemSlotRefresh struct {
	ListType  byte
	SlotIndex int16
}

// PostAction describes a bridge-owned state refresh that must run only after
// every durable owner mutation and protocol ACK in UpperResponses succeeds.
// Business modules select the action; the bridge remains responsible for the
// current EXE-specific packet body and session context.
type PostAction string

const (
	PostActionRefreshSelectedActorAppearance PostAction = "refresh_selected_actor_appearance"
	PostActionRefreshSelectedItemContainers  PostAction = "refresh_selected_item_containers"
	// Equipment slot item rows are list 3.  They carry mutable equipped-item
	// instance state (for example socket/emblem tail bytes) that mode0
	// appearance refreshes do not reliably update.
	PostActionRefreshSelectedEquipmentSlots PostAction = "refresh_selected_equipment_slots"
	// Account cargo is list 12.  It must be rebuilt by dnfbridge from the
	// authenticated account record and account inventory, never by replaying a
	// captured cargo body or reading the selected character's settings.
	PostActionRefreshSelectedAccountCargo PostAction = "refresh_selected_account_cargo"
	PostActionRefreshSelectedActorSkills  PostAction = "refresh_selected_actor_skills"
	// Creature state is the current scene class0/op105 raw table. The bridge
	// sends it only after the selected actor is scene-ready; it is not part of
	// the class1/op19 success body.
	PostActionRefreshSelectedCreatureState PostAction = "refresh_selected_creature_state"
	// Crystal-contract state is current NoPack class1/op898. It must follow a
	// durable type-97 activation so the native client can bind its persisted
	// cube selection to the newly active contract immediately.
	PostActionRefreshCrystalContractState PostAction = "refresh_crystal_contract_state"
	// Damage-font ownership is a character-stat projection consumed by NOTI
	// 1239. It follows the class1 op515 ACK and source-slot refresh.
	PostActionRefreshSelectedDamageFontState PostAction = "refresh_selected_damage_font_state"
	// Party-frame state is carried inside the current NoPack class0/op9 kind0
	// actor record. The bridge rebuilds that record from the selected actor and
	// live party slots before op153/op7 update the visible frame.
	PostActionRefreshSelectedPartyFrame PostAction = "refresh_selected_party_frame"
)

// UpperResponse 描述已经有证据允许 dnfbridge 发出的 raw upper 响应。
// Body 必须是完整业务包体；如果包体已经包含成功字节，调用方不能再套 upperSuccessBody。
type UpperResponse struct {
	MsgID          uint16
	Body           []byte
	Classification byte
	AllowCodec     bool
}

// Handler 是单个 DNF 业务域的命令处理入口。
type Handler interface {
	Domain() dnfenum.AlignedDomain
	Handle(context.Context, Request) (Result, error)
}

// Registry 按业务域保存 handler，避免 dnfbridge 直接堆积各玩法 switch。
type Registry struct {
	handlers map[dnfenum.AlignedDomain]Handler
}

// NewRegistry 创建模块化命令注册表。
func NewRegistry(handlers ...Handler) Registry {
	registry := Registry{handlers: make(map[dnfenum.AlignedDomain]Handler)}
	for _, handler := range handlers {
		if handler == nil {
			continue
		}
		registry.handlers[handler.Domain()] = handler
	}
	return registry
}

// DefaultRegistry 返回当前可用的模块化注册表。
// 具体业务 handler 尚未补齐时保持空表，只做证据化 pending/blocked 分流。
func DefaultRegistry() Registry {
	return NewRegistry()
}

// Route 根据 EXE 证据分类并交给对应业务域 handler。
func (r Registry) Route(ctx context.Context, req Request) (Result, bool, error) {
	// A numeric type is only a CmdPacket inside the current EXE command
	// dispatcher. Other command classes have independent registration tables.
	if req.CommandKnown && dnfenum.GameCmd(req.Command) != dnfenum.GameCmdCommand {
		return Result{}, false, nil
	}
	decision, ok := Classify(req.Opcode)
	if !ok {
		return Result{}, false, nil
	}
	if decision.Action == ActionBlocked {
		return Result{
			Decision:        decision,
			Handled:         true,
			ResponseAllowed: false,
			Operation:       string(decision.Action),
			Reason:          decision.Reason,
		}, true, nil
	}
	handler := r.handlers[decision.Domain]
	if handler == nil {
		return Result{
			Decision:        decision,
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "module_unregistered",
			Reason:          "模块 handler 尚未注册",
		}, true, nil
	}
	req.Body = append([]byte(nil), req.Body...)
	result, err := handler.Handle(ctx, req)
	if result.Decision == (Decision{}) {
		result.Decision = decision
	}
	for i := range result.UpperResponses {
		result.UpperResponses[i].Body = append([]byte(nil), result.UpperResponses[i].Body...)
	}
	result.PostActions = append([]PostAction(nil), result.PostActions...)
	return result, true, err
}
