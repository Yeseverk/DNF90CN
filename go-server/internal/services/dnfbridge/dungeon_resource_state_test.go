package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestBuildCurrentDungeonResourceStateBody(t *testing.T) {
	want := []byte{1, 0, 0xe9, 0x1b, 0, 0, 1}
	if got := buildCurrentDungeonResourceStateBody(7145); !bytes.Equal(got, want) {
		t.Fatalf("resource state body=%x want=%x", got, want)
	}
}

func TestBuildCurrentDungeonResourceStateEntriesBody(t *testing.T) {
	entries := []currentDungeonResourceStateEntry{
		{DungeonID: 3, State: 1},
		{DungeonID: 7145, State: 4},
	}
	want := []byte{2, 0, 3, 0, 0, 0, 1, 0xe9, 0x1b, 0, 0, 4}
	if got := buildCurrentDungeonResourceStateEntriesBody(entries); !bytes.Equal(got, want) {
		t.Fatalf("resource state entries body=%x want=%x", got, want)
	}
}

func TestCurrentDungeonNextClearStateUsesPVFMaxDifficulty(t *testing.T) {
	dungeon := worldmap.Dungeon{Metadata: worldmap.DungeonMetadata{
		DifficultyLevels: []int64{1, 2, 3, 4, 5},
	}}
	if got := currentDungeonNextClearState(dungeon, 0); got != 1 {
		t.Fatalf("normal clear state=%d want 1", got)
	}
	if got := currentDungeonNextClearState(dungeon, 4); got != 4 {
		t.Fatalf("highest clear state=%d want capped 4", got)
	}
	dungeon.Metadata.DifficultyLevels = []int64{1}
	if got := currentDungeonNextClearState(dungeon, 0); got != 0 {
		t.Fatalf("single difficulty clear state=%d want 0", got)
	}
	dungeon.Metadata.DifficultyLevels = nil
	dungeon.Metadata.DesignatedDifficulties = []int64{0, 1}
	if got := currentDungeonNextClearState(dungeon, 3); got != 4 {
		t.Fatalf("designated clear state=%d want 4", got)
	}
}

func TestSelectCharacterSendsDungeonPermissionSnapshotImmediatelyAfterAck(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["dungeon/test.dgn"] = "[name]\n`Synthetic Dungeon`\n" +
		"[minimum required level]\n10\n" +
		"[difficulty level]\n0 1 2\n" +
		"[maze info]\n[size]\n1 1\n[greed]\n`A`\n" +
		"[map specification]\n`map` 0 0 100\n[start map]\n0 0\n[boss map]\n0 0\n"
	table, resolver, _ := loadBridgeDungeonStaticData(t, source)
	repositories := testRepositoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        3,
		Name:        "worldmap-owner",
		Job:         "0",
		Level:       20,
		Stats: map[string]int64{
			"town_id": 38, "area_id": 1,
			"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
			currentDungeonTutorialCompletedKey: 1,
		},
	}
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if _, updated, err := repositories.DungeonPermission.UpsertMax(context.Background(), "99", 700, 1); err != nil || !updated {
		t.Fatalf("seed dungeon permission updated=%t err=%v", updated, err)
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		worldMapTable:      table,
		worldMapResolver:   resolver,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:            connection,
		channel:         channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
		residentChannel: channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
		rosterRequested: true,
	}
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 99)
	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("first packet header=%+v body=%x", ack.Header, ack.Body)
	}
	permission, rest := splitGameServerUpperPacket(t, rest)
	wantPermission := buildCurrentDungeonResourceStateEntriesBody([]currentDungeonResourceStateEntry{{DungeonID: 700, State: 1}})
	if permission.Header.Classification != 0 || permission.Header.MsgID != currentDungeonResourceStateMsgID || !bytes.Equal(permission.Body, wantPermission) {
		t.Fatalf("post-select ACK permission header=%+v body=%x want=%x", permission.Header, permission.Body, wantPermission)
	}
	digest, _ := splitGameServerUpperPacket(t, rest)
	if digest.Header.MsgID != currentStoryDigestLastLevelMsgID {
		t.Fatalf("packet after early permission header=%+v body=%x", digest.Header, digest.Body)
	}
}

func TestEnterSelectDungeonDoesNotReplayLatePermissionSnapshot(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["dungeon/test.dgn"] = "[name]\n`Synthetic Dungeon`\n" +
		"[difficulty level]\n0 1 2\n" +
		"[maze info]\n[size]\n1 1\n[greed]\n`A`\n" +
		"[map specification]\n`map` 0 0 100\n[start map]\n0 0\n[boss map]\n0 0\n"
	table, resolver, _ := loadBridgeDungeonStaticData(t, source)
	service, session, repositories := newTownMoveTest(t)
	service.worldMapTable = table
	service.worldMapResolver = resolver
	if _, updated, err := repositories.DungeonPermission.UpsertMax(context.Background(), "29", 700, 1); err != nil || !updated {
		t.Fatalf("seed dungeon permission updated=%t err=%v", updated, err)
	}
	if err := service.handleTownSetUserArea(session, buildTownMoveRequest(38, 0, 900, 250, 5)); err != nil {
		t.Fatal(err)
	}
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(884, 248, 6, 100)); err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()
	if err := service.sendEnterSelectDungeonState(session, "test_no_late_permission", false, true); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) || ack.Header.Classification != dnfproto.DefaultChannelClassification {
		t.Fatalf("op15 ACK header=%+v", ack.Header)
	}
	fatigue, rest := splitGameServerUpperPacket(t, rest)
	if fatigue.Header.MsgID != currentFatigueMsgID {
		t.Fatalf("packet after op15 ACK header=%+v", fatigue.Header)
	}
	contextPacket, trailing := splitGameServerUpperPacket(t, rest)
	if contextPacket.Header.MsgID != currentDungeonContextMsgID || len(trailing) != 0 {
		t.Fatalf("late selector sequence context=%+v trailing=%x", contextPacket.Header, trailing)
	}
}
