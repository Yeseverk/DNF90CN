package dnfbridge

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonWorldMapUnavailable        = errors.New("dnf dungeon worldmap runtime is unavailable")
	errDungeonMonsterCatalogUnavailable  = errors.New("dnf dungeon monster catalog is unavailable")
	errDungeonCharacterRequired          = errors.New("dnf dungeon selected character is required")
	errDungeonCharacterNotFound          = errors.New("dnf dungeon selected character was not found")
	errDungeonAccountMismatch            = errors.New("dnf dungeon selected character account mismatch")
	errDungeonQuestRepositoryUnavailable = errors.New("dnf dungeon quest repository is unavailable")
	errDungeonDifficultyInvalid          = errors.New("dnf dungeon difficulty is invalid")
	errDungeonLevelTooLow                = dnfdungeon.ErrEntryLevelTooLow
	errDungeonFatigueUnknown             = dnfdungeon.ErrEntryFatigueUnknown
	errDungeonFatigueExhausted           = dnfdungeon.ErrEntryFatigueExhausted
	errDungeonPartyLimit                 = dnfdungeon.ErrEntryPartyLimit
	errDungeonAlreadyActive              = errors.New("dnf dungeon session is already active")
	errDungeonChoiceRequired             = errors.New("dnf dungeon explicit PVF choice is required")
	errDungeonMonsterOwnerMismatch       = errors.New("dnf dungeon monster death related actor object key mismatch")
)

type runtimeDungeonState struct {
	Request                              dungeoncmd.SelectDungeonRequest
	Dungeon                              worldmap.Dungeon
	MazeIndex                            int
	Character                            dnfrepo.CharacterRecord
	Session                              *worldmap.DungeonSession
	Room                                 *runtimeDungeonRoom
	DropOwner                            *runtimeDungeonDropOwner
	NextObjectKey                        uint32
	BossCoordinate                       worldmap.RoomCoordinate
	BossSet                              bool
	LayeredMapIndex                      int
	LayeredMapActive                     bool
	LayerChains                          map[worldmap.RoomCoordinate]*runtimeDungeonLayerChain
	StoryStages                          []worldmap.DungeonStoryStage
	StoryStageIndex                      int
	Seed                                 uint32
	partyMemberIndex                     byte
	partyMemberIndexed                   bool
	Rooms                                map[runtimeDungeonRoomKey]*runtimeDungeonRoomVisit
	startedAt                            time.Time
	bossDieCheckPending                  bool
	bossDieCheckPendingRequest           dungeoncmd.BossDieCheckRequest
	bossDieCheckAccepted                 bool
	bossDieCheckRelatedActorObjectKey    uint16
	bossDieCheckTargetObjectKey          uint16
	bossDieCheckResponseSent             bool
	ordinaryFinalRoomClearAccepted       bool
	tutorialFinalRoomClearPending        bool
	tutorialFinalRoomClearAccepted       bool
	settlementPhase                      currentDungeonSettlementPhase
	settlementEntrySent                  bool
	settlementEntryAt                    time.Time
	settlementPlayResultReceived         bool
	settlementPlayResultBody             []byte
	settlementPlayResultDynamicRows      uint8
	settlementPlayResultOptionalField    bool
	settlementStatisticReceived          bool
	settlementStatisticBody              []byte
	settlementResultPlan                 *currentDungeonSettlementPacketPlan
	settlementResultNoticeSent           bool
	settlementCharacterStateSent         bool
	settlementClearRewardSent            bool
	settlementCardScrollStateSent        bool
	settlementCardRightStateSent         bool
	settlementCardLayoutSent             bool
	settlementCardSelectionKnown         bool
	settlementCardSelected               byte
	settlementCardSelectionSent          bool
	settlementCardRewardCommitted        bool
	settlementCardSideSelectionKnown     [dungeonCardSideCount]bool
	settlementCardSideMember             [dungeonCardSideCount]byte
	settlementCardSideSelectionSent      [dungeonCardSideCount]bool
	settlementCardSideRewardCommitted    [dungeonCardSideCount]bool
	settlementCardAutoFlipGeneration     uint64
	settlementCardAutoFlipTimerName      string
	settlementCardExitAckSent            bool
	settlementCardRewardState            *dungeonCardState
	settlementMonsterExperienceTotal     uint32
	settlementMonsterGrowthContractBonus uint32
	settlementChampionExperience         uint32
	settlementSuperChampionExperience    uint32
	settlementBossExperience             uint32
	tutorialCompletionPersisted          bool
	clearMapCompletionPhaseAPersisted    bool
	clearMapCompletionKey                string
	clearMapCompletionAt                 time.Time
	clearMapCompletionQuestIDs           []int64
	clearMapCompletionPendingQuestIDs    []int64
	clearMapCompletionActiveSnapshotSent bool
	clearMapCompletionNotificationClosed bool
	tutorialCompletedReentry             bool
	tutorialCompletedReentryExitSent     bool
	tutorialFinalFlagAckSent             bool
	tutorialInitialUserStateSent         bool
	townReturnPending                    bool
	townReturnOp24Sent                   bool
	townReturnRequestMsgID               uint16
	townReturnSource                     string
	townReturnOrigin                     currentTownPositionSnapshot
	townReturnTransition                 currentDungeonTownTransition
	lifecycleToken                       uint64
	deathReturnWaiting                   bool
	deathReturnGeneration                uint64
	deathReturnTimerName                 string
	deathReturnDueAt                     time.Time
	deathReturnTransition                currentDungeonTownTransition
	actorPositionX                       uint16
	actorPositionY                       uint16
	actorPositionValid                   bool
	settlementDungeonPermissionSent      bool
	suspiciousVillageElevator            currentDungeonElevatorState
}

