package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestAuxiliarySameCharacterSessionCannotEvictResidentPartyOrTownPresence(t *testing.T) {
	service := &Service{
		onlinePlayers: newOnlinePlayerManager(),
		gameSessions:  make(map[uint16]*gameSession),
	}
	partyState := alignedcmd.PartyState{
		PartyID: 7,
		UserID:  7,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 7, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1, UserState: 1, HPPercent: 100, MPPercent: 100},
		},
	}
	resident := &gameSession{selectedCharacterID: 7, party: partySessionState{state: cloneRuntimePartyState(partyState)}}
	resident.spendTime.characterID = 7
	resident.spendTime.anchor = time.Unix(1, 0)
	resident.spendTime.generation = 3
	resident.spendTime.catalog = &currentSpendTimeRuntimeCatalog{}
	auxiliary := &gameSession{}
	service.bindGameSessionCharacter(resident, 7)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 7, TownID: 38, AreaID: 0, Session: resident})

	service.bindGameSessionCharacter(auxiliary, 7)
	if got, ok := service.onlineGameSession(7); !ok || got != resident {
		t.Fatalf("auxiliary bind replaced resident session: got=%p ok=%t want=%p", got, ok, resident)
	}
	if got := runtimePartyStateSnapshot(auxiliary); got.PartyID != 7 || len(got.Members) != 2 {
		t.Fatalf("auxiliary session did not inherit resident party: %+v", got)
	}
	if resident.spendTime.characterID != 7 || resident.spendTime.anchor.IsZero() ||
		resident.spendTime.generation != 3 || resident.spendTime.catalog == nil {
		t.Fatalf("auxiliary bind stopped resident clock char=%d anchor=%s generation=%d catalog_nil=%t",
			resident.spendTime.characterID,
			resident.spendTime.anchor,
			resident.spendTime.generation,
			resident.spendTime.catalog == nil)
	}

	service.cleanupOnlinePlayer(auxiliary)
	service.unbindGameSession(auxiliary)
	if got := service.onlinePlayers.SessionForCharacter(7); got != resident {
		t.Fatalf("auxiliary disconnect evicted resident town presence: got=%p want=%p", got, resident)
	}
	if got := runtimePartyStateSnapshot(resident); got.PartyID != 7 || len(got.Members) != 2 {
		t.Fatalf("auxiliary disconnect detached resident party: %+v", got)
	}
}

func TestChannelReconnectBindStopsResidentSpendTimeBeforeReplacementBootstrap(t *testing.T) {
	service := &Service{
		onlinePlayers: newOnlinePlayerManager(),
		gameSessions:  make(map[uint16]*gameSession),
	}
	resident := &gameSession{selectedCharacterID: 7}
	resident.spendTime.characterID = 7
	resident.spendTime.accountID = "account-1"
	resident.spendTime.anchor = time.Unix(1, 0)
	resident.spendTime.generation = 3
	resident.spendTime.catalog = &currentSpendTimeRuntimeCatalog{}
	service.bindGameSessionCharacter(resident, 7)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 7, TownID: 38, AreaID: 0, Session: resident})

	reconnect := &gameSession{channelReconnect: true}
	service.bindGameSessionCharacter(reconnect, 7)
	if resident.spendTime.characterID != 0 || !resident.spendTime.anchor.IsZero() ||
		resident.spendTime.catalog != nil {
		t.Fatalf("reconnect bind did not stop resident clock char=%d anchor=%s catalog_nil=%t",
			resident.spendTime.characterID,
			resident.spendTime.anchor,
			resident.spendTime.catalog == nil)
	}
}

func TestCurrentTownRemoteActorOwnerContextUsesCommittedTownChannel(t *testing.T) {
	target := &gameSession{townActorOwnerChannel: 42}
	for _, characterID := range []uint16{0, 1, 42, 256, 298, 0xffff} {
		owner := currentTownRemoteActorOwnerContext(target, characterID)
		if owner != target.townActorOwnerChannel {
			t.Fatalf(
				"character %d projected owner %d, want committed town channel %d",
				characterID,
				owner,
				target.townActorOwnerChannel,
			)
		}
	}
}

