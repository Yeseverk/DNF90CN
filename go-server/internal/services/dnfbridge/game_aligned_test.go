// 本文件验证 DNF game 端已对齐命令的桥接回包顺序。
package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	"longheng.io/server/internal/modules/dnf/raid"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestGameAlignedRegistryRoutesRegisteredDomains(t *testing.T) {
	tests := []struct {
		name            string
		opcode          dnfenum.CmdPacket
		domain          dnfenum.AlignedDomain
		responseAllowed bool
	}{
		{name: "inventory", opcode: dnfenum.CmdPacketDeleteItem, domain: dnfenum.AlignedDomainInventory},
		{name: "pet", opcode: dnfenum.CmdPacketHatchCreatureEgg, domain: dnfenum.AlignedDomainPet},
		{name: "mail", opcode: dnfenum.CmdPacketMailboxOpen, domain: dnfenum.AlignedDomainMail, responseAllowed: true},
		{name: "quest", opcode: dnfenum.CmdPacketAcceptQuest, domain: dnfenum.AlignedDomainQuest},
		{name: "skill", opcode: dnfenum.CmdPacketBuySkill, domain: dnfenum.AlignedDomainSkill},
		{name: "cargo", opcode: dnfenum.CmdPacketCreateAccountCargo, domain: dnfenum.AlignedDomainCargo},
		{name: "item lock", opcode: dnfenum.CmdPacketRequestItemLock, domain: dnfenum.AlignedDomainItemLock},
		{name: "dungeon", opcode: dnfenum.CmdPacketGetItem, domain: dnfenum.AlignedDomainDungeon},
		{name: "avatar title", opcode: dnfenum.CmdPacketTitleBookPut, domain: dnfenum.AlignedDomainAvatarTitle},
		{name: "package", opcode: dnfenum.CmdPacketUseRandomboxItem, domain: dnfenum.AlignedDomainPackage},
		{name: "package expand", opcode: dnfenum.CmdPacketUseRandomboxItemExpand, domain: dnfenum.AlignedDomainPackage},
		{name: "party", opcode: dnfenum.CmdPacketSetPartyInfo, domain: dnfenum.AlignedDomainParty, responseAllowed: true},
		{name: "raid", opcode: dnfenum.CmdPacketRaidManagerWork, domain: dnfenum.AlignedDomainRaid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := gameAlignedRegistry.Route(context.Background(), alignedcmd.Request{Opcode: uint16(tt.opcode)})
			if err != nil {
				t.Fatalf("Route error = %v", err)
			}
			if !ok {
				t.Fatalf("Route(%d) not classified", tt.opcode)
			}
			if got.Decision.Domain != tt.domain {
				t.Fatalf("domain = %q, want %q", got.Decision.Domain, tt.domain)
			}
			if got.ResponseAllowed != tt.responseAllowed {
				t.Fatalf("responseAllowed = %v, want %v", got.ResponseAllowed, tt.responseAllowed)
			}
			if strings.Contains(got.Reason, "not registered") {
				t.Fatalf("opcode %d was not handled by registered module: %q", tt.opcode, got.Reason)
			}
		})
	}
}

func TestGameAlignedRegistryCoversAlignedDomains(t *testing.T) {
	for _, command := range dnfenum.AlignedCommands() {
		if command.Domain == dnfenum.AlignedDomainCharacter {
			continue
		}
		t.Run(dnfenum.CmdPacketName(uint16(command.Opcode)), func(t *testing.T) {
			got, ok, err := gameAlignedRegistry.Route(context.Background(), alignedcmd.Request{Opcode: uint16(command.Opcode)})
			if err != nil {
				t.Fatalf("Route error = %v", err)
			}
			if !ok {
				t.Fatalf("Route(%d) not classified", command.Opcode)
			}
			if got.Decision.Domain != command.Domain {
				t.Fatalf("domain = %q, want %q", got.Decision.Domain, command.Domain)
			}
			if strings.Contains(got.Reason, "not registered") {
				t.Fatalf("aligned opcode %d has no registered module: %q", command.Opcode, got.Reason)
			}
		})
	}
}

func TestAlignedSkillOwnerRulesRequiredForMutationCommands(t *testing.T) {
	tests := []struct {
		name   string
		opcode dnfenum.CmdPacket
		want   bool
	}{
		{name: "change slot", opcode: dnfenum.CmdPacketChangeSkillslot, want: true},
		{name: "buy skill", opcode: dnfenum.CmdPacketBuySkill, want: true},
		{name: "skill init", opcode: dnfenum.CmdPacketSkillInit, want: true},
		{name: "unrelated command", opcode: dnfenum.CmdPacketMailboxOpen, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := alignedSkillOwnerRulesRequired(tt.opcode); got != tt.want {
				t.Fatalf("alignedSkillOwnerRulesRequired(%d) = %t, want %t", tt.opcode, got, tt.want)
			}
		})
	}
}

func TestHandleAlignedGameCommandSendsMailboxOpenAck(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	service := &Service{
		options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038},
		selectedCharacterID: 77,
	}

	handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketMailboxOpen),
		nil,
	)
	if err != nil {
		t.Fatalf("handleAlignedGameCommand error = %v", err)
	}
	if !handled {
		t.Fatalf("MailboxOpen should be handled by aligned module")
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	snapshot, trailing := splitGameServerUpperPacket(t, rest)
	if packet.Header.Classification != dnfproto.DefaultChannelClassification {
		t.Fatalf("classification = %d, want %d", packet.Header.Classification, dnfproto.DefaultChannelClassification)
	}
	if packet.Header.MsgID != uint16(dnfenum.CmdPacketMailboxOpen) {
		t.Fatalf("msgID = %d, want %d", packet.Header.MsgID, dnfenum.CmdPacketMailboxOpen)
	}
	if !bytes.Equal(packet.Body, []byte{0x01, 0x00, 0x00}) {
		t.Fatalf("body = % X, want 01 00 00", packet.Body)
	}
	if snapshot.Header.Classification != 0 || snapshot.Header.MsgID != 0x0061 || !bytes.Equal(snapshot.Body, []byte{0, 0, 0, 0, 0, 0}) {
		t.Fatalf("mailbox snapshot header=%+v body=%x, want class0/op97 empty snapshot", snapshot.Header, snapshot.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("mailbox open trailing bytes = %d, want only op96 ACK and class0/op97 snapshot", len(trailing))
	}
}

func TestHandleAlignedGameCommandRepliesToMailboxAccountRoleRequestWithOp718(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	for _, character := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "dnf:mail-picker", Name: "sender", Slot: 0},
		{CharacterID: "2", AccountID: "dnf:mail-picker", Name: "target", Slot: 1},
		{CharacterID: "3", AccountID: "another-account", Name: "unrelated", Slot: 0},
	} {
		if err := repos.Character.Save(ctx, character); err != nil {
			t.Fatalf("save character %s: %v", character.CharacterID, err)
		}
	}
	service := &Service{
		options: options{
			accountID:          "dnf:mail-picker",
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038},
		selectedCharacterID: 1,
	}

	handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestServerCharacterList),
		[]byte{0},
	)
	if err != nil {
		t.Fatalf("handleAlignedGameCommand error = %v", err)
	}
	if !handled {
		t.Fatalf("mailbox account-role request should be handled by aligned module")
	}
	packet, trailing := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(trailing) != 0 {
		t.Fatalf("mailbox account-role trailing bytes = %d, want 0", len(trailing))
	}
	if got := packet.Header; got.Classification != 0 || got.MsgID != 718 {
		t.Fatalf("mailbox account-role header = %+v, want class0/op718", got)
	}
	want := []byte{0, 1, 2, 0, 0, 0, 6, 0, 0, 0, 't', 'a', 'r', 'g', 'e', 't', 0}
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("mailbox account-role body = % X, want % X", packet.Body, want)
	}
}

