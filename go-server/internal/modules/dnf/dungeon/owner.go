package dungeon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	LuckyStarMetadataKey = "rental_lucky_stars"
	LuckyStarMaximum     = uint32(999)
)

var (
	ErrOwnerUnavailable       = errors.New("dungeon asset owner is unavailable")
	ErrCharacterNotFound      = errors.New("dungeon character is not found")
	ErrAccountNotFound        = errors.New("dungeon account is not found")
	ErrRewardInvalid          = errors.New("dungeon reward is invalid")
	ErrInventoryFull          = errors.New("dungeon reward inventory is full")
	ErrGoldOverflow           = errors.New("dungeon gold would overflow")
	ErrStackProjectorRequired = errors.New("dungeon expiring stack projector is required")
	ErrPremiumDailyLimit      = errors.New("dungeon premium service daily limit reached")
)

// StackProjector lets the transport compatibility layer project current-client
// raw-entry details while the dungeon owner retains the durable transaction and
// inventory rules.
type StackProjector func(dnfrepo.ItemStack, time.Time) (dnfrepo.ItemStack, error)

type CardItemReward struct {
	Stack     dnfrepo.ItemStack
	Stackable bool
	SlotStart int16
	SlotEnd   int16
	ExpireAt  time.Time
}

type CardRewardBundle struct {
	Gold  int64
	Items []CardItemReward
}

type CardRewardCommand struct {
	CharacterID         string
	MainSlots           uint16
	Bundle              CardRewardBundle
	UpdatedAt           time.Time
	Project             StackProjector
	ConsumePremiumDaily bool
	PremiumDailySlot    int64
}

type CardRewardResult struct {
	GoldBefore int64
	GoldAfter  int64
	ItemSlots  []int16
	// OverflowMailID is set only when one or more card items could not fit in
	// the real main inventory and were committed as a system-mail attachment.
	OverflowMailID string
}

type GoldPickupCommand struct {
	CharacterID string
	Amount      uint32
	UpdatedAt   time.Time
}

type GoldPickupResult struct {
	GoldBefore int64
	GoldAfter  int64
}

type LuckyStarCommand struct {
	AccountID      string
	CharacterLevel int
	RecommendedMin int
	RecommendedMax int
	UpdatedAt      time.Time
}

type LuckyStarResult struct {
	Awarded bool
	Before  uint32
	After   uint32
}

// Owner is the durable mutation boundary for dungeon rewards. Packet parsing,
// runtime room state, packet ordering, and current-client raw projection stay
// in dnfbridge.
type Owner struct {
	assets        dnfrepo.CharacterAssetUnitOfWork
	accounts      dnfrepo.AccountRepository
	settlements   dnfrepo.CharacterSettlementUnitOfWork
	items         dnfrepo.CharacterItemUnitOfWork
	inventory     dnfrepo.InventoryRepository
	characters    dnfrepo.CharacterRepository
	mailboxAssets dnfrepo.MailboxAssetUnitOfWork
	accountAssets dnfrepo.AccountCharacterAssetUnitOfWork
}

func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.CharacterAssets == nil && repos.Account == nil && repos.CharacterSettlement == nil &&
		repos.CharacterItems == nil && repos.Inventory == nil && repos.Character == nil &&
		repos.AccountAssets == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{
		assets:        repos.CharacterAssets,
		accounts:      repos.Account,
		settlements:   repos.CharacterSettlement,
		items:         repos.CharacterItems,
		inventory:     repos.Inventory,
		characters:    repos.Character,
		mailboxAssets: repos.MailboxAssets,
		accountAssets: repos.AccountAssets,
	}, nil
}

