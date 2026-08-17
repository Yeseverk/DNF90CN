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
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentSetQuestTriggerPersistsBeforeAckAndSuppressesNoopReplay(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19", AccountID: defaultAccountPrefix + "1", Job: "11", Level: 90,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			4728: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn: connection, connID: "set-quest-trigger-test",
		channel: channelcatalog.Channel{ID: 19}, selectedCharacterID: 19,
	}
	body := []byte{0x21, 0x00, 0x78, 0x12, 0x00, 0x00}
	if err := service.handleCurrentSetQuestTrigger(session, body); err != nil {
		t.Fatal(err)
	}
	stored, ok, err := repositories.Quest.Load(context.Background(), "19")
	if err != nil || !ok {
		t.Fatalf("load persisted quest ok=%t err=%v", ok, err)
	}
	if got := stored.States[4728].ProgressValue; got != 0 {
		t.Fatalf("persisted trigger=%d want=0", got)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketSetQuestTrigger) ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification {
		t.Fatalf("ack header=%+v", ack.Header)
	}
	if want := []byte{0x01, 0x78, 0x12, 0x00, 0x00, 0x00, 0x00}; !bytes.Equal(ack.Body, want) {
		t.Fatalf("ack body=%x want=%x", ack.Body, want)
	}
	active, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || active.Header.MsgID != currentActiveQuestSnapshotMsgID || active.Header.Classification != 0 {
		t.Fatalf("active header=%+v trailing=%x", active.Header, trailing)
	}
	if want := []byte{0x01, 0x00, 0x00, 0x00, 0x78, 0x12, 0x00, 0x00, 0x00, 0x00}; !bytes.Equal(active.Body, want) {
		t.Fatalf("active body=%x want=%x", active.Body, want)
	}

	connection.write.Reset()
	if err := service.handleCurrentSetQuestTrigger(session, body); err != nil {
		t.Fatal(err)
	}
	if got := connection.write.Bytes(); len(got) != 0 {
		t.Fatalf("same-session replay wrote packets=%x", got)
	}

	// A fresh session may be the first op33 after clear-map persistence already
	// reached active zero. It receives the one transition ACK, but no op574.
	newConnection := &bufferConn{}
	newSession := &gameSession{
		conn: newConnection, connID: "set-quest-trigger-reload-test",
		channel: channelcatalog.Channel{ID: 19}, selectedCharacterID: 19,
	}
	if err := service.handleCurrentSetQuestTrigger(newSession, body); err != nil {
		t.Fatal(err)
	}
	replayedACK, replayedTrailing := splitGameServerUpperPacket(t, newConnection.write.Bytes())
	if len(replayedTrailing) != 0 || replayedACK.Header.MsgID != uint16(dnfenum.CmdPacketSetQuestTrigger) ||
		!bytes.Equal(replayedACK.Body, []byte{0x01, 0x78, 0x12, 0x00, 0x00, 0x00, 0x00}) {
		t.Fatalf("persisted active-zero transition ACK=%+v trailing=%x", replayedACK, replayedTrailing)
	}
}

func TestCurrentSetQuestTriggerPublishesPackedChannelsWithoutFeedbackPollution(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "20", AccountID: defaultAccountPrefix + "1", Job: "11", Level: 90,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "20",
		States: map[int64]dnfrepo.QuestState{
			3193: {Status: "active", ProgressValue: 513},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn: connection, connID: "set-packed-quest-trigger-test",
		channel: channelcatalog.Channel{ID: 20}, selectedCharacterID: 20,
	}

	for _, step := range []struct {
		triggerType byte
		want        uint32
	}{
		{triggerType: 0x10, want: 512},
		{triggerType: 0x20, want: 0},
	} {
		before := connection.write.Len()
		body := []byte{0x21, 0x00, 0x79, 0x0c, step.triggerType, 0x00}
		if err := service.handleCurrentSetQuestTrigger(session, body); err != nil {
			t.Fatal(err)
		}
		ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes()[before:])
		if ack.Header.MsgID != uint16(dnfenum.CmdPacketSetQuestTrigger) || binary.LittleEndian.Uint32(ack.Body[3:7]) != step.want {
			t.Fatalf("type=%#x ACK header=%+v body=%x want trigger=%d", step.triggerType, ack.Header, ack.Body, step.want)
		}
		active, trailing := splitGameServerUpperPacket(t, rest)
		if len(trailing) != 0 || active.Header.MsgID != currentActiveQuestSnapshotMsgID ||
			binary.LittleEndian.Uint32(active.Body[6:10]) != step.want {
			t.Fatalf("type=%#x active header=%+v body=%x trailing=%x want trigger=%d", step.triggerType, active.Header, active.Body, trailing, step.want)
		}
	}
}

func TestNormalizeLegacySetQuestTriggerStripsOnlyExactCloneTrailer(t *testing.T) {
	semantic := []byte{0x21, 0x00, 0x78, 0x12, 0x00, 0x00}
	wrapped := append(append([]byte(nil), semantic...), 0xaa, 0xbb, 0xcc, 0xdd)
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSetQuestTrigger), wrapped); !bytes.Equal(got, semantic) {
		t.Fatalf("normalized=%x want=%x", got, semantic)
	}
	opaque := append(append([]byte(nil), semantic...), 1, 2, 3, 4, 5)
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSetQuestTrigger), opaque); !bytes.Equal(got, opaque) {
		t.Fatalf("non-exact body was changed: %x", got)
	}
}

func TestNormalizeLegacySetQuestTriggerStripsObservedZeroTail(t *testing.T) {
	// Live current-client legacy op33 body: u16 opcode echo | u16 quest |
	// u8 trigger channel | u8 increment | u8 zero tail. Only the fully
	// matched wrapper is normalized; a same-sized opaque body remains
	// untouched.
	wrapped := []byte{0x21, 0x00, 0x7b, 0x0d, 0x10, 0x00, 0x00}
	want := []byte{0x21, 0x00, 0x7b, 0x0d, 0x10, 0x00}
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSetQuestTrigger), wrapped); !bytes.Equal(got, want) {
		t.Fatalf("normalized=%x want=%x", got, want)
	}
	opaque := []byte{0x20, 0x00, 0x7b, 0x0d, 0x10, 0x00, 0x00}
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSetQuestTrigger), opaque); !bytes.Equal(got, opaque) {
		t.Fatalf("non-matching wrapped body was changed: %x", got)
	}
}
