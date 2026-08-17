package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func currentDungeonVariableOwnerSentinelDeathBody(objectKey uint32) []byte {
	body := make([]byte, dungeoncmd.DieMonsterVariableBaseSize)
	binary.LittleEndian.PutUint32(body[0:4], objectKey)
	binary.LittleEndian.PutUint16(body[4:6], ^uint16(0))
	body[22] = 0
	return body
}

func TestCurrentDungeonOwnerSentinelAnnouncedOrdinaryMonsterRetiresWithoutBossPromotion(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial:    true,
		currentRoomNonBoss: true,
		bossRank:           "[normal]",
	})
	runtime.Dungeon.Path = "dungeon/ordinary/owner_sentinel.dgn"
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	if len(room.Monsters) != 2 {
		t.Fatalf("fixture monster count=%d want=2", len(room.Monsters))
	}
	target := room.Monsters[1]
	remaining := room.Monsters[0]
	scene := runtime.Session.Snapshot().Scene
	if got, ok := currentDungeonOwnerSentinelAnnouncedOrdinaryMonster(runtime, scene, target.ObjectKey); !ok || got.ObjectKey != target.ObjectKey {
		t.Fatalf("announced ordinary sentinel target rejected ok=%t got=%+v target=%+v", ok, got, target)
	}
	if _, source, ok := currentDungeonOwnerSentinelBlockingClearTarget(runtime, scene, target.ObjectKey); ok {
		t.Fatalf("ordinary sentinel target was promoted to clear target source=%q target=%+v", source, target)
	}

	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "owner-sentinel-announced-ordinary-monster",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonMonsterDeath(
		session,
		currentDungeonVariableOwnerSentinelDeathBody(target.ObjectKey),
	); err != nil {
		t.Fatal(err)
	}

	afterRoom := runtime.Room.Snapshot()
	afterScene := runtime.Session.Snapshot().Scene
	if afterRoom.Monsters[1].State != runtimeDungeonMonsterDefeated ||
		afterRoom.Monsters[0].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("ordinary sentinel retirement changed monster state unexpectedly: before=%+v after=%+v", room.Monsters, afterRoom.Monsters)
	}
	if afterScene.Cleared ||
		!dungeonSceneObjectDefeated(afterScene.DefeatedObjects, target.ObjectKey) ||
		dungeonSceneObjectDefeated(afterScene.DefeatedObjects, remaining.ObjectKey) {
		t.Fatalf("ordinary sentinel retirement promoted room clear: target=%d remaining=%d scene=%+v",
			target.ObjectKey, remaining.ObjectKey, afterScene)
	}

	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantBody, err := buildCurrentDungeonDeathNotificationBody(
		target.ObjectKey,
		currentDungeonDeathResponseMonster,
	)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		packet.Header.Classification != 0 || !bytes.Equal(packet.Body, wantBody) || len(rest) != 0 {
		t.Fatalf("ordinary sentinel death notification: header=%+v body=%x rest=%x want=%x",
			packet.Header, packet.Body, rest, wantBody)
	}
}

