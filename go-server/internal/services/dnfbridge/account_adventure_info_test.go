package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestBuildCurrentAdventureInfoBodyProjectsShopAndGrowthRuntimeState(t *testing.T) {
	projection := adventuregroup.Projection{
		Runtime: adventuregroup.RuntimeState{
			ShopPoints:       [adventuregroup.ShopPointTypeCount]uint32{7, 11, 13},
			GrowthExperience: 123456,
			Purchases: map[string]uint32{
				"0:10304122": 2,
				"1:10303899": 3,
			},
		},
	}
	body := buildCurrentAdventureInfoBodyWithState(
		1,
		adventuregroup.Summary{},
		nil,
		"",
		0,
		projection,
	)
	raw := body[18 : 18+currentAdventureInfoRawLength]
	for index, want := range projection.Runtime.ShopPoints {
		offset := currentAdventureInfoShopPointOffset + index*currentAdventureInfoShopPointEntrySize
		if key := binary.LittleEndian.Uint32(raw[offset : offset+4]); key != uint32(index) {
			t.Fatalf("shop key[%d]=%d", index, key)
		}
		if got := binary.LittleEndian.Uint32(raw[offset+4 : offset+8]); got != want {
			t.Fatalf("shop points[%d]=%d want=%d", index, got, want)
		}
	}
	first := raw[currentAdventureInfoPurchaseOffset : currentAdventureInfoPurchaseOffset+currentAdventureInfoPurchaseEntrySize]
	second := raw[currentAdventureInfoPurchaseOffset+currentAdventureInfoPurchaseEntrySize : currentAdventureInfoPurchaseOffset+2*currentAdventureInfoPurchaseEntrySize]
	if binary.LittleEndian.Uint32(first[:4]) != 10304122 || binary.LittleEndian.Uint32(first[4:]) != 2 ||
		binary.LittleEndian.Uint32(second[:4]) != 10303899 || binary.LittleEndian.Uint32(second[4:]) != 3 {
		t.Fatalf("purchase projection first=%x second=%x", first, second)
	}
	if got := binary.LittleEndian.Uint64(raw[currentAdventureInfoGrowthCapsuleOffset : currentAdventureInfoGrowthCapsuleOffset+8]); got != 123456 {
		t.Fatalf("growth=%d", got)
	}
}

