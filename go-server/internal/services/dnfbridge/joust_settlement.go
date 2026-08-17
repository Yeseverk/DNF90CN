package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (service *Service) settleCurrentJoustForSession(
	ctx context.Context,
	session *gameSession,
	now time.Time,
) (dnfjoust.SettlementResult, error) {
	if service == nil || session == nil || session.selectedCharacterID == 0 {
		return dnfjoust.SettlementResult{}, nil
	}
	results, err := service.settleCurrentJoustAccount(ctx, service.accountIDForSession(session), now)
	for _, result := range results {
		if result.CharacterID == strconv.FormatUint(uint64(session.selectedCharacterID), 10) {
			return result, err
		}
	}
	return dnfjoust.SettlementResult{}, err
}

func (service *Service) settleCurrentJoustAccount(
	ctx context.Context,
	accountID string,
	now time.Time,
) ([]dnfjoust.SettlementResult, error) {
	if service == nil {
		return nil, dnfjoust.ErrOwnerUnavailable
	}
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Character == nil {
		return nil, dnfjoust.ErrOwnerUnavailable
	}
	characters, err := repositories.Character.ListByAccount(ctx, accountID, 32)
	if err != nil {
		return nil, err
	}
	timeline := dnfjoust.TimelineAt(now)
	pendingByRound := make(map[uint16][]dnfrepo.CharacterRecord)
	for _, character := range characters {
		round, _, _, valid := currentJoustBettingLedgerForCharacter(character)
		if !valid || character.Stats[dnfjoust.PendingStat] == 0 || !currentJoustRoundIsComplete(round, timeline) {
			continue
		}
		pendingByRound[round] = append(pendingByRound[round], character)
	}
	if len(pendingByRound) == 0 {
		return nil, nil
	}
	catalog, err := service.currentJoustCatalog(ctx)
	if err != nil {
		return nil, err
	}
	itemCatalog, err := service.currentPVFItemCatalog()
	if err != nil {
		return nil, err
	}
	owner, err := dnfjoust.NewOwner(repositories)
	if err != nil {
		return nil, err
	}
	results := make([]dnfjoust.SettlementResult, 0, len(characters))
	var settleErr error
	for round, pending := range pendingByRound {
		opening, openingErr := service.currentJoustOpeningForRound(ctx, catalog, accountID, round)
		if openingErr != nil {
			settleErr = errors.Join(settleErr, openingErr)
			continue
		}
		tournament, tournamentErr := catalog.TournamentFor(round)
		if tournamentErr != nil {
			settleErr = errors.Join(settleErr, tournamentErr)
			continue
		}
		multiplier, found := currentJoustWinnerMultiplier(opening, tournament.Champion())
		if !found {
			settleErr = errors.Join(settleErr, fmt.Errorf("joust round=%d winner=%d multiplier missing", round, tournament.Champion()))
			continue
		}
		if historyErr := service.persistCurrentJoustHistoryRecord(ctx, accountID, currentJoustHistoryRecord{
			Round: round, Winner: tournament.Champion(), Multiplier: multiplier,
		}, now); historyErr != nil {
			settleErr = errors.Join(settleErr, historyErr)
			continue
		}
		for _, character := range pending {
			characterID, parseErr := strconv.ParseUint(character.CharacterID, 10, 16)
			if parseErr != nil || characterID == 0 {
				settleErr = errors.Join(settleErr, fmt.Errorf("joust character id=%q", character.CharacterID))
				continue
			}
			result, ownerErr := owner.Settle(ctx, dnfjoust.SettlementCommand{
				AccountID:           accountID,
				SelectedCharacterID: uint16(characterID),
				Round:               round,
				Winner:              tournament.Champion(),
				Multiplier:          multiplier,
				SettledAt:           now,
				DeliverReward: func(mailbox dnfrepo.MailboxRepository, itemID int64, count uint32) (string, error) {
					if itemID <= 0 || itemID > math.MaxUint32 || count == 0 {
						return "", fmt.Errorf("%w: settlement material item=%d", dnfjoust.ErrRewardPlacement, itemID)
					}
					definition, resolveErr := itemCatalog.ResolveItem(uint32(itemID))
					if resolveErr != nil || definition.Kind != dungeonDropItemStackable {
						return "", fmt.Errorf("%w: settlement material item=%d", dnfjoust.ErrRewardPlacement, itemID)
					}
					// The mailbox attachment is an item instance, not merely an item
					// ID. Preserve the live PVF deadline here so the current client
					// never treats a zero instance timestamp as the retired 2017
					// joust-event expiration when the reward is opened or claimed.
					return dnfrepo.AppendSystemMail(ctx, mailbox, dnfrepo.SystemMailDelivery{
						RecipientCharacterID: character.CharacterID,
						Title:                "骑士马战大竞猜奖励",
						Body:                 "竞猜获胜，押注材料已按最终倍率返还。",
						Source:               currentJoustSettlementMailSource,
						Attachments: []dnfrepo.MailAttachment{{
							ItemID:   itemID,
							Count:    int64(count),
							ExpireAt: definition.ExpirationDate,
						}},
						CreatedAt: now,
					})
				},
			})
			if ownerErr != nil {
				settleErr = errors.Join(settleErr, fmt.Errorf("settle character=%s round=%d: %w", character.CharacterID, round, ownerErr))
				continue
			}
			if result.Settled {
				results = append(results, result)
			}
		}
	}
	return results, settleErr
}

func currentJoustRoundIsComplete(round uint16, timeline dnfjoust.Timeline) bool {
	if round == timeline.Round {
		return timeline.Phase == dnfjoust.PhaseSettled
	}
	distance := uint16(timeline.Round - round)
	return distance > 0 && distance < 1<<15
}
