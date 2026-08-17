package dnfbridge

import (
	"context"
	"fmt"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const currentDungeonQuestEnemyTypePassiveObject = int64(3)

// currentDungeonQuestPassiveObjectTarget attempts a guarded binding for an
// unannounced client-side quest-object death. The current EXE packet proves
// only a variable op39 with owner=0xffff and a runtime object key; it does not
// carry or prove the PVF enemy code. The PVF code below is therefore selected
// only as a constrained inference from durable active quest state.
//
// The binding is deliberately narrow: exactly one active type-3 hunt target,
// a no-reward sub quest with an active main quest, and an object key directly
// preceding the first announced room actor. Ambiguous or ordinary unknown
// op39 requests remain rejected.
func (s *Service) currentDungeonQuestPassiveObjectTarget(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	objectKey uint32,
) (dnfquest.ActiveHuntEnemyTarget, bool, error) {
	if session == nil || runtime == nil || runtime.Room == nil || session.dungeon.runtime != runtime ||
		!dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) ||
		!currentDungeonQuestObjectImmediatelyPrecedesAnnouncedActor(runtime.Room, objectKey) {
		return dnfquest.ActiveHuntEnemyTarget{}, false, nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Quest == nil {
		return dnfquest.ActiveHuntEnemyTarget{}, false, errDungeonHuntEnemyCompletionUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	record, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil {
		return dnfquest.ActiveHuntEnemyTarget{}, false, err
	}
	if !found {
		return dnfquest.ActiveHuntEnemyTarget{}, false, nil
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return dnfquest.ActiveHuntEnemyTarget{}, false, err
	}
	targets := catalog.ActiveHuntEnemyTargets(
		record,
		runtime.Dungeon.ID,
		int64(runtime.Request.Difficulty),
		currentDungeonQuestEnemyTypePassiveObject,
	)
	if len(targets) != 1 {
		return dnfquest.ActiveHuntEnemyTarget{}, false, nil
	}
	target := targets[0]
	definition, known := catalog.Find(target.QuestID)
	if !known || normalizeDungeonPVFSymbol(definition.Grade) != "sub" || definition.MainQuestID <= 0 ||
		definition.HasGoldReward || len(definition.RewardItems) != 0 || len(definition.RewardSelectionItems) != 0 {
		return dnfquest.ActiveHuntEnemyTarget{}, false, nil
	}
	parent, parentKnown := currentDungeonHuntEnemyQuestState(record, definition.MainQuestID)
	if !parentKnown || parent.Status != "active" || parent.ProgressValue <= 0 {
		return dnfquest.ActiveHuntEnemyTarget{}, false, nil
	}
	return target, true, nil
}

func currentDungeonQuestObjectImmediatelyPrecedesAnnouncedActor(room *runtimeDungeonRoom, objectKey uint32) bool {
	if room == nil || objectKey == 0 || objectKey >= uint32(^uint16(0)) {
		return false
	}
	snapshot := room.Snapshot()
	first := uint32(0)
	consider := func(key uint32, state runtimeDungeonMonsterState) {
		if state != runtimeDungeonMonsterAnnounced || key == 0 {
			return
		}
		if first == 0 || key < first {
			first = key
		}
	}
	for _, monster := range snapshot.Monsters {
		consider(monster.ObjectKey, monster.State)
	}
	for _, actor := range snapshot.ExtendedActors {
		consider(actor.ObjectKey, actor.State)
	}
	return first != 0 && objectKey+1 == first
}

func (s *Service) handleCurrentDungeonQuestPassiveObjectDeathLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	target, matched, err := s.currentDungeonQuestPassiveObjectTarget(ctx, session, runtime, objectKey)
	if err != nil || !matched {
		return false, err
	}
	responseBody, err := buildCurrentDungeonDeathNotificationBody(
		objectKey,
		currentDungeonDeathResponseAICharacter,
	)
	if err != nil {
		return false, err
	}
	// Match the established monster owner order: acknowledge the authoritative
	// current-EXE op39 first, then persist/send the active-quest snapshot.
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketNotifyDieMonster), responseBody, 0); err != nil {
		return false, err
	}
	if err := s.persistCurrentDungeonHuntEnemyTargetPhaseA(
		ctx,
		session,
		runtime,
		scene,
		target.EnemyCode,
		target.EnemyType,
		objectKey,
		"guarded_inference_current_exe_op39_unique_active_pvf_passive_target",
	); err != nil {
		return true, fmt.Errorf("persist passive-object hunt target: %w", err)
	}
	s.logGameEvent(session, "game-dungeon-quest-passive-object-destroyed",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"map_id", scene.Map.Map.ID,
		"object_key", objectKey,
		"quest_id", target.QuestID,
		"enemy_code", target.EnemyCode,
		"enemy_type", target.EnemyType,
		"response_opcode", uint16(dnfenum.CmdPacketNotifyDieMonster),
		"response_body_len", len(responseBody),
		"source", "guarded_inference_current_exe_variable_op39_owner_ffff_unique_active_pvf_passive_target")
	return true, nil
}
