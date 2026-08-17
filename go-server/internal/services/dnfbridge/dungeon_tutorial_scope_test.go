package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	tutorialScopeATSwordmanDungeonReference = "Cataclysm/NewTutorial/Tutorial_ATSwordman.dgn"
	tutorialScopeATSwordmanMapDirectory     = "Cataclysm/NewTutorial/ATSwordman"
	tutorialScopeKnightFDungeonReference    = "Cataclysm/NewTutorial/knight_F.dgn"
	tutorialScopeKnightFMapDirectory        = "Cataclysm/NewTutorial/knight_F"
	tutorialScopeOtherDungeonReference      = "Cataclysm/NewTutorial/Tutorial_Swordman.dgn"
	tutorialScopeOtherMapDirectory          = "Cataclysm/NewTutorial/Swordman"
	tutorialScopeGunnerDungeonReference     = "Cataclysm/NewTutorial/Tutorial_Gunner.dgn"
	tutorialScopeGunnerMapDirectory         = "Cataclysm/NewTutorial/Gunner"
)

func TestIsATSwordmanTutorialSceneRequiresExactScopedOwner(t *testing.T) {
	_, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	scene, ok := runtime.Session.Scene()
	if !ok {
		t.Fatal("prepared tutorial runtime has no current scene")
	}
	if runtime.Character.Job != "11" {
		t.Fatalf("fixture job=%q want=11", runtime.Character.Job)
	}
	if runtime.Dungeon.Path != "dungeon/"+tutorialScopeATSwordmanDungeonReference {
		t.Fatalf("fixture dungeon path=%q", runtime.Dungeon.Path)
	}
	if scene.Map.Map.Path != "map/"+tutorialScopeATSwordmanMapDirectory+"/boss.map" || !scene.Boss {
		t.Fatalf("fixture scene path=%q boss=%t", scene.Map.Map.Path, scene.Boss)
	}
	if !isATSwordmanTutorialScene(runtime, scene) {
		t.Fatal("exact ATSwordman tutorial Boss scene was not recognized")
	}

	// The runtime PVF preserves mixed case. Also require slash/case
	// normalization without relaxing either the exact dungeon owner or the map
	// directory boundary.
	normalizedRuntime := *runtime
	normalizedRuntime.Dungeon = runtime.Dungeon
	normalizedRuntime.Dungeon.Path = ` .\DUNGEON\Cataclysm\NewTutorial\Tutorial_ATSwordman.dgn `
	normalizedScene := scene
	normalizedScene.Map.Map.Path = ` MAP\Cataclysm\NewTutorial\ATSwordman\boss.map `
	if !isATSwordmanTutorialScene(&normalizedRuntime, normalizedScene) {
		t.Fatal("normalized ATSwordman tutorial paths were not recognized")
	}

	tests := []struct {
		name   string
		mutate func(*runtimeDungeonState, *worldmap.DungeonRoomScene)
	}{
		{
			name: "other job",
			mutate: func(candidate *runtimeDungeonState, _ *worldmap.DungeonRoomScene) {
				candidate.Character.Job = "0"
			},
		},
		{
			name: "other profession dungeon path",
			mutate: func(candidate *runtimeDungeonState, _ *worldmap.DungeonRoomScene) {
				candidate.Dungeon.Path = "dungeon/" + tutorialScopeOtherDungeonReference
			},
		},
		{
			name: "dungeon path suffix collision",
			mutate: func(candidate *runtimeDungeonState, _ *worldmap.DungeonRoomScene) {
				candidate.Dungeon.Path = "dungeon/" + tutorialScopeATSwordmanDungeonReference + ".copy"
			},
		},
		{
			name: "other profession map path",
			mutate: func(_ *runtimeDungeonState, candidate *worldmap.DungeonRoomScene) {
				candidate.Map.Map.Path = "map/" + tutorialScopeOtherMapDirectory + "/boss.map"
			},
		},
		{
			name: "map prefix collision",
			mutate: func(_ *runtimeDungeonState, candidate *worldmap.DungeonRoomScene) {
				candidate.Map.Map.Path = "map/" + tutorialScopeATSwordmanMapDirectory + "Extra/boss.map"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateRuntime := *runtime
			candidateRuntime.Dungeon = runtime.Dungeon
			candidateRuntime.Character = runtime.Character
			candidateScene := scene
			test.mutate(&candidateRuntime, &candidateScene)
			if isATSwordmanTutorialScene(&candidateRuntime, candidateScene) {
				t.Fatalf("unscoped scene accepted: job=%q dungeon=%q map=%q boss=%t",
					candidateRuntime.Character.Job,
					candidateRuntime.Dungeon.Path,
					candidateScene.Map.Map.Path,
					candidateScene.Boss,
				)
			}
		})
	}
}