func dungeonRuntimeOwnsCharacter(runtime *runtimeDungeonState, characterID uint16) bool {
	return runtime != nil &&
		dnfdungeon.RuntimeOwnsCharacter(runtime.Character.CharacterID, characterID)
}

func (s *Service) preloadDungeonWorldMap(ctx context.Context) error {
	if s == nil {
		return errDungeonWorldMapUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return fmt.Errorf("preload dungeon worldmap pvf: %w", err)
	}
	table, err := worldmap.LoadSource(ctx, archive, worldmap.Options{})
	if err != nil {
		return fmt.Errorf("preload dungeon worldmap table: %w", err)
	}
	resolver, err := worldmap.NewResolver(table)
	if err != nil {
		return fmt.Errorf("preload dungeon worldmap resolver: %w", err)
	}
	monsterTable, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		return fmt.Errorf("preload dungeon monster table: %w", err)
	}
	aiCharacterTable, err := newPVFDungeonAICharacterCatalog(archive)
	if err != nil {
		return fmt.Errorf("preload dungeon AI character table: %w", err)
	}
	tutorialScripts, err := newPVFDungeonTutorialScriptCatalog(archive)
	if err != nil {
		return fmt.Errorf("preload dungeon tutorial script table: %w", err)
	}
	tutorialScripts.indexBasicActionDestroyTargets(archive, table)

	s.worldMapMu.Lock()
	s.worldMapTable = table
	s.worldMapResolver = resolver
	s.dungeonMonsterTable = monsterTable
	s.dungeonAICharacterTable = aiCharacterTable
	s.dungeonTutorialScripts = tutorialScripts
	s.worldMapMu.Unlock()
	snapshot := table.Snapshot()
	tutorialSnapshot := tutorialScripts.Snapshot()
	s.logPacketEvent("dnf-dungeon-worldmap-loaded",
		"maps", snapshot.Maps,
		"areas", snapshot.Areas,
		"dungeons", snapshot.Dungeons,
		"mazes", snapshot.Mazes,
		"dungeon_refs", snapshot.DungeonRefs,
		"monsters", monsterTable.Count(),
		"AI_characters", aiCharacterTable.Count(),
		"tutorial_cinematic_entries", tutorialSnapshot.CinematicEntries,
		"tutorial_cinematics_with_destroy_targets", tutorialSnapshot.CinematicsWithTargets,
		"tutorial_maps_with_destroy_targets", tutorialSnapshot.MapsWithTargets,
		"tutorial_monster_destroy_targets", tutorialSnapshot.MonsterTargets,
		"tutorial_cinematic_read_failures", tutorialSnapshot.ReadFailures,
		"tutorial_cinematic_parse_failures", tutorialSnapshot.ParseFailures,
		"tutorial_basic_action_entries", tutorialSnapshot.BasicActionEntries,
		"tutorial_basic_actions_with_destroy_targets", tutorialSnapshot.BasicActionsWithTargets,
		"tutorial_basic_action_maps", tutorialSnapshot.BasicActionMaps,
		"tutorial_basic_action_monster_targets", tutorialSnapshot.BasicActionTargets,
		"tutorial_basic_action_read_failures", tutorialSnapshot.BasicActionReadFailures,
		"tutorial_basic_action_parse_failures", tutorialSnapshot.BasicActionParseFailures)
	return nil
}

