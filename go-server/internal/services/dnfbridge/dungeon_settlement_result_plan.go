package dnfbridge

import (
	"errors"
	"fmt"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	errCurrentDungeonSettlementPlanShape = errors.New("current dungeon settlement packet plan shape is invalid")
	errCurrentDungeonSettlementPlanOwner = errors.New("current dungeon settlement packet plan owner mismatch")
)

// currentDungeonSettlementPacketPlan freezes the three current-EXE result
// packets only after the reward transaction has committed. Keeping the exact
// bodies makes a retry resume after the last successful write without reading
// newer repository state into an older settlement.
type currentDungeonSettlementPacketPlan struct {
	CharacterID           uint16
	CompletionKey         string
	Source                string
	ClientRankPoint       byte
	PlayResultBody        []byte
	CharacterBody         []byte
	ClearRewardBody       []byte
	DungeonPermissionBody []byte
}

func buildCurrentDungeonSettlementPacketPlan(
	clientRankPoint byte,
	notice currentDungeonPlayResultNotice,
	reward currentDungeonClearRewardSnapshot,
	character dnfrepo.CharacterRecord,
	points dnfrepo.SkillPointState,
) (currentDungeonSettlementPacketPlan, error) {
	characterID := numericCharacterID(character)
	if characterID <= 0 || characterID > int(^uint16(0)) || uint16(characterID) != reward.CharacterID {
		return currentDungeonSettlementPacketPlan{}, fmt.Errorf(
			"%w: character=%d reward_character=%d",
			errCurrentDungeonSettlementPlanOwner,
			characterID,
			reward.CharacterID,
		)
	}
	if notice.RankPoint != clientRankPoint {
		return currentDungeonSettlementPacketPlan{}, fmt.Errorf(
			"%w: notice_client_rank=%d request_client_rank=%d",
			errCurrentDungeonSettlementPlanShape,
			notice.RankPoint,
			clientRankPoint,
		)
	}
	ownedParticipants := 0
	for _, participant := range notice.Participants {
		if participant.ObjectKey == reward.CharacterID {
			ownedParticipants++
		}
	}
	if ownedParticipants != 1 {
		return currentDungeonSettlementPacketPlan{}, fmt.Errorf(
			"%w: character=%d owned_participants=%d",
			errCurrentDungeonSettlementPlanOwner,
			reward.CharacterID,
			ownedParticipants,
		)
	}

	playResultBody, err := buildCurrentDungeonPlayResultNoticeBody(notice)
	if err != nil {
		return currentDungeonSettlementPacketPlan{}, err
	}
	clearRewardBody, err := buildCurrentDungeonClearRewardBody(reward)
	if err != nil {
		return currentDungeonSettlementPacketPlan{}, err
	}
	characterBody := buildCurrentFinishLoadingCharacterStateBody(character, points)
	if len(characterBody) != currentFinishLoadingCharacterStateBodySize {
		return currentDungeonSettlementPacketPlan{}, fmt.Errorf(
			"%w: character_body=%d want=%d",
			errCurrentDungeonSettlementPlanShape,
			len(characterBody),
			currentFinishLoadingCharacterStateBodySize,
		)
	}

	return currentDungeonSettlementPacketPlan{
		CharacterID:     reward.CharacterID,
		CompletionKey:   reward.CompletionKey,
		Source:          reward.Source,
		ClientRankPoint: clientRankPoint,
		PlayResultBody:  append([]byte(nil), playResultBody...),
		CharacterBody:   append([]byte(nil), characterBody...),
		ClearRewardBody: append([]byte(nil), clearRewardBody...),
	}, nil
}