func TestIsPVFTutorialDungeonSceneUsesMetadataNotProfessionOrPath(t *testing.T) {
	tests := []tutorialScopeFixtureOptions{
		{},
		{
			job:                "12",
			dungeonReference:   tutorialScopeKnightFDungeonReference,
			mapDirectory:       tutorialScopeKnightFMapDirectory,
			currentRoomNonBoss: true,
			bossRank:           "[normal]",
			singleMonster:      true,
		},
		{
			job:                "0",
			dungeonReference:   tutorialScopeOtherDungeonReference,
			mapDirectory:       tutorialScopeOtherMapDirectory,
			currentRoomNonBoss: true,
		},
	}
	for index, fixture := range tests {
		t.Run(fmt.Sprintf("profession_%d", index), func(t *testing.T) {
			_, runtime := prepareTutorialScopeRuntime(t, fixture)
			scene, ok := runtime.Session.Scene()
			if !ok {
				t.Fatal("prepared tutorial runtime has no current scene")
			}
			if !isPVFTutorialDungeonScene(runtime, scene) {
				t.Fatalf("PVF tutorial scene rejected: job=%q dungeon=%q map=%q metadata=%+v",
					runtime.Character.Job,
					runtime.Dungeon.Path,
					scene.Map.Map.Path,
					runtime.Dungeon.Metadata.TutorialDungeon,
				)
			}
		})
	}

	for _, fixture := range []tutorialScopeFixtureOptions{
		{omitTutorialFlag: true},
		{disableTutorial: true},
	} {
		_, runtime := prepareTutorialScopeRuntime(t, fixture)
		scene, ok := runtime.Session.Scene()
		if !ok {
			t.Fatal("prepared non-tutorial runtime has no current scene")
		}
		if isPVFTutorialDungeonScene(runtime, scene) {
			t.Fatalf("non-enabled tutorial metadata accepted: %+v", runtime.Dungeon.Metadata.TutorialDungeon)
		}
	}

	_, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	scene, ok := runtime.Session.Scene()
	if !ok {
		t.Fatal("prepared tutorial runtime has no current scene")
	}
	staleScene := scene
	staleScene.Map.Map.ID++
	if isPVFTutorialDungeonScene(runtime, staleScene) {
		t.Fatal("stale map scene was accepted as the current tutorial room")
	}
	staleScene = scene
	staleScene.Coordinate.X++
	if isPVFTutorialDungeonScene(runtime, staleScene) {
		t.Fatal("stale coordinate was accepted as the current tutorial room")
	}
}