func (s *Service) dungeonMonsterCatalog() (*pvfDungeonMonsterCatalog, error) {
	if s == nil {
		return nil, errDungeonWorldMapUnavailable
	}
	s.worldMapMu.RLock()
	defer s.worldMapMu.RUnlock()
	if s.dungeonMonsterTable == nil {
		return nil, errDungeonMonsterCatalogUnavailable
	}
	return s.dungeonMonsterTable, nil
}

func (s *Service) dungeonAICharacterCatalog() (*pvfDungeonAICharacterCatalog, error) {
	if s == nil {
		return nil, errDungeonWorldMapUnavailable
	}
	s.worldMapMu.RLock()
	defer s.worldMapMu.RUnlock()
	if s.dungeonAICharacterTable == nil {
		return nil, errDungeonAICharacterUnavailable
	}
	return s.dungeonAICharacterTable, nil
}

func (s *Service) dungeonTutorialScriptCatalog() (*pvfDungeonTutorialScriptCatalog, bool) {
	if s == nil {
		return nil, false
	}
	s.worldMapMu.RLock()
	defer s.worldMapMu.RUnlock()
	if s.dungeonTutorialScripts == nil {
		return nil, false
	}
	return s.dungeonTutorialScripts, true
}

func (s *Service) dungeonWorldMap() (*worldmap.Table, *worldmap.Resolver, error) {
	if s == nil {
		return nil, nil, errDungeonWorldMapUnavailable
	}
	s.worldMapMu.RLock()
	defer s.worldMapMu.RUnlock()
	if s.worldMapTable == nil || s.worldMapResolver == nil {
		return nil, nil, errDungeonWorldMapUnavailable
	}
	return s.worldMapTable, s.worldMapResolver, nil
}

func (s *Service) prepareDungeonRuntime(
	ctx context.Context,
	session *gameSession,
	request dungeoncmd.SelectDungeonRequest,
) (*runtimeDungeonState, worldmap.DungeonRoomScene, error) {
	return s.prepareDungeonRuntimePlanned(ctx, session, request, nil)
}