func TestBuildCurrentAdventureInfoBodyMatchesCurrentExeLayout(t *testing.T) {
	const (
		characterID = uint16(0x1234)
		totalPoint  = uint64(0x0102030405060708)
	)
	characters := []dnfrepo.CharacterRecord{{
		CharacterID: "20",
		Name:        "冒险家",
		Job:         "11",
		Level:       90,
		Stats:       map[string]int64{"grow_type": 2},
	}}
	body := buildCurrentAdventureInfoBody(characterID, adventuregroup.Summary{TotalPoint: totalPoint, ManageLevel: 4}, characters)
	if len(body) != 7442 {
		t.Fatalf("body length = %d, want 7442", len(body))
	}
	if got := binary.LittleEndian.Uint16(body[0:2]); got != characterID {
		t.Fatalf("character id = %#x, want %#x", got, characterID)
	}
	for index, value := range body[2:14] {
		if value != 0 {
			t.Fatalf("neutral top-level byte %d = %#x", index+2, value)
		}
	}
	if got := binary.LittleEndian.Uint32(body[14:18]); got != currentAdventureInfoRawLength {
		t.Fatalf("raw length = %d, want %d", got, currentAdventureInfoRawLength)
	}
	raw := body[18 : 18+currentAdventureInfoRawLength]
	if got := binary.LittleEndian.Uint32(raw[0:4]); got != 4 {
		t.Fatalf("manage level = %d, want 4", got)
	}
	if got := binary.LittleEndian.Uint64(raw[8:16]); got != totalPoint {
		t.Fatalf("current point = %#x, want %#x", got, totalPoint)
	}
	if got := binary.LittleEndian.Uint16(raw[currentAdventureInfoCharacterCountOffset : currentAdventureInfoCharacterCountOffset+2]); got != 1 {
		t.Fatalf("total character count = %d, want 1", got)
	}
	entry := raw[currentAdventureInfoRosterOffset : currentAdventureInfoRosterOffset+currentAdventureInfoRosterEntrySize]
	if entry[0] != 11 || entry[1] != 2 || entry[currentAdventureInfoRosterLevelOffset] != 90 {
		t.Fatalf(
			"real roster job/grow/detail level = %d/%d/%d",
			entry[0],
			entry[1],
			entry[currentAdventureInfoRosterLevelOffset],
		)
	}
	if got := binary.LittleEndian.Uint32(
		entry[currentAdventureInfoRosterCardLevelOffset : currentAdventureInfoRosterCardLevelOffset+4],
	); got != 90 {
		t.Fatalf("collection-card level = %d, want 90", got)
	}
	if got := binary.LittleEndian.Uint32(entry[4:8]); got != 20 {
		t.Fatalf("real roster character id = %d, want 20", got)
	}
	wantName := rosterRawNameBytes(characters[0])
	if !bytes.Equal(entry[12:12+len(wantName)], wantName) {
		t.Fatalf("real roster name = %x, want %x", entry[12:12+len(wantName)], wantName)
	}
	for index := 0; index < currentAdventureInfoTripleCount; index++ {
		offset := currentAdventureInfoTripleOffset + index*currentAdventureInfoTripleSize
		if !bytes.Equal(raw[offset:offset+3], []byte{0, 0, byte(index)}) {
			t.Fatalf("triple %d = %x", index, raw[offset:offset+3])
		}
	}
	if got := binary.LittleEndian.Uint64(raw[currentAdventureInfoGrowthCapsuleOffset : currentAdventureInfoGrowthCapsuleOffset+8]); got != 0 {
		t.Fatalf("unowned growth-capsule progress = %#x, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(raw[7416:7420]); got != 0 {
		t.Fatalf("unproved independent tail = %#x, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(body[len(body)-4:]); got != 0 {
		t.Fatalf("display name length = %d, want 0", got)
	}
}

func TestBuildCurrentAdventureInfoBodyWritesOwnedActivityFields(t *testing.T) {
	projection := adventuregroup.Projection{ConsecutiveLoginDays: 24}
	projection.ContentCounts[adventuregroup.ContentTypeRecommendedDungeon] = 7
	body := buildCurrentAdventureInfoBodyWithState(
		20,
		adventuregroup.Summary{TotalPoint: 23310, ManageLevel: 4},
		nil,
		"group",
		20260716,
		projection,
	)
	raw := body[18 : 18+currentAdventureInfoRawLength]
	if got := binary.LittleEndian.Uint32(
		raw[currentAdventureInfoLoginDaysOffset : currentAdventureInfoLoginDaysOffset+4],
	); got != 24 {
		t.Fatalf("consecutive login days=%d, want 24", got)
	}
	for index := 0; index < currentAdventureInfoTripleCount; index++ {
		offset := currentAdventureInfoTripleOffset + index*currentAdventureInfoTripleSize
		if raw[offset+currentAdventureInfoTripleTypeOffset] != byte(index) {
			t.Fatalf("content type %d wire type=%d", index, raw[offset+currentAdventureInfoTripleTypeOffset])
		}
		want := uint16(0)
		if index == adventuregroup.ContentTypeRecommendedDungeon {
			want = 7
		}
		if got := binary.LittleEndian.Uint16(
			raw[offset+currentAdventureInfoTripleCountOffset : offset+currentAdventureInfoTripleCountOffset+2],
		); got != want {
			t.Fatalf("content type %d count=%d, want %d", index, got, want)
		}
	}
}

func TestBuildCurrentAdventureActorRefreshBodyMatchesCurrentExeLayout(t *testing.T) {
	const (
		currentObjectKey = uint16(0x1234)
		totalPoint       = uint64(0x0102030405060708)
	)
	body := buildCurrentAdventureActorRefreshBody(
		currentObjectKey,
		adventuregroup.Summary{TotalPoint: totalPoint, ManageLevel: 4},
	)
	if len(body) != 26 {
		t.Fatalf("body length = %d, want 26", len(body))
	}
	if got := binary.LittleEndian.Uint16(body[0:2]); got != currentObjectKey {
		t.Fatalf("current object key = %#x, want %#x", got, currentObjectKey)
	}
	if got := binary.LittleEndian.Uint32(body[2:6]); got != currentAdventureActorRefreshRawLength {
		t.Fatalf("raw state length = %d, want %d", got, currentAdventureActorRefreshRawLength)
	}
	raw := body[6 : 6+currentAdventureActorRefreshRawLength]
	if got := binary.LittleEndian.Uint32(raw[0:4]); got != 4 {
		t.Fatalf("manage level = %d, want 4", got)
	}
	if got := binary.LittleEndian.Uint32(raw[4:8]); got != 0 {
		t.Fatalf("reserved state = %#x, want 0", got)
	}
	if got := binary.LittleEndian.Uint64(raw[8:16]); got != totalPoint {
		t.Fatalf("current point = %#x, want %#x", got, totalPoint)
	}
	if !bytes.Equal(body[22:26], make([]byte, 4)) {
		t.Fatalf("actor refresh flags = %x, want zero", body[22:26])
	}
}

func TestBuildCurrentAdventureInfoBodyWritesPersistedCreationDate(t *testing.T) {
	body := buildCurrentAdventureInfoBodyWithIdentity(
		0x1234,
		adventuregroup.Summary{TotalPoint: 23310, ManageLevel: 4},
		nil,
		"group",
		20260716,
	)
	if got := binary.LittleEndian.Uint32(body[2:6]); got != 20260716 {
		t.Fatalf("adventure-group created date = %d, want 20260716", got)
	}
	if got := binary.LittleEndian.Uint32(body[14:18]); got != currentAdventureInfoRawLength {
		t.Fatalf("raw length shifted by creation date = %d, want %d", got, currentAdventureInfoRawLength)
	}
}

func TestCurrentAdventureInfoRosterNameKeepsFixedFieldNULTerminated(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{{
		CharacterID: "20",
		Name:        "一二三四五六七八九十甲乙丙丁戊",
		Job:         "11",
		Level:       90,
	}}
	body := buildCurrentAdventureInfoBody(20, adventuregroup.Summary{TotalPoint: 23310, ManageLevel: 4}, characters)
	raw := body[18 : 18+currentAdventureInfoRawLength]
	entry := raw[currentAdventureInfoRosterOffset : currentAdventureInfoRosterOffset+currentAdventureInfoRosterEntrySize]
	name := entry[currentAdventureInfoRosterNameOffset : currentAdventureInfoRosterNameOffset+currentAdventureInfoRosterNameSize]
	if name[len(name)-1] != 0 {
		t.Fatalf("fixed roster name is not NUL terminated: %x", name)
	}
	if got := bytes.IndexByte(name, 0); got != 28 {
		t.Fatalf("GB18030 roster name terminator offset = %d, want 28: %x", got, name)
	}
}

func TestUpperGetUserInfoDefersSingleRememberedSelectorAdventureInfoUntilHiddenProbe(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID:            "dnf:1",
		State:                "active",
		RepresentAccountName: "group",
		Metadata: map[string]string{
			adventureGroupCreatedDateMetadataKey:     "2026-07-16",
			currentSelectorAdventureInfoSlotMetadata: "1",
		},
	}); err != nil {
		t.Fatal(err)
	}
	for _, character := range []dnfrepo.CharacterRecord{
		{CharacterID: "19", AccountID: "dnf:1", Slot: 0, Name: "one", Job: "0", Level: 1},
		{CharacterID: "20", AccountID: "dnf:1", Slot: 1, Name: "two", Job: "1", Level: 2},
	} {
		if err := repositories.Character.Save(ctx, character); err != nil {
			t.Fatal(err)
		}
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{
		conn:    connection,
		connID:  "game-adventure-info-push-test",
		channel: channelcatalog.Channel{ID: 19},
	}
	if err := service.sendUpperGetUserInfoBootstrap(session); err != nil {
		t.Fatal(err)
	}

	roster, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if roster.Header.Classification != 0 || roster.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) ||
		len(roster.Body) == 0 || roster.Body[0] != 2 {
		t.Fatalf("first packet is not upper mode2 roster: header=%+v body_prefix=%x", roster.Header, roster.Body[:minInt(len(roster.Body), 4)])
	}
	if len(rest) != 0 {
		t.Fatalf("GET_USERINFO emitted %d unsafe trailing bytes", len(rest))
	}
	if session.pendingCharacterRosterBootstrap {
		t.Fatal("GET_USERINFO must not defer roster behind hidden-character probe")
	}
	if !session.selectorAdventureInfoPending || session.selectorAdventureInfoSlot != 1 {
		t.Fatalf("selector adventure pending=%t slot=%d, want true/1", session.selectorAdventureInfoPending, session.selectorAdventureInfoSlot)
	}

	connection.write.Reset()
	hiddenFrame, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgCharacViewHiddenInfo),
		nil,
		7,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, hiddenFrame); err != nil {
		t.Fatal(err)
	}
	hiddenAck, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if hiddenAck.Header.MsgID != uint16(dnfenum.UpperMsgRebirthHardcoreCharac) ||
		hiddenAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(hiddenAck.Body, []byte{1}) {
		t.Fatalf("hidden-info ACK header=%+v body=%x", hiddenAck.Header, hiddenAck.Body)
	}
	selectorPacket, rest := splitLongHengGameServerUpperPacket(t, rest)
	if selectorPacket.Header.Classification != 0 || selectorPacket.Header.MsgID != currentAdventureInfoPushMsgID {
		t.Fatalf("selector adventure header=%+v", selectorPacket.Header)
	}
	if len(selectorPacket.Body) < 18+currentAdventureInfoRawLength {
		t.Fatalf("selector adventure body length=%d", len(selectorPacket.Body))
	}
	if got := binary.LittleEndian.Uint16(selectorPacket.Body[0:2]); got != 1 {
		t.Fatalf("selector adventure key=%d, want remembered slot 1", got)
	}
	if got := binary.LittleEndian.Uint32(selectorPacket.Body[2:6]); got != 20260716 {
		t.Fatalf("selector adventure created date=%d", got)
	}
	raw := selectorPacket.Body[18 : 18+currentAdventureInfoRawLength]
	if got := binary.LittleEndian.Uint16(raw[currentAdventureInfoCharacterCountOffset : currentAdventureInfoCharacterCountOffset+2]); got != 2 {
		t.Fatalf("selector adventure roster count=%d", got)
	}
	if len(rest) != 0 {
		t.Fatalf("hidden probe emitted %d unexpected trailing bytes", len(rest))
	}
	if session.selectorAdventureInfoPending {
		t.Fatal("selector adventure push must be one-shot")
	}

	connection.write.Reset()
	if err := service.handleGameUpper(session, hiddenFrame); err != nil {
		t.Fatal(err)
	}
	_, rest = splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("repeated hidden probe replayed selector adventure bytes: %x", rest)
	}
}

