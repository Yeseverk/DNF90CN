package joust

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// SettlementMailDelivery creates the durable reward mail inside the same
// mailbox-assets transaction that clears the betting claim.
type SettlementMailDelivery func(dnfrepo.MailboxRepository, int64, uint32) (string, error)

type SettlementCommand struct {
	AccountID           string
	SelectedCharacterID uint16
	Round               uint16
	Winner              byte
	Multiplier          float32
	SettledAt           time.Time
	DeliverReward       SettlementMailDelivery
}

type SettlementResult struct {
	CharacterID  string
	Round        uint16
	Knight       byte
	Amount       uint32
	Winner       byte
	Payout       uint32
	RewardItemID int64
	MailID       string
	Won          bool
	Settled      bool
}

// Settle clears one pending claim and writes the winning material payout as a
// system-mail attachment in the same mailbox-assets transaction. A failed
// delivery rolls back the pending flag, so reconnect/restart retries cannot
// lose or duplicate the result.
func (o *Owner) Settle(ctx context.Context, cmd SettlementCommand) (SettlementResult, error) {
	if o == nil || o.mailboxAssets == nil {
		return SettlementResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 || cmd.Multiplier <= 0 ||
		math.IsNaN(float64(cmd.Multiplier)) || math.IsInf(float64(cmd.Multiplier), 0) ||
		cmd.DeliverReward == nil {
		return SettlementResult{}, ErrSettlementInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	accountID := strings.TrimSpace(cmd.AccountID)
	result := SettlementResult{CharacterID: characterID, Round: cmd.Round, Winner: cmd.Winner}
	err := o.mailboxAssets.WithinMailboxAssets(ctx, characterID, characterID, func(
		characters dnfrepo.CharacterRepository,
		_ dnfrepo.InventoryRepository,
		mailboxes dnfrepo.MailboxRepository,
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
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil || character.Stats[PendingStat] == 0 {
			return nil
		}
		round := character.Stats[RoundStat]
		bets, totalAmount, valid := CurrentRoundBets(character.Stats, cmd.Round)
		if round != int64(cmd.Round) || !valid {
			return ErrSettlementInvalid
		}
		rewardItemID, valid := CurrentRoundSourceItem(character.Stats, cmd.Round)
		if !valid {
			return ErrSettlementInvalid
		}
		winningAmount := bets[cmd.Winner]
		result.Knight = cmd.Winner
		result.Amount = totalAmount
		result.Won = winningAmount > 0
		if result.Won {
			payout := math.Floor(float64(winningAmount) * float64(cmd.Multiplier))
			if payout < 1 || payout > math.MaxUint32 {
				return ErrSettlementInvalid
			}
			result.Payout = uint32(payout)
			mailID, err := cmd.DeliverReward(mailboxes, rewardItemID, result.Payout)
			if err != nil || strings.TrimSpace(mailID) == "" {
				if err == nil {
					err = ErrRewardPlacement
				}
				return fmt.Errorf("%w: %v", ErrRewardPlacement, err)
			}
			result.RewardItemID = rewardItemID
			result.MailID = mailID
			settledAt := cmd.SettledAt
			if settledAt.IsZero() {
				settledAt = time.Now().UTC()
			}
		}
		settledAt := cmd.SettledAt
		if settledAt.IsZero() {
			settledAt = time.Now().UTC()
		}
		character.Stats[PendingStat] = 0
		character.Stats[WinnerStat] = int64(cmd.Winner)
		character.Stats[PayoutStat] = int64(result.Payout)
		character.Stats[SettledUnixStat] = settledAt.UTC().Unix()
		character.UpdatedAt = settledAt.UTC()
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		result.Settled = true
		return nil
	})
	if err != nil {
		return SettlementResult{}, err
	}
	return result, nil
}