func (s *Service) prepareDungeonRuntimePlanned(
	ctx context.Context,
	session *gameSession,
	request dungeoncmd.SelectDungeonRequest,
	partyPlan *runtimePartyDungeonEntryPlan,
) (*runtimeDungeonState, worldmap.DungeonRoomScene, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return nil, worldmap.DungeonRoomScene{}, errDungeonCharacterRequired
	}
	if request.DungeonID == 0 {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("%w: dungeon=0", worldmap.ErrDungeonNotIndexed)
	}
	if request.Difficulty > 4 {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("%w: got=%d max=4", errDungeonDifficultyInvalid, request.Difficulty)
	}
	table, resolver, err := s.dungeonWorldMap()
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, err
	}
	dungeon, ok := table.FindDungeon(int64(request.DungeonID))
	if !ok {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("%w: dungeon=%d", worldmap.ErrDungeonNotIndexed, request.DungeonID)
	}
	character, err := s.dungeonCharacter(ctx, session.selectedCharacterID, session)
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, err
	}
	if err := validateDungeonEntry(character, dungeon, session); err != nil {
		return nil, worldmap.DungeonRoomScene{}, err
	}
	questRecord, questRecordFound, err := s.dungeonQuestRecord(ctx, session.selectedCharacterID)
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, err
	}
	if reconciled, reconcileErr := s.reconcileActiveQuestClearLinkedSubQuestsForDungeon(ctx, session, "select_dungeon_before_maze_selection"); reconcileErr != nil {
		s.logGameEvent(session, "game-dungeon-quest-clear-linked-subquest-reconcile-failed",
			"dungeon_id", request.DungeonID,
			"difficulty", request.Difficulty,
			"source", "select_dungeon_before_maze_selection",
			"error", reconcileErr)
	} else if reconciled {
		questRecord, questRecordFound, err = s.dungeonQuestRecord(ctx, session.selectedCharacterID)
		if err != nil {
			return nil, worldmap.DungeonRoomScene{}, err
		}
	}

	session.dungeon.mu.Lock()
	if session.dungeon.runtime != nil && session.dungeon.runtime.Session != nil {
		snapshot := session.dungeon.runtime.Session.Snapshot().Run
		if snapshot.Status == worldmap.DungeonRunActive {
			session.dungeon.mu.Unlock()
			return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("%w: dungeon=%d maze=%d", errDungeonAlreadyActive, snapshot.DungeonID, snapshot.MazeIndex)
		}
	}
	session.dungeon.mu.Unlock()

	mazeSelection := dungeonMazeSelection{}
	if partyPlan != nil {
		if partyPlan.dungeonID != dungeon.ID || partyPlan.mazeIndex < 0 || partyPlan.mazeIndex >= len(dungeon.Mazes) {
			return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("party leader dungeon selection is invalid: dungeon=%d maze=%d", partyPlan.dungeonID, partyPlan.mazeIndex)
		}
		mazeSelection.Index = partyPlan.mazeIndex
		mazeSelection.Reason = "ordinary_party_leader_frozen_selection"
	} else {
		var err error
		mazeSelection, err = selectDungeonMaze(dungeon.Mazes, request.Difficulty, questRecord, s.chooseDungeonIndex)
		if err != nil {
			return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("choose dungeon maze: %w", err)
		}
	}
	mazeIndex := mazeSelection.Index
	s.logGameEvent(session, "game-dungeon-maze-selected",
		"dungeon_id", request.DungeonID,
		"difficulty", request.Difficulty,
		"maze_index", mazeIndex,
		"selection_reason", mazeSelection.Reason,
		"quest_id", mazeSelection.QuestID,
		"quest_record_found", questRecordFound,
		"body_source", "runtime_pvf_quest_connection_and_persisted_quest_state")
	chooser := func(choice worldmap.DungeonMapChoice) (int64, error) {
		if partyPlan != nil {
			mapID, ok := partyPlan.maps[choice.Coordinate]
			if !ok {
				return 0, fmt.Errorf("party leader has no frozen map for room %s", choice.Coordinate)
			}
			for _, candidate := range choice.Candidates {
				if candidate.ID == mapID {
					return mapID, nil
				}
			}
			return 0, fmt.Errorf("party leader map %d is not a candidate for room %s", mapID, choice.Coordinate)
		}
		index, chooseErr := s.chooseDungeonIndex(len(choice.Candidates))
		if chooseErr != nil {
			return 0, chooseErr
		}
		return choice.Candidates[index].ID, nil
	}
	topology, err := worldmap.BuildDungeonLayout(resolver, dungeon.ID, mazeIndex, chooser)
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("build dungeon layout: %w", err)
	}
	storyStages, storyErr := resolver.StoryStages(dungeon.ID, mazeIndex)
	if storyErr != nil && !errors.Is(storyErr, worldmap.ErrStoryStageMissing) {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("resolve dungeon story stages: %w", storyErr)
	}
	var bossCoordinate worldmap.RoomCoordinate
	if len(storyStages) != 0 {
		bossCoordinate = storyStages[len(storyStages)-1].Coordinate
	} else {
		bossIndex := 0
		if partyPlan != nil {
			bossIndex = -1
			for index, coordinate := range topology.Bosses {
				if coordinate == partyPlan.boss {
					bossIndex = index
					break
				}
			}
			if bossIndex < 0 {
				return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("party leader boss room %s is not in follower topology", partyPlan.boss)
			}
		} else {
			var err error
			bossIndex, err = s.chooseDungeonIndex(len(topology.Bosses))
			if err != nil {
				return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("choose dungeon boss coordinate: %w", err)
			}
		}
		bossCoordinate = topology.Bosses[bossIndex]
	}
	run, err := worldmap.NewDungeonRun(topology)
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("create dungeon run: %w", err)
	}
	dungeonSession, err := worldmap.NewDungeonSession(run)
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("create dungeon session: %w", err)
	}
	scene, ok := dungeonSession.Scene()
	if !ok {
		return nil, worldmap.DungeonRoomScene{}, errors.New("created dungeon session has no current scene")
	}
	monsterCatalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, err
	}
	room, nextObjectKey, err := newRuntimeDungeonRoom(
		scene,
		monsterCatalog,
		firstDungeonMonsterObjectKey,
	)
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("create dungeon monster owner: %w", err)
	}
	var aiCharacterCatalog *pvfDungeonAICharacterCatalog
	if len(scene.AICharacters) != 0 {
		aiCharacterCatalog, err = s.dungeonAICharacterCatalog()
		if err != nil {
			return nil, worldmap.DungeonRoomScene{}, err
		}
	}
	extendedPlan, err := planRuntimeDungeonExtendedActors(
		scene,
		monsterCatalog,
		aiCharacterCatalog,
		dungeon.Metadata.BasisLevel,
		nextObjectKey,
	)
	if err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("create dungeon extended actor owner: %w", err)
	}
	if err := room.AttachExtendedActors(extendedPlan); err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("attach dungeon extended actor owner: %w", err)
	}
	if _, err := s.configurePVFTutorialBasicActionRoom(dungeon, scene, room); err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("configure tutorial basic-action room: %w", err)
	}
	nextObjectKey = extendedPlan.NextObjectKey
	seed := uint32(0)
	if partyPlan != nil {
		seed = partyPlan.seed
	} else {
		var err error
		seed, err = s.chooseDungeonSeed()
		if err != nil {
			return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("create dungeon room seed: %w", err)
		}
	}
	runtime := &runtimeDungeonState{
		Request: request, Dungeon: dungeon, MazeIndex: mazeIndex,
		Character: character, Session: dungeonSession, Room: room, NextObjectKey: nextObjectKey,
		BossCoordinate: bossCoordinate, BossSet: true,
		LayeredMapIndex: -1, StoryStageIndex: -1, Seed: seed,
		LayerChains: make(map[worldmap.RoomCoordinate]*runtimeDungeonLayerChain),
		StoryStages: append([]worldmap.DungeonStoryStage(nil), storyStages...),
		Rooms:       make(map[runtimeDungeonRoomKey]*runtimeDungeonRoomVisit),
		startedAt:   s.gameplayNow(),
	}
	if _, err := s.applyPVFTutorialBasicActionRoomToSession(
		session,
		runtime,
		scene,
		"initial_room_before_start_map",
	); err != nil {
		return nil, worldmap.DungeonRoomScene{}, fmt.Errorf("apply tutorial basic-action initial room: %w", err)
	}
	scene, _ = dungeonSession.Scene()
	if isPVFTutorialDungeon(runtime) && hasPersistedDungeonTutorialCompletion(character) {
		runtime.tutorialCompletionPersisted = true
		runtime.tutorialCompletedReentry = true
	}
	runtime.Rooms[runtimeDungeonRoomKeyFromScene(scene)] = &runtimeDungeonRoomVisit{
		Scene:   scene,
		Room:    room,
		Seed:    seed,
		DropRNG: seed,
	}
	return runtime, scene, nil
}