func TestPersistCurrentSelectorAdventureInfoSlotPreservesAccountState(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID:            "dnf:1",
		State:                "active",
		HonorExp:             77,
		RepresentAccountName: "group",
		Metadata: map[string]string{
			adventureGroupCreatedDateMetadataKey: "2026-07-16",
			"account_cera":                       "123",
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options: options{accountID: "dnf:1"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	service.persistCurrentSelectorAdventureInfoSlot(ctx, &gameSession{}, 4)

	account, found, err := repositories.Account.Load(ctx, "dnf:1")
	if err != nil || !found {
		t.Fatalf("load account found=%t err=%v", found, err)
	}
	if account.Metadata[currentSelectorAdventureInfoSlotMetadata] != "4" {
		t.Fatalf("remembered selector slot=%q, want 4", account.Metadata[currentSelectorAdventureInfoSlotMetadata])
	}
	if account.Metadata["account_cera"] != "123" || account.HonorExp != 77 ||
		account.RepresentAccountName != "group" {
		t.Fatalf("unrelated account state changed: %+v", account)
	}
}

func TestHandleCurrentRequestAdventureInfoSendsRealAccountSummary(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "dnf:1",
		State:     "active",
		Metadata:  map[string]string{adventureGroupCreatedDateMetadataKey: "2026-07-16"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "20",
		AccountID:   "dnf:1",
		Slot:        0,
		Level:       2,
		Stats: map[string]int64{
			adventuregroup.RecommendedDungeonClearStatKey: 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{
		conn:                connection,
		connID:              "game-adventure-info-test",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 20,
	}
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestAdventureInfo),
		make([]byte, currentAdventureInfoRequestWireLength),
	); err != nil {
		t.Fatal(err)
	}

	packet, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("trailing packet bytes = %d", len(rest))
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestAdventureInfo) {
		t.Fatalf("header = %+v", packet.Header)
	}
	if len(packet.Body) != 7443 || packet.Body[0] != 1 {
		t.Fatalf("success body length/prefix = %d/%x", len(packet.Body), packet.Body[:1])
	}
	payload := packet.Body[1:]
	if got := binary.LittleEndian.Uint16(payload[0:2]); got != 20 {
		t.Fatalf("response character id = %d, want 20", got)
	}
	if got := binary.LittleEndian.Uint32(payload[2:6]); got != 20260716 {
		t.Fatalf("response adventure created date = %d, want 20260716", got)
	}
	raw := payload[18 : 18+currentAdventureInfoRawLength]
	if got := binary.LittleEndian.Uint64(raw[currentAdventureInfoCurrentPointOffset : currentAdventureInfoCurrentPointOffset+8]); got != 30 {
		t.Fatalf("real account total point = %d, want 30", got)
	}
	if got := binary.LittleEndian.Uint32(raw[currentAdventureInfoManageLevelOffset : currentAdventureInfoManageLevelOffset+4]); got != 1 {
		t.Fatalf("real account manage level = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(raw[currentAdventureInfoLoginDaysOffset : currentAdventureInfoLoginDaysOffset+4]); got != 1 {
		t.Fatalf("real account consecutive login days = %d, want 1", got)
	}
	recommendedOffset := currentAdventureInfoTripleOffset +
		adventuregroup.ContentTypeRecommendedDungeon*currentAdventureInfoTripleSize
	if got := binary.LittleEndian.Uint16(
		raw[recommendedOffset+currentAdventureInfoTripleCountOffset : recommendedOffset+currentAdventureInfoTripleCountOffset+2],
	); got != 3 {
		t.Fatalf("real account recommended dungeon clears = %d, want 3", got)
	}
	if got := binary.LittleEndian.Uint64(raw[currentAdventureInfoGrowthCapsuleOffset : currentAdventureInfoGrowthCapsuleOffset+8]); got != 0 {
		t.Fatalf("growth-capsule progress = %d, want 0", got)
	}
}

func TestHandleCurrentRequestAdventureInfoAcceptsSceneIconRequest(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "dnf:1",
		State:     "active",
		Metadata:  map[string]string{adventureGroupCreatedDateMetadataKey: "2026-07-16"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Slot:        0,
		Level:       90,
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{
		conn:                connection,
		connID:              "game-adventure-info-scene-icon-test",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}
	request := make([]byte, currentAdventureInfoSceneRequestLength)
	binary.LittleEndian.PutUint16(request, currentSceneActorObjectKey(session.selectedCharacterID))
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestAdventureInfo),
		request,
	); err != nil {
		t.Fatal(err)
	}

	packet, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("trailing packet bytes = %d", len(rest))
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestAdventureInfo) {
		t.Fatalf("header = %+v", packet.Header)
	}
	if len(packet.Body) <= 1 || packet.Body[0] != 1 {
		t.Fatalf("success body length/prefix = %d/%x", len(packet.Body), packet.Body[:minInt(len(packet.Body), 1)])
	}
	payload := packet.Body[1:]
	if got := binary.LittleEndian.Uint16(payload[0:2]); got != 19 {
		t.Fatalf("response character id = %d, want 19", got)
	}
}

