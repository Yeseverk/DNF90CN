package dnfbridge

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const questObjectRoomClearQuestList = "3146 `parent.qst`\n3157 `passive.qst`\n"

var questObjectRoomClearQuestFiles = map[string]string{
	"n_quest/parent.qst":  "[grade]\n`[epic]`\n[type]\n`[quest clear]`\n[int data]\n3157\n",
	"n_quest/passive.qst": "[grade]\n`[sub]`\n[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13099 3 1\n",
}

func newQuestObjectRoomClearHarness(
	t *testing.T,
	questList string,
	questFiles map[string]string,
	connection []int64,
	states map[int64]dnfrepo.QuestState,
) (*Service, *gameSession, *runtimeDungeonState, worldmap.DungeonRoomScene, dnfrepo.Group, *bufferConn) {
	t.Helper()
	ctx := context.Background()
	source := bridgePVFSource{dnfquest.DefaultList: questList}
	for path, body := range questFiles {
		source[path] = body
	}
	index, err := dnfpvf.Build(ctx, source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(ctx, index)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Level: 5}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: states}); err != nil {
		t.Fatal(err)
	}
	runtime := &runtimeDungeonState{
		Request:   dungeoncmd.SelectDungeonRequest{DungeonID: 3, Difficulty: 0},
		Dungeon:   worldmap.Dungeon{ID: 3, Mazes: []worldmap.Maze{{}, {}, {QuestConnection: connection}}},
		Character: dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Level: 5},
		Session:   &worldmap.DungeonSession{},
		Room: &runtimeDungeonRoom{
			monsters:    []runtimeDungeonMonster{{ObjectKey: 433, State: runtimeDungeonMonsterDefeated}},
			byObjectKey: map[uint32]int{433: 0},
		},
		MazeIndex:      2,
		BossSet:        true,
		BossCoordinate: worldmap.RoomCoordinate{X: 4, Y: 1},
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "quest-object-room-clear", selectedCharacterID: 19, dungeon: dungeonSessionState{runtime: runtime}}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate: worldmap.RoomCoordinate{X: 4, Y: 1},
		Boss:       true,
		Map:        worldmap.ResolvedMap{Map: worldmap.Map{ID: 76136}},
	}
	return service, session, runtime, scene, repositories, conn
}

func TestQuestObjectRoomClearCredits3157AndRecomputes3146(t *testing.T) {
	ctx := context.Background()
	service, session, runtime, scene, repositories, conn := newQuestObjectRoomClearHarness(
		t,
		questObjectRoomClearQuestList,
		questObjectRoomClearQuestFiles,
		[]int64{0, 3146},
		map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 1},
			3157: {Status: "active", ProgressValue: 1},
		},
	)

	credited, err := service.creditCurrentDungeonQuestObjectRoomClearLocked(session, runtime, scene, 433, "test_boss_forced_room_clear")
	if err != nil || !credited {
		t.Fatalf("room-clear credit: credited=%t err=%v", credited, err)
	}
	questPacket, trailing := splitGameServerUpperPacket(t, conn.write.Bytes())
	if questPacket.Header.MsgID != currentActiveQuestSnapshotMsgID || questPacket.Header.Classification != 0 || len(trailing) != 0 {
		t.Fatalf("quest snapshot header=%+v body=%x trailing=%x", questPacket.Header, questPacket.Body, trailing)
	}
	persisted, found, err := repositories.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load persisted quest: found=%t err=%v", found, err)
	}
	if child := persisted.States[3157]; child.Status != "completed" || child.ProgressValue != 0 ||
		child.Extra["completion_enemy_code"] != "13099" || child.Extra["completion_enemy_type"] != "3" {
		t.Fatalf("3157=%+v", child)
	}
	if parent := persisted.States[3146]; parent.Status != "active" || parent.ProgressValue != 0 {
		t.Fatalf("3146=%+v", parent)
	}
}

func TestQuestObjectRoomClearSecondClearStaysQuiet(t *testing.T) {
	service, session, runtime, scene, _, conn := newQuestObjectRoomClearHarness(
		t,
		questObjectRoomClearQuestList,
		questObjectRoomClearQuestFiles,
		[]int64{0, 3146},
		map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 1},
			3157: {Status: "active", ProgressValue: 1},
		},
	)

	credited, err := service.creditCurrentDungeonQuestObjectRoomClearLocked(session, runtime, scene, 433, "test_boss_forced_room_clear")
	if err != nil || !credited {
		t.Fatalf("first room-clear credit: credited=%t err=%v", credited, err)
	}
	firstBytes := len(conn.write.Bytes())
	credited, err = service.creditCurrentDungeonQuestObjectRoomClearLocked(session, runtime, scene, 433, "test_boss_forced_room_clear")
	if err != nil {
		t.Fatalf("second room-clear credit: err=%v", err)
	}
	if credited {
		t.Fatal("second room-clear credit was applied again")
	}
	if len(conn.write.Bytes()) != firstBytes {
		t.Fatalf("second room-clear credit wrote %d extra bytes", len(conn.write.Bytes())-firstBytes)
	}
}