func (o *Owner) GrantCardReward(ctx context.Context, cmd CardRewardCommand) (CardRewardResult, error) {
	if o == nil || o.assets == nil || strings.TrimSpace(cmd.CharacterID) == "" {
		return CardRewardResult{}, ErrOwnerUnavailable
	}
	if err := validateCardReward(cmd); err != nil {
		return CardRewardResult{}, err
	}
	ctx = contextOrBackground(ctx)
	now := updatedAtOrNow(cmd.UpdatedAt)

	var result CardRewardResult
	apply := func(
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		mailboxes dnfrepo.MailboxRepository,
	) error {
		character, found, err := characters.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrCharacterNotFound
		}
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64)
		}
		if cmd.ConsumePremiumDaily && !premium.TryConsumeDaily(&character, cmd.PremiumDailySlot, now) {
			return ErrPremiumDailyLimit
		}
		currentGold := character.Stats["gold"]
		if cmd.Bundle.Gold > 0 && currentGold > math.MaxInt64-cmd.Bundle.Gold {
			return ErrGoldOverflow
		}
		result.GoldBefore = currentGold
		result.GoldAfter = currentGold + cmd.Bundle.Gold
		character.Stats["gold"] = result.GoldAfter

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
		overflow := make([]dnfrepo.MailAttachment, 0)
		for _, reward := range cmd.Bundle.Items {
			slot, err := addCardItem(&inventory, cmd.MainSlots, reward, cmd.Project)
			if err != nil {
				if !errors.Is(err, ErrInventoryFull) || mailboxes == nil {
					return err
				}
				attachment, attachmentErr := cardRewardMailAttachment(reward, cmd.Project)
				if attachmentErr != nil {
					return attachmentErr
				}
				overflow = append(overflow, attachment)
				result.ItemSlots = append(result.ItemSlots, -1)
				continue
			}
			result.ItemSlots = append(result.ItemSlots, slot)
		}

		character.UpdatedAt = now
		inventory.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		if len(overflow) == 0 {
			return nil
		}
		mailID, mailErr := dnfrepo.AppendSystemMail(ctx, mailboxes, dnfrepo.SystemMailDelivery{
			RecipientCharacterID: cmd.CharacterID,
			Title:                "背包已满：通关奖励",
			Body:                 "背包空间不足，通关奖励已通过邮件发送。请清理对应道具分页后领取。",
			Source:               "dungeon_card_reward_inventory_full",
			Attachments:          overflow,
			CreatedAt:            now,
		})
		if mailErr != nil {
			return mailErr
		}
		result.OverflowMailID = mailID
		return nil
	}
	var err error
	if o.mailboxAssets != nil {
		err = o.mailboxAssets.WithinMailboxAssets(ctx, cmd.CharacterID, cmd.CharacterID, apply)
	} else {
		err = o.assets.WithinCharacterAssets(ctx, cmd.CharacterID, func(
			characters dnfrepo.CharacterRepository,
			inventories dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			return apply(characters, inventories, nil)
		})
	}
	if err != nil {
		return CardRewardResult{}, err
	}
	return result, nil
}

func cardRewardMailAttachment(reward CardItemReward, project StackProjector) (dnfrepo.MailAttachment, error) {
	stack := cloneStack(reward.Stack)
	if reward.Stackable && !reward.ExpireAt.IsZero() {
		if project == nil {
			return dnfrepo.MailAttachment{}, ErrStackProjectorRequired
		}
		var err error
		stack, err = project(stack, reward.ExpireAt)
		if err != nil {
			return dnfrepo.MailAttachment{}, err
		}
	}
	return dnfrepo.MailAttachment{
		ItemID:   stack.ItemID,
		Count:    stack.Count,
		Bind:     stack.Bind,
		ExpireAt: stack.ExpireAt,
		RawEntry: append([]byte(nil), stack.RawEntry...),
		Extra:    cloneCardRewardExtra(stack.Extra),
	}, nil
}

func cloneCardRewardExtra(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (o *Owner) GrantPickupGold(ctx context.Context, cmd GoldPickupCommand) (GoldPickupResult, error) {
	if o == nil || o.assets == nil || strings.TrimSpace(cmd.CharacterID) == "" {
		return GoldPickupResult{}, ErrOwnerUnavailable
	}
	if cmd.Amount == 0 {
		return GoldPickupResult{}, ErrRewardInvalid
	}
	ctx = contextOrBackground(ctx)
	now := updatedAtOrNow(cmd.UpdatedAt)

	var result GoldPickupResult
	err := o.assets.WithinCharacterAssets(ctx, cmd.CharacterID, func(
		characters dnfrepo.CharacterRepository,
		_ dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		character, found, err := characters.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrCharacterNotFound
		}
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64)
		}
		result.GoldBefore = character.Stats["gold"]
		delta := int64(cmd.Amount)
		if result.GoldBefore < 0 || result.GoldBefore > math.MaxInt64-delta {
			return ErrGoldOverflow
		}
		result.GoldAfter = result.GoldBefore + delta
		character.Stats["gold"] = result.GoldAfter
		character.UpdatedAt = now
		return dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats)
	})
	if err != nil {
		return GoldPickupResult{}, err
	}
	return result, nil
}