func TestHandleGameUpperFallbackRoutesUnlistedAccountCargoToOwner(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "dnf:upper-cargo"}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "91",
		AccountID:   "dnf:upper-cargo",
		Stats:       map[string]int64{"gold": 500000},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	service := &Service{
		options: options{
			accountID:                "dnf:upper-cargo",
			gameUpperBodyCodec:       gameUpperBodyCodecPlain,
			gameUpperClientBodyCodec: gameUpperClientBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "game-upper-cargo-fallback",
		channel:             channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038},
		selectedCharacterID: 91,
	}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketCreateAccountCargo), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build frame: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handleGameUpper create account cargo: %v", err)
	}

	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketCreateAccountCargo) ||
		!bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("create cargo ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	gold, rest := splitGameServerUpperPacket(t, rest)
	if gold.Header.Classification != 0 || gold.Header.MsgID != 0x000E || len(gold.Body) != 3+0x77 {
		t.Fatalf("gold refresh = class %d msg %d len=%d body=% X", gold.Header.Classification, gold.Header.MsgID, len(gold.Body), gold.Body)
	}
	if got := binary.LittleEndian.Uint32(gold.Body[9:13]); got != 400000 {
		t.Fatalf("gold refresh value = %d, want 400000", got)
	}
	accountCargo, rest := splitGameServerUpperPacketWithHeader(t, rest, dnfproto.GameServerUpperHeaderSize16)
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	if accountCargo.Header.Classification != 0 ||
		accountCargo.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) ||
		len(accountCargo.Body) != 9 ||
		accountCargo.Body[0] != 12 ||
		binary.LittleEndian.Uint16(accountCargo.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint32(accountCargo.Body[3:7]) != 0 ||
		binary.LittleEndian.Uint16(accountCargo.Body[7:9]) != 0 {
		t.Fatalf("account cargo refresh packet = class %d msg %d body=% X", accountCargo.Header.Classification, accountCargo.Header.MsgID, accountCargo.Body)
	}
	account, ok, err := repos.Account.Load(ctx, "dnf:upper-cargo")
	if err != nil || !ok {
		t.Fatalf("load account ok=%t err=%v", ok, err)
	}
	if account.Metadata["account_cargo_created"] != "true" || account.Metadata["account_cargo_level"] != "1" {
		t.Fatalf("account cargo metadata = %+v", account.Metadata)
	}
	character, ok, err := repos.Character.Load(ctx, "91")
	if err != nil || !ok {
		t.Fatalf("load character ok=%t err=%v", ok, err)
	}
	if got := character.Stats["gold"]; got != 400000 {
		t.Fatalf("character gold = %d, want 400000", got)
	}
}

func TestHandleAlignedGameCommandSendsSingleMemberPartySequence(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                      conn,
		channel:                   channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038},
		selectedCharacterID:       1001,
		townSceneReadyCharacterID: 1001,
	}
	service.bindGameSessionCharacter(session, 1001)
	body := []byte{0x00, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x9c, 0x00}

	handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketSetPartyInfo),
		body,
	)
	if err != nil {
		t.Fatalf("handleAlignedGameCommand error = %v", err)
	}
	if !handled {
		t.Fatalf("SetPartyInfo should be handled by aligned module")
	}

	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketSetPartyInfo) ||
		!bytes.Equal(ack.Body, []byte{0x01}) {
		t.Fatalf("ack packet = class %d msg %d body % X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	assertCurrentPartyFrameSelectedSlot(t, rest, 0)
	rest = assertRuntimePartySnapshot(t, rest, 1)
	if len(rest) != 0 {
		t.Fatalf("unexpected unsolicited party-directory bytes: %d", len(rest))
	}
}

func TestHandleOnlineEntryIntoPartyLinksOnlineSessions(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	sourceConn := &bufferConn{}
	targetConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	source := &gameSession{
		conn:                sourceConn,
		channel:             channel,
		selectedCharacterID: 1001,
		party: partySessionState{state: alignedcmd.PartyState{
			PartyID:    77,
			UserID:     1001,
			UserState:  1,
			NameBytes:  []byte("party"),
			MaxMembers: 4,
			Members: []alignedcmd.PartyMemberState{
				{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			},
		}},
	}
	target := &gameSession{
		conn:                targetConn,
		channel:             channel,
		selectedCharacterID: 1002,
	}
	service.bindGameSessionCharacter(source, 1001)
	service.bindGameSessionCharacter(target, 1002)

	handled, err := service.handleAlignedGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketEntryIntoParty),
		[]byte{0xea, 0x03, 0x00, 0x00},
	)
	if err != nil {
		t.Fatalf("handle entry into party: %v", err)
	}
	if !handled {
		t.Fatalf("EntryIntoParty should be handled by online party bridge")
	}
	if len(source.party.state.Members) != 2 || len(target.party.state.Members) != 2 {
		t.Fatalf("member counts source=%d target=%d", len(source.party.state.Members), len(target.party.state.Members))
	}

	ack, rest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketEntryIntoParty) ||
		!bytes.Equal(ack.Body, []byte{1, 0xea, 0x03, 0x00, 0x00, 0xe9, 0x03, 0x00, 0x00}) {
		t.Fatalf("entry ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	assertCurrentPartyFrameSelectedSlot(t, rest, 0)
	rest = assertRuntimePartySnapshot(t, rest, 2)
	if len(rest) != 0 {
		t.Fatalf("source trailing bytes = %d", len(rest))
	}
	targetData := targetConn.write.Bytes()
	assertCurrentPartyFrameSelectedSlot(t, targetData, 1)
	targetRest := assertRuntimePartySnapshot(t, targetData, 2)
	if len(targetRest) != 0 {
		t.Fatalf("target trailing bytes = %d", len(targetRest))
	}
}

func TestSendCurrentPartyFrameProjectionRejectsPartyMissingReceiver(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	receiverConn := &bufferConn{}
	receiver := &gameSession{
		conn:                receiverConn,
		selectedCharacterID: 1001,
	}
	other := &gameSession{
		conn:                &bufferConn{},
		selectedCharacterID: 1002,
	}
	service.bindGameSessionCharacter(receiver, 1001)
	service.bindGameSessionCharacter(other, 1002)

	state := alignedcmd.PartyState{
		PartyID:    77,
		UserID:     1002,
		MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1002, UserState: 1},
		},
	}
	if _, err := service.sendCurrentPartyFrameProjection(receiver, state, "test_missing_receiver"); err == nil {
		t.Fatal("party projection without the receiving character should fail closed")
	}
	if receiverConn.write.Len() != 0 {
		t.Fatalf("fail-closed party projection wrote %d bytes", receiverConn.write.Len())
	}

	emptyState := alignedcmd.PartyState{
		PartyID:    78,
		UserID:     1001,
		MaxMembers: 4,
	}
	if _, err := service.sendCurrentPartyFrameProjection(receiver, emptyState, "test_empty_active_party"); err == nil {
		t.Fatal("active party projection without members should fail closed")
	}
	if receiverConn.write.Len() != 0 {
		t.Fatalf("empty active party projection wrote %d bytes", receiverConn.write.Len())
	}
}

func TestSendCurrentPartyActorFrameProjectionUpdatesRemoteJoinState(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	receiverConn := &bufferConn{}
	receiver := &gameSession{conn: receiverConn, selectedCharacterID: 1001}
	actor := &gameSession{conn: &bufferConn{}, selectedCharacterID: 1002}
	service.bindGameSessionCharacter(receiver, 1001)
	service.bindGameSessionCharacter(actor, 1002)
	createdSoloParty := alignedcmd.PartyState{
		PartyID: 1002, UserID: 1002, IsLeader: true, MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{{UserID: 1002, UserState: 1}},
	}
	if sent, err := service.sendCurrentPartyActorFrameProjection(receiver, actor, createdSoloParty, "test_remote_party_created"); err != nil || !sent {
		t.Fatalf("active remote projection sent=%t err=%v", sent, err)
	}
	packet, rest := splitGameServerUpperPacket(t, receiverConn.write.Bytes())
	if len(packet.Body) < 6 || binary.LittleEndian.Uint16(packet.Body[4:6]) != currentSceneActorObjectKey(1002) {
		t.Fatalf("remote actor key body=% X", packet.Body)
	}
	assertCurrentPartyFrameProjection(t, receiverConn.write.Bytes(), 1, true)
	if len(rest) != 0 {
		t.Fatalf("active remote projection trailing=%d", len(rest))
	}

	receiverConn.write.Reset()
	if sent, err := service.sendCurrentPartyActorFrameProjection(receiver, actor, alignedcmd.PartyState{}, "test_remote_party_cleared"); err != nil || !sent {
		t.Fatalf("empty remote projection sent=%t err=%v", sent, err)
	}
	removed, rest := splitGameServerUpperPacket(t, receiverConn.write.Bytes())
	wantRemoved := buildCurrentSceneOp9ActorRemovalBodyInContext(
		currentSceneActorObjectKey(1002),
		receiver.townActorOwnerChannel,
	)
	if removed.Header.Classification != 0 || removed.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) ||
		!bytes.Equal(removed.Body, wantRemoved) || len(rest) != 0 {
		t.Fatalf("empty remote projection class=%d msg=%d body=% X trailing=%d",
			removed.Header.Classification, removed.Header.MsgID, removed.Body, len(rest))
	}
}

func TestSendRuntimePartySnapshotDefersWholeSequenceForUnboundSession(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		selectedCharacterID: 1001,
	}
	state := alignedcmd.PartyState{
		PartyID:    77,
		UserID:     1001,
		MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1},
		},
	}
	if err := service.sendRuntimePartySnapshot(session, state); err != nil {
		t.Fatalf("deferred party snapshot: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("deferred party snapshot wrote a partial sequence of %d bytes", conn.write.Len())
	}
}

