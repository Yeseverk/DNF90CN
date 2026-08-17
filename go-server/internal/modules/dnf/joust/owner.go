package joust

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

const (
	MinimumLevel        = 50
	MaximumBetPerRound  = 10_000
	OpeningKnightCount  = 8
	PermanentCrystalID  = 490005585
	EventCrystalID      = 490005586
	ParticipationItemID = 490005593

	RoundStat        = "joust_round"
	KnightStat       = "joust_knight"
	AmountStat       = "joust_amount"
	SourceItemIDStat = "joust_source_item_id"
	PendingStat      = "joust_pending"
	LastBetUnixStat  = "joust_last_bet_unix"
	WinnerStat       = "joust_winner"
	PayoutStat       = "joust_payout"
	SettledUnixStat  = "joust_settled_unix"

	// KnightAmountStatPrefix stores the current round's per-knight support.
	// AmountStat remains the total sent to the current client in op1242, while
	// these entries preserve split support across all active riders.
	KnightAmountStatPrefix = "joust_knight_amount_"
)

var (
	ErrOwnerUnavailable    = errors.New("joust owner unavailable")
	ErrRequestInvalid      = errors.New("joust betting request invalid")
	ErrCharacterNotFound   = errors.New("joust character not found")
	ErrAccountMismatch     = errors.New("joust character account mismatch")
	ErrLevelTooLow         = errors.New("joust character level is too low")
	ErrInventoryNotFound   = errors.New("joust inventory not found")
	ErrCrystalMissing      = errors.New("joust crystal is missing")
	ErrCrystalInvalid      = errors.New("joust source is not a supported crystal")
	ErrCrystalInsufficient = errors.New("joust crystal count is insufficient")
	ErrCrystalChanged      = errors.New("joust source crystal cannot change within one round")
	ErrKnightUnavailable   = errors.New("joust knight is not in the active round")
	ErrKnightChanged       = errors.New("joust knight cannot change within one round")
	ErrRoundLimit          = errors.New("joust round betting limit exceeded")
	ErrRewardPlacement     = errors.New("joust participation reward placement failed")
	ErrSettlementRequired  = errors.New("previous joust round requires settlement")
	ErrSettlementInvalid   = errors.New("joust settlement is invalid")
)

// RewardPlacer projects the current PVF participation item into the already
// cloned inventory aggregate. The transport supplies the current-client raw
// row construction, while Owner keeps the mutation inside one repository
// transaction with the crystal debit and durable bet ledger.
type RewardPlacer func(*dnfrepo.InventoryRecord) (int16, dnfrepo.ItemStack, error)

type Command struct {
	AccountID           string
	SelectedCharacterID uint16
	Round               uint16
	Knight              byte
	SourceSlot          int16
	Amount              uint32
	UpdatedAt           time.Time
	KnightAllowed       func(byte) bool
	PlaceReward         RewardPlacer
}

type Result struct {
	CharacterID       string
	Round             uint16
	Knight            byte
	Amount            uint32
	RoundTotal        uint32
	SourceSlot        int16
	SourceItemID      int64
	SourceRemoved     bool
	RemainingSource   dnfrepo.ItemStack
	RewardSlot        int16
	ParticipationItem dnfrepo.ItemStack
}

// KnightAmountStat returns the durable stat key for one rider's support in
// the current joust round. Rider identifiers are current-PVF bytes, so using
// their decimal representation keeps the state inspectable in the local DB.
func KnightAmountStat(knight byte) string {
	return KnightAmountStatPrefix + strconv.FormatUint(uint64(knight), 10)
}

// CurrentRoundBets reconstructs every rider wager for one round. It accepts
// the original one-rider ledger as a fallback, preserving wagers placed before
// split support was enabled.
func CurrentRoundBets(stats map[string]int64, round uint16) (map[byte]uint32, uint32, bool) {
	if stats == nil || stats[RoundStat] != int64(round) {
		return nil, 0, false
	}
	totalValue := stats[AmountStat]
	if totalValue <= 0 || totalValue > MaximumBetPerRound {
		return nil, 0, false
	}
	wantTotal := uint32(totalValue)
	bets := make(map[byte]uint32, OpeningKnightCount)
	var actualTotal uint32
	for key, value := range stats {
		if !strings.HasPrefix(key, KnightAmountStatPrefix) || value == 0 {
			continue
		}
		if value < 0 || value > MaximumBetPerRound {
			return nil, 0, false
		}
		id, err := strconv.ParseUint(strings.TrimPrefix(key, KnightAmountStatPrefix), 10, 8)
		if err != nil {
			return nil, 0, false
		}
		amount := uint32(value)
		if amount > MaximumBetPerRound-actualTotal {
			return nil, 0, false
		}
		actualTotal += amount
		bets[byte(id)] += amount
	}
	if actualTotal == 0 {
		knight := stats[KnightStat]
		if knight < 0 || knight > math.MaxUint8 {
			return nil, 0, false
		}
		bets[byte(knight)] = wantTotal
		return bets, wantTotal, true
	}
	if actualTotal != wantTotal {
		return nil, 0, false
	}
	return bets, actualTotal, true
}

// CurrentRoundSourceItem returns the exact material debited for a round. Old
// one-rider ledgers predate this field and are permanently interpreted as the
// original permanent crystal, preserving a claim that was already placed.
func CurrentRoundSourceItem(stats map[string]int64, round uint16) (int64, bool) {
	if stats == nil || stats[RoundStat] != int64(round) {
		return 0, false
	}
	itemID := stats[SourceItemIDStat]
	if itemID == 0 {
		return PermanentCrystalID, true
	}
	return itemID, itemID == PermanentCrystalID || itemID == EventCrystalID
}