func TestQuestObjectRoomClearBlockedGuards(t *testing.T) {
	activeStates := map[int64]dnfrepo.QuestState{
		3146: {Status: "active", ProgressValue: 1},
		3157: {Status: "active", ProgressValue: 1},
	}
	cloneStates := func() map[int64]dnfrepo.QuestState {
		out := make(map[int64]dnfrepo.QuestState, len(activeStates))
		for id, state := range activeStates {
			out[id] = state
		}
		return out
	}
	cases := []struct {
		name        string
		questList   string
		questFiles  map[string]string
		connection  []int64
		states      map[int64]dnfrepo.QuestState
		mutateScene func(*worldmap.DungeonRoomScene)
	}{
		{
			name:       "maze_not_quest_connected",
			questList:  questObjectRoomClearQuestList,
			questFiles: questObjectRoomClearQuestFiles,
			connection: nil,
			states:     cloneStates(),
		},
		{
			name:       "maze_quest_connection_not_parent",
			questList:  questObjectRoomClearQuestList,
			questFiles: questObjectRoomClearQuestFiles,
			connection: []int64{0, 9999},
			states:     cloneStates(),
		},
		{
			name:       "parent_main_quest_not_active",
			questList:  questObjectRoomClearQuestList,
			questFiles: questObjectRoomClearQuestFiles,
			connection: []int64{0, 3146},
			states: func() map[int64]dnfrepo.QuestState {
				states := cloneStates()
				states[3146] = dnfrepo.QuestState{Status: "active", ProgressValue: 0}
				return states
			}(),
		},
		{
			name:       "current_room_not_authoritative_runtime_boss",
			questList:  questObjectRoomClearQuestList,
			questFiles: questObjectRoomClearQuestFiles,
			connection: []int64{0, 3146},
			states:     cloneStates(),
			mutateScene: func(scene *worldmap.DungeonRoomScene) {
				scene.Boss = false
			},
		},
		{
			name:       "boss_coordinate_mismatch",
			questList:  questObjectRoomClearQuestList,
			questFiles: questObjectRoomClearQuestFiles,
			connection: []int64{0, 3146},
			states:     cloneStates(),
			mutateScene: func(scene *worldmap.DungeonRoomScene) {
				scene.Coordinate = worldmap.RoomCoordinate{X: 3, Y: 1}
			},
		},
		{
			name:      "active_type3_target_count_not_unique",
			questList: strings.Replace(questObjectRoomClearQuestList, "\n", "\n3158 `passive2.qst`\n", 1),
			questFiles: func() map[string]string {
				files := make(map[string]string, len(questObjectRoomClearQuestFiles)+1)
				for path, body := range questObjectRoomClearQuestFiles {
					files[path] = body
				}
				files["n_quest/passive2.qst"] = "[grade]\n`[sub]`\n[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13098 3 1\n"
				return files
			}(),
			connection: []int64{0, 3146},
			states: func() map[int64]dnfrepo.QuestState {
				states := cloneStates()
				states[3158] = dnfrepo.QuestState{Status: "active", ProgressValue: 1}
				return states
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			service, session, runtime, scene, repositories, conn := newQuestObjectRoomClearHarness(
				t, tc.questList, tc.questFiles, tc.connection, tc.states,
			)
			if tc.mutateScene != nil {
				tc.mutateScene(&scene)
			}
			credited, err := service.creditCurrentDungeonQuestObjectRoomClearLocked(session, runtime, scene, 433, "test_boss_forced_room_clear")
			if err != nil {
				t.Fatalf("blocked room-clear credit: err=%v", err)
			}
			if credited {
				t.Fatal("guarded room-clear credit was applied")
			}
			if len(conn.write.Bytes()) != 0 {
				t.Fatalf("blocked room-clear credit wrote %d bytes", len(conn.write.Bytes()))
			}
			persisted, found, err := repositories.Quest.Load(ctx, "19")
			if err != nil || !found {
				t.Fatalf("load persisted quest: found=%t err=%v", found, err)
			}
			if child := persisted.States[3157]; child.Status != "active" || child.ProgressValue != 1 {
				t.Fatalf("3157 changed under blocked guard: %+v", child)
			}
		})
	}
}

func TestForceClearWithoutTargetsSkipsRoomClearCredit(t *testing.T) {
	service, session, runtime, _, repositories, conn := newQuestObjectRoomClearHarness(
		t,
		questObjectRoomClearQuestList,
		questObjectRoomClearQuestFiles,
		[]int64{0, 3146},
		map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 1},
			3157: {Status: "active", ProgressValue: 1},
		},
	)
	runtime.Room = &runtimeDungeonRoom{}

	forced, visual, cleared, err := service.forceCurrentDungeonRemainingHostilesForCombatEndLocked(session, runtime, 433, "test_empty_room")
	if err != nil || forced != 0 || visual != 0 || cleared {
		t.Fatalf("force clear empty room: forced=%d visual=%d cleared=%t err=%v", forced, visual, cleared, err)
	}
	if len(conn.write.Bytes()) != 0 {
		t.Fatalf("empty-room force clear wrote %d bytes", len(conn.write.Bytes()))
	}
	persisted, found, err := repositories.Quest.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load persisted quest: found=%t err=%v", found, err)
	}
	if child := persisted.States[3157]; child.Status != "active" || child.ProgressValue != 1 {
		t.Fatalf("3157 changed without forced targets: %+v", child)
	}
}