func TestRuntimeDungeonRoomAnnouncedMonsterReturnsOnlyAnnouncedOrdinaryMonster(t *testing.T) {
	_, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	before := runtime.Room.Snapshot()
	if len(before.Monsters) != 2 {
		t.Fatalf("fixture monsters=%+v", before.Monsters)
	}
	bossKey := before.Monsters[0].ObjectKey
	normalKey := before.Monsters[1].ObjectKey
	if _, ok := runtime.Room.AnnouncedMonster(bossKey); ok {
		t.Fatal("planned ordinary monster was reported as announced")
	}

	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce tutorial actors: %v", err)
	}
	boss, ok := runtime.Room.AnnouncedMonster(bossKey)
	if !ok || boss.ObjectKey != bossKey || boss.State != runtimeDungeonMonsterAnnounced || boss.Spawn.Rank != "[boss]" {
		t.Fatalf("announced Boss lookup=(%+v,%t)", boss, ok)
	}
	normal, ok := runtime.Room.AnnouncedMonster(normalKey)
	if !ok || normal.ObjectKey != normalKey || normal.State != runtimeDungeonMonsterAnnounced || normal.Spawn.Rank != "[normal]" {
		t.Fatalf("announced normal lookup=(%+v,%t)", normal, ok)
	}
	if _, ok := runtime.Room.AnnouncedMonster(999999); ok {
		t.Fatal("unknown object key was reported as an announced ordinary monster")
	}

	if _, cleared, err := runtime.Room.CommitActorDeathReport(bossKey, runtime.Session); err != nil || cleared {
		t.Fatalf("commit one of two hostiles: cleared=%t error=%v", cleared, err)
	}
	if _, ok := runtime.Room.AnnouncedMonster(bossKey); ok {
		t.Fatal("defeated ordinary monster was still reported as currently announced")
	}
	if _, ok := runtime.Room.AnnouncedMonster(normalKey); !ok {
		t.Fatal("unrelated announced ordinary monster disappeared after Boss death")
	}
}

func TestHandleDungeonMonsterDeathAcceptsPVFTutorialScriptedBossSentinelOwner(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce tutorial actors: %v", err)
	}
	roomBefore := runtime.Room.Snapshot()
	if len(roomBefore.Monsters) != 2 || roomBefore.Monsters[0].Spawn.Rank != "[boss]" || roomBefore.Monsters[1].Spawn.Rank != "[normal]" {
		t.Fatalf("fixture room=%+v", roomBefore)
	}
	bossKey := roomBefore.Monsters[0].ObjectKey
	normalKey := roomBefore.Monsters[1].ObjectKey
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "pvf-tutorial-scripted-boss-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}

	if err := service.handleDungeonMonsterDeath(session, tutorialScopeVariableZeroCombatDeathBody(bossKey)); err != nil {
		t.Fatalf("accept PVF tutorial scripted Boss death: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantBody, err := buildCurrentDungeonDeathNotificationBody(bossKey, currentDungeonDeathResponseMonster)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.Classification != 0 ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		!bytes.Equal(packet.Body, wantBody) || len(rest) != 0 {
		t.Fatalf("escape Boss response header=%+v body=%x want=%x rest=%x", packet.Header, packet.Body, wantBody, rest)
	}

	roomAfter := runtime.Room.Snapshot()
	if roomAfter.Monsters[0].ObjectKey != bossKey || roomAfter.Monsters[0].State != runtimeDungeonMonsterDefeated {
		t.Fatalf("Boss state=%+v", roomAfter.Monsters[0])
	}
	if roomAfter.Monsters[1].ObjectKey != normalKey || roomAfter.Monsters[1].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("normal hostile state=%+v", roomAfter.Monsters[1])
	}
	if _, ok := runtime.Room.AnnouncedMonster(bossKey); ok {
		t.Fatal("ACKed escape Boss remained currently announced")
	}
	if _, ok := runtime.Room.AnnouncedMonster(normalKey); !ok {
		t.Fatal("escape Boss ACK retired an unrelated normal hostile")
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Scene.Cleared || len(snapshot.Scene.DefeatedObjects) != 1 || snapshot.Scene.DefeatedObjects[0] != bossKey {
		t.Fatalf("escape Boss fabricated room clear/deaths: %+v", snapshot.Scene)
	}
	if snapshot.Run.Status != worldmap.DungeonRunActive || snapshot.Run.Current != (worldmap.RoomCoordinate{X: 0, Y: 0}) {
		t.Fatalf("escape Boss changed run lifecycle=%+v", snapshot.Run)
	}
	if session.dungeon.runtime != runtime {
		t.Fatalf("escape Boss changed runtime ownership: session_runtime=%p want=%p", session.dungeon.runtime, runtime)
	}
}

