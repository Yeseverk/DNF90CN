package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	// Current NoPack registers class0/op357 at sub_1D7F3B2 and handles the
	// exact u16 actor-key + u8 state-bits body in sub_1D56150.
	currentActorUserStateMsgID       uint16 = 357
	currentActorDefaultUserStateBits byte   = 3
)

func buildCurrentActorUserStateBody(actorKey uint16, stateBits byte) []byte {
	body := make([]byte, 3)
	binary.LittleEndian.PutUint16(body[0:2], actorKey)
	body[2] = stateBits
	return body
}

// sendSelectedActorUserStateRefresh reapplies the repository-backed visibility
// flags to the already-bound live actor. Current NoPack's sub_2690230 does not
// treat an unchanged value as a no-op: when the actor model owner exists it
// detaches and reinserts the visible equipment/avatar components. This is the
// native redraw boundary after op19; unlike class0/op2 mode0 it never replaces
// the actor object and therefore preserves the town camera binding.
func (s *Service) sendSelectedActorUserStateRefresh(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("selected actor user state requires an active character")
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return fmt.Errorf("selected actor user state repository is unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return fmt.Errorf("load selected actor user state character %s: %w", characterID, err)
	}
	if !found {
		return fmt.Errorf("selected actor user state character %s was not found", characterID)
	}
	if accountID := strings.TrimSpace(s.accountIDForSession(session)); accountID != "" &&
		accountID != strings.TrimSpace(character.AccountID) {
		return fmt.Errorf("selected actor user state character %s owner mismatch", characterID)
	}

	stateBits := currentActorDefaultUserStateBits
	if character.Stats != nil {
		if value, exists := character.Stats["user_state_bits"]; exists {
			if value < 0 || value > 0xff {
				return fmt.Errorf("selected actor user state bits %d are outside u8", value)
			}
			stateBits = byte(value)
		}
	}
	actorKey := currentSceneActorObjectKey(session.selectedCharacterID)
	body := buildCurrentActorUserStateBody(actorKey, stateBits)
	s.logPacketEvent("game-upper-selected-actor-user-state-refresh-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"actor_object_key", actorKey,
		"msg_id", currentActorUserStateMsgID,
		"classification", 0,
		"user_state_bits", stateBits,
		"body_len", len(body),
		"body_source", "current_exe_sub_1D56150_u16_actor_key_u8_state_bits_sub_2690230_native_redraw")
	return s.sendGameUpperRawClass(session, currentActorUserStateMsgID, body, 0)
}
