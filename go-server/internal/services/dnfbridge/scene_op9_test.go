package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildCurrentSceneOp9ActorRemovalBodyUsesKind3PrefixOnly(t *testing.T) {
	body := buildCurrentSceneOp9ActorRemovalBody(0x1234)
	want := []byte{1, 0, 0x0f, 0x27, 0x34, 0x12, 0, currentSceneObjectContext, 0, currentSceneOp9ActorRemoveKind}
	if !bytes.Equal(body, want) {
		t.Fatalf("op9 kind3 removal body=%x want=%x", body, want)
	}
}

func assertCurrentPartylessTownOp9Body(t *testing.T, body []byte, ownerChannel byte) {
	t.Helper()
	if len(body) != 10 ||
		binary.LittleEndian.Uint16(body[0:2]) != 1 ||
		binary.LittleEndian.Uint16(body[2:4]) != currentSceneOp9StableSceneValue ||
		binary.LittleEndian.Uint16(body[4:6]) == 0 ||
		body[7] != ownerChannel ||
		body[9] != currentSceneOp9ActorRemoveKind {
		t.Fatalf("partyless town op9 body=%x want kind3 owner=%02x", body, ownerChannel)
	}
}

func TestBuildCurrentSceneOp9ActorPartyDisplayCarriesCurrentSlotTable(t *testing.T) {
	body := buildCurrentSceneOp9ActorPartyDisplayBodyInContext(
		1001,
		dnfrepo.CharacterRecord{Name: "leader", Job: "1", Level: 90},
		true,
		"",
		currentSceneObjectContext,
		38,
		[]currentSceneOp9PartyMemberProjection{
			{State: alignedcmd.PartyMemberState{UserID: 1001, UserState: 1}, Name: "leader", Job: 1, Level: 90},
			{State: alignedcmd.PartyMemberState{UserID: 1002, UserState: 2}, Name: "member", Job: 2, Level: 80, Grow: 1},
		},
		1,
	)
	outerName := rosterNameBytes("leader")
	offset := 16 + len(outerName) + 13
	if body[offset] != 2 {
		t.Fatalf("op9 party slot count=%d want=2 body=% X", body[offset], body)
	}
	offset++
	for i, want := range []struct {
		id    uint16
		name  string
		job   byte
		level byte
		grow  byte
		state byte
	}{
		{id: 1001, name: "leader", job: 1, level: 90, state: 1},
		{id: 1002, name: "member", job: 2, level: 80, grow: 1, state: 2},
	} {
		if body[offset] != byte(i) || binary.LittleEndian.Uint16(body[offset+1:offset+3]) != want.id {
			t.Fatalf("op9 party member[%d] header=% X want slot=%d id=%d", i, body[offset:offset+3], i, want.id)
		}
		offset += 3
		if !bytes.Equal(body[offset:offset+4], []byte{want.job, want.level, want.grow, 0}) {
			t.Fatalf("op9 party member[%d] identity=% X", i, body[offset:offset+4])
		}
		offset += 4
		nameLen := int(binary.LittleEndian.Uint32(body[offset : offset+4]))
		offset += 4
		name := rosterNameBytes(want.name)
		if nameLen != len(name) || !bytes.Equal(body[offset:offset+nameLen], name) {
			t.Fatalf("op9 party member[%d] name len=%d bytes=% X want=% X", i, nameLen, body[offset:offset+nameLen], name)
		}
		offset += nameLen
		if !bytes.Equal(body[offset:offset+3], []byte{0, want.state, 0}) {
			t.Fatalf("op9 party member[%d] state=% X want=% X", i, body[offset:offset+3], []byte{0, want.state, 0})
		}
		offset += 3
	}
	if !bytes.Equal(body[offset:], []byte{0, 1, 0, 0}) {
		t.Fatalf("op9 party post-slot fields=% X want selected member slot 1", body[offset:])
	}
}

func TestBuildCurrentSceneOp9ActorPartyDisplaySelectsSoloMemberSlotZero(t *testing.T) {
	body := buildCurrentSceneOp9ActorPartyDisplayBodyInContext(
		1001,
		dnfrepo.CharacterRecord{Name: "leader", Job: "1", Level: 90},
		true,
		"",
		currentSceneObjectContext,
		38,
		[]currentSceneOp9PartyMemberProjection{
			{State: alignedcmd.PartyMemberState{UserID: 1001, UserState: 1}, Name: "leader", Job: 1, Level: 90},
		},
		0,
	)
	if !bytes.Equal(body[len(body)-4:], []byte{0, 0, 0, 0}) {
		t.Fatalf("op9 solo party post-slot fields=% X want selected member slot 0", body[len(body)-4:])
	}
}

func TestCurrentSceneOp9SelectedPartySlotRequiresLocalMember(t *testing.T) {
	members := []currentSceneOp9PartyMemberProjection{
		{State: alignedcmd.PartyMemberState{UserID: 1001}},
	}
	if got, ok := currentSceneOp9SelectedPartySlot(members, 1002); ok || got != 0 {
		t.Fatalf("op9 missing selected member slot=%02x ok=%v want 00 false", got, ok)
	}
}

func TestBuildCurrentSceneOp9ActorDisplayKeepsInlineIdentityWithNonzeroOuterOwner(t *testing.T) {
	const owner = byte(0xce)
	body := buildCurrentSceneOp9ActorDisplayBodyInContext(
		0x1234,
		dnfrepo.CharacterRecord{Name: "hero", Job: "11", Level: 90},
		true,
		"",
		owner,
	)
	if len(body) < 18 ||
		body[7] != owner ||
		body[10] != currentSceneObjectRoute ||
		body[11] != currentSceneObjectContext {
		t.Fatalf("op9 owner/identity header=%x want outer=%02x nested=00", body[:minInt(len(body), 12)], owner)
	}
	name := rosterNameBytes("hero")
	nameLength := int(binary.LittleEndian.Uint32(body[12:16]))
	if nameLength != len(name) ||
		len(body) < 16+nameLength+2 ||
		!bytes.Equal(body[16:16+nameLength], name) {
		t.Fatalf("op9 inline name length=%d body=%x want=%x", nameLength, body, name)
	}
	tail := body[16+nameLength:]
	if tail[0] != 11 || tail[1] != 90 {
		t.Fatalf("op9 inline identity tail=%x want job=11 level=90", tail)
	}
}

func TestSendCurrentSceneOp9PreviewActorRemovalOnceClearsPreviewState(t *testing.T) {
	conn := &bufferConn{}
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	session := &gameSession{
		conn:                         conn,
		connID:                       "op9-preview-remove",
		selectedCharacterID:          0x1234,
		selectPreviewObjectStateSent: true,
		currentSceneObjectListSent:   true,
	}
	if err := service.sendCurrentSceneOp9PreviewActorRemovalOnce(session, "test_before_op27"); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.Classification != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) ||
		!bytes.Equal(packet.Body, buildCurrentSceneOp9ActorRemovalBody(0x1234)) || len(rest) != 0 {
		t.Fatalf("op9 removal packet=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
	if !session.selectPreviewActorRemoved || session.currentSceneObjectListSent {
		t.Fatalf("preview removal flags removed=%v object_list=%v", session.selectPreviewActorRemoved, session.currentSceneObjectListSent)
	}
	writes := conn.write.Len()
	if err := service.sendCurrentSceneOp9PreviewActorRemovalOnce(session, "test_duplicate"); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != writes {
		t.Fatalf("duplicate op9 removal wrote %d extra bytes", conn.write.Len()-writes)
	}
}