func TestCurrentDungeonOwnerSentinelBossSuffixClearsIntermediateRoomAndAllowsMove(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial:    true,
		currentRoomNonBoss: true,
		bossRank:           "[dummy]",
		bossSuffixMarker:   "[boss]",
	})
	runtime.Dungeon.Path = "dungeon/ordinary/owner_sentinel.dgn"
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	if len(room.Monsters) != 2 {
		t.Fatalf("fixture monster count=%d want=2", len(room.Monsters))
	}
	targetKey := room.Monsters[0].ObjectKey
	remainingKey := room.Monsters[1].ObjectKey
	scene := runtime.Session.Snapshot().Scene
	if _, source, ok := currentDungeonOwnerSentinelBlockingClearTarget(runtime, scene, targetKey); !ok || source != "current_pvf_monster_suffix_boss" {
		t.Fatalf("owner-sentinel target proof ok=%t source=%q room=%+v scene=%+v", ok, source, room, scene)
	}

	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "owner-sentinel-boss-suffix-intermediate-room",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonMonsterDeath(
		session,
		currentDungeonVariableOwnerSentinelDeathBody(targetKey),
	); err != nil {
		t.Fatal(err)
	}
	after := runtime.Session.Snapshot()
	if !after.Scene.Cleared || after.Run.Status != worldmap.DungeonRunActive ||
		runtime.ordinaryFinalRoomClearAccepted || runtime.settlementEntrySent {
		t.Fatalf("intermediate owner-sentinel clear state runtime=%+v snapshot=%+v", runtime, after)
	}
	if !dungeonSceneObjectDefeated(after.Scene.DefeatedObjects, targetKey) ||
		!dungeonSceneObjectDefeated(after.Scene.DefeatedObjects, remainingKey) {
		t.Fatalf("boss clear did not retire both blockers: defeated=%v target=%d remaining=%d",
			after.Scene.DefeatedObjects, targetKey, remainingKey)
	}

	moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	moveBody[0] = byte(runtime.BossCoordinate.X)
	moveBody[1] = byte(runtime.BossCoordinate.Y)
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatal(err)
	}
	moved := runtime.Session.Snapshot()
	if moved.Scene.Coordinate != runtime.BossCoordinate || moved.Scene.Map.Map.ID != 101 {
		t.Fatalf("cleared intermediate room did not open next room: boss=%s moved=%+v",
			runtime.BossCoordinate, moved.Scene)
	}
}

func TestCurrentDungeonOwnerSentinelBlockingClearTargetStrictOwnership(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial:    true,
		currentRoomNonBoss: true,
		bossRank:           "[dummy]",
		bossSuffixMarker:   "[boss]",
	})
	_ = service
	runtime.Dungeon.Path = "dungeon/ordinary/owner_sentinel.dgn"
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	targetKey := room.Monsters[0].ObjectKey
	scene := runtime.Session.Snapshot().Scene
	if _, _, ok := currentDungeonOwnerSentinelBlockingClearTarget(runtime, scene, targetKey); !ok {
		t.Fatal("valid blocking boss target rejected")
	}

	tests := []struct {
		name   string
		mutate func(*worldmap.DungeonRoomScene) uint32
	}{
		{
			name: "not blocking",
			mutate: func(scene *worldmap.DungeonRoomScene) uint32 {
				scene.BlockingHostiles = nil
				return targetKey
			},
		},
		{
			name: "stale other room map",
			mutate: func(scene *worldmap.DungeonRoomScene) uint32 {
				scene.Map.Map.ID++
				return targetKey
			},
		},
		{
			name: "runtime binding mismatch",
			mutate: func(scene *worldmap.DungeonRoomScene) uint32 {
				scene.RuntimeObjects[targetKey] = worldmap.HostileReference{Kind: worldmap.HostileMonster, Index: 999}
				return targetKey
			},
		},
		{
			name: "already defeated scene",
			mutate: func(scene *worldmap.DungeonRoomScene) uint32 {
				scene.DefeatedObjects = append(scene.DefeatedObjects, targetKey)
				return targetKey
			},
		},
		{
			name: "extended apc binding is not an ordinary monster",
			mutate: func(scene *worldmap.DungeonRoomScene) uint32 {
				key := targetKey + 1000
				reference := worldmap.HostileReference{Kind: worldmap.HostileAICharacter, Index: 0}
				scene.RuntimeObjects[key] = reference
				scene.ExpectedHostiles = append(scene.ExpectedHostiles, reference)
				scene.BlockingHostiles = append(scene.BlockingHostiles, reference)
				return key
			},
		},
		{
			name: "passive client object has no announced binding",
			mutate: func(_ *worldmap.DungeonRoomScene) uint32 {
				return targetKey + 2000
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := scene
			candidate.RuntimeObjects = make(map[uint32]worldmap.HostileReference, len(scene.RuntimeObjects)+1)
			for key, reference := range scene.RuntimeObjects {
				candidate.RuntimeObjects[key] = reference
			}
			candidate.ExpectedHostiles = append([]worldmap.HostileReference(nil), scene.ExpectedHostiles...)
			candidate.BlockingHostiles = append([]worldmap.HostileReference(nil), scene.BlockingHostiles...)
			candidate.DefeatedObjects = append([]uint32(nil), scene.DefeatedObjects...)
			key := test.mutate(&candidate)
			if _, source, ok := currentDungeonOwnerSentinelBlockingClearTarget(runtime, candidate, key); ok {
				t.Fatalf("unowned target accepted source=%q scene=%+v key=%d", source, candidate, key)
			}
		})
	}
}

