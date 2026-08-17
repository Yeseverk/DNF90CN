package dnfbridge

import (
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
)

func TestCurrentDungeonDuplicateAuthoritativeDeathCannotMutateBlockedDropStateTwice(t *testing.T) {
	service, runtime, _ := prepareCurrentDungeonDropTest(t, 2, 3227)
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "dungeon-kill-reward-idempotency-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	monster := runtime.Room.Snapshot().Monsters[0]
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], monster.ObjectKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], currentSceneActorObjectKey(99))

	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() == 0 || runtime.DropOwner != nil {
		t.Fatalf("first authoritative death response bytes=%d owner=%+v", connection.write.Len(), runtime.DropOwner)
	}
	firstNextObjectKey := runtime.NextObjectKey

	connection.write.Reset()
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("duplicate death emitted a second response: %x", connection.write.Bytes())
	}
	if runtime.NextObjectKey != firstNextObjectKey || runtime.DropOwner != nil {
		t.Fatalf(
			"duplicate death changed blocked drop state: next=%d/%d owner=%+v",
			runtime.NextObjectKey,
			firstNextObjectKey,
			runtime.DropOwner,
		)
	}
}