func TestHandleCurrentRequestAdventureInfoAcceptsVisiblePeerSceneIcon(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	for _, account := range []dnfrepo.AccountRecord{
		{AccountID: "dnf:1", State: "active", Metadata: map[string]string{adventureGroupCreatedDateMetadataKey: "2026-07-16"}},
		{AccountID: "dnf:2", State: "active", RepresentAccountName: "peer-group", Metadata: map[string]string{adventureGroupCreatedDateMetadataKey: "2026-07-17"}},
	} {
		if err := repositories.Account.Save(ctx, account); err != nil {
			t.Fatal(err)
		}
	}
	for _, character := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "dnf:1", Name: "source", Level: 90},
		{CharacterID: "5", AccountID: "dnf:2", Name: "target", Level: 2},
	} {
		if err := repositories.Character.Save(ctx, character); err != nil {
			t.Fatal(err)
		}
	}
	sourceConn := &bufferConn{}
	source := &gameSession{conn: sourceConn, accountID: "dnf:1", selectedCharacterID: 1}
	target := &gameSession{conn: &bufferConn{}, accountID: "dnf:2", selectedCharacterID: 5}
	service := &Service{
		options:             options{gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:       newOnlinePlayerManager(),
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	service.bindGameSessionCharacter(source, 1)
	service.bindGameSessionCharacter(target, 5)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1, TownID: 1, AreaID: 2, Session: source})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 5, TownID: 1, AreaID: 2, Session: target})

	if err := service.handleGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketRequestAdventureInfo),
		[]byte{5, 0},
	); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitLongHengGameServerUpperPacket(t, sourceConn.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestAdventureInfo) || len(rest) != 0 ||
		len(packet.Body) < 7443 || packet.Body[0] != 1 {
		t.Fatalf("peer adventure header=%+v body_len=%d trailing=%d", packet.Header, len(packet.Body), len(rest))
	}
	payload := packet.Body[1:]
	if binary.LittleEndian.Uint16(payload[:2]) != 5 || binary.LittleEndian.Uint32(payload[2:6]) != 20260717 {
		t.Fatalf("peer adventure identity/date=%x", payload[:6])
	}
}