func TestHandleDungeonMonsterDeathAcceptsGenericPVFTutorialScriptedMonsterDeath(t *testing.T) {
	tests := []struct {
		name    string
		fixture tutorialScopeFixtureOptions
	}{
		{
			name: "knight_f_invisible_start_guide",
			fixture: tutorialScopeFixtureOptions{
				job:                "12",
				dungeonReference:   tutorialScopeKnightFDungeonReference,
				mapDirectory:       tutorialScopeKnightFMapDirectory,
				currentRoomNonBoss: true,
				bossRank:           "[normal]",
				singleMonster:      true,
			},
		},
		{
			name: "another_profession_tutorial",
			fixture: tutorialScopeFixtureOptions{
				job:                "0",
				dungeonReference:   tutorialScopeOtherDungeonReference,
				mapDirectory:       tutorialScopeOtherMapDirectory,
				currentRoomNonBoss: true,
				bossRank:           "[normal]",
				singleMonster:      true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, test.fixture)
			if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
				t.Fatalf("announce tutorial actors: %v", err)
			}
			roomBefore := runtime.Room.Snapshot()
			if len(roomBefore.Monsters) != 1 || roomBefore.Monsters[0].Spawn.Rank != "[normal]" {
				t.Fatalf("single scripted tutorial monster fixture=%+v", roomBefore.Monsters)
			}
			sceneBefore, ok := runtime.Session.Scene()
			if !ok || !sceneBefore.Start || sceneBefore.Boss || sceneBefore.Cleared {
				t.Fatalf("tutorial start scene=%+v ok=%t", sceneBefore, ok)
			}
			objectKey := roomBefore.Monsters[0].ObjectKey
			conn := &bufferConn{}
			session := &gameSession{
				conn:                conn,
				connID:              "generic-pvf-tutorial-scripted-death-test",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}

			if err := service.handleDungeonMonsterDeath(session, tutorialScopeVariableZeroCombatDeathBody(objectKey)); err != nil {
				t.Fatalf("accept generic tutorial scripted death: %v", err)
			}
			packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
			wantBody, err := buildCurrentDungeonDeathNotificationBody(objectKey, currentDungeonDeathResponseMonster)
			if err != nil {
				t.Fatal(err)
			}
			if packet.Header.Classification != 0 ||
				packet.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
				!bytes.Equal(packet.Body, wantBody) || len(rest) != 0 {
				t.Fatalf("scripted tutorial response header=%+v body=%x want=%x rest=%x", packet.Header, packet.Body, wantBody, rest)
			}

			roomAfter := runtime.Room.Snapshot()
			if len(roomAfter.Monsters) != 1 || roomAfter.Monsters[0].State != runtimeDungeonMonsterDefeated {
				t.Fatalf("scripted tutorial monster state=%+v", roomAfter.Monsters)
			}
			if _, announced := runtime.Room.AnnouncedMonster(objectKey); announced {
				t.Fatal("ACKed scripted tutorial monster remained announced")
			}
			snapshot := runtime.Session.Snapshot()
			if !snapshot.Scene.Cleared || len(snapshot.Scene.DefeatedObjects) != 1 || snapshot.Scene.DefeatedObjects[0] != objectKey {
				t.Fatalf("scripted tutorial death did not clear exactly the owned hostile: %+v", snapshot.Scene)
			}
			if snapshot.Run.Status != worldmap.DungeonRunActive || snapshot.Run.Current != (worldmap.RoomCoordinate{X: 0, Y: 0}) {
				t.Fatalf("scripted tutorial death changed run lifecycle=%+v", snapshot.Run)
			}
			if session.dungeon.runtime != runtime {
				t.Fatalf("scripted tutorial death changed runtime ownership: session_runtime=%p want=%p", session.dungeon.runtime, runtime)
			}
		})
	}
}

