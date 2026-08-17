package dnfbridge

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestInitialTownHUDReadyReplaysAdventureNameModelOnce(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID:            "dnf:1",
		State:                "active",
		RepresentAccountName: "AdventureGroup",
		Metadata:             map[string]string{adventureGroupCreatedDateMetadataKey: "2026-07-16"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19", AccountID: "dnf:1", Slot: 0, Name: "test", Job: "0", Level: 90,
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:             options{accountID: "dnf:1"},
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		connID:                      "adventure-overhead-refresh",
		accountID:                   "dnf:1",
		selectedCharacterID:         19,
		initialTownRouteCharacterID: 19,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	if err := service.sendInitialTownAdventureOverheadRefresh(session, "test_hud_ready"); err != nil {
		t.Fatal(err)
	}
	packets := splitAllCurrentUpperPackets(t, connection.write.Bytes())
	if len(packets) != 3 || packets[0].Header.MsgID != currentAdventureInfoPushMsgID ||
		packets[1].Header.MsgID != currentAdventureActorRefreshMsgID ||
		packets[2].Header.MsgID != currentRepresentAccountNameStateMsgID {
		t.Fatalf("packets=%+v", packets)
	}
	if !session.initialTownAdventureOverheadRefreshSent {
		t.Fatal("HUD-ready adventure overhead replay gate was not committed")
	}
	connection.write.Reset()
	if err := service.sendInitialTownAdventureOverheadRefresh(session, "duplicate"); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("duplicate HUD-ready adventure replay wrote=%x", connection.write.Bytes())
	}
}
