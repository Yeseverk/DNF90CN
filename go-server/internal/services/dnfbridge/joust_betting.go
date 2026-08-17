package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentJoustBettingBusinessSize = 9

	currentJoustBettingResultSuccess      = uint32(0)
	currentJoustBettingResultInvalid      = uint32(4)
	currentJoustBettingResultInsufficient = uint32(6)
	currentJoustBettingResultLimit        = uint32(7)
	currentJoustBettingResultInventory    = uint32(8)
)

type currentJoustBettingRequest struct {
	Knight     byte
	SourceSlot int16
	Amount     uint32
}

func decodeCurrentJoustBettingRequest(body []byte) (currentJoustBettingRequest, error) {
	if len(body) != currentJoustBettingBusinessSize {
		return currentJoustBettingRequest{}, fmt.Errorf("joust betting body length %d", len(body))
	}
	slot := binary.LittleEndian.Uint32(body[1:5])
	amount := binary.LittleEndian.Uint32(body[5:9])
	if slot > math.MaxInt16 || amount == 0 || amount > dnfjoust.MaximumBetPerRound {
		return currentJoustBettingRequest{}, fmt.Errorf("joust betting values knight=%d slot=%d amount=%d", body[0], slot, amount)
	}
	return currentJoustBettingRequest{Knight: body[0], SourceSlot: int16(slot), Amount: amount}, nil
}

func (s *Service) handleCurrentJoustBetting(session *gameSession, body []byte) error {
	request, err := decodeCurrentJoustBettingRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-joust-betting-rejected", "body_len", len(body), "reason", err.Error())
		return s.sendCurrentJoustBettingResult(session, currentJoustBettingResultInvalid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	now := s.gameplayNow().UTC()
	timeline := dnfjoust.TimelineAt(now)
	if timeline.Phase != dnfjoust.PhaseBetting {
		s.logGameEvent(session, "game-joust-betting-rejected", "round", timeline.Round, "phase", timeline.Phase, "reason", "betting_closed")
		return s.sendCurrentJoustBettingResult(session, currentJoustBettingResultInvalid)
	}
	s.joustOperationMu.Lock()
	defer s.joustOperationMu.Unlock()
	if _, err := s.settleCurrentJoustForSession(ctx, session, now); err != nil {
		s.logGameEvent(session, "game-joust-betting-rejected", "reason", err.Error())
		return s.sendCurrentJoustBettingResult(session, currentJoustBettingResultInvalid)
	}
	opening, err := s.currentJoustOpening(ctx, session, now)
	if err != nil {
		s.logGameEvent(session, "game-joust-betting-rejected", "reason", err.Error())
		return s.sendCurrentJoustBettingResult(session, currentJoustBettingResultInvalid)
	}
	result, err := s.commitCurrentJoustBetting(ctx, session, opening, request, now)
	if err != nil {
		code := currentJoustBettingErrorResult(err)
		s.logGameEvent(session, "game-joust-betting-rejected",
			"round", opening.Number,
			"knight", request.Knight,
			"source_slot", request.SourceSlot,
			"amount", request.Amount,
			"result", code,
			"reason", err.Error())
		return s.sendCurrentJoustBettingResult(session, code)
	}
	if err := s.sendCurrentJoustBettingResult(session, currentJoustBettingResultSuccess); err != nil {
		return err
	}
	// Reload the committed main list instead of composing a partial row stream.
	// The reward may merge into an existing stack or reuse the consumed source
	// slot, and the repository snapshot is the one authoritative projection for
	// all of those cases.
	listBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, dnfrepo.MainInventoryListType)
	if !ok {
		return dnfjoust.ErrOwnerUnavailable
	}
	if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), listBody); err != nil {
		return err
	}
	// op1241 carries the float multiplier and op1242 carries the support pool.
	// Refresh both after the atomic debit so every rider's total/personal-share
	// dependent multiplier changes immediately without replaying op1240 (which
	// the current EXE turns into another op1291 query).
	if err := s.sendCurrentJoustOpeningRoster(session); err != nil {
		return err
	}
	accountID := s.accountIDForSession(session)
	for _, peer := range s.currentJoustSessionSnapshot() {
		if peer == session || s.accountIDForSession(peer) != accountID {
			continue
		}
		if err := s.sendCurrentJoustOpeningRoster(peer); err != nil {
			s.logGameEvent(peer, "game-joust-pool-broadcast-failed", "round", result.Round, "reason", err.Error())
		}
	}
	s.logGameEvent(session, "game-joust-betting-committed",
		"round", result.Round,
		"knight", result.Knight,
		"source_slot", result.SourceSlot,
		"source_item", result.SourceItemID,
		"amount", result.Amount,
		"round_total", result.RoundTotal,
		"reward_item", result.ParticipationItem.ItemID,
		"reward_slot", result.RewardSlot)
	return nil
}