func TestCurrentDungeonOwnerSentinelHostileRequiresExactVariableZeroCountBoundary(t *testing.T) {
	for _, test := range []struct {
		name string
		body func(uint32) []byte
	}{
		{
			name: "fixed layout",
			body: func(key uint32) []byte {
				body := make([]byte, dungeoncmd.DieMonsterRequestSize)
				binary.LittleEndian.PutUint32(body[0:4], key)
				binary.LittleEndian.PutUint16(body[4:6], ^uint16(0))
				return body
			},
		},
		{
			name: "variable combat entry",
			body: func(key uint32) []byte {
				body := make([]byte, dungeoncmd.DieMonsterVariableBaseSize+dungeoncmd.DieMonsterVariableCombatEntrySize)
				binary.LittleEndian.PutUint32(body[0:4], key)
				binary.LittleEndian.PutUint16(body[4:6], ^uint16(0))
				body[22] = 1
				return body
			},
		},
		{
			name: "opaque tail",
			body: func(key uint32) []byte {
				return append(currentDungeonVariableOwnerSentinelDeathBody(key), 0xaa)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
				disableTutorial:    true,
				currentRoomNonBoss: true,
				bossRank:           "[dummy]",
				bossSuffixMarker:   "[boss]",
			})
			if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
				t.Fatal(err)
			}
			targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
			runtime.Dungeon.Path = "dungeon/ordinary/owner_sentinel.dgn"
			conn := &bufferConn{}
			session := &gameSession{
				conn:                conn,
				connID:              "owner-sentinel-hostile-strict-request-boundary",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}
			if err := service.handleDungeonMonsterDeath(session, test.body(targetKey)); err != nil {
				t.Fatal(err)
			}
			if conn.write.Len() != 0 || runtime.Session.Snapshot().Scene.Cleared ||
				runtime.Room.Snapshot().Monsters[0].State != runtimeDungeonMonsterAnnounced {
				t.Fatalf("unproved sentinel request mutated state: writes=%x runtime=%+v scene=%+v",
					conn.write.Bytes(), runtime.Room.Snapshot(), runtime.Session.Snapshot().Scene)
			}
		})
	}
}

func TestCurrentDungeonOwnerSentinelExplicitPVFClearTargetAccepted(t *testing.T) {
	_, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial:    true,
		currentRoomNonBoss: true,
		bossRank:           "[normal]",
		clearCondition:     "[clear condition]\n[hunt monster]\n3001 1\n[/clear condition]\n",
	})
	runtime.Dungeon.Path = "dungeon/ordinary/owner_sentinel.dgn"
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	monster := runtime.Room.Snapshot().Monsters[0]
	if normalizeDungeonPVFSymbol(monster.Spawn.Rank) == "boss" ||
		normalizeDungeonPVFSymbol(monster.Spawn.SuffixMarker) == "boss" {
		t.Fatalf("fixture unexpectedly uses boss syntax: %+v", monster.Spawn)
	}
	if _, source, ok := currentDungeonOwnerSentinelBlockingClearTarget(
		runtime,
		runtime.Session.Snapshot().Scene,
		monster.ObjectKey,
	); !ok || source == "" {
		t.Fatalf("explicit PVF clear target rejected ok=%t source=%q monster=%+v", ok, source, monster)
	}
}
