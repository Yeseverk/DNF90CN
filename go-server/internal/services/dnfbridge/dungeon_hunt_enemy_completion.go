package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

var errDungeonHuntEnemyCompletionUnavailable = errors.New("dnf dungeon hunt-enemy completion owner is unavailable")

// persistCurrentDungeonHuntEnemyKillPhaseA mirrors 86JP's kill-trigger domain
// path for [hunt enemy]. It only updates the quest trigger/pending marker. Item
// and EXP rewards remain behind the normal FinishQuest atomic owner.
func (s *Service) persistCurrentDungeonHuntEnemyKillPhaseA(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	monster runtimeDungeonMonster,
	objectKey uint32,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime ||
		monster.Spawn.MonsterID <= 0 || !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		return errDungeonHuntEnemyCompletionUnavailable
	}
	return s.persistCurrentDungeonHuntEnemyTargetPhaseA(
		ctx,
		session,
		runtime,
		scene,
		monster.Spawn.MonsterID,
		currentDungeonQuestEnemyTypeForMonster(monster),
		objectKey,
		"runtime_monster_op39",
	)
}

// persistCurrentDungeonHuntMonsterKillPhaseA is the sibling path for PVF
// [hunt monster] quests. Earlier code initialized those quest triggers but
// never consumed ordinary monster deaths, which left targets such as 巨枪多里安
// stuck at 0/1 forever.
func (s *Service) persistCurrentDungeonHuntMonsterKillPhaseA(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	monster runtimeDungeonMonster,
	objectKey uint32,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime ||
		monster.Spawn.MonsterID <= 0 || !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		return errDungeonHuntEnemyCompletionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Quest == nil {
		return errDungeonHuntEnemyCompletionUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	if _, found, err := repositories.Character.Load(ctx, characterID); err != nil {
		return err
	} else if !found {
		return dnfquest.ErrCharacterNotFound
	}
	if _, found, err := repositories.Quest.Load(ctx, characterID); err != nil {
		return err
	} else if !found {
		return nil
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return fmt.Errorf("load hunt-monster quest catalog: %w", err)
	}
	owner, err := dnfquest.NewOwner(repositories)
	if err != nil {
		return fmt.Errorf("create hunt-monster quest owner: %w", err)
	}
	completedAt := s.gameplayNow().UTC()
	completionKey := fmt.Sprintf(
		"op39:char:%d:dungeon:%d:maze:%d:map:%d:seed:%d:monster:%d:object:%d:completed_at_unix_nano:%d",
		session.selectedCharacterID,
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		scene.Map.Map.ID,
		runtime.Seed,
		monster.Spawn.MonsterID,
		objectKey,
		completedAt.UnixNano(),
	)
	result, err := owner.ApplyHuntMonsterKill(ctx, catalog, characterID, dnfquest.HuntMonsterKillInput{
		DungeonID:     runtime.Dungeon.ID,
		Difficulty:    int64(runtime.Request.Difficulty),
		MonsterCode:   monster.Spawn.MonsterID,
		CompletionKey: completionKey,
		CompletedAt:   completedAt,
	})
	if err != nil {
		return fmt.Errorf("persist hunt-monster quest phase A: %w", err)
	}
	if len(result.ChangedFields) == 0 && len(result.Completions) == 0 {
		return nil
	}
	record, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found {
		return dnfquest.ErrQuestPersistVerify
	}
	body := buildCurrentActiveQuestSnapshotBody(record, true)
	questIDs := make([]int64, 0, len(result.Completions))
	for _, completion := range result.Completions {
		questIDs = append(questIDs, completion.QuestID)
	}
	s.logGameEvent(session, "game-dungeon-hunt-monster-phase-a-persisted",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"map_id", scene.Map.Map.ID,
		"monster_id", monster.Spawn.MonsterID,
		"monster_object_key", objectKey,
		"completion_key", completionKey,
		"completion_count", len(result.Completions),
		"quest_ids", questIDs,
		"changed_fields", result.ChangedFields,
		"idempotent", result.Idempotent,
		"reward_granted", currentDungeonHuntMonsterResultAutoGranted(record, result),
		"snapshot_msg_id", currentActiveQuestSnapshotMsgID,
		"snapshot_body_len", len(body),
		"source", "runtime_monster_op39")
	return s.sendGameUpperRawClass(session, currentActiveQuestSnapshotMsgID, body, 0)
}

func (s *Service) persistCurrentDungeonHuntEnemyTargetPhaseA(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	enemyCode int64,
	enemyType int64,
	objectKey uint32,
	source string,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime ||
		enemyCode <= 0 || enemyType <= 0 || !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		return errDungeonHuntEnemyCompletionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Quest == nil {
		return errDungeonHuntEnemyCompletionUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	if _, found, err := repositories.Character.Load(ctx, characterID); err != nil {
		return err
	} else if !found {
		return dnfquest.ErrCharacterNotFound
	}
	if _, found, err := repositories.Quest.Load(ctx, characterID); err != nil {
		return err
	} else if !found {
		return nil
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return fmt.Errorf("load hunt-enemy quest catalog: %w", err)
	}
	owner, err := dnfquest.NewOwner(repositories)
	if err != nil {
		return fmt.Errorf("create hunt-enemy quest owner: %w", err)
	}
	completedAt := s.gameplayNow().UTC()
	completionKey := fmt.Sprintf(
		"op39:char:%d:dungeon:%d:maze:%d:map:%d:seed:%d:enemy:%d:object:%d:completed_at_unix_nano:%d",
		session.selectedCharacterID,
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		scene.Map.Map.ID,
		runtime.Seed,
		enemyCode,
		objectKey,
		completedAt.UnixNano(),
	)
	result, err := owner.ApplyHuntEnemyKill(ctx, catalog, characterID, dnfquest.HuntEnemyKillInput{
		DungeonID:     runtime.Dungeon.ID,
		Difficulty:    int64(runtime.Request.Difficulty),
		EnemyCode:     enemyCode,
		EnemyType:     enemyType,
		CompletionKey: completionKey,
		CompletedAt:   completedAt,
	})
	if err != nil {
		return fmt.Errorf("persist hunt-enemy quest phase A: %w", err)
	}
	if len(result.ChangedFields) == 0 && len(result.Completions) == 0 {
		return nil
	}
	record, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found {
		return dnfquest.ErrQuestPersistVerify
	}
	body := buildCurrentActiveQuestSnapshotBody(record, true)
	questIDs := make([]int64, 0, len(result.Completions))
	for _, completion := range result.Completions {
		questIDs = append(questIDs, completion.QuestID)
	}
	s.logGameEvent(session, "game-dungeon-hunt-enemy-phase-a-persisted",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"map_id", scene.Map.Map.ID,
		"enemy_code", enemyCode,
		"monster_object_key", objectKey,
		"enemy_type", enemyType,
		"completion_key", completionKey,
		"completion_count", len(result.Completions),
		"quest_ids", questIDs,
		"changed_fields", result.ChangedFields,
		"idempotent", result.Idempotent,
		"reward_granted", currentDungeonHuntEnemyResultAutoGranted(record, result),
		"snapshot_msg_id", currentActiveQuestSnapshotMsgID,
		"snapshot_body_len", len(body),
		"source", source)
	return s.sendGameUpperRawClass(session, currentActiveQuestSnapshotMsgID, body, 0)
}

func currentDungeonHuntEnemyResultAutoGranted(record dnfrepo.QuestRecord, result dnfquest.HuntEnemyPersistResult) bool {
	for _, completion := range result.Completions {
		state, known := currentDungeonHuntEnemyQuestState(record, completion.QuestID)
		if known && state.Status == "completed" && state.Extra["reward_state"] == "granted" {
			return true
		}
	}
	return false
}

func currentDungeonHuntMonsterResultAutoGranted(record dnfrepo.QuestRecord, result dnfquest.HuntMonsterPersistResult) bool {
	for _, completion := range result.Completions {
		state, known := currentDungeonHuntEnemyQuestState(record, completion.QuestID)
		if known && state.Status == "completed" && state.Extra["reward_state"] == "granted" {
			return true
		}
	}
	return false
}

func currentDungeonHuntEnemyQuestState(record dnfrepo.QuestRecord, questID int64) (dnfrepo.QuestState, bool) {
	if state, ok := record.States[questID]; ok {
		return state, true
	}
	if state, ok := record.Progress[questID]; ok {
		return state, true
	}
	return dnfrepo.QuestState{}, false
}

func currentDungeonQuestEnemyTypeForMonster(monster runtimeDungeonMonster) int64 {
	// 86JP's hunt-enemy domain uses type 1 for monsters and type 3 for passive
	// objects. Dungeon actor wire types/ranks are a different namespace: a boss
	// actor must not be reclassified as passive-object quest target type 3.
	_ = monster
	return 1
}