func TestHandleResponsePeerAcceptsOnlinePartyInvite(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	inviterConn := &bufferConn{}
	acceptorConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	inviter := &gameSession{
		conn:                inviterConn,
		channel:             channel,
		selectedCharacterID: 1001,
		party: partySessionState{state: alignedcmd.PartyState{
			PartyID:    88,
			UserID:     1001,
			UserState:  1,
			NameBytes:  []byte("party"),
			MaxMembers: 4,
			Members: []alignedcmd.PartyMemberState{
				{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			},
		}},
	}
	acceptor := &gameSession{
		conn:                acceptorConn,
		channel:             channel,
		selectedCharacterID: 1002,
	}
	service.bindGameSessionCharacter(inviter, 1001)
	service.bindGameSessionCharacter(acceptor, 1002)
	inviterIdentity, inviterBound := service.boundGameSessionCharacterSnapshot(inviter)
	acceptorIdentity, acceptorBound := service.boundGameSessionCharacterSnapshot(acceptor)
	if !inviterBound || !acceptorBound || !service.runtimePartyManagerForService().RecordInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		inviterIdentity.character, inviterIdentity.generation, 0, 13,
	) {
		t.Fatal("could not install a current-session party invite")
	}

	handled, err := service.handleAlignedGameCommand(
		acceptor,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketResponsePeer),
		[]byte{0xe9, 0x03, 13, 1, 0, 0, 0},
	)
	if err != nil {
		t.Fatalf("handle response peer: %v", err)
	}
	if !handled {
		t.Fatalf("ResponsePeer should be handled by online party bridge")
	}
	if _, pending := service.runtimePartyManagerForService().ConsumeInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		inviterIdentity.character, inviterIdentity.generation, 13,
	); pending {
		t.Fatal("central pending invite was not consumed")
	}
	ack, rest := splitGameServerUpperPacket(t, acceptorConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketResponsePeer) ||
		!bytes.Equal(ack.Body, []byte{1, 0xe9, 0x03, 13}) {
		t.Fatalf("response peer ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	// The class1/op11 acceptance ACK already completed the ordinary invite.
	// Sending class1/op705 here makes the current client enter its raid flow.
	rest = assertRuntimePartySnapshot(t, rest, 2)
	if len(rest) != 0 {
		t.Fatalf("acceptor trailing bytes = %d", len(rest))
	}
	responseNotice, inviterRest := splitGameServerUpperPacket(t, inviterConn.write.Bytes())
	if responseNotice.Header.Classification != 0 || responseNotice.Header.MsgID != 8 ||
		!bytes.Equal(responseNotice.Body, []byte{0xea, 0x03, 13, 1, 0, 0, 0}) {
		t.Fatalf("response peer notice = class %d msg %d body=% X", responseNotice.Header.Classification, responseNotice.Header.MsgID, responseNotice.Body)
	}
	inviterRest = assertRuntimePartySnapshot(t, inviterRest, 2)
	if len(inviterRest) != 0 {
		t.Fatalf("inviter trailing bytes = %d", len(inviterRest))
	}
}

func TestPendingPartyInviteRejectsReplacedInviterSession(t *testing.T) {
	service := &Service{}
	inviterOld := &gameSession{}
	acceptor := &gameSession{}
	service.bindGameSessionCharacter(inviterOld, 1001)
	service.bindGameSessionCharacter(acceptor, 1002)

	oldIdentity, oldBound := service.boundGameSessionCharacterSnapshot(inviterOld)
	acceptorIdentity, acceptorBound := service.boundGameSessionCharacterSnapshot(acceptor)
	if !oldBound || !acceptorBound || !service.runtimePartyManagerForService().RecordInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		oldIdentity.character, oldIdentity.generation, 0, 13,
	) {
		t.Fatal("could not register original invite")
	}

	// A reconnect keeps the same character ID but creates a new game session.
	// An old invitation must not attach the acceptor to that new connection.
	inviterReplacement := &gameSession{}
	service.bindGameSessionCharacter(inviterReplacement, 1001)
	replacementIdentity, replacementBound := service.onlineGameSessionCharacterSnapshot(1001)
	if !replacementBound || replacementIdentity.session != inviterReplacement {
		t.Fatal("replacement inviter was not installed as the authoritative session")
	}
	if _, accepted := service.runtimePartyManagerForService().ConsumeInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		replacementIdentity.character, replacementIdentity.generation, 13,
	); accepted {
		t.Fatal("invite accepted after inviter session replacement")
	}

	if !service.runtimePartyManagerForService().RecordInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		replacementIdentity.character, replacementIdentity.generation, 0, 13,
	) {
		t.Fatal("could not replace stale invite with current session")
	}
	if _, accepted := service.runtimePartyManagerForService().ConsumeInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		replacementIdentity.character, replacementIdentity.generation, 13,
	); !accepted {
		t.Fatal("current-session invite was not accepted")
	}
}

func TestOnlinePartyCommandIgnoresReplacedSourceSession(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	staleSource := &gameSession{}
	target := &gameSession{}
	service.bindGameSessionCharacter(staleSource, 1001)
	service.bindGameSessionCharacter(target, 1002)

	// Replace character 1001 before its old connection's direct-join command
	// is processed. The old packet must not form a party with the live target.
	replacement := &gameSession{}
	service.bindGameSessionCharacter(replacement, 1001)
	handled, err := service.handleOnlinePartyCommand(
		staleSource,
		uint16(dnfenum.CmdPacketEntryIntoParty),
		[]byte{0xea, 0x03, 0x00, 0x00},
	)
	if err != nil || !handled {
		t.Fatalf("stale direct join handled=%t err=%v", handled, err)
	}
	if got := runtimePartyStateSnapshot(target); got.PartyID != 0 || len(got.Members) != 0 {
		t.Fatalf("stale direct join changed target party: %+v", got)
	}
	if got := runtimePartyStateSnapshot(replacement); got.PartyID != 0 || len(got.Members) != 0 {
		t.Fatalf("stale direct join changed replacement party: %+v", got)
	}
}

func TestHandleRequestPeerAcknowledgesVisibleTownTarget(t *testing.T) {
	service := &Service{
		options:       options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers: newOnlinePlayerManager(),
	}
	sourceConn := &bufferConn{}
	source := &gameSession{
		conn:                sourceConn,
		connID:              "peer-source",
		selectedCharacterID: 1001,
	}
	target := &gameSession{
		conn:                &bufferConn{},
		connID:              "peer-target",
		selectedCharacterID: 1002,
	}
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{
		CharacterID: 1001,
		TownID:      1,
		AreaID:      2,
		Session:     source,
	})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{
		CharacterID: 1002,
		TownID:      1,
		AreaID:      2,
		Session:     target,
	})
	service.bindGameSessionCharacter(source, 1001)
	service.bindGameSessionCharacter(target, 1002)

	handled, err := service.handleAlignedGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestPeer),
		[]byte{0xea, 0x03, 15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	)
	if err != nil {
		t.Fatalf("handle request peer: %v", err)
	}
	if !handled {
		t.Fatal("RequestPeer should be handled by online peer bridge")
	}
	packet, rest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestPeer) ||
		!bytes.Equal(packet.Body, []byte{1}) {
		t.Fatalf("request peer ack = class %d msg %d body=% X",
			packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
	partyState, rest := splitGameServerUpperPacket(t, rest)
	wantPartyless := buildCurrentSceneOp9ActorRemovalBodyInContext(
		currentSceneActorObjectKey(1002),
		source.townActorOwnerChannel,
	)
	if partyState.Header.Classification != 0 ||
		partyState.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) ||
		!bytes.Equal(partyState.Body, wantPartyless) {
		t.Fatalf("request peer partyless target state = class %d msg %d body=% X",
			partyState.Header.Classification, partyState.Header.MsgID, partyState.Body)
	}
	selection, rest := splitGameServerUpperPacket(t, rest)
	if selection.Header.Classification != 0 || selection.Header.MsgID != 0x0007 ||
		!bytes.Equal(selection.Body, []byte{0xea, 0x03, 15, 0, 0, 0, 0, 0xff, 0xff}) {
		t.Fatalf("request peer selection notice = class %d msg %d body=% X",
			selection.Header.Classification, selection.Header.MsgID, selection.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("request peer trailing bytes = %d", len(rest))
	}
}

func TestHandleRequestPeerRefreshesActiveTargetBeforeOpeningMenu(t *testing.T) {
	service := &Service{
		options:       options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers: newOnlinePlayerManager(),
	}
	sourceConn := &bufferConn{}
	source := &gameSession{conn: sourceConn, connID: "peer-source-active", selectedCharacterID: 1001}
	target := &gameSession{conn: &bufferConn{}, connID: "peer-target-active", selectedCharacterID: 1002}
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1001, TownID: 1, AreaID: 2, Session: source})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1002, TownID: 1, AreaID: 2, Session: target})
	service.bindGameSessionCharacter(source, 1001)
	service.bindGameSessionCharacter(target, 1002)
	storeRuntimePartyState(target, alignedcmd.PartyState{
		PartyID: 1002, UserID: 1002, IsLeader: true, MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{{UserID: 1002, UserState: 1}},
	})

	handled, err := service.handleAlignedGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestPeer),
		[]byte{0xea, 0x03, 15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	)
	if err != nil || !handled {
		t.Fatalf("handle active target request peer handled=%t err=%v", handled, err)
	}
	ack, rest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketRequestPeer) ||
		!bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("active target request ack class=%d msg=%d body=%x", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	rest = assertCurrentPartyFrameProjection(t, rest, 1, true)
	selection, rest := splitGameServerUpperPacket(t, rest)
	if selection.Header.Classification != 0 || selection.Header.MsgID != 0x0007 ||
		!bytes.Equal(selection.Body, []byte{0xea, 0x03, 15, 0, 0, 0, 0, 0, 0}) || len(rest) != 0 {
		t.Fatalf("active target selection class=%d msg=%d body=%x trailing=%d",
			selection.Header.Classification, selection.Header.MsgID, selection.Body, len(rest))
	}
}