func TestBroadcastTownPlayerEnterCreatesRepositoryBackedRemoteActorsBeforePeerState(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, character := range []dnfrepo.CharacterRecord{
		{
			CharacterID: "17",
			AccountID:   "account-existing",
			Name:        "existing",
			Job:         "2",
			Level:       35,
			Stats:       map[string]int64{"grow_type": 1},
		},
		{
			CharacterID: "23",
			AccountID:   "account-newcomer",
			Name:        "newcomer",
			Job:         "5",
			Level:       48,
			Stats:       map[string]int64{"grow_type": 2},
		},
	} {
		if err := repositories.Character.Save(context.Background(), character); err != nil {
			t.Fatal(err)
		}
	}
	existingConn := &bufferConn{}
	newcomerConn := &bufferConn{}
	existingSession := &gameSession{
		conn:                  existingConn,
		connID:                "existing",
		selectedCharacterID:   17,
		accountID:             "account-existing",
		townActorOwnerChannel: 7,
	}
	newcomerSession := &gameSession{
		conn:                  newcomerConn,
		connID:                "newcomer",
		selectedCharacterID:   23,
		accountID:             "account-newcomer",
		townActorOwnerChannel: 8,
	}
	existing := onlinePlayerInfo{
		CharacterID: 17,
		AccountID:   "account-existing",
		Name:        "existing",
		Job:         2,
		GrowType:    1,
		Level:       35,
		TownID:      1,
		AreaID:      2,
		PositionX:   120,
		PositionY:   240,
		Direction:   1,
		AreaState:   3,
		Session:     existingSession,
	}
	newcomer := &onlinePlayerInfo{
		CharacterID: 23,
		AccountID:   "account-newcomer",
		Name:        "newcomer",
		Job:         5,
		GrowType:    2,
		Level:       48,
		TownID:      1,
		AreaID:      2,
		PositionX:   360,
		PositionY:   480,
		Direction:   0,
		AreaState:   4,
		Session:     newcomerSession,
	}
	service := &Service{
		options: options{
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		onlinePlayers: newOnlinePlayerManager(),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	service.bindGameSessionCharacter(existingSession, 17)
	service.bindGameSessionCharacter(newcomerSession, 23)

	service.broadcastTownPlayerEnter(newcomer, []onlinePlayerInfo{existing})

	assertTownRemoteActorSequence(
		t,
		existingConn.write.Bytes(),
		existingSession,
		*newcomer,
	)
	assertTownRemoteActorSequence(
		t,
		newcomerConn.write.Bytes(),
		newcomerSession,
		existing,
	)
}

func TestBroadcastTownPlayerEnterDoesNotReplaySoloSelfTownTransition(t *testing.T) {
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "solo",
		selectedCharacterID: 29,
	}
	service := &Service{onlinePlayers: newOnlinePlayerManager()}

	newcomer := &onlinePlayerInfo{
		CharacterID: 29,
		TownID:      38,
		AreaID:      0,
		PositionX:   768,
		PositionY:   238,
		Direction:   5,
		AreaState:   3,
		Session:     session,
	}
	service.broadcastTownPlayerEnter(newcomer, nil)

	if got := connection.write.Bytes(); len(got) != 0 {
		t.Fatalf("solo co-presence replayed town transition: %x", got)
	}
}

func assertTownRemoteActorSequence(
	t *testing.T,
	stream []byte,
	target *gameSession,
	actor onlinePlayerInfo,
) {
	t.Helper()
	wantOwner := currentTownRemoteActorOwnerContext(target, actor.CharacterID)
	mode0, rest := splitGameServerUpperPacketWithHeader(
		t,
		stream,
		dnfproto.GameServerUpperHeaderSize16,
	)
	if mode0.Header.Classification != 0 ||
		mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode0.Body) < 0x4e ||
		mode0.Body[0] != 0 ||
		mode0.Body[3] != currentSceneObjectRoute ||
		mode0.Body[4] != wantOwner ||
		binary.LittleEndian.Uint16(mode0.Body[0x4c:0x4e]) !=
			currentSceneActorObjectKey(actor.CharacterID) {
		t.Fatalf(
			"remote mode0 header=%+v body=%x owner=%d key=%d",
			mode0.Header,
			mode0.Body,
			wantOwner,
			currentSceneActorObjectKey(actor.CharacterID),
		)
	}

	mode1, rest := splitGameServerUpperPacketWithHeader(
		t,
		rest,
		dnfproto.GameServerUpperHeaderSize16,
	)
	if mode1.Header.Classification != 0 ||
		mode1.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode1.Body) < currentMode1BaseWireSize ||
		mode1.Body[0] != 1 ||
		mode1.Body[3] != currentSceneObjectRoute ||
		mode1.Body[4] != wantOwner ||
		binary.LittleEndian.Uint16(mode1.Body[0x15:0x17]) !=
			currentSceneActorObjectKey(actor.CharacterID) {
		t.Fatalf(
			"remote mode1 header=%+v body=%x owner=%d key=%d",
			mode1.Header,
			mode1.Body,
			wantOwner,
			currentSceneActorObjectKey(actor.CharacterID),
		)
	}

	display, rest := splitGameServerUpperPacketWithHeader(
		t,
		rest,
		dnfproto.GameServerUpperHeaderSize16,
	)
	wantPartyClear := buildCurrentSceneOp9ActorRemovalBodyInContext(
		currentSceneActorObjectKey(actor.CharacterID),
		wantOwner,
	)
	if display.Header.Classification != 0 ||
		display.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) ||
		!bytes.Equal(display.Body, wantPartyClear) {
		t.Fatalf(
			"remote same-owner op9 party clear header=%+v body=%x want=%x owner=%d key=%d",
			display.Header,
			display.Body,
			wantPartyClear,
			wantOwner,
			currentSceneActorObjectKey(actor.CharacterID),
		)
	}

	state, trailing := splitGameServerUpperPacketWithHeader(
		t,
		rest,
		dnfproto.GameServerUpperHeaderSize16,
	)
	wantState := buildCurrentTownUserAreaNotificationBody(
		currentSceneActorObjectKey(actor.CharacterID),
		actor.TownID,
		actor.AreaID,
		actor.PositionX,
		actor.PositionY,
		actor.Direction,
		actor.AreaState,
	)
	if state.Header.Classification != 0 ||
		state.Header.MsgID != currentTownUserAreaNotificationMsgID ||
		!bytes.Equal(state.Body, wantState) {
		t.Fatalf(
			"remote area state header=%+v body=%x want=%x",
			state.Header,
			state.Body,
			wantState,
		)
	}
	if len(trailing) != 0 {
		t.Fatalf("remote actor sequence replayed a town transition: %x", trailing)
	}
}