func (s *Service) commitCurrentJoustBetting(
	ctx context.Context,
	session *gameSession,
	opening dnfjoust.OpeningRound,
	request currentJoustBettingRequest,
	updatedAt time.Time,
) (dnfjoust.Result, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return dnfjoust.Result{}, dnfjoust.ErrOwnerUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return dnfjoust.Result{}, dnfjoust.ErrOwnerUnavailable
	}
	owner, err := dnfjoust.NewOwner(repositories)
	if err != nil {
		return dnfjoust.Result{}, err
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return dnfjoust.Result{}, err
	}
	rewardDefinition, err := catalog.ResolveItem(dnfjoust.ParticipationItemID)
	if err != nil || rewardDefinition.Kind != dungeonDropItemStackable {
		return dnfjoust.Result{}, fmt.Errorf("%w: reward item=%d", dnfjoust.ErrRewardPlacement, dnfjoust.ParticipationItemID)
	}
	// The original PVF carries the 2017 event end date. Reactivating this event
	// in the requested all-day runtime must not create an item that is already
	// expired on arrival; existing event crystals use the same permanent runtime
	// projection.
	rewardDefinition.ExpirationDate = time.Time{}
	return owner.Bet(ctx, dnfjoust.Command{
		AccountID:           s.accountIDForSession(session),
		SelectedCharacterID: session.selectedCharacterID,
		Round:               opening.Number,
		Knight:              request.Knight,
		SourceSlot:          request.SourceSlot,
		Amount:              request.Amount,
		UpdatedAt:           updatedAt,
		KnightAllowed:       opening.Contains,
		PlaceReward: func(inventory *dnfrepo.InventoryRecord) (int16, dnfrepo.ItemStack, error) {
			slots, err := grantCurrentCeraShopProduct(inventory, rewardDefinition, request.Amount)
			if err != nil || len(slots) == 0 || slots[0] > math.MaxInt16 {
				return 0, dnfrepo.ItemStack{}, dnfjoust.ErrRewardPlacement
			}
			slot := int16(slots[0])
			key := fmt.Sprintf("%d:%d", dnfrepo.MainInventoryListType, slot)
			stack, found := inventory.Slots[key]
			if !found {
				return 0, dnfrepo.ItemStack{}, dnfjoust.ErrRewardPlacement
			}
			return slot, stack, nil
		},
	})
}

func (s *Service) sendCurrentJoustBettingResult(session *gameSession, result uint32) error {
	var response packetWriter
	response.writeUint32(result)
	return s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketJoustBetting), response.bytes())
}

func currentJoustBettingErrorResult(err error) uint32 {
	switch {
	case errors.Is(err, dnfjoust.ErrCrystalMissing),
		errors.Is(err, dnfjoust.ErrCrystalInvalid),
		errors.Is(err, dnfjoust.ErrCrystalInsufficient):
		return currentJoustBettingResultInsufficient
	case errors.Is(err, dnfjoust.ErrRoundLimit),
		errors.Is(err, dnfjoust.ErrKnightChanged):
		return currentJoustBettingResultLimit
	case errors.Is(err, dnfjoust.ErrRewardPlacement):
		return currentJoustBettingResultInventory
	default:
		return currentJoustBettingResultInvalid
	}
}

func currentJoustBettingLedgerForCharacter(character dnfrepo.CharacterRecord) (uint16, byte, uint32, bool) {
	if character.Stats == nil {
		return 0, 0, 0, false
	}
	round := character.Stats[dnfjoust.RoundStat]
	if round < 0 || round > math.MaxUint16 {
		return 0, 0, 0, false
	}
	bets, amount, valid := dnfjoust.CurrentRoundBets(character.Stats, uint16(round))
	if !valid || amount == 0 || len(bets) == 0 {
		return 0, 0, 0, false
	}
	// Knight is retained only for legacy callers; the full map remains
	// authoritative for pool projection and settlement.
	knight := character.Stats[dnfjoust.KnightStat]
	if knight < 0 || knight > math.MaxUint8 {
		return 0, 0, 0, false
	}
	return uint16(round), byte(knight), amount, true
}