func TestHandleRequestPeerDoesNotAcknowledgeTargetInAnotherArea(t *testing.T) {
	service := &Service{
		options:       options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers: newOnlinePlayerManager(),
	}
	sourceConn := &bufferConn{}
	source := &gameSession{
		conn:                sourceConn,
		connID:              "peer-source",
		selectedCharacterID: 1001,
	}
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{
		CharacterID: 1001,
		TownID:      1,
		AreaID:      2,
		Session:     source,
	})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{
		CharacterID: 1002,
		TownID:      1,
		AreaID:      3,
		Session:     &gameSession{selectedCharacterID: 1002},
	})

	handled, err := service.handleAlignedGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestPeer),
		[]byte{0xea, 0x03, 15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	)
	if err != nil {
		t.Fatalf("handle request peer outside area: %v", err)
	}
	if !handled {
		t.Fatal("RequestPeer should be consumed even when the target is outside the area")
	}
	if sourceConn.write.Len() != 0 {
		t.Fatalf("outside-area request wrote %d bytes", sourceConn.write.Len())
	}
}

func TestHandleRequestPeerForwardsOnlineQuickPartyInvite(t *testing.T) {
	service := &Service{
		options:       options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers: newOnlinePlayerManager(),
	}
	sourceConn := &bufferConn{}
	targetConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	source := &gameSession{
		conn:                sourceConn,
		channel:             channel,
		selectedCharacterID: 1001,
	}
	target := &gameSession{
		conn:                targetConn,
		channel:             channel,
		selectedCharacterID: 1002,
	}
	service.bindGameSessionCharacter(source, 1001)
	service.bindGameSessionCharacter(target, 1002)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1001, TownID: 1, AreaID: 2, Session: source})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1002, TownID: 1, AreaID: 2, Session: target})

	handled, err := service.handleAlignedGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestPeer),
		[]byte{0xea, 0x03, 13, 0, 0, 0, 0, 0, 0, 0, 0},
	)
	if err != nil {
		t.Fatalf("handle request peer invite forward: %v", err)
	}
	if !handled {
		t.Fatalf("RequestPeer invite should be handled by online party bridge")
	}
	sourceAck, sourceRest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if sourceAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		sourceAck.Header.MsgID != uint16(dnfenum.CmdPacketRequestPeer) || !bytes.Equal(sourceAck.Body, []byte{1}) || len(sourceRest) != 0 {
		t.Fatalf("request peer ack = class %d msg %d body=% X trailing=%d", sourceAck.Header.Classification, sourceAck.Header.MsgID, sourceAck.Body, len(sourceRest))
	}
	sourceIdentity, sourceBound := service.boundGameSessionCharacterSnapshot(source)
	targetIdentity, targetBound := service.boundGameSessionCharacterSnapshot(target)
	if !sourceBound || !targetBound {
		t.Fatal("invite endpoints were not bound")
	}
	if _, pending := service.runtimePartyManagerForService().ConsumeInvite(
		targetIdentity.character, targetIdentity.generation,
		sourceIdentity.character, sourceIdentity.generation, 13,
	); !pending {
		t.Fatal("central invite was not recorded")
	}
	packet, rest := splitGameServerUpperPacket(t, targetConn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("target trailing bytes = %d", len(rest))
	}
	if packet.Header.Classification != 0 || packet.Header.MsgID != 7 ||
		!bytes.Equal(packet.Body, []byte{0xe9, 0x03, 13, 0, 0, 0, 0}) {
		t.Fatalf("forwarded invite packet = class %d msg %d body=% X", packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
}

func TestHandleResponsePeerRejectsPendingInviteWithoutForwarding(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	inviterConn := &bufferConn{}
	acceptorConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	inviter := &gameSession{
		conn:                inviterConn,
		channel:             channel,
		selectedCharacterID: 1001,
	}
	acceptor := &gameSession{
		conn:                acceptorConn,
		channel:             channel,
		selectedCharacterID: 1002,
	}
	service.bindGameSessionCharacter(inviter, 1001)
	service.bindGameSessionCharacter(acceptor, 1002)
	inviterIdentity, inviterBound := service.boundGameSessionCharacterSnapshot(inviter)
	acceptorIdentity, acceptorBound := service.boundGameSessionCharacterSnapshot(acceptor)
	if !inviterBound || !acceptorBound || !service.runtimePartyManagerForService().RecordInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		inviterIdentity.character, inviterIdentity.generation, 0, 13,
	) {
		t.Fatal("could not install a current-session party invite")
	}

	handled, err := service.handleAlignedGameCommand(
		acceptor,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketResponsePeer),
		[]byte{0xe9, 0x03, 13, 0, 0, 0, 0},
	)
	if err != nil {
		t.Fatalf("handle response peer reject: %v", err)
	}
	if !handled {
		t.Fatalf("ResponsePeer reject should be handled by online party bridge")
	}
	if _, pending := service.runtimePartyManagerForService().ConsumeInvite(
		acceptorIdentity.character, acceptorIdentity.generation,
		inviterIdentity.character, inviterIdentity.generation, 13,
	); pending {
		t.Fatal("central pending invite was not consumed")
	}
	ack, acceptorRest := splitGameServerUpperPacket(t, acceptorConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != uint16(dnfenum.CmdPacketResponsePeer) ||
		!bytes.Equal(ack.Body, []byte{1, 0xe9, 0x03, 13}) || len(acceptorRest) != 0 {
		t.Fatalf("reject ack = class %d msg %d body=% X trailing=%d", ack.Header.Classification, ack.Header.MsgID, ack.Body, len(acceptorRest))
	}
	notice, inviterRest := splitGameServerUpperPacket(t, inviterConn.write.Bytes())
	if notice.Header.Classification != 0 || notice.Header.MsgID != 8 ||
		!bytes.Equal(notice.Body, []byte{0xea, 0x03, 13, 0, 0, 0, 0}) || len(inviterRest) != 0 {
		t.Fatalf("reject notice = class %d msg %d body=% X trailing=%d", notice.Header.Classification, notice.Header.MsgID, notice.Body, len(inviterRest))
	}
}