func clearKnightAmountStats(stats map[string]int64) {
	for key := range stats {
		if strings.HasPrefix(key, KnightAmountStatPrefix) {
			delete(stats, key)
		}
	}
}

type Owner struct {
	assets        dnfrepo.CharacterAssetUnitOfWork
	mailboxAssets dnfrepo.MailboxAssetUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.CharacterAssets == nil || repositories.MailboxAssets == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{assets: repositories.CharacterAssets, mailboxAssets: repositories.MailboxAssets}, nil
}

func (o *Owner) Bet(ctx context.Context, cmd Command) (Result, error) {
	if o == nil || o.assets == nil {
		return Result{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 || cmd.KnightAllowed == nil ||
		cmd.SourceSlot < 0 || cmd.Amount == 0 || cmd.Amount > MaximumBetPerRound ||
		cmd.PlaceReward == nil {
		return Result{}, ErrRequestInvalid
	}
	if !cmd.KnightAllowed(cmd.Knight) {
		return Result{}, ErrKnightUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	accountID := strings.TrimSpace(cmd.AccountID)
	result := Result{
		CharacterID: characterID,
		Round:       cmd.Round,
		Knight:      cmd.Knight,
		Amount:      cmd.Amount,
		SourceSlot:  cmd.SourceSlot,
	}
	err := o.assets.WithinCharacterAssets(ctx, characterID, func(
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(character.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrCharacterNotFound, characterID)
		}
		if accountID != "" && strings.TrimSpace(character.AccountID) != accountID {
			return fmt.Errorf("%w: character=%s", ErrAccountMismatch, characterID)
		}
		if character.Level < MinimumLevel {
			return fmt.Errorf("%w: level=%d", ErrLevelTooLow, character.Level)
		}
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64, 5)
		}

		previousAmount := uint32(0)
		previousSourceItemID := int64(0)
		previousBets := make(map[byte]uint32, OpeningKnightCount)
		if character.Stats[PendingStat] != 0 && character.Stats[RoundStat] != int64(cmd.Round) {
			return ErrSettlementRequired
		}
		if character.Stats[RoundStat] == int64(cmd.Round) && character.Stats[AmountStat] != 0 {
			var valid bool
			previousBets, previousAmount, valid = CurrentRoundBets(character.Stats, cmd.Round)
			if !valid {
				return ErrRequestInvalid
			}
			if previousSourceItemID, valid = CurrentRoundSourceItem(character.Stats, cmd.Round); !valid {
				return ErrRequestInvalid
			}
		} else {
			// A settled prior round must not leak its split support into this
			// new opening. It remains available until the next wager begins.
			clearKnightAmountStats(character.Stats)
		}
		if cmd.Amount > MaximumBetPerRound-previousAmount {
			return ErrRoundLimit
		}

		inventory, found, err := inventories.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrInventoryNotFound, characterID)
		}
		inventory = dnfrepo.CloneInventory(inventory)
		key := fmt.Sprintf("%d:%d", dnfrepo.MainInventoryListType, cmd.SourceSlot)
		source, found := inventory.Slots[key]
		if !found || source.ItemID <= 0 || source.Count <= 0 {
			return fmt.Errorf("%w: slot=%d", ErrCrystalMissing, cmd.SourceSlot)
		}
		if source.ItemID != PermanentCrystalID && source.ItemID != EventCrystalID {
			return fmt.Errorf("%w: slot=%d item=%d", ErrCrystalInvalid, cmd.SourceSlot, source.ItemID)
		}
		if previousAmount != 0 && previousSourceItemID != source.ItemID {
			return fmt.Errorf("%w: previous=%d requested=%d", ErrCrystalChanged, previousSourceItemID, source.ItemID)
		}
		if source.Count < int64(cmd.Amount) {
			return fmt.Errorf("%w: have=%d need=%d", ErrCrystalInsufficient, source.Count, cmd.Amount)
		}
		result.SourceItemID = source.ItemID
		source.Count -= int64(cmd.Amount)
		if source.Count == 0 {
			delete(inventory.Slots, key)
			result.SourceRemoved = true
		} else {
			inventory.Slots[key] = source
			result.RemainingSource = source
		}

		rewardSlot, reward, err := cmd.PlaceReward(&inventory)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrRewardPlacement, err)
		}
		if rewardSlot < 0 || reward.ItemID != ParticipationItemID || reward.Count <= 0 ||
			reward.Count > math.MaxUint32 {
			return ErrRewardPlacement
		}
		rewardKey := fmt.Sprintf("%d:%d", dnfrepo.MainInventoryListType, rewardSlot)
		storedReward, found := inventory.Slots[rewardKey]
		if !found || storedReward.ItemID != reward.ItemID || storedReward.Count != reward.Count {
			return ErrRewardPlacement
		}
		result.RewardSlot = rewardSlot
		result.ParticipationItem = reward
		result.RoundTotal = previousAmount + cmd.Amount
		previousBets[cmd.Knight] += cmd.Amount

		now := cmd.UpdatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		} else {
			now = now.UTC()
		}
		character.Stats[RoundStat] = int64(cmd.Round)
		character.Stats[KnightStat] = int64(cmd.Knight)
		character.Stats[AmountStat] = int64(result.RoundTotal)
		character.Stats[SourceItemIDStat] = source.ItemID
		for knight, amount := range previousBets {
			character.Stats[KnightAmountStat(knight)] = int64(amount)
		}
		character.Stats[PendingStat] = 1
		character.Stats[LastBetUnixStat] = now.Unix()
		character.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		inventory.UpdatedAt = now
		return dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots)
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}