func TestHandleCurrentRequestAdventureInfoRejectsInvalidOwnershipAndShape(t *testing.T) {
	tests := []struct {
		name       string
		selectedID uint16
		body       []byte
		bodyLength int
		accountID  string
	}{
		{name: "no selected character", bodyLength: currentAdventureInfoRequestWireLength, accountID: "dnf:1"},
		{name: "wrong protected body length", selectedID: 20, bodyLength: 3, accountID: "dnf:1"},
		{name: "wrong scene icon character", selectedID: 20, body: []byte{19, 0}, accountID: "dnf:1"},
		{name: "owner mismatch", selectedID: 20, bodyLength: currentAdventureInfoRequestWireLength, accountID: "dnf:2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositories := dnfrepomemory.NewMemoryGroup()
			if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{AccountID: "dnf:1"}); err != nil {
				t.Fatal(err)
			}
			if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{CharacterID: "20", AccountID: test.accountID, Level: 1}); err != nil {
				t.Fatal(err)
			}
			connection := &bufferConn{}
			service := &Service{
				options:             options{accountID: "dnf:1", gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain},
				adventureGroupTable: loadAdventureGroupTestTables(t),
				repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
			}
			session := &gameSession{conn: connection, selectedCharacterID: test.selectedID}
			body := test.body
			if body == nil {
				body = make([]byte, test.bodyLength)
			}
			if err := service.handleGameCommand(
				session,
				byte(dnfenum.GameCmdCommand),
				uint16(dnfenum.CmdPacketRequestAdventureInfo),
				body,
			); err != nil {
				t.Fatal(err)
			}
			if connection.write.Len() != 0 {
				t.Fatalf("rejected request wrote %d bytes", connection.write.Len())
			}
		})
	}
}