func TestHandleRequestAndResponsePeerForwardsTradePrompt(t *testing.T) {
	service := &Service{
		options:       options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers: newOnlinePlayerManager(),
	}
	sourceConn := &bufferConn{}
	targetConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 42, Name: "ch.42"}
	source := &gameSession{conn: sourceConn, channel: channel, selectedCharacterID: 1}
	target := &gameSession{conn: targetConn, channel: channel, selectedCharacterID: 5}
	service.bindGameSessionCharacter(source, 1)
	service.bindGameSessionCharacter(target, 5)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1, TownID: 1, AreaID: 2, Session: source})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 5, TownID: 1, AreaID: 2, Session: target})

	requestBody := []byte{5, 0, 1, 0x0a, 0x82, 0x01, 0, 0, 0, 0, 0}
	handled, err := service.handleAlignedGameCommand(source, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketRequestPeer), requestBody)
	if err != nil || !handled {
		t.Fatalf("trade request handled=%t err=%v", handled, err)
	}
	requestAck, sourceRest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if requestAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		requestAck.Header.MsgID != uint16(dnfenum.CmdPacketRequestPeer) || !bytes.Equal(requestAck.Body, []byte{1}) || len(sourceRest) != 0 {
		t.Fatalf("trade request ack = class %d msg %d body=% X trailing=%d", requestAck.Header.Classification, requestAck.Header.MsgID, requestAck.Body, len(sourceRest))
	}
	prompt, targetRest := splitGameServerUpperPacket(t, targetConn.write.Bytes())
	if prompt.Header.Classification != 0 || prompt.Header.MsgID != 7 ||
		!bytes.Equal(prompt.Body, []byte{1, 0, 1, 0x0a, 0x82, 0x01, 0, 0, 0, 0, 0}) || len(targetRest) != 0 {
		t.Fatalf("trade prompt = class %d msg %d body=% X trailing=%d", prompt.Header.Classification, prompt.Header.MsgID, prompt.Body, len(targetRest))
	}
	// The response below consumes the generation-bound central request. Its ACK
	// and forwarded notice prove that this request was recorded without
	// destructively reading the pending-invite table here.

	sourceConn.write.Reset()
	targetConn.write.Reset()
	handled, err = service.handleAlignedGameCommand(target, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketResponsePeer), []byte{1, 0, 1, 0, 0, 0, 0})
	if err != nil || !handled {
		t.Fatalf("trade response handled=%t err=%v", handled, err)
	}
	responseAck, targetRest := splitGameServerUpperPacket(t, targetConn.write.Bytes())
	if responseAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		responseAck.Header.MsgID != uint16(dnfenum.CmdPacketResponsePeer) ||
		!bytes.Equal(responseAck.Body, []byte{1, 1, 0, 1, 0}) || len(targetRest) != 0 {
		t.Fatalf("trade response ack = class %d msg %d body=% X trailing=%d", responseAck.Header.Classification, responseAck.Header.MsgID, responseAck.Body, len(targetRest))
	}
	notice, sourceRest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if notice.Header.Classification != 0 || notice.Header.MsgID != 8 ||
		!bytes.Equal(notice.Body, []byte{5, 0, 1, 0, 0, 0, 0}) || len(sourceRest) != 0 {
		t.Fatalf("trade response notice = class %d msg %d body=% X trailing=%d", notice.Header.Classification, notice.Header.MsgID, notice.Body, len(sourceRest))
	}
}

func TestHandleOnlineLeavePartyClearsResidualSingletonProjection(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	leaderConn := &bufferConn{}
	memberConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	state := alignedcmd.PartyState{
		PartyID:    77,
		UserID:     1001,
		IsLeader:   true,
		NameBytes:  []byte("party"),
		MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1002, UserState: 1, HPPercent: 90, MPPercent: 80},
		},
	}
	leader := &gameSession{conn: leaderConn, channel: channel, selectedCharacterID: 1001, party: partySessionState{state: cloneRuntimePartyState(state)}}
	member := &gameSession{conn: memberConn, channel: channel, selectedCharacterID: 1002, party: partySessionState{state: cloneRuntimePartyState(state)}}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(member, 1002)

	handled, err := service.handleAlignedGameCommand(
		leader,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketLeaveParty),
		nil,
	)
	if err != nil {
		t.Fatalf("handle leave party: %v", err)
	}
	if !handled {
		t.Fatalf("LeaveParty should be handled by online party bridge")
	}
	if leader.party.state.PartyID != 0 {
		t.Fatalf("leader party was not cleared: %+v", leader.party.state)
	}
	if member.party.state.PartyID != 0 {
		t.Fatalf("member residual singleton was not cleared: %+v", member.party.state)
	}

	ack, rest := splitGameServerUpperPacket(t, leaderConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) ||
		!bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("leave ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	rest = assertRuntimePartyRemoved(t, rest, 1001, 0)
	if len(rest) != 0 {
		t.Fatalf("leader trailing bytes = %d", len(rest))
	}
	memberRest := assertRuntimePartyRemoved(t, memberConn.write.Bytes(), 1001, 0)
	if len(memberRest) != 0 {
		t.Fatalf("member trailing bytes = %d", len(memberRest))
	}
}

func TestHandleOnlineWalkoutPartyMemberClearsLeaderSingletonProjection(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	leaderConn := &bufferConn{}
	memberConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	state := alignedcmd.PartyState{
		PartyID:    77,
		UserID:     1001,
		IsLeader:   true,
		NameBytes:  []byte("party"),
		MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1002, UserState: 1, HPPercent: 90, MPPercent: 80},
		},
	}
	leader := &gameSession{conn: leaderConn, channel: channel, selectedCharacterID: 1001, party: partySessionState{state: cloneRuntimePartyState(state)}}
	member := &gameSession{conn: memberConn, channel: channel, selectedCharacterID: 1002, party: partySessionState{state: cloneRuntimePartyState(state)}}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(member, 1002)

	handled, err := service.handleAlignedGameCommand(
		leader,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketWalkoutPartyMember),
		[]byte{1},
	)
	if err != nil {
		t.Fatalf("handle walkout party member: %v", err)
	}
	if !handled {
		t.Fatalf("WalkoutPartyMember should be handled by online party bridge")
	}
	if leader.party.state.PartyID != 0 {
		t.Fatalf("leader residual singleton was not cleared: %+v", leader.party.state)
	}
	if member.party.state.PartyID != 0 {
		t.Fatalf("kicked member party was not cleared: %+v", member.party.state)
	}

	ack, rest := splitGameServerUpperPacket(t, leaderConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		!bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("walkout ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	rest = assertRuntimePartyWalkoutNotice(t, rest, 1, 0)
	rest = assertRuntimePartyRemoved(t, rest, 1002, 0)
	if len(rest) != 0 {
		t.Fatalf("leader trailing bytes = %d", len(rest))
	}
	memberRest := assertRuntimePartyWalkoutNotice(t, memberConn.write.Bytes(), 1, 0)
	memberRest = assertRuntimePartyRemoved(t, memberRest, 1002, 0)
	if len(memberRest) != 0 {
		t.Fatalf("member trailing bytes = %d", len(memberRest))
	}
}

func TestHandleOnlineChangePartyMemberPositionBroadcastsSlots(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	leaderConn := &bufferConn{}
	memberAConn := &bufferConn{}
	memberBConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	state := alignedcmd.PartyState{
		PartyID:    77,
		UserID:     1001,
		IsLeader:   true,
		NameBytes:  []byte("party"),
		MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1002, UserState: 1, HPPercent: 90, MPPercent: 80},
			{UserID: 1003, UserState: 1, HPPercent: 70, MPPercent: 60},
		},
	}
	leader := &gameSession{conn: leaderConn, channel: channel, selectedCharacterID: 1001, party: partySessionState{state: cloneRuntimePartyState(state)}}
	memberA := &gameSession{conn: memberAConn, channel: channel, selectedCharacterID: 1002, party: partySessionState{state: cloneRuntimePartyState(state)}}
	memberB := &gameSession{conn: memberBConn, channel: channel, selectedCharacterID: 1003, party: partySessionState{state: cloneRuntimePartyState(state)}}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(memberA, 1002)
	service.bindGameSessionCharacter(memberB, 1003)

	handled, err := service.handleAlignedGameCommand(
		leader,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketChangePartyMemberPosition),
		[]byte{1, 3},
	)
	if err != nil {
		t.Fatalf("handle change party member position: %v", err)
	}
	if !handled {
		t.Fatalf("ChangePartyMemberPosition should be handled by online party bridge")
	}
	for name, session := range map[string]*gameSession{
		"leader":  leader,
		"memberA": memberA,
		"memberB": memberB,
	} {
		if len(session.party.state.Members) != 3 ||
			session.party.state.Members[0].UserID != 1001 ||
			session.party.state.Members[1].UserID != 1003 ||
			session.party.state.Members[2].UserID != 1002 {
			t.Fatalf("%s party members = %+v, want 1001,1003,1002", name, session.party.state.Members)
		}
	}
	for name, test := range map[string]struct {
		data         []byte
		selectedSlot byte
	}{
		"leader":  {data: leaderConn.write.Bytes(), selectedSlot: 0},
		"memberA": {data: memberAConn.write.Bytes(), selectedSlot: 2},
		"memberB": {data: memberBConn.write.Bytes(), selectedSlot: 1},
	} {
		_, frameData := splitGameServerUpperPacket(t, test.data)
		assertCurrentPartyFrameSelectedSlot(t, frameData, test.selectedSlot)
		rest := assertRuntimePartyChangePosition(t, test.data, 1, 3)
		rest = assertRuntimePartySceneRefresh(t, rest, 3)
		if len(rest) != 0 {
			t.Fatalf("%s trailing bytes = %d", name, len(rest))
		}
	}
}

