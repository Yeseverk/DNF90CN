package dnfbridge

import (
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestStoryAIBossAndDummyBossRequireSeparateAuthoritativeDeaths(t *testing.T) {
	for _, order := range []struct {
		name  string
		first string
	}{
		{name: "hostile AI boss then dummy boss", first: "ai"},
		{name: "dummy boss then hostile AI boss", first: "dummy"},
	} {
		t.Run(order.name, func(t *testing.T) {
			service, runtime, dummyObjectKey, aiObjectKey := prepareStoryAIBossDeathGateRuntime(t)
			firstObjectKey, secondObjectKey := aiObjectKey, dummyObjectKey
			if order.first == "dummy" {
				firstObjectKey, secondObjectKey = dummyObjectKey, aiObjectKey
			}

			connection := &bufferConn{}
			session := &gameSession{
				conn:                connection,
				connID:              "story-ai-boss-dual-op39-" + order.first,
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}
			if err := service.handleDungeonMonsterDeath(session, storyAIBossDeathBody(firstObjectKey)); err != nil {
				t.Fatal(err)
			}
			if got := storyAIBossDeathNotificationKeys(t, connection.write.Bytes()); len(got) != 1 || got[0] != firstObjectKey {
				t.Fatalf("first death notifications=%v want only authoritative key %d", got, firstObjectKey)
			}
			firstSnapshot := runtime.Session.Snapshot()
			firstRoom := runtime.Room.Snapshot()
			if runtime.ordinaryFinalRoomClearAccepted || runtime.bossDieCheckAccepted || runtime.settlementEntrySent ||
				firstSnapshot.Run.Status == worldmap.DungeonRunCompleted {
				t.Fatalf("first death completed room early: runtime=%+v snapshot=%+v", runtime, firstSnapshot)
			}
			if !dungeonSceneObjectDefeated(firstSnapshot.Scene.DefeatedObjects, firstObjectKey) ||
				dungeonSceneObjectDefeated(firstSnapshot.Scene.DefeatedObjects, secondObjectKey) {
				t.Fatalf("first death state=%v first=%d second=%d", firstSnapshot.Scene.DefeatedObjects, firstObjectKey, secondObjectKey)
			}
			gate, active := currentDungeonStoryAIBossDeathGate(runtime, firstSnapshot.Scene)
			if !active || gate.Ready {
				t.Fatalf("first death gate active=%t gate=%+v room=%+v", active, gate, firstRoom)
			}

			connection.write.Reset()
			if err := service.handleDungeonMonsterDeath(session, storyAIBossDeathBody(secondObjectKey)); err != nil {
				t.Fatal(err)
			}
			if got := storyAIBossDeathNotificationKeys(t, connection.write.Bytes()); len(got) != 1 || got[0] != secondObjectKey {
				t.Fatalf("second death notifications=%v want only authoritative key %d", got, secondObjectKey)
			}
			completed := runtime.Session.Snapshot()
			if completed.Run.Status != worldmap.DungeonRunCompleted || !completed.Scene.Cleared ||
				!runtime.ordinaryFinalRoomClearAccepted || !runtime.bossDieCheckAccepted || !runtime.settlementEntrySent {
				t.Fatalf("dual op39 did not complete final room: runtime=%+v snapshot=%+v", runtime, completed)
			}
			if !dungeonSceneObjectDefeated(completed.Scene.DefeatedObjects, dummyObjectKey) ||
				!dungeonSceneObjectDefeated(completed.Scene.DefeatedObjects, aiObjectKey) {
				t.Fatalf("completed defeated=%v dummy=%d AI=%d", completed.Scene.DefeatedObjects, dummyObjectKey, aiObjectKey)
			}
			gate, active = currentDungeonStoryAIBossDeathGate(runtime, completed.Scene)
			if !active || !gate.Ready {
				t.Fatalf("completed gate active=%t gate=%+v", active, gate)
			}
		})
	}
}

func prepareStoryAIBossDeathGateRuntime(
	t *testing.T,
) (*Service, *runtimeDungeonState, uint32, uint32) {
	t.Helper()
	source := bridgeDungeonPVF(false)
	source["map/dungeon/test/start.map"] = "[map name]\n`story dual boss`\n" +
		"[dungeon]\n700\n" +
		"[type]\n`[boss]`\n" +
		"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[dummy]` `[boss]`\n" +
		"[ai character]\n4001 10 20 0 `[monster]` `[boss]` 0 0\n"
	source[defaultDungeonAICharacterList] = "4001 `Test/StoryBoss.aic`\n"
	source["AICharacter/Test/StoryBoss.aic"] = "[minimum info]\n`Story AI Boss` 1 2 3 4 25\n"

	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	aiCharacters, err := newPVFDungeonAICharacterCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats:       map[string]int64{"fatigue": 100},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:                 options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:           table,
		worldMapResolver:        resolver,
		dungeonMonsterTable:     monsters,
		dungeonAICharacterTable: aiCharacters,
		dungeonChoice:           func(int) (int, error) { return 0, nil },
		dungeonSeed:             func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
	}
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(),
		&gameSession{selectedCharacterID: 99},
		dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	scene := runtime.Session.Snapshot().Scene
	gate, active := currentDungeonStoryAIBossDeathGate(runtime, scene)
	if !active || gate.Ready || len(gate.DummyBossObjectKeys) != 1 || len(gate.AIBossObjectKeys) != 1 {
		t.Fatalf("initial gate active=%t gate=%+v scene=%+v", active, gate, scene)
	}
	return service, runtime, gate.DummyBossObjectKeys[0], gate.AIBossObjectKeys[0]
}

func storyAIBossDeathBody(objectKey uint32) []byte {
	body := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], objectKey)
	binary.LittleEndian.PutUint16(body[4:6], currentSceneActorObjectKey(99))
	return body
}

func storyAIBossDeathNotificationKeys(t *testing.T, data []byte) []uint32 {
	t.Helper()
	var keys []uint32
	for len(data) != 0 {
		packet, rest := splitGameServerUpperPacket(t, data)
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketNotifyDieMonster) {
			if len(packet.Body) < 2 {
				t.Fatalf("death notification body=%x", packet.Body)
			}
			keys = append(keys, uint32(binary.LittleEndian.Uint16(packet.Body[:2])))
		}
		data = rest
	}
	return keys
}