func TestPVFTutorialSentinelOwnerKeepsNonHostileAPCRetirementSeparate(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["dungeon/test.dgn"] += "[tutorial dungeon]\n1\n"
	source["map/dungeon/test/start.map"] += "[ai character]\n4002 30 20 0 `[character]` `[normal]` 0 0\n"
	source[defaultDungeonAICharacterList] = "4002 `Test/Friend.aic`\n"
	source["AICharacter/Test/Friend.aic"] = "[minimum info]\n`Friend APC` 1 2 3 4 25\n"
	source[defaultDungeonCinematicList] = "9001 `Dungeon/Tutorial/apc.cmt`\n"
	source["cinematic/Dungeon/Tutorial/apc.cmt"] = "[MAP]\n100\n[SCENE]\n[BEHAVIOR]\n" +
		"[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n0\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n[/SCENE]\n"
	aiCatalog, err := newPVFDungeonAICharacterCatalog(source)
	if err != nil {
		t.Fatalf("load tutorial APC catalog: %v", err)
	}
	tutorialScripts, err := newPVFDungeonTutorialScriptCatalog(source)
	if err != nil {
		t.Fatalf("load tutorial cinematic catalog: %v", err)
	}
	runtime := prepareSyntheticDungeonRuntimeForEntryTest(t, source, aiCatalog)
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce tutorial monster and APC: %v", err)
	}
	before := runtime.Room.Snapshot()
	if len(before.Monsters) != 1 || len(before.ExtendedActors) != 1 {
		t.Fatalf("tutorial APC fixture=%+v", before)
	}
	monsterKey := before.Monsters[0].ObjectKey
	apcKey := before.ExtendedActors[0].ObjectKey
	if _, ordinary := runtime.Room.AnnouncedMonster(apcKey); ordinary {
		t.Fatal("non-hostile APC appeared in ordinary announced-monster ownership")
	}

	conn := &bufferConn{}
	service := &Service{
		options:                options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		dungeonTutorialScripts: tutorialScripts,
	}
	session := &gameSession{conn: conn, connID: "pvf-tutorial-apc-retirement-test", dungeon: dungeonSessionState{runtime: runtime}}
	if err := service.handleDungeonMonsterDeath(session, tutorialScopeVariableZeroCombatDeathBody(apcKey)); err != nil {
		t.Fatalf("retire tutorial APC: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantBody, err := buildCurrentDungeonDeathNotificationBody(apcKey, currentDungeonDeathResponseAICharacter)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.Classification != 0 ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		!bytes.Equal(packet.Body, wantBody) || len(rest) != 0 {
		t.Fatalf("tutorial APC retirement response header=%+v body=%x want=%x rest=%x", packet.Header, packet.Body, wantBody, rest)
	}
	after := runtime.Room.Snapshot()
	if after.ExtendedActors[0].State != runtimeDungeonMonsterDefeated || after.Monsters[0].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("tutorial APC retirement crossed actor ownership: before=%+v after=%+v", before, after)
	}
	if _, ordinary := runtime.Room.AnnouncedMonster(monsterKey); !ordinary {
		t.Fatal("tutorial APC retirement removed the ordinary hostile")
	}
	scene, ok := runtime.Session.Scene()
	if !ok || scene.Cleared || len(scene.DefeatedObjects) != 0 {
		t.Fatalf("tutorial APC retirement changed hostile clear state: scene=%+v ok=%t", scene, ok)
	}
}