func TestHandleOnlineChangeHostMovesLeaderToFirstSlotAndRebuildsAllMembers(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	leaderConn := &bufferConn{}
	memberConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	state := alignedcmd.PartyState{
		PartyID:    1001,
		UserID:     1001,
		IsLeader:   true,
		MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1002, UserState: 1, HPPercent: 90, MPPercent: 80},
		},
	}
	leader := &gameSession{conn: leaderConn, channel: channel, selectedCharacterID: 1001, party: partySessionState{state: cloneRuntimePartyState(state)}}
	member := &gameSession{conn: memberConn, channel: channel, selectedCharacterID: 1002, party: partySessionState{state: cloneRuntimePartyState(state)}}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(member, 1002)

	handled, err := service.handleAlignedGameCommand(
		leader,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketChangeHost),
		[]byte{1},
	)
	if err != nil {
		t.Fatalf("handle change host: %v", err)
	}
	if !handled {
		t.Fatal("ChangeHost should be handled by online party bridge")
	}
	for name, session := range map[string]*gameSession{"old_leader": leader, "new_leader": member} {
		got := runtimePartyStateSnapshot(session)
		members := runtimePartyMembers(got)
		if got.UserID != 1002 || got.PartyID == 1001 || len(members) != 2 || members[0].UserID != 1002 || members[1].UserID != 1001 {
			t.Fatalf("%s party state=%+v members=%+v", name, got, members)
		}
	}
	if leaderConn.write.Len() == 0 || memberConn.write.Len() == 0 {
		t.Fatalf("change host did not rebuild both clients: leader=%d member=%d", leaderConn.write.Len(), memberConn.write.Len())
	}
	for name, data := range map[string][]byte{
		"old_leader": leaderConn.write.Bytes(),
		"new_leader": memberConn.write.Bytes(),
	} {
		var msgIDs []uint16
		for len(data) > 0 {
			packet, rest := splitGameServerUpperPacket(t, data)
			msgIDs = append(msgIDs, packet.Header.MsgID)
			data = rest
		}
		if !reflect.DeepEqual(msgIDs, []uint16{
			uint16(dnfenum.CmdPacketRecoverStamina),
			0x0099,
			0x0099,
			0x000b,
			uint16(dnfenum.CmdPacketRecoverStamina),
		}) {
			t.Fatalf("%s change-host packets=%v, want old-table clear then fresh 86JP realtime/endpoints/roster generation", name, msgIDs)
		}
	}
}

func TestHandleOnlineReserveLeavePartyBroadcastsToMembers(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	sourceConn := &bufferConn{}
	memberConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	state := alignedcmd.PartyState{
		PartyID:    77,
		UserID:     1001,
		IsLeader:   true,
		NameBytes:  []byte("party"),
		MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1002, UserState: 1, HPPercent: 90, MPPercent: 80},
		},
	}
	source := &gameSession{conn: sourceConn, channel: channel, selectedCharacterID: 1001, party: partySessionState{state: cloneRuntimePartyState(state)}}
	member := &gameSession{conn: memberConn, channel: channel, selectedCharacterID: 1002, party: partySessionState{state: cloneRuntimePartyState(state)}}
	service.bindGameSessionCharacter(source, 1001)
	service.bindGameSessionCharacter(member, 1002)

	handled, err := service.handleAlignedGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketReserveLeaveParty),
		[]byte{1},
	)
	if err != nil {
		t.Fatalf("handle reserve leave party: %v", err)
	}
	if !handled {
		t.Fatalf("ReserveLeaveParty should be handled by online party bridge")
	}
	for name, data := range map[string][]byte{
		"source": sourceConn.write.Bytes(),
		"member": memberConn.write.Bytes(),
	} {
		packet, rest := splitGameServerUpperPacket(t, data)
		if len(rest) != 0 {
			t.Fatalf("%s trailing bytes = %d", name, len(rest))
		}
		if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
			packet.Header.MsgID != uint16(dnfenum.CmdPacketReserveLeaveParty) ||
			!bytes.Equal(packet.Body, []byte{1, 1, 0xe9, 0x03}) {
			t.Fatalf("%s reserve packet = class %d msg %d body=% X",
				name, packet.Header.Classification, packet.Header.MsgID, packet.Body)
		}
	}
}

func TestHandleOnlineEntryIntoPartyFinishUsesCurrentExeClassZeroBody(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	conn := &bufferConn{}
	state := alignedcmd.PartyState{
		PartyID:   1,
		UserID:    1001,
		UserState: 1,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1},
			{UserID: 1002, UserState: 1},
		},
	}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038},
		selectedCharacterID: 1001,
		party:               partySessionState{state: cloneRuntimePartyState(state)},
	}

	handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketEntryIntoPartyFinish),
		nil,
	)
	if err != nil {
		t.Fatalf("handle entry into party finish: %v", err)
	}
	if !handled {
		t.Fatalf("EntryIntoPartyFinish should be handled by online party bridge")
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("entry finish trailing bytes = %d", len(rest))
	}
	if packet.Header.Classification != 0 ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketEntryIntoPartyFinish) ||
		!bytes.Equal(packet.Body, []byte{1, 0}) {
		t.Fatalf("entry finish packet = class %d msg %d body=% X",
			packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
}

func TestHandleOnlineCreateRaidBuildsRuntimeRefresh(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 200, Type: 23, Name: "raid", Port: 10038}
	session := &gameSession{conn: conn, channel: channel}
	service.bindGameSessionCharacter(session, 1001)

	handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketCreateRaid),
		buildRaidInfoRequestForBridgeTest(1, []byte("anton"), 5),
	)
	if err != nil {
		t.Fatalf("handle create raid: %v", err)
	}
	if !handled {
		t.Fatalf("CreateRaid should be handled by online raid bridge")
	}
	wantRaidKey := uint32(200)<<16 | 1001
	state := service.raids[wantRaidKey]
	if state == nil || state.LeaderID != 1001 || len(state.Members) != 1 || !bytes.Equal(state.NameBytes, []byte("anton")) {
		t.Fatalf("runtime raid state = %+v, want leader 1001 with one member", state)
	}

	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantCreateBody := raid.BuildCreateRaidResultBody(wantRaidKey)
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketCreateRaid) ||
		!bytes.Equal(ack.Body, wantCreateBody) {
		t.Fatalf("create raid ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	members, rest := splitRaidRefreshPacket(t, rest, wantRaidKey)
	if len(rest) != 0 {
		t.Fatalf("create raid trailing bytes = %d", len(rest))
	}
	if len(members) != 1 || members[0].CharID != 1001 || members[0].GroupIndex != 1 || members[0].SlotOrder != 0 {
		t.Fatalf("raid refresh members = %+v, want char 1001 group 1 slot 0", members)
	}
}

func TestHandleOnlineRaidManagerWorkMovesMemberWithoutAck(t *testing.T) {
	service := &Service{
		options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		raids:   make(map[uint32]*runtimeRaidState),
	}
	leaderConn := &bufferConn{}
	memberConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 200, Type: 23, Name: "raid", Port: 10038}
	leader := &gameSession{conn: leaderConn, channel: channel}
	member := &gameSession{conn: memberConn, channel: channel}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(member, 1002)
	raidKey := uint32(200)<<16 | 1001
	service.raids[raidKey] = &runtimeRaidState{
		RaidKey:   raidKey,
		LeaderID:  1001,
		NameBytes: []byte("anton"),
		Members: []runtimeRaidMemberState{
			{CharID: 1001, Name: "leader", GroupIndex: 1, SlotOrder: 0, UserState: 1, HPPercent: 100, MPPercent: 100},
			{CharID: 1002, Name: "member", GroupIndex: 1, SlotOrder: 1, UserState: 1, HPPercent: 100, MPPercent: 100},
		},
	}

	handled, err := service.handleAlignedGameCommand(
		leader,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRaidManagerWork),
		buildRaidManagerWorkRequestForBridgeTest(0, 1002, 2),
	)
	if err != nil {
		t.Fatalf("handle raid manager work: %v", err)
	}
	if !handled {
		t.Fatalf("RaidManagerWork should be handled by online raid bridge")
	}
	members, rest := splitRaidRefreshPacket(t, leaderConn.write.Bytes(), raidKey)
	if len(rest) != 0 {
		t.Fatalf("leader trailing bytes = %d", len(rest))
	}
	assertRaidRefreshMember(t, members, 1002, 2, 0)
	members, rest = splitRaidRefreshPacket(t, memberConn.write.Bytes(), raidKey)
	if len(rest) != 0 {
		t.Fatalf("member trailing bytes = %d", len(rest))
	}
	assertRaidRefreshMember(t, members, 1002, 2, 0)
}

