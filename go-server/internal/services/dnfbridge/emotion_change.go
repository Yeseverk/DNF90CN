package dnfbridge

import (
	"context"
	"encoding/binary"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfemotion "longheng.io/server/internal/modules/dnf/emotion"
)

// currentChangeEmotionOpcode is CMD 254 (0x00FE): change emotion/mood.
const currentChangeEmotionOpcode uint16 = uint16(dnfenum.CmdPacketChangeEmotion)

func (s *Service) handleCurrentChangeEmotion(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if len(body) < 2 {
		s.logGameEvent(session, "game-change-emotion-blocked", "body_len", len(body), "reason", "body_too_short")
		return nil
	}
	emotionIndex := binary.LittleEndian.Uint16(body[0:2])
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)

	// Persist emotion_index to character stats.
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if ok {
		owner, err := dnfemotion.NewOwner(repos)
		if err == nil {
			_, err = owner.Apply(ctx, dnfemotion.Command{
				AccountID:           s.accountIDForSession(session),
				SelectedCharacterID: session.selectedCharacterID,
				EmotionIndex:        emotionIndex,
			})
		}
		if err != nil {
			// Emotion ACK behavior is compatibility-sensitive: the old handler
			// acknowledged even when persistence was unavailable.
			s.logGameEvent(session, "game-change-emotion-save-failed",
				"char_id", characterID,
				"emotion_index", emotionIndex,
				"reason", err)
		}
	}

	s.logGameEvent(session, "game-change-emotion",
		"char_id", characterID,
		"emotion_index", emotionIndex)

	// ACK: IDA sub_1D18900 reads u16 emotionIndex from success body.
	// Note: extended-login dispatch re-registers 254 as DoNothing, but the
	// lobby handler sub_1CF5280 may still consume the body.
	var w packetWriter
	w.writeUint16(emotionIndex)
	return s.sendGameUpperSuccess(session, currentChangeEmotionOpcode, w.bytes())
}