func TestHandleDungeonMonsterDeathRejectsUnprovenTutorialSentinelOwner(t *testing.T) {
	tests := []struct {
		name        string
		options     tutorialScopeFixtureOptions
		announce    bool
		targetIndex int
		body        func(uint32) []byte
	}{
		{
			name:     "PVF tutorial flag missing",
			options:  tutorialScopeFixtureOptions{omitTutorialFlag: true},
			announce: true,
		},
		{
			name:     "PVF tutorial flag disabled",
			options:  tutorialScopeFixtureOptions{disableTutorial: true},
			announce: true,
		},
		{
			name:     "ordinary monster not announced",
			announce: false,
		},
		{
			name:        "announced ordinary monster is not a CMT destroy target",
			announce:    true,
			targetIndex: 1,
		},
		{
			name:     "fixed62 request shape",
			announce: true,
			body:     tutorialScopeFixedDeathBody,
		},
		{
			name:     "variable request with one combat entry",
			announce: true,
			body:     tutorialScopeVariableOneCombatDeathBody,
		},
		{
			name:     "variable request with opaque tail",
			announce: true,
			body:     tutorialScopeVariableDeathBodyWithTail,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, test.options)
			before := runtime.Room.Snapshot()
			if len(before.Monsters) != 2 {
				t.Fatalf("fixture monsters=%+v", before.Monsters)
			}
			if test.targetIndex < 0 || test.targetIndex >= len(before.Monsters) {
				t.Fatalf("target index=%d monsters=%+v", test.targetIndex, before.Monsters)
			}
			targetKey := before.Monsters[test.targetIndex].ObjectKey
			if test.announce {
				if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
					t.Fatalf("announce tutorial actors: %v", err)
				}
			}
			roomBefore := runtime.Room.Snapshot()
			sessionBefore := runtime.Session.Snapshot()
			conn := &bufferConn{}
			session := &gameSession{
				conn:                conn,
				connID:              "atswordman-escape-negative-test",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}
			bodyBuilder := test.body
			if bodyBuilder == nil {
				bodyBuilder = tutorialScopeVariableZeroCombatDeathBody
			}
			if err := service.handleDungeonMonsterDeath(session, bodyBuilder(targetKey)); err != nil {
				t.Fatalf("rejected tutorial sentinel request returned error: %v", err)
			}
			if conn.write.Len() != 0 {
				t.Fatalf("unproven tutorial sentinel emitted response=%x", conn.write.Bytes())
			}

			roomAfter := runtime.Room.Snapshot()
			sessionAfter := runtime.Session.Snapshot()
			if len(roomAfter.Monsters) != len(roomBefore.Monsters) {
				t.Fatalf("monster count changed: before=%+v after=%+v", roomBefore.Monsters, roomAfter.Monsters)
			}
			for index := range roomBefore.Monsters {
				if roomAfter.Monsters[index].ObjectKey != roomBefore.Monsters[index].ObjectKey ||
					roomAfter.Monsters[index].State != roomBefore.Monsters[index].State {
					t.Fatalf("monster[%d] changed: before=%+v after=%+v", index, roomBefore.Monsters[index], roomAfter.Monsters[index])
				}
			}
			if sessionAfter.Scene.Cleared || len(sessionAfter.Scene.DefeatedObjects) != 0 {
				t.Fatalf("rejected tutorial sentinel changed clear/death state=%+v", sessionAfter.Scene)
			}
			if sessionAfter.Run.Status != worldmap.DungeonRunActive ||
				sessionAfter.Run.Current != sessionBefore.Run.Current ||
				sessionAfter.Scene.Coordinate != sessionBefore.Scene.Coordinate ||
				sessionAfter.Scene.Map.Map.ID != sessionBefore.Scene.Map.Map.ID {
				t.Fatalf("rejected tutorial sentinel changed run/scene: before=%+v after=%+v", sessionBefore, sessionAfter)
			}
			if session.dungeon.runtime != runtime {
				t.Fatalf("rejected tutorial sentinel changed runtime owner: runtime=%p want=%p", session.dungeon.runtime, runtime)
			}
		})
	}
}