func TestHandleOnlineRaidMemberChangeStateBroadcastsRefreshWithoutAck(t *testing.T) {
	service := &Service{
		options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		raids:   make(map[uint32]*runtimeRaidState),
	}
	leaderConn := &bufferConn{}
	memberConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 200, Type: 23, Name: "raid", Port: 10038}
	leader := &gameSession{conn: leaderConn, channel: channel}
	member := &gameSession{conn: memberConn, channel: channel}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(member, 1002)
	raidKey := uint32(200)<<16 | 1001
	service.raids[raidKey] = &runtimeRaidState{
		RaidKey:   raidKey,
		LeaderID:  1001,
		NameBytes: []byte("anton"),
		Members: []runtimeRaidMemberState{
			{CharID: 1001, Name: "leader", GroupIndex: 1, SlotOrder: 0, UserState: 1, HPPercent: 100, MPPercent: 100},
			{CharID: 1002, Name: "member", GroupIndex: 1, SlotOrder: 1, UserState: 1, HPPercent: 100, MPPercent: 100},
		},
	}

	handled, err := service.handleAlignedGameCommand(
		member,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRaidMemberChangeState),
		[]byte{3},
	)
	if err != nil {
		t.Fatalf("handle raid member change state: %v", err)
	}
	if !handled {
		t.Fatalf("RaidMemberChangeState should be handled by online raid bridge")
	}
	members, rest := splitRaidRefreshPacket(t, memberConn.write.Bytes(), raidKey)
	if len(rest) != 0 {
		t.Fatalf("member trailing bytes = %d", len(rest))
	}
	assertRaidRefreshMemberState(t, members, 1002, 1)
	members, rest = splitRaidRefreshPacket(t, leaderConn.write.Bytes(), raidKey)
	if len(rest) != 0 {
		t.Fatalf("leader trailing bytes = %d", len(rest))
	}
	assertRaidRefreshMemberState(t, members, 1002, 1)
}

func TestHandleOnlineStartRaidCreatesPartiesFromRaidGroups(t *testing.T) {
	service := &Service{
		options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		raids:   make(map[uint32]*runtimeRaidState),
	}
	leaderConn := &bufferConn{}
	memberAConn := &bufferConn{}
	memberBConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 200, Type: 23, Name: "raid", Port: 10038}
	leader := &gameSession{conn: leaderConn, channel: channel}
	memberA := &gameSession{conn: memberAConn, channel: channel}
	memberB := &gameSession{conn: memberBConn, channel: channel}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(memberA, 1002)
	service.bindGameSessionCharacter(memberB, 1003)
	raidKey := uint32(200)<<16 | 1001
	service.raids[raidKey] = &runtimeRaidState{
		RaidKey:   raidKey,
		LeaderID:  1001,
		NameBytes: []byte("anton"),
		Members: []runtimeRaidMemberState{
			{CharID: 1001, Name: "leader", GroupIndex: 1, SlotOrder: 0, UserState: 1, HPPercent: 100, MPPercent: 100},
			{CharID: 1002, Name: "memberA", GroupIndex: 1, SlotOrder: 1, UserState: 1, HPPercent: 90, MPPercent: 80},
			{CharID: 1003, Name: "memberB", GroupIndex: 2, SlotOrder: 0, UserState: 1, HPPercent: 70, MPPercent: 60},
		},
	}

	handled, err := service.handleAlignedGameCommand(
		leader,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketStartRaid),
		nil,
	)
	if err != nil {
		t.Fatalf("handle start raid: %v", err)
	}
	if !handled {
		t.Fatalf("StartRaid should be handled by online raid bridge")
	}
	if !service.raids[raidKey].Started {
		t.Fatalf("raid was not marked started")
	}

	ack, rest := splitGameServerUpperPacket(t, leaderConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketStartRaid) ||
		!bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("start raid ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	members, rest := splitRaidRefreshPacket(t, rest, raidKey)
	if len(members) != 3 {
		t.Fatalf("leader raid refresh member count = %d, want 3", len(members))
	}
	rest = assertRuntimePartySnapshot(t, rest, 2)
	if len(rest) != 0 {
		t.Fatalf("leader trailing bytes = %d", len(rest))
	}
	_, rest = splitRaidRefreshPacket(t, memberAConn.write.Bytes(), raidKey)
	rest = assertRuntimePartySnapshot(t, rest, 2)
	if len(rest) != 0 {
		t.Fatalf("memberA trailing bytes = %d", len(rest))
	}
	_, rest = splitRaidRefreshPacket(t, memberBConn.write.Bytes(), raidKey)
	rest = assertRuntimePartySnapshot(t, rest, 1)
	if len(rest) != 0 {
		t.Fatalf("memberB trailing bytes = %d", len(rest))
	}
	if got := runtimePartyStateSnapshot(leader); len(got.Members) != 2 || got.Members[0].UserID != 1001 || got.Members[1].UserID != 1002 {
		t.Fatalf("leader party state = %+v, want group 1 members 1001/1002", got)
	}
	if got := runtimePartyStateSnapshot(memberB); len(got.Members) != 1 || got.Members[0].UserID != 1003 {
		t.Fatalf("memberB party state = %+v, want solo group 2 member 1003", got)
	}
}

func TestHandleOnlineStartRaidRejectsOversizedGroup(t *testing.T) {
	service := &Service{
		options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		raids:   make(map[uint32]*runtimeRaidState),
	}
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 200, Type: 23, Name: "raid", Port: 10038}
	leader := &gameSession{conn: conn, channel: channel}
	service.bindGameSessionCharacter(leader, 1001)
	raidKey := uint32(200)<<16 | 1001
	state := &runtimeRaidState{RaidKey: raidKey, LeaderID: 1001}
	for i := 0; i < runtimeRaidGroupSize+1; i++ {
		state.Members = append(state.Members, runtimeRaidMemberState{
			CharID:     uint16(1001 + i),
			GroupIndex: 1,
			SlotOrder:  byte(i),
			UserState:  1,
			HPPercent:  100,
			MPPercent:  100,
		})
	}
	service.raids[raidKey] = state

	handled, err := service.handleAlignedGameCommand(
		leader,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketStartRaid),
		nil,
	)
	if err != nil {
		t.Fatalf("handle start raid oversized group: %v", err)
	}
	if !handled {
		t.Fatalf("StartRaid should be handled by online raid bridge")
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("oversized group trailing bytes = %d", len(rest))
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketStartRaid) ||
		!bytes.Equal(packet.Body, []byte{0, runtimeRaidFailureCode}) {
		t.Fatalf("oversized group response = class %d msg %d body=% X", packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
	if service.raids[raidKey].Started {
		t.Fatalf("oversized group raid was marked started")
	}
}

func TestRuntimeRaidPartyGroupsNormalizesZeroGroup(t *testing.T) {
	groups := runtimeRaidPartyGroups(runtimeRaidState{
		Members: []runtimeRaidMemberState{
			{CharID: 1001, GroupIndex: 0, SlotOrder: 0},
		},
	})
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("groups = %+v, want one normalized group", groups)
	}
	if groups[0][0].GroupIndex != runtimeRaidDefaultGroup {
		t.Fatalf("group index = %d, want %d", groups[0][0].GroupIndex, runtimeRaidDefaultGroup)
	}
}

type raidRefreshMemberForTest struct {
	CharID     uint16
	State      byte
	GroupIndex byte
	SlotOrder  byte
}

func buildRaidInfoRequestForBridgeTest(route byte, name []byte, tail byte) []byte {
	body := make([]byte, 1+4+len(name)+1)
	body[0] = route
	binary.LittleEndian.PutUint32(body[1:5], uint32(len(name)))
	copy(body[5:], name)
	body[len(body)-1] = tail
	return body
}

func buildRaidManagerWorkRequestForBridgeTest(action uint32, member uint32, group uint32) []byte {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[0:4], action)
	binary.LittleEndian.PutUint32(body[4:8], member)
	binary.LittleEndian.PutUint32(body[8:12], group)
	return body
}

func splitRaidRefreshPacket(t *testing.T, data []byte, wantRaidKey uint32) ([]raidRefreshMemberForTest, []byte) {
	t.Helper()
	packet, rest := splitGameServerUpperPacket(t, data)
	if packet.Header.Classification != 0 || packet.Header.MsgID != raid.RaidMemberRefreshMsgID {
		t.Fatalf("raid refresh packet = class %d msg %d", packet.Header.Classification, packet.Header.MsgID)
	}
	members := parseRaidRefreshBodyForTest(t, packet.Body, wantRaidKey)
	return members, rest
}

func parseRaidRefreshBodyForTest(t *testing.T, body []byte, wantRaidKey uint32) []raidRefreshMemberForTest {
	t.Helper()
	if len(body) < 9 {
		t.Fatalf("raid refresh body too short: %d", len(body))
	}
	raidKey := binary.LittleEndian.Uint32(body[0:4])
	mode := binary.LittleEndian.Uint32(body[4:8])
	if raidKey != wantRaidKey || mode != raid.RaidMemberRefreshMode3 {
		t.Fatalf("raid refresh header = key 0x%08X mode %d, want key 0x%08X mode %d",
			raidKey, mode, wantRaidKey, raid.RaidMemberRefreshMode3)
	}
	count := int(body[8])
	offset := 9
	members := make([]raidRefreshMemberForTest, 0, count)
	for i := 0; i < count; i++ {
		if len(body) < offset+2+1+4 {
			t.Fatalf("raid refresh member[%d] truncated before name: offset=%d len=%d", i, offset, len(body))
		}
		member := raidRefreshMemberForTest{CharID: binary.LittleEndian.Uint16(body[offset : offset+2])}
		offset += 2
		member.State = body[offset]
		offset++
		nameLen := int(binary.LittleEndian.Uint32(body[offset : offset+4]))
		offset += 4
		if len(body) < offset+nameLen+2+2+4+1+1+1+4+2+4 {
			t.Fatalf("raid refresh member[%d] truncated after name: offset=%d name_len=%d len=%d", i, offset, nameLen, len(body))
		}
		offset += nameLen
		offset += 2
		member.GroupIndex = body[offset]
		offset++
		member.SlotOrder = body[offset]
		offset++
		offset += 4 + 1 + 1 + 1 + 4 + 2 + 4
		members = append(members, member)
	}
	if offset != len(body) {
		t.Fatalf("raid refresh body trailing bytes = %d", len(body)-offset)
	}
	return members
}

