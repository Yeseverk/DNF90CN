// 本文件按 C# UserInfoBodyBuilder 构造 class0/op2 的 subtype0/subtype1 选角初始化包。
package dnfbridge

import (
	"context"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentActorBaseStatScalePercent byte = 100

const (
	currentMode1StatBlobWireSize  = 92
	currentMode1ExperienceOffset  = 0x17
	currentMode1StatLengthOffset  = 0x1b
	currentMode1StatDataOffset    = 0x1f
	currentMode1ObjectTailOffset  = currentMode1StatDataOffset + currentMode1StatBlobWireSize
	currentMode1CreateCountOffset = currentMode1ObjectTailOffset + 1
	currentMode1CreateRowsOffset  = currentMode1CreateCountOffset + 1
	currentMode1BaseWireSize      = 71 + currentMode1StatBlobWireSize
)

func (s *Service) buildCSharpSelectedUserInfoBody(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, occurrence int, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16, charName string) []byte {
	pvfStats, hasPVFStats := s.characterPVFStatsForUserInfo(ctx, session, character, hasCharacter)
	reader := csharpLegacyUserInfoReader{
		ctx:         ctx,
		repo:        repo,
		characterID: strconv.Itoa(int(charID)),
		service:     s,
		session:     session,
		pvfStats:    pvfStats,
		hasPVFStats: hasPVFStats,
	}
	switch occurrence {
	case 0, 2:
		return reader.buildCSharpUserInfoSubtype0(character, hasCharacter, charID, charName)
	case 1:
		return reader.buildCSharpUserInfoSubtype1(character, hasCharacter, charID)
	default:
		return buildCSharpUserInfoBody(occurrence, charID, charName)
	}
}

func (s *Service) buildCurrentSelectedUserInfoMode1Body(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16) []byte {
	return s.buildCurrentSelectedUserInfoMode1BodyInContext(
		ctx,
		session,
		repo,
		character,
		hasCharacter,
		charID,
		currentSceneObjectContext,
	)
}

func (s *Service) buildCurrentSelectedUserInfoMode1BodyInContext(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16, ownerChannel byte) []byte {
	summary := s.currentAccountAdventureGroupSummaryForPacket(ctx, session, character, hasCharacter)
	return s.buildCurrentSelectedUserInfoMode1BodyWithAdventureLevelInContext(
		ctx,
		session,
		repo,
		character,
		hasCharacter,
		charID,
		uint32(summary.ManageLevel),
		ownerChannel,
	)
}

func (s *Service) buildCurrentSelectedUserInfoMode1BodyWithAdventureLevel(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16, adventureLevel uint32) []byte {
	return s.buildCurrentSelectedUserInfoMode1BodyWithAdventureLevelInContext(
		ctx,
		session,
		repo,
		character,
		hasCharacter,
		charID,
		adventureLevel,
		currentSceneObjectContext,
	)
}

func (s *Service) buildCurrentSelectedUserInfoMode1BodyWithAdventureLevelInContext(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16, adventureLevel uint32, ownerChannel byte) []byte {
	pvfStats, hasPVFStats := s.characterPVFStatsForUserInfo(ctx, session, character, hasCharacter)
	reader := csharpLegacyUserInfoReader{
		ctx:         ctx,
		repo:        repo,
		characterID: strconv.Itoa(int(charID)),
		service:     s,
		session:     session,
		pvfStats:    pvfStats,
		hasPVFStats: hasPVFStats,
	}
	return reader.buildCurrentUserInfoMode1InContext(
		character,
		hasCharacter,
		currentSceneActorObjectKey(charID),
		adventureLevel,
		ownerChannel,
	)
}

func buildCurrentActorBindingMode1Body(objectKey uint16, adventureLevel uint32) []byte {
	return buildCurrentActorBindingMode1BodyInContext(objectKey, adventureLevel, currentSceneObjectContext)
}

func buildCurrentActorBindingMode1BodyInContext(objectKey uint16, adventureLevel uint32, ownerChannel byte) []byte {
	reader := csharpLegacyUserInfoReader{}
	return reader.buildCurrentUserInfoMode1WithEquipmentInContext(
		dnfrepo.CharacterRecord{Level: 1},
		true,
		objectKey,
		false,
		adventureLevel,
		ownerChannel,
	)
}

func (s *Service) buildCurrentActorBindingMode1BodyForSelected(
	ctx context.Context,
	session *gameSession,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	charID uint16,
	adventureLevel uint32,
) []byte {
	return s.buildCurrentActorBindingMode1BodyForSelectedInContext(
		ctx,
		session,
		character,
		hasCharacter,
		charID,
		adventureLevel,
		currentSceneObjectContext,
	)
}

func (s *Service) buildCurrentActorBindingMode1BodyForSelectedInContext(
	ctx context.Context,
	session *gameSession,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	charID uint16,
	adventureLevel uint32,
	ownerChannel byte,
) []byte {
	return s.buildCurrentActorBindingMode1BodyForSelectedWithEquipmentInContext(
		ctx,
		session,
		character,
		hasCharacter,
		charID,
		adventureLevel,
		ownerChannel,
		false,
	)
}

func (s *Service) buildCurrentActorBindingMode1BodyForSelectedWithEquipmentInContext(
	ctx context.Context,
	session *gameSession,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	charID uint16,
	adventureLevel uint32,
	ownerChannel byte,
	includeEquipment bool,
) []byte {
	var repo dnfrepo.LegacyUserInfoRepository
	if repos, ok := s.repositoryGroup(); ok {
		repo = repos.LegacyUserInfo
	}
	pvfStats, hasPVFStats := s.characterPVFStatsForUserInfo(ctx, session, character, hasCharacter)
	reader := csharpLegacyUserInfoReader{
		ctx:         ctx,
		repo:        repo,
		characterID: strconv.Itoa(int(charID)),
		service:     s,
		session:     session,
		pvfStats:    pvfStats,
		hasPVFStats: hasPVFStats,
	}
	return reader.buildCurrentUserInfoMode1WithEquipmentInContext(
		character,
		hasCharacter,
		currentSceneActorObjectKey(charID),
		includeEquipment,
		adventureLevel,
		ownerChannel,
	)
}

func (s *Service) buildCurrentSelectedUserInfoMode3Body(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16) []byte {
	return s.buildCurrentSelectedUserInfoMode3BodyInContext(
		ctx,
		session,
		repo,
		character,
		hasCharacter,
		charID,
		currentSceneObjectContext,
	)
}

func (s *Service) buildCurrentSelectedUserInfoMode3BodyInContext(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16, ownerChannel byte) []byte {
	summary := s.currentAccountAdventureGroupSummaryForPacket(ctx, session, character, hasCharacter)
	return s.buildCurrentSelectedUserInfoMode3BodyWithAdventureLevelInContext(
		ctx,
		session,
		repo,
		character,
		hasCharacter,
		charID,
		uint32(summary.ManageLevel),
		ownerChannel,
	)
}

func (s *Service) buildCurrentSelectedUserInfoMode3BodyWithAdventureLevel(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16, adventureLevel uint32) []byte {
	return s.buildCurrentSelectedUserInfoMode3BodyWithAdventureLevelInContext(
		ctx,
		session,
		repo,
		character,
		hasCharacter,
		charID,
		adventureLevel,
		currentSceneObjectContext,
	)
}

func (s *Service) buildCurrentSelectedUserInfoMode3BodyWithAdventureLevelInContext(
	ctx context.Context,
	session *gameSession,
	repo dnfrepo.LegacyUserInfoRepository,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	charID uint16,
	adventureLevel uint32,
	ownerChannel byte,
) []byte {
	pvfStats, hasPVFStats := s.characterPVFStatsForUserInfo(ctx, session, character, hasCharacter)
	reader := csharpLegacyUserInfoReader{
		ctx:         ctx,
		repo:        repo,
		characterID: strconv.Itoa(int(charID)),
		service:     s,
		session:     session,
		pvfStats:    pvfStats,
		hasPVFStats: hasPVFStats,
	}
	return reader.buildCurrentUserInfoMode3InContext(
		character,
		hasCharacter,
		currentSceneActorObjectKey(charID),
		adventureLevel,
		ownerChannel,
	)
}