type tutorialScopeFixtureOptions struct {
	job                         string
	dungeonReference            string
	mapDirectory                string
	currentRoomNonBoss          bool
	bossRank                    string
	bossSuffixMarker            string
	singleMonster               bool
	incompleteCinematicCoverage bool
	omitTutorialFlag            bool
	disableTutorial             bool
	tutorialCompleted           bool
	clearCondition              string
}

func prepareTutorialScopeRuntime(
	t *testing.T,
	fixture tutorialScopeFixtureOptions,
) (*Service, *runtimeDungeonState) {
	t.Helper()
	if fixture.job == "" {
		fixture.job = "11"
	}
	if fixture.dungeonReference == "" {
		fixture.dungeonReference = tutorialScopeATSwordmanDungeonReference
	}
	if fixture.mapDirectory == "" {
		fixture.mapDirectory = tutorialScopeATSwordmanMapDirectory
	}
	if fixture.bossRank == "" {
		fixture.bossRank = "[boss]"
	}
	source := tutorialScopePVF(fixture)
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	tutorialScripts, err := newPVFDungeonTutorialScriptCatalog(source)
	if err != nil {
		t.Fatalf("load synthetic tutorial script catalog: %v", err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	characterStats := map[string]int64{
		"fatigue":    100,
		"town_id":    7,
		"area_id":    3,
		"pos_x":      474,
		"pos_y":      234,
		"direction":  5,
		"area_state": 3,
	}
	if fixture.tutorialCompleted {
		characterStats[currentDungeonTutorialCompletedKey] = currentDungeonTutorialCompleteFlag
	}
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Name:        "ATSwordman",
		Job:         fixture.job,
		Level:       20,
		Stats:       characterStats,
	}); err != nil {
		t.Fatalf("save tutorial character: %v", err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("save tutorial inventory: %v", err)
	}
	service := &Service{
		options:                options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:          table,
		worldMapResolver:       resolver,
		dungeonMonsterTable:    monsters,
		dungeonTutorialScripts: tutorialScripts,
		dungeonChoice:          func(int) (int, error) { return 0, nil },
		dungeonSeed:            func() (uint32, error) { return 1, nil },
		repositoryProvider:     func() (dnfrepo.Group, bool) { return repositories, true },
	}
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(),
		&gameSession{selectedCharacterID: 99},
		dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatalf("prepare ATSwordman tutorial runtime: %v", err)
	}
	return service, runtime
}

