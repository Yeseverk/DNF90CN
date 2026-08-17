package dnfbridge

import (
	"context"
	"encoding/hex"
	"testing"

	dnfenum "longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestParseCurrentSoloTeleportRequestLiveSamples(t *testing.T) {
	tests := []struct {
		hex  string
		x, y int16
	}{
		{hex: "ffffffffffffffff28049d014b0105", x: 413, y: 331},
		{hex: "ffffffffffffffff28040c010f0105", x: 268, y: 271},
		{hex: "ffffffffffffffff28042101390105", x: 289, y: 313},
		{hex: "ffffffffffffffff2804c701510105", x: 455, y: 337},
		{hex: "ffffffffffffffff2804c7013f0105", x: 455, y: 319},
	}
	for _, test := range tests {
		body, err := hex.DecodeString(test.hex)
		if err != nil {
			t.Fatal(err)
		}
		request, err := parseCurrentSoloTeleportRequest(body)
		if err != nil {
			t.Fatal(err)
		}
		if request.OpaqueI32A != -1 || request.OpaqueI32B != -1 ||
			request.TownID != 40 || request.AreaID != 4 ||
			request.PositionX != test.x || request.PositionY != test.y ||
			request.Direction != 5 {
			t.Fatalf("sample %s parsed=%+v", test.hex, request)
		}
	}
}

func TestParseCurrentSoloTeleportRequestRejectsWrongBoundary(t *testing.T) {
	for _, size := range []int{
		0,
		currentSoloTeleportRequestWireSize - 1,
		currentSoloTeleportRequestWireSize + 1,
	} {
		if _, err := parseCurrentSoloTeleportRequest(make([]byte, size)); err == nil {
			t.Fatalf("size %d unexpectedly accepted", size)
		}
	}
}

func TestHandleCurrentSoloTeleportCommitsThroughOp24WithoutOp470Ack(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	body, err := hex.DecodeString("ffffffffffffffff26008403fa0005")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleCurrentSoloTeleport(session, body); err != nil {
		t.Fatal(err)
	}
	if session.conn.(*bufferConn).write.Len() == 0 {
		stored, found, loadErr := repositories.Character.Load(context.Background(), "29")
		t.Fatalf("solo teleport wrote no transition; found=%t err=%v stats=%+v", found, loadErr, stored.Stats)
	}

	packet, rest := splitTownTransitionAndPostState(
		t,
		session.conn.(*bufferConn).write.Bytes(),
		session,
		29,
		38,
		0,
		900,
		250,
		5,
		3,
		townMoveSkillProjectionBody(t, repositories, "29"),
		false,
	)
	if packet.Header.MsgID != currentSceneTransitionMsgID ||
		packet.Header.Classification != 0 || len(rest) != 0 {
		t.Fatalf(
			"solo teleport response header=%+v body=%x trailing=%x",
			packet.Header,
			packet.Body,
			rest,
		)
	}
	if packet.Header.MsgID == uint16(dnfenum.CmdPacketSoloTelepoart) {
		t.Fatalf("solo teleport emitted failure-callback op470 body=%x", packet.Body)
	}
	stored, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found ||
		stored.Stats["town_id"] != 38 ||
		stored.Stats["area_id"] != 0 ||
		stored.Stats["pos_x"] != 900 ||
		stored.Stats["pos_y"] != 250 ||
		stored.Stats["direction"] != 5 {
		t.Fatalf("solo teleport location found=%t err=%v stats=%+v", found, err, stored.Stats)
	}
}

func TestHandleCurrentSoloTeleportUsesPVFTransportRouteForCrossTownTarget(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	body, err := hex.DecodeString("ffffffffffffffff27008403fa0005")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleCurrentSoloTeleport(session, body); err != nil {
		t.Fatal(err)
	}
	if session.conn.(*bufferConn).write.Len() == 0 {
		stored, found, loadErr := repositories.Character.Load(context.Background(), "29")
		t.Fatalf("cross-town solo teleport wrote no transition; found=%t err=%v stats=%+v", found, loadErr, stored.Stats)
	}
	packet, rest := splitTownTransitionAndPostState(
		t,
		session.conn.(*bufferConn).write.Bytes(),
		session,
		29,
		39,
		0,
		900,
		250,
		5,
		3,
		townMoveSkillProjectionBody(t, repositories, "29"),
		false,
	)
	if packet.Header.MsgID != currentSceneTransitionMsgID ||
		packet.Header.Classification != 0 || len(rest) != 0 {
		t.Fatalf(
			"cross-town solo teleport response header=%+v body=%x trailing=%x",
			packet.Header,
			packet.Body,
			rest,
		)
	}
	stored, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found ||
		stored.Stats["town_id"] != 39 ||
		stored.Stats["area_id"] != 0 ||
		stored.Stats["pos_x"] != 900 ||
		stored.Stats["pos_y"] != 250 {
		t.Fatalf(
			"cross-town solo teleport location found=%t err=%v stats=%+v",
			found,
			err,
			stored.Stats,
		)
	}
}

func TestBuildCurrentSoloTeleportMoveBodyMarksTransportSource(t *testing.T) {
	request := currentSoloTeleportRequest{
		TownID:    40,
		AreaID:    4,
		PositionX: 330,
		PositionY: 264,
		Direction: 5,
	}
	body := buildCurrentSoloTeleportMoveBody(request, 38)
	parsed, err := parseCurrentTownSetUserAreaRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TownID != 40 || parsed.AreaID != 4 ||
		parsed.PositionX != 330 || parsed.PositionY != 264 ||
		parsed.Direction != 5 || parsed.OpaqueU16A != 38 ||
		parsed.OpaqueU16B != 0 || parsed.OpaqueU32 != 0 ||
		parsed.OpaqueTailU8 != 5 || !parsed.LooksLikeTeleportArraySelection() {
		t.Fatalf("solo teleport move body parsed=%+v", parsed)
	}
}