func assertRaidRefreshMember(t *testing.T, members []raidRefreshMemberForTest, charID uint16, group byte, slot byte) {
	t.Helper()
	for _, member := range members {
		if member.CharID == charID {
			if member.GroupIndex != group || member.SlotOrder != slot {
				t.Fatalf("member %d = group %d slot %d, want group %d slot %d",
					charID, member.GroupIndex, member.SlotOrder, group, slot)
			}
			return
		}
	}
	t.Fatalf("member %d missing from raid refresh: %+v", charID, members)
}

func assertRaidRefreshMemberState(t *testing.T, members []raidRefreshMemberForTest, charID uint16, state byte) {
	t.Helper()
	for _, member := range members {
		if member.CharID == charID {
			if member.State != state {
				t.Fatalf("member %d state = %d, want %d", charID, member.State, state)
			}
			return
		}
	}
	t.Fatalf("member %d missing from raid refresh: %+v", charID, members)
}

func assertRuntimePartySnapshot(t *testing.T, data []byte, wantMembers byte) []byte {
	t.Helper()
	realtime, rest := splitGameServerUpperPacket(t, data)
	if realtime.Header.Classification != 0 || realtime.Header.MsgID != 0x0099 {
		t.Fatalf("realtime packet = class %d msg %d", realtime.Header.Classification, realtime.Header.MsgID)
	}
	if len(realtime.Body) == 0 || realtime.Body[0] != wantMembers {
		t.Fatalf("realtime members = % X, want count %d", realtime.Body, wantMembers)
	}
	peerEndpoints, rest := splitGameServerUpperPacket(t, rest)
	if peerEndpoints.Header.Classification != 0 || peerEndpoints.Header.MsgID != 0x000b {
		t.Fatalf("peer endpoint packet = class %d msg %d", peerEndpoints.Header.Classification, peerEndpoints.Header.MsgID)
	}
	if len(peerEndpoints.Body) != 1+int(wantMembers)*22 || peerEndpoints.Body[0] != wantMembers {
		t.Fatalf("peer endpoint members = % X, want count %d", peerEndpoints.Body, wantMembers)
	}
	rest = assertCurrentPartyFrameProjection(t, rest, wantMembers, wantMembers > 0)
	return rest
}

// assertRuntimePartySceneRefresh validates the deliberately smaller scene
// reconstruction sequence: realtime slots/HP (op153) must precede the op9
// frame, while op11 is intentionally absent so a town transition does not
// restart the already-established peer relay.
func assertRuntimePartySceneRefresh(t *testing.T, data []byte, wantMembers byte) []byte {
	t.Helper()
	realtime, rest := splitGameServerUpperPacket(t, data)
	if realtime.Header.Classification != 0 || realtime.Header.MsgID != 0x0099 {
		t.Fatalf("scene party realtime packet = class %d msg %d", realtime.Header.Classification, realtime.Header.MsgID)
	}
	if len(realtime.Body) != 1+int(wantMembers)*5 || realtime.Body[0] != wantMembers {
		t.Fatalf("scene party realtime body = % X, want count %d", realtime.Body, wantMembers)
	}
	return assertCurrentPartyFrameProjection(t, rest, wantMembers, wantMembers > 0)
}

func assertRuntimePartyRemoved(t *testing.T, data []byte, _ uint16, wantMembers byte) []byte {
	t.Helper()
	data = assertCurrentPartyFrameProjection(t, data, wantMembers, wantMembers > 0)
	realtime, rest := splitGameServerUpperPacket(t, data)
	if realtime.Header.Classification != 0 || realtime.Header.MsgID != 0x0099 {
		t.Fatalf("party removed realtime packet = class %d msg %d", realtime.Header.Classification, realtime.Header.MsgID)
	}
	if len(realtime.Body) == 0 || realtime.Body[0] != wantMembers {
		t.Fatalf("party removed realtime = % X, want count %d", realtime.Body, wantMembers)
	}
	return rest
}

func assertRuntimePartyWalkoutNotice(t *testing.T, data []byte, slot byte, mode byte) []byte {
	t.Helper()
	packet, rest := splitGameServerUpperPacket(t, data)
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestPeer) ||
		!bytes.Equal(packet.Body, []byte{slot, mode}) {
		t.Fatalf("walkout notice = class %d msg %d body=% X, want class %d msg %d body=% X",
			packet.Header.Classification,
			packet.Header.MsgID,
			packet.Body,
			dnfproto.DefaultChannelClassification,
			dnfenum.CmdPacketRequestPeer,
			[]byte{slot, mode})
	}
	return rest
}

func assertRuntimePartyChangePosition(t *testing.T, data []byte, fromSlot byte, toSlot byte) []byte {
	t.Helper()
	packet, rest := splitGameServerUpperPacket(t, data)
	want := []byte{toSlot, 1}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketChangePartyMemberPosition) ||
		!bytes.Equal(packet.Body, want) {
		t.Fatalf("change position = class %d msg %d body=% X, want class %d msg %d body=% X",
			packet.Header.Classification,
			packet.Header.MsgID,
			packet.Body,
			dnfproto.DefaultChannelClassification,
			dnfenum.CmdPacketChangePartyMemberPosition,
			want)
	}
	return rest
}

func assertCurrentPartyFrameProjection(t *testing.T, data []byte, wantMembers byte, wantActive bool) []byte {
	t.Helper()
	frame, rest := splitGameServerUpperPacket(t, data)
	if frame.Header.Classification != 0 || frame.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) {
		t.Fatalf("party frame packet = class %d msg %d", frame.Header.Classification, frame.Header.MsgID)
	}
	if !wantActive {
		if wantMembers != 0 {
			t.Fatalf("inactive party frame requested with %d members", wantMembers)
		}
		if len(frame.Body) != 10 ||
			binary.LittleEndian.Uint16(frame.Body[0:2]) != 1 ||
			binary.LittleEndian.Uint16(frame.Body[2:4]) != currentSceneOp9StableSceneValue ||
			frame.Body[9] != currentSceneOp9ActorRemoveKind {
			t.Fatalf("inactive party frame is not a kind-3 actor clear: len=%d body=% X", len(frame.Body), frame.Body)
		}
		return rest
	}
	if len(frame.Body) < 20 {
		t.Fatalf("party frame body too short: len=%d body=% X", len(frame.Body), frame.Body)
	}
	nameLen := int(binary.LittleEndian.Uint32(frame.Body[12:16]))
	slotCountOffset := 16 + nameLen + 13
	if slotCountOffset >= len(frame.Body)-4 {
		t.Fatalf("party frame slot offset=%d exceeds body len=%d body=% X", slotCountOffset, len(frame.Body), frame.Body)
	}
	if frame.Body[slotCountOffset] != wantMembers {
		t.Fatalf("party frame slots=%d want=%d body=% X", frame.Body[slotCountOffset], wantMembers, frame.Body)
	}
	// The byte at len-3 is the selected member's zero-based slot, so the
	// leader legitimately carries zero there.  Party activity is represented
	// by the current slot-table count instead.
	active := frame.Body[slotCountOffset] > 0
	if active != wantActive {
		t.Fatalf("party frame active=%t want=%t body=% X", active, wantActive, frame.Body)
	}
	return rest
}

func assertCurrentPartyFrameSelectedSlot(t *testing.T, data []byte, want byte) {
	t.Helper()
	var frame dnfproto.ChannelPacket
	for len(data) > 0 {
		packet, rest := splitGameServerUpperPacket(t, data)
		if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketRecoverStamina) {
			frame = packet
			break
		}
		data = rest
	}
	if frame.Header.Classification != 0 || frame.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) {
		t.Fatalf("party frame packet = class %d msg %d", frame.Header.Classification, frame.Header.MsgID)
	}
	if len(frame.Body) < 4 {
		t.Fatalf("party frame body too short for selected slot: len=%d body=% X", len(frame.Body), frame.Body)
	}
	wantTail := []byte{0, want, 0, 0}
	if !bytes.Equal(frame.Body[len(frame.Body)-4:], wantTail) {
		t.Fatalf("party frame tail=% X want=% X", frame.Body[len(frame.Body)-4:], wantTail)
	}
}