func (s *Service) commitDungeonRuntime(session *gameSession, runtime *runtimeDungeonState) error {
	if session == nil || runtime == nil || runtime.Session == nil {
		return errDungeonWorldMapUnavailable
	}
	if !isPVFTutorialDungeon(runtime) {
		characterID := session.selectedCharacterID
		townID, areaID, err := currentSceneTransitionLocation(runtime.Character, true)
		if err != nil {
			return fmt.Errorf("%w: %v", errCurrentDungeonTownReturnOriginUnavailable, err)
		}
		if err := validateCurrentDungeonTownReturnOrigin(runtime.townReturnOrigin, characterID, townID, areaID); err != nil {
			return err
		}
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	if session.dungeon.runtime != nil && session.dungeon.runtime.Session != nil {
		snapshot := session.dungeon.runtime.Session.Snapshot().Run
		if snapshot.Status == worldmap.DungeonRunActive {
			return fmt.Errorf("%w: dungeon=%d maze=%d", errDungeonAlreadyActive, snapshot.DungeonID, snapshot.MazeIndex)
		}
		s.cancelCurrentDungeonDeathReturnLocked(session, session.dungeon.runtime, "new_dungeon_run_replaces_previous_runtime")
		s.cancelCurrentDungeonCardAutoFlipLocked(session, session.dungeon.runtime, "new_dungeon_run_replaces_previous_runtime")
	}
	session.dungeon.runToken = nextCurrentDungeonDeathGeneration(session.dungeon.runToken)
	runtime.lifecycleToken = session.dungeon.runToken
	session.dungeon.runtime = runtime
	return nil
}

func (s *Service) dungeonCharacter(ctx context.Context, characterID uint16, sessions ...*gameSession) (dnfrepo.CharacterRecord, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return dnfrepo.CharacterRecord{}, dungeoncmd.ErrOwnerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	character, found, err := repositories.Character.Load(ctx, strconv.Itoa(int(characterID)))
	if err != nil {
		return dnfrepo.CharacterRecord{}, err
	}
	if !found {
		return dnfrepo.CharacterRecord{}, errDungeonCharacterNotFound
	}
	character = dnfrepo.CloneCharacter(character)
	accountID := strings.TrimSpace(s.accountIDForSession(sessions...))
	if stored := strings.TrimSpace(character.AccountID); stored != "" && accountID != "" && stored != accountID {
		return dnfrepo.CharacterRecord{}, fmt.Errorf("%w: selected=%s stored=%s", errDungeonAccountMismatch, accountID, stored)
	}
	return character, nil
}

func (s *Service) dungeonQuestRecord(ctx context.Context, characterID uint16) (dnfrepo.QuestRecord, bool, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Quest == nil {
		return dnfrepo.QuestRecord{}, false, errDungeonQuestRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	characterKey := strconv.Itoa(int(characterID))
	record, found, err := repositories.Quest.Load(ctx, characterKey)
	if err != nil {
		return dnfrepo.QuestRecord{}, false, err
	}
	if !found {
		return dnfrepo.QuestRecord{CharacterID: characterKey}, false, nil
	}
	return dnfrepo.CloneQuest(record), true, nil
}

func validateDungeonEntry(character dnfrepo.CharacterRecord, dungeon worldmap.Dungeon, session *gameSession) error {
	partyCount := 1
	if session != nil {
		session.party.mu.Lock()
		if len(session.party.state.Members) > partyCount {
			partyCount = len(session.party.state.Members)
		}
		session.party.mu.Unlock()
	}
	fatigue, fatigueKnown := dnfdungeon.CharacterStat(character.Stats, "fatigue", "fp", "疲劳")
	return dnfdungeon.ValidateEntry(dnfdungeon.EntryPolicy{
		CharacterLevel:      character.Level,
		MinimumLevel:        dungeon.Metadata.MinimumRequiredLevel.Value,
		MinimumLevelSet:     dungeon.Metadata.MinimumRequiredLevel.Set,
		PartyCount:          partyCount,
		PartyLimit:          dungeon.Metadata.LimitPartyCount.Value,
		PartyLimitSet:       dungeon.Metadata.LimitPartyCount.Set,
		NoFatigue:           dungeon.Metadata.NoFatigue,
		EnterWithoutFatigue: dungeon.Metadata.Flags["enter without fatigue"],
		Fatigue:             fatigue,
		FatigueKnown:        fatigueKnown,
	})
}

func (s *Service) chooseDungeonIndex(limit int) (int, error) {
	if limit <= 0 {
		return 0, worldmap.ErrMazeNotFound
	}
	if limit == 1 {
		return 0, nil
	}
	if s != nil && s.dungeonChoice != nil {
		index, err := s.dungeonChoice(limit)
		if err != nil {
			return 0, err
		}
		if index < 0 || index >= limit {
			return 0, fmt.Errorf("dungeon choice index %d outside [0,%d)", index, limit)
		}
		return index, nil
	}
	index, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, fmt.Errorf("choose dungeon random index: %w", err)
	}
	return int(index.Int64()), nil
}

func (s *Service) chooseDungeonSeed() (uint32, error) {
	if s != nil && s.dungeonSeed != nil {
		return s.dungeonSeed()
	}
	var data [4]byte
	if _, err := cryptorand.Read(data[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data[:]), nil
}
