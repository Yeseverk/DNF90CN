// character.go 负责 DNF 最新客户端创建角色链路的协议适配。
// 它只解析/编码旧客户端 wire 形态，并通过 DNF repository 写基础记录；完整玩法规则后续应下沉到角色 owner。
package dnfbridge

import (
	"context"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/jobmap"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) handleUpperCreateCharacter(session *gameSession, body []byte) error {
	req, err := parseCreateCharacter(body, 30)
	if err != nil {
		s.logGameEvent(session, "game-upper-create-parse-failed", "body_len", len(body), "error", err)
		if looksOpaqueNoPackUpperCreate(body) {
			// IDA sub_33AAFE0 和本地 hook 明密文对照证明 op5 C2S body 使用 msgID%14=5 的 Skipjack 8 字节块保护。
			// 创建角色是写路径，必须先还原真实职业/名字字段，再允许写仓储。
			plain, decErr := decodeUpperKey5(body)
			if decErr == nil {
				req, err = parseCreateCharacter(plain, 30)
			}
			if decErr != nil || err != nil {
				s.logGameEvent(session, "game-upper-create-decode-failed", "body_len", len(body), "decode_error", decErr, "parse_error", err)
				return s.sendUpperProtectedCreateDecodeDeferred(session)
			}
			s.logGameEvent(session, "game-upper-create-decoded", "body_len", len(body), "plain_len", len(plain), "name_len", len(req.nameRaw), "option_len", len(req.options))
		}
		if err != nil {
			return s.sendUpperCreateError(session, 0x04)
		}
	}
	if !jobmap.Valid(int(req.job)) {
		return s.sendUpperCreateError(session, 0x04)
	}
	return s.createCharacter(session, req, true)
}

func (s *Service) handleGameCreateCharacter(session *gameSession, body []byte) error {
	req, err := parseCreateCharacter(body, 18)
	if err != nil {
		s.logGameEvent(session, "game-create-parse-failed", "body_len", len(body), "error", err)
		return s.sendGameCreateError(session, 0x12)
	}
	if !jobmap.Valid(int(req.job)) {
		return s.sendGameCreateError(session, 0x04)
	}
	return s.createCharacter(session, req, false)
}

func (s *Service) handleGameCheckName(session *gameSession, body []byte) error {
	name, code, ok := parseCheckName(body)
	if !ok {
		if looksNoPackCheckName(body, code) {
			// NoPack 当前 692 请求 body 是 8/16 字节保护态；日志里请求仍是 cmd=1/type=692，
			// 因此保持同 opcode 回 01，先让客户端进入确认创建。
			s.logGameEvent(session, "game-check-name-opaque-accepted", "body_len", len(body), "code", 0x01)
			return s.sendGameCheckName(session, 0x01)
		}
		s.logGameEvent(session, "game-check-name-parse-failed", "body_len", len(body), "code", code)
		return s.sendGameCheckName(session, code)
	}
	repos, hasRepos := s.repositoryGroup()
	if !hasRepos || repos.Character == nil {
		s.logGameEvent(session, "game-check-name-repository-missing", "name", name)
		return s.sendGameCheckName(session, 0x14)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	existing, err := s.characterNameExists(ctx, repos, nil, name)
	if err != nil {
		s.logGameEvent(session, "game-check-name-failed", "name", name, "error", err)
		return s.sendGameCheckName(session, 0x14)
	}
	if existing {
		s.logGameEvent(session, "game-check-name-duplicated", "name", name)
		return s.sendGameCheckName(session, 0x00)
	}
	s.logGameEvent(session, "game-check-name-available", "name", name)
	return s.sendGameCheckName(session, 0x01)
}

func (s *Service) sendCreateSuccess(session *gameSession, upper bool, created dnfrepo.CharacterRecord, characters []dnfrepo.CharacterRecord) error {
	if upper {
		if err := s.sendUpperCreateSuccess(session, created); err != nil {
			return err
		}
		return s.sendUpperRoster(session, characters)
	}
	if err := s.sendGame(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeCreateCharacter), []byte{0x01}); err != nil {
		return err
	}
	return s.sendCharacterList(session, characters)
}

func (s *Service) sendCreateFailure(session *gameSession, upper bool, code byte) error {
	if upper {
		return s.sendUpperCreateError(session, code)
	}
	return s.sendGameCreateError(session, code)
}

func (s *Service) sendUpperCreateError(session *gameSession, code byte) error {
	return s.sendGameUpperFailure(session, uint16(dnfenum.UpperMsgCreateCharacter), code)
}

func (s *Service) sendUpperCreateSuccess(session *gameSession, record dnfrepo.CharacterRecord) error {
	// Current EXE upper_handle_msg5_create_character_result receives the
	// common command-success discriminator first, then reads u16 character ID
	// and WSTR name from the remaining body.  Do not let the character ID low
	// byte stand in for that discriminator: IDs other than 1 would shift every
	// subsequent field by one byte.
	return s.sendGameUpperSuccess(session, uint16(dnfenum.UpperMsgCreateCharacter), buildUpperCreateSuccessBody(record))
}

func buildUpperCreateSuccessBody(record dnfrepo.CharacterRecord) []byte {
	var writer packetWriter
	writer.writeUint16(uint16(numericCharacterID(record)))
	writer.writeRawDstr(rosterRawNameBytes(record))
	return writer.bytes()
}

func (s *Service) sendUpperProtectedCreateDecodeDeferred(session *gameSession) error {
	// 当前 NoPack 未开启 create hook 时会发送动态保护态 msg5；未还原真实 job/name 前不能写库。
	// 这里回非致命拒绝码，避免 0x14 被客户端显示成网络连接中断；真实创建由明文 hook/解码路径处理。
	return s.sendUpperCreateError(session, createCodeDuplicated)
}

func (s *Service) sendGameCreateError(session *gameSession, code byte) error {
	return s.sendGame(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeCreateCharacter), []byte{code})
}

func (s *Service) sendGameCheckName(session *gameSession, code byte) error {
	// Current sub_1CFC9C0 is registered through the common class1 result
	// dispatcher: success consumes no command-specific body, while failure
	// receives the second byte as its error code. Keep this response plaintext.
	body := []byte{1}
	if code != 1 {
		body = []byte{0, code}
	}
	return s.sendGameUpperRawClassNoCodec(session, uint16(dnfenum.UpperMsgCheckDoubleCharName), body, dnfproto.DefaultChannelClassification)
}