func tutorialScopePVF(fixture tutorialScopeFixtureOptions) bridgePVFSource {
	startReference := fixture.mapDirectory + "/boss.map"
	startDocument := "map/" + startReference
	dungeonDocument := "dungeon/" + fixture.dungeonReference
	startMap := "[map name]\n`ATSwordman tutorial Boss`\n" +
		"[dungeon]\n700\n" +
		"[type]\n`[boss]`\n" +
		"[monster]\n" +
		"3001 10 0 100 200 0 0 0 `[fixed]` `" + fixture.bossRank + "`"
	if fixture.bossSuffixMarker != "" {
		startMap += " `" + fixture.bossSuffixMarker + "`"
	}
	startMap += "\n"
	if !fixture.singleMonster {
		startMap += "3001 10 0 140 200 0 0 0 `[fixed]` `[normal]`\n"
	}

	mapList := "100 `" + startReference + "`\n"
	dungeonText := "[name]\n`Synthetic ATSwordman Tutorial`\n" +
		"[minimum required level]\n10\n" +
		"[basis level]\n20\n" +
		"[limit party count]\n1\n" +
		"[no fatigue]\n"
	if !fixture.omitTutorialFlag {
		tutorialValue := 1
		if fixture.disableTutorial {
			tutorialValue = 0
		}
		dungeonText += "[tutorial dungeon]\n" + fmt.Sprintf("%d\n", tutorialValue)
	}
	dungeonText += "[maze info]\n"
	cinematic := "[MAP]\n100\n" +
		"[SCENE]\n[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n0\n[/ACTOR]\n" +
		"[DESTROY]\n[IS SHOW DIE]\n0\n[/DESTROY]\n[/BEHAVIOR]\n"
	if !fixture.singleMonster && !fixture.incompleteCinematicCoverage {
		cinematic += "[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n1\n[/ACTOR]\n[MOVE]\n[/MOVE]\n[/BEHAVIOR]\n"
	}
	cinematic += "[/SCENE]\n"
	source := bridgePVFSource{
		worldmap.DefaultDungeonList:            "700 `" + fixture.dungeonReference + "`\n",
		dungeonDocument:                        dungeonText,
		worldmap.DefaultWorldMapList:           "1 `tutorial_scope.wdm`\n",
		"worldmap/tutorial_scope.wdm":          "[name]\n`Synthetic Tutorial Area`\n[dungeon]\n700 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:                 "3001 `tutorial_scope.gob`\n",
		"monster/tutorial_scope.gob":           "[name]\n`Synthetic Tutorial Monster`\n[level]\n10\n[hp]\n500\n[exp]\n25\n",
		defaultDungeonCinematicList:            "9001 `Dungeon/Tutorial/scope.cmt`\n",
		"cinematic/Dungeon/Tutorial/scope.cmt": cinematic,
	}
	if !fixture.currentRoomNonBoss {
		source[worldmap.DefaultMapList] = mapList
		source[startDocument] = startMap
		source[dungeonDocument] += "[size]\n1 1\n" +
			"[greed]\n`A`\n" +
			"[map specification]\n`map` 0 0 100\n" +
			"[start map]\n0 0\n" +
			"[boss map]\n0 0\n"
		if fixture.clearCondition != "" {
			source[dungeonDocument] += fixture.clearCondition
		}
		return source
	}

	bossReference := fixture.mapDirectory + "/next.map"
	source[worldmap.DefaultMapList] = mapList + "101 `" + bossReference + "`\n"
	source[startDocument] = startMap
	source["map/"+bossReference] = "[map name]\n`Later Boss`\n[dungeon]\n700\n[type]\n`[boss]`\n"
	source[dungeonDocument] += "[size]\n2 1\n" +
		"[greed]\n`AA`\n" +
		"[map specification]\n`map` 0 0 100 `boss` 1 0 101\n" +
		"[start map]\n0 0\n" +
		"[boss map]\n1 0\n"
	if fixture.clearCondition != "" {
		source[dungeonDocument] += fixture.clearCondition
	}
	return source
}

func tutorialScopeVariableZeroCombatDeathBody(objectKey uint32) []byte {
	body := make([]byte, dungeoncmd.DieMonsterVariableBaseSize)
	binary.LittleEndian.PutUint32(body[0:4], objectKey)
	binary.LittleEndian.PutUint16(body[4:6], ^uint16(0))
	body[22] = 0
	return body
}

func tutorialScopeFixedDeathBody(objectKey uint32) []byte {
	body := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], objectKey)
	binary.LittleEndian.PutUint16(body[4:6], ^uint16(0))
	return body
}

func tutorialScopeVariableOneCombatDeathBody(objectKey uint32) []byte {
	body := make([]byte, dungeoncmd.DieMonsterVariableBaseSize+dungeoncmd.DieMonsterVariableCombatEntrySize)
	binary.LittleEndian.PutUint32(body[0:4], objectKey)
	binary.LittleEndian.PutUint16(body[4:6], ^uint16(0))
	body[22] = 1
	return body
}

func tutorialScopeVariableDeathBodyWithTail(objectKey uint32) []byte {
	return append(tutorialScopeVariableZeroCombatDeathBody(objectKey), 0xfa)
}