func (o *Owner) AwardLuckyStar(ctx context.Context, cmd LuckyStarCommand) (LuckyStarResult, error) {
	if o == nil || o.accounts == nil || strings.TrimSpace(cmd.AccountID) == "" {
		return LuckyStarResult{}, ErrOwnerUnavailable
	}
	if cmd.CharacterLevel <= 0 || cmd.RecommendedMin <= 0 || cmd.RecommendedMax < cmd.RecommendedMin {
		return LuckyStarResult{}, ErrRewardInvalid
	}
	if cmd.CharacterLevel < cmd.RecommendedMin || cmd.CharacterLevel > cmd.RecommendedMax {
		return LuckyStarResult{}, nil
	}
	ctx = contextOrBackground(ctx)
	account, found, err := o.accounts.Load(ctx, cmd.AccountID)
	if err != nil {
		return LuckyStarResult{}, err
	}
	if !found {
		return LuckyStarResult{}, ErrAccountNotFound
	}
	account = dnfrepo.CloneAccount(account)
	if account.Metadata == nil {
		account.Metadata = make(map[string]string, 1)
	}
	current := parseLuckyStars(account.Metadata[LuckyStarMetadataKey])
	result := LuckyStarResult{Before: current, After: current}
	if current >= LuckyStarMaximum {
		return result, nil
	}
	result.Awarded = true
	result.After = current + 1
	account.Metadata[LuckyStarMetadataKey] = strconv.FormatUint(uint64(result.After), 10)
	account.UpdatedAt = updatedAtOrNow(cmd.UpdatedAt)
	if err := o.accounts.Save(ctx, account); err != nil {
		return LuckyStarResult{}, err
	}
	return result, nil
}

func validateCardReward(cmd CardRewardCommand) error {
	if cmd.Bundle.Gold < 0 ||
		cmd.ConsumePremiumDaily && (cmd.PremiumDailySlot < 0 || cmd.PremiumDailySlot >= premium.DevilSlotCount) {
		return ErrRewardInvalid
	}
	for _, reward := range cmd.Bundle.Items {
		if reward.Stack.ItemID <= 0 || reward.Stack.Count <= 0 ||
			!reward.Stackable && reward.Stack.Count != 1 {
			return ErrRewardInvalid
		}
		hasPVFRange := reward.SlotStart >= 3 && reward.SlotEnd >= reward.SlotStart
		if !hasPVFRange && cmd.MainSlots == 0 {
			return ErrRewardInvalid
		}
		if reward.Stackable && !reward.ExpireAt.IsZero() && cmd.Project == nil {
			return ErrStackProjectorRequired
		}
	}
	return nil
}

func addCardItem(
	record *dnfrepo.InventoryRecord,
	mainSlots uint16,
	reward CardItemReward,
	project StackProjector,
) (int16, error) {
	if record == nil {
		return 0, ErrRewardInvalid
	}
	slotStart := int16(0)
	slotEnd := int16(mainSlots) - 1
	if reward.SlotStart >= 3 && reward.SlotEnd >= reward.SlotStart {
		slotStart = reward.SlotStart
		slotEnd = reward.SlotEnd
	}
	if reward.Stackable {
		for slot := slotStart; slot <= slotEnd; slot++ {
			key := mainSlotKey(uint16(slot))
			stack, ok := record.Slots[key]
			if !ok || stack.ItemID != reward.Stack.ItemID || stack.Bind != reward.Stack.Bind {
				continue
			}
			if reward.ExpireAt.IsZero() {
				if !stack.ExpireAt.IsZero() {
					continue
				}
			} else {
				var err error
				stack, err = project(stack, reward.ExpireAt)
				if err != nil {
					return 0, err
				}
			}
			if stack.Count > math.MaxInt64-reward.Stack.Count {
				return 0, fmt.Errorf("%w: item=%d", ErrRewardInvalid, reward.Stack.ItemID)
			}
			stack.Count += reward.Stack.Count
			record.Slots[key] = stack
			return int16(slot), nil
		}
	}
	for slot := slotStart; slot <= slotEnd; slot++ {
		key := mainSlotKey(uint16(slot))
		if _, occupied := record.Slots[key]; occupied {
			continue
		}
		stack := cloneStack(reward.Stack)
		if reward.Stackable && !reward.ExpireAt.IsZero() {
			var err error
			stack, err = project(stack, reward.ExpireAt)
			if err != nil {
				return 0, err
			}
		}
		record.Slots[key] = stack
		return int16(slot), nil
	}
	return 0, ErrInventoryFull
}

func mainSlotKey(slot uint16) string {
	return "0:" + strconv.FormatUint(uint64(slot), 10)
}

func cloneStack(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	if len(stack.Extra) == 0 {
		stack.Extra = nil
		return stack
	}
	extra := make(map[string]string, len(stack.Extra))
	for key, value := range stack.Extra {
		extra[key] = value
	}
	stack.Extra = extra
	return stack
}

func parseLuckyStars(raw string) uint32 {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(value)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func updatedAtOrNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value
}
