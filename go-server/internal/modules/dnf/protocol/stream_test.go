package protocol

import (
	"bytes"
	"testing"
)

func TestSplitLatestGameStreamAcceptsUpperAndTransport(t *testing.T) {
	upper, err := BuildChannelPacket(4, []byte{1, 0}, 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build upper packet: %v", err)
	}
	transport, err := BuildLatestGameTCP(1, 1, []byte{9, 8, 7}, TransportOptions{Sequence: 3})
	if err != nil {
		t.Fatalf("build transport: %v", err)
	}
	stream := append(append([]byte(nil), upper...), transport...)

	packets, remaining, skipped, err := SplitLatestGameStream(stream, 1024)
	if err != nil {
		t.Fatalf("split stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%d", skipped, len(remaining))
	}
	if len(packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(packets))
	}
	if packets[0].Kind != LatestGameStreamUpper {
		t.Fatalf("first packet kind = %d, want upper", packets[0].Kind)
	}
	if packets[1].Kind != LatestGameStreamTransport {
		t.Fatalf("second packet kind = %d, want transport", packets[1].Kind)
	}
}

func TestSplitLatestGameStreamClassifiesNativeDprotoBeforeLegacy(t *testing.T) {
	dproto, err := BuildDprotoClientEnvelope([]byte{0x00, 0xad, 0x00, 0x10, 0x00, 1, 2, 3}, 55)
	if err != nil {
		t.Fatalf("build dproto: %v", err)
	}
	heartbeat, err := BuildChannelPacket(1276, nil, 56, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build heartbeat: %v", err)
	}
	stream := append(append([]byte(nil), dproto...), heartbeat...)
	packets, remaining, skipped, err := SplitLatestGameStream(stream, 1024)
	if err != nil {
		t.Fatalf("split stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 2 {
		t.Fatalf("packets=%d skipped=%d remaining=%d", len(packets), skipped, len(remaining))
	}
	if packets[0].Kind != LatestGameStreamDproto || packets[1].Kind != LatestGameStreamUpper {
		t.Fatalf("kinds=%d,%d", packets[0].Kind, packets[1].Kind)
	}
}

func TestSplitLatestGameStreamClassifiesExact90CNChannelReconnectLifecycleBeforeLegacy(t *testing.T) {
	tests := []struct {
		name    string
		msgID   uint16
		bodyLen int
	}{
		{name: "target channel probe", msgID: 2, bodyLen: currentChannelReconnectProbeSize},
		{name: "current exe post commit display rebind", msgID: 1, bodyLen: currentChannelDisplayRebindSize},
		{name: "90cn reference post commit display rebind", msgID: 1, bodyLen: reference90CNChannelDisplayRebindBodySize},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle, err := BuildChannelPacket(
				test.msgID,
				make([]byte, test.bodyLen),
				55,
				DefaultChannelClassification,
			)
			if err != nil {
				t.Fatal(err)
			}
			heartbeat, err := BuildChannelPacket(1276, nil, 56, DefaultChannelClassification)
			if err != nil {
				t.Fatal(err)
			}
			stream := append(append([]byte(nil), lifecycle...), heartbeat...)
			packets, remaining, skipped, err := SplitLatestGameStream(stream, 4096)
			if err != nil {
				t.Fatal(err)
			}
			if skipped != 0 || len(remaining) != 0 || len(packets) != 2 {
				t.Fatalf(
					"msg%d split packets=%d skipped=%d remaining=%d",
					test.msgID,
					len(packets),
					skipped,
					len(remaining),
				)
			}
			if packets[0].Kind != LatestGameStreamUpper ||
				packets[1].Kind != LatestGameStreamUpper {
				t.Fatalf("msg%d kinds=%d/%d want upper/upper", test.msgID, packets[0].Kind, packets[1].Kind)
			}
			parsed, err := ParseChannelPacketUnchecked(packets[0].Data)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Header.MsgID != test.msgID || len(parsed.Body) != test.bodyLen {
				t.Fatalf("msg%d parsed header=%+v body_len=%d", test.msgID, parsed.Header, len(parsed.Body))
			}
		})
	}
}

func TestKnown90CNChannelReconnectLifecycleRejectsAdjacentLengths(t *testing.T) {
	for _, test := range []struct {
		msgID   uint16
		bodyLen int
	}{
		{msgID: 1, bodyLen: currentChannelDisplayRebindSize - 1},
		{msgID: 1, bodyLen: currentChannelDisplayRebindSize + 1},
		{msgID: 1, bodyLen: reference90CNChannelDisplayRebindBodySize - 1},
		{msgID: 1, bodyLen: reference90CNChannelDisplayRebindBodySize + 1},
		{msgID: 2, bodyLen: currentChannelReconnectProbeSize - 1},
		{msgID: 2, bodyLen: currentChannelReconnectProbeSize + 1},
	} {
		if isKnownClientUpperMsg(test.msgID, make([]byte, test.bodyLen)) {
			t.Fatalf("msg%d adjacent body length %d accepted", test.msgID, test.bodyLen)
		}
	}
}

func TestSplitLatestGameStreamClassifiesExactChangeCharacterSlotBeforeLegacy(t *testing.T) {
	body := []byte{0, 0, 0, 0, 1, 0, 0, 0}
	changeSlot, err := BuildChannelPacket(295, body, 55, DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat, err := BuildChannelPacket(1276, nil, 56, DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	stream := append(append([]byte(nil), changeSlot...), heartbeat...)
	packets, remaining, skipped, err := SplitLatestGameStream(stream, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 2 {
		t.Fatalf("packets=%d skipped=%d remaining=%d", len(packets), skipped, len(remaining))
	}
	if packets[0].Kind != LatestGameStreamUpper || packets[1].Kind != LatestGameStreamUpper {
		t.Fatalf("kinds=%d/%d want upper/upper", packets[0].Kind, packets[1].Kind)
	}
	parsed, err := ParseChannelPacketUnchecked(packets[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.MsgID != 295 || !bytes.Equal(parsed.Body, body) {
		t.Fatalf("change-slot header=%+v body=%x", parsed.Header, parsed.Body)
	}
	for _, adjacentLength := range []int{7, 9} {
		if isKnownClientUpperMsg(295, make([]byte, adjacentLength)) {
			t.Fatalf("op295 adjacent body length %d accepted", adjacentLength)
		}
	}
}

func TestSplitLatestGameStreamAcceptsCurrentDungeonDeathMoveSettlementBossStatisticTutorialMissionAndReturnUpper(t *testing.T) {
	townMove, err := BuildChannelPacket(36, make([]byte, currentTownSetUserAreaPlainBodySize), 6, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build town-move upper: %v", err)
	}
	deathBody := make([]byte, 86)
	deathBody[22] = 2
	death, err := BuildChannelPacket(39, deathBody, 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build death upper: %v", err)
	}
	characterDeath, err := BuildChannelPacket(40, make([]byte, currentDieCharacterPlainBodySize), 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build character-death upper: %v", err)
	}
	useCoin, err := BuildChannelPacket(41, make([]byte, currentUseCoinPlainBodySize), 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build use-coin upper: %v", err)
	}
	move, err := BuildChannelPacket(45, make([]byte, currentDungeonMoveMapPlainBodySize), 8, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build move upper: %v", err)
	}
	playResult, err := BuildChannelPacket(46, make([]byte, currentPlayResultBaseBodySize+currentPlayResultDynamicRowSize), 9, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build play-result upper: %v", err)
	}
	giveupGame, err := BuildChannelPacket(42, nil, 10, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build giveup-game upper: %v", err)
	}
	bossDieCheck, err := BuildChannelPacket(117, make([]byte, currentBossDieCheckPlainBodySize), 10, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build boss-die-check upper: %v", err)
	}
	characterStatistic, err := BuildChannelPacket(123, make([]byte, 24), 11, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build character-statistic upper: %v", err)
	}
	missionCheck, err := BuildChannelPacket(560, nil, 11, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build mission-check upper: %v", err)
	}
	backToVillage, err := BuildChannelPacket(132, nil, 12, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build return-to-village upper: %v", err)
	}
	tutorialFlag, err := BuildChannelPacket(143, []byte{0, 31, 0, 0, 0, 1}, 13, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build tutorial-flag upper: %v", err)
	}
	stream := append(append(append(append(append(append(append(append(append(append(append(append([]byte(nil), townMove...), death...), characterDeath...), useCoin...), move...), playResult...), giveupGame...), bossDieCheck...), characterStatistic...), tutorialFlag...), missionCheck...), backToVillage...)
	packets, remaining, skipped, err := SplitLatestGameStream(stream, 4096)
	if err != nil {
		t.Fatalf("split dungeon stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 12 {
		t.Fatalf("split dungeon stream packets=%d skipped=%d remaining=%d", len(packets), skipped, len(remaining))
	}
	for index, packet := range packets {
		if packet.Kind != LatestGameStreamUpper {
			t.Fatalf("packet[%d] kind=%d, want upper", index, packet.Kind)
		}
	}
}

func TestKnownCurrentDungeonUpperRejectsUnprovenBoundaries(t *testing.T) {
	if !isKnownClientUpperMsg(39, make([]byte, 62)) {
		t.Fatal("fixed 62-byte op39 rejected")
	}
	variable := make([]byte, 86)
	variable[22] = 2
	if !isKnownClientUpperMsg(39, variable) {
		t.Fatal("variable 86-byte op39 rejected")
	}
	variable[22] = 3
	if isKnownClientUpperMsg(39, variable) {
		t.Fatal("op39 count/body mismatch accepted")
	}
	if !isKnownClientUpperMsg(132, nil) {
		t.Fatal("bodyless op132 rejected")
	}
	if !isKnownClientUpperMsg(36, make([]byte, currentTownSetUserAreaPlainBodySize)) {
		t.Fatal("exact 16-byte town op36 rejected")
	}
	if !isKnownClientUpperMsg(31, []byte{0x1f, 0x00, 0x49, 0x0c}) {
		t.Fatal("exact accept-quest op31 rejected")
	}
	if !isKnownClientUpperMsg(42, nil) {
		t.Fatal("bodyless op42 rejected")
	}
	if !isKnownClientUpperMsg(40, make([]byte, currentDieCharacterPlainBodySize)) {
		t.Fatal("two-u16 op40 rejected")
	}
	if !isKnownClientUpperMsg(41, make([]byte, currentUseCoinPlainBodySize)) {
		t.Fatal("single-u16 op41 rejected")
	}
	if !isKnownClientUpperMsg(123, make([]byte, 24)) {
		t.Fatal("six-u32 op123 rejected")
	}
	for rows := 0; rows <= currentPlayResultMaximumDynamicRows; rows++ {
		for _, bodyLen := range []int{
			currentPlayResultBaseBodySize + rows*currentPlayResultDynamicRowSize,
			currentPlayResultOptionalBodySize + rows*currentPlayResultDynamicRowSize,
		} {
			if !isKnownClientUpperMsg(46, make([]byte, bodyLen)) {
				t.Fatalf("plaintext op46 rows=%d body_len=%d rejected", rows, bodyLen)
			}
		}
	}
	if !isKnownClientUpperMsg(117, make([]byte, currentBossDieCheckPlainBodySize)) {
		t.Fatal("plaintext 39-byte op117 rejected")
	}
	if !isKnownClientUpperMsg(143, make([]byte, currentTutorialFlagPlainBodySize)) {
		t.Fatal("plaintext six-byte op143 rejected")
	}
	if !isKnownClientUpperMsg(560, nil) {
		t.Fatal("bodyless op560 rejected")
	}
	if isKnownClientUpperMsg(39, make([]byte, 63)) ||
		isKnownClientUpperMsg(31, []byte{0x1f, 0x00, 0x49}) ||
		isKnownClientUpperMsg(31, []byte{0x20, 0x00, 0x49, 0x0c}) ||
		isKnownClientUpperMsg(36, make([]byte, currentTownSetUserAreaPlainBodySize-1)) ||
		isKnownClientUpperMsg(36, make([]byte, currentTownSetUserAreaPlainBodySize+1)) ||
		isKnownClientUpperMsg(45, make([]byte, currentDungeonMoveMapPlainBodySize-1)) ||
		isKnownClientUpperMsg(45, make([]byte, currentDungeonMoveMapPlainBodySize+1)) ||
		isKnownClientUpperMsg(45, make([]byte, 112)) ||
		isKnownClientUpperMsg(40, make([]byte, currentDieCharacterPlainBodySize-1)) ||
		isKnownClientUpperMsg(40, make([]byte, currentDieCharacterPlainBodySize+1)) ||
		isKnownClientUpperMsg(41, make([]byte, currentUseCoinPlainBodySize-1)) ||
		isKnownClientUpperMsg(41, make([]byte, currentUseCoinPlainBodySize+1)) ||
		isKnownClientUpperMsg(46, make([]byte, currentPlayResultBaseBodySize-1)) ||
		isKnownClientUpperMsg(46, make([]byte, currentPlayResultBaseBodySize+1)) ||
		isKnownClientUpperMsg(46, make([]byte, currentPlayResultOptionalBodySize+currentPlayResultMaximumDynamicRows*currentPlayResultDynamicRowSize+1)) ||
		isKnownClientUpperMsg(117, make([]byte, currentBossDieCheckPlainBodySize-1)) ||
		isKnownClientUpperMsg(117, make([]byte, currentBossDieCheckPlainBodySize+1)) ||
		isKnownClientUpperMsg(143, make([]byte, currentTutorialFlagPlainBodySize-1)) ||
		isKnownClientUpperMsg(143, make([]byte, 16)) ||
		isKnownClientUpperMsg(42, []byte{0}) ||
		isKnownClientUpperMsg(123, make([]byte, 23)) ||
		isKnownClientUpperMsg(123, make([]byte, 25)) ||
		isKnownClientUpperMsg(132, []byte{0}) ||
		isKnownClientUpperMsg(560, []byte{0}) {
		t.Fatal("unproven dungeon upper boundary accepted")
	}
}

func TestSplitLatestGameStreamClassifiesExactAcceptQuestAsUpper(t *testing.T) {
	packet, err := BuildChannelPacket(31, []byte{0x1f, 0x00, 0x49, 0x0c}, 34, DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	packets, remaining, skipped, err := SplitLatestGameStream(packet, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 1 {
		t.Fatalf("op31 split packets=%d skipped=%d remaining=%d", len(packets), skipped, len(remaining))
	}
	if packets[0].Kind != LatestGameStreamUpper {
		t.Fatalf("op31 kind=%d, want upper", packets[0].Kind)
	}
	parsed, err := ParseChannelPacketUnchecked(packets[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.MsgID != 31 || !bytes.Equal(parsed.Body, []byte{0x1f, 0x00, 0x49, 0x0c}) {
		t.Fatalf("op31 packet header=%+v body=%x", parsed.Header, parsed.Body)
	}
}

func TestSplitLatestGameStreamClassifiesExactSetQuestTriggerAsUpper(t *testing.T) {
	body := []byte{0x21, 0x00, 0x78, 0x12, 0x00, 0x00}
	packet, err := BuildChannelPacket(33, body, 35, DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	packets, remaining, skipped, err := SplitLatestGameStream(packet, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 1 || packets[0].Kind != LatestGameStreamUpper {
		t.Fatalf("op33 split packets=%d skipped=%d remaining=%d kind=%d", len(packets), skipped, len(remaining), packets[0].Kind)
	}
	parsed, err := ParseChannelPacketUnchecked(packets[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.MsgID != 33 || !bytes.Equal(parsed.Body, body) {
		t.Fatalf("op33 packet header=%+v body=%x", parsed.Header, parsed.Body)
	}
}

func TestSplitLatestGameStreamKeepsSetQuestTriggerCloneTrailerLegacy(t *testing.T) {
	body := []byte{0x21, 0x00, 0x78, 0x12, 0x00, 0x00, 0xaa, 0xbb, 0xcc, 0xdd}
	packet, err := BuildChannelPacket(33, body, 36, DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	packets, remaining, skipped, err := SplitLatestGameStream(packet, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 1 || packets[0].Kind != LatestGameStreamLegacy {
		t.Fatalf("op33 clone split packets=%d skipped=%d remaining=%d kind=%d", len(packets), skipped, len(remaining), packets[0].Kind)
	}
}

func TestSplitLatestGameStreamClassifiesBodylessMissionCheckAsUpperNotLegacy(t *testing.T) {
	packet, err := BuildChannelPacket(560, nil, 12, DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	packets, remaining, skipped, err := SplitLatestGameStream(packet, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 1 {
		t.Fatalf("op560 split packets=%d skipped=%d remaining=%d", len(packets), skipped, len(remaining))
	}
	if packets[0].Kind != LatestGameStreamUpper {
		t.Fatalf("op560 kind=%d want upper", packets[0].Kind)
	}
}

func TestSplitLatestGameStreamKeepsProtectedFortyByteBossDieCheckLegacy(t *testing.T) {
	packet := buildLegacyGamePacketForTest(1, 117, 8, make([]byte, 40))

	packets, remaining, skipped, err := SplitLatestGameStream(packet, 1024)
	if err != nil {
		t.Fatalf("split protected boss-die-check: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 1 {
		t.Fatalf("split protected boss-die-check packets=%d skipped=%d remaining=%d", len(packets), skipped, len(remaining))
	}
	if packets[0].Kind != LatestGameStreamLegacy {
		t.Fatalf("protected 40-byte op117 kind=%d want legacy", packets[0].Kind)
	}
	parsed, err := ParseLegacyGamePacket(packets[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Type != 117 || len(parsed.Body) != 40 {
		t.Fatalf("protected op117 type/body=%d/%d", parsed.Header.Type, len(parsed.Body))
	}
}

func TestSplitLatestGameStreamWaitsForPartialUpper(t *testing.T) {
	upper, err := BuildChannelPacket(4, []byte{1, 0}, 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build upper packet: %v", err)
	}

	packets, remaining, skipped, err := SplitLatestGameStream(upper[:len(upper)-1], 1024)
	if err != nil {
		t.Fatalf("split partial upper: %v", err)
	}
	if len(packets) != 0 || skipped != 0 {
		t.Fatalf("partial upper should wait, packets=%d skipped=%d", len(packets), skipped)
	}
	if len(remaining) != len(upper)-1 {
		t.Fatalf("remaining = %d, want %d", len(remaining), len(upper)-1)
	}
}

func TestSplitLatestGameStreamAcceptsLegacyGamePackets(t *testing.T) {
	first := buildLegacyGamePacketForTest(1, 0x00ab, 1, []byte{1, 2, 3})
	second := buildLegacyGamePacketForTest(1, 0x01dc, 2, []byte{4, 5})
	stream := append(append([]byte(nil), first...), second...)

	packets, remaining, skipped, err := SplitLatestGameStream(stream, 1024)
	if err != nil {
		t.Fatalf("split stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%d", skipped, len(remaining))
	}
	if len(packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(packets))
	}
	if packets[0].Kind != LatestGameStreamLegacy || packets[1].Kind != LatestGameStreamLegacy {
		t.Fatalf("packet kinds = %d/%d, want legacy/legacy", packets[0].Kind, packets[1].Kind)
	}
	parsed, err := ParseLegacyGamePacket(packets[0].Data)
	if err != nil {
		t.Fatalf("parse legacy packet: %v", err)
	}
	if parsed.Header.Type != 0x00ab || len(parsed.Body) != 3 {
		t.Fatalf("parsed legacy packet type=0x%04x body=%d", parsed.Header.Type, len(parsed.Body))
	}
}

func TestSplitLatestGameStreamAcceptsPostCreateUpperChain(t *testing.T) {
	for _, msgID := range []uint16{0x02b1, 0x02b2, 0x02b5, 0x000f, 0x0010, 0x0007, 0x02a7} {
		upper, err := BuildChannelPacket(msgID, []byte{1, 2, 3, 4}, 7, DefaultChannelClassification)
		if err != nil {
			t.Fatalf("build upper packet 0x%04x: %v", msgID, err)
		}
		packets, remaining, skipped, err := SplitLatestGameStream(upper, 1024)
		if err != nil {
			t.Fatalf("split upper 0x%04x: %v", msgID, err)
		}
		if skipped != 0 || len(remaining) != 0 || len(packets) != 1 {
			t.Fatalf("split upper 0x%04x state packets=%d skipped=%d remaining=%d", msgID, len(packets), skipped, len(remaining))
		}
		if packets[0].Kind != LatestGameStreamUpper {
			t.Fatalf("upper 0x%04x kind = %d, want upper", msgID, packets[0].Kind)
		}
	}
}

func TestSplitLatestGameStreamAcceptsObservedInitUpperChain(t *testing.T) {
	cases := []struct {
		msgID   uint16
		bodyLen int
	}{
		{msgID: 171, bodyLen: 48},
		{msgID: 476, bodyLen: 16},
		{msgID: 477, bodyLen: 8},
		{msgID: 1320, bodyLen: 32},
		{msgID: 1262, bodyLen: 32},
		{msgID: 593, bodyLen: 24},
		{msgID: 8, bodyLen: 8},
		{msgID: 415, bodyLen: 0},
		{msgID: 441, bodyLen: 8},
		{msgID: 645, bodyLen: 0},
		{msgID: 1276, bodyLen: 0},
		{msgID: 1518, bodyLen: 96},
		{msgID: 1518, bodyLen: 112},
		{msgID: 1518, bodyLen: 152},
		{msgID: 1516, bodyLen: 32},
		{msgID: 1516, bodyLen: 48},
		{msgID: 1516, bodyLen: 64},
		{msgID: 1516, bodyLen: 80},
	}
	for _, tc := range cases {
		upper, err := BuildChannelPacket(tc.msgID, make([]byte, tc.bodyLen), 7, DefaultChannelClassification)
		if err != nil {
			t.Fatalf("build observed upper %d: %v", tc.msgID, err)
		}
		packets, remaining, skipped, err := SplitLatestGameStream(upper, 1024)
		if err != nil {
			t.Fatalf("split observed upper %d: %v", tc.msgID, err)
		}
		if skipped != 0 || len(remaining) != 0 || len(packets) != 1 {
			t.Fatalf("split observed upper %d state packets=%d skipped=%d remaining=%d", tc.msgID, len(packets), skipped, len(remaining))
		}
		if packets[0].Kind != LatestGameStreamUpper {
			t.Fatalf("observed upper %d kind = %d, want upper", tc.msgID, packets[0].Kind)
		}
	}
}

func TestSplitLatestGameStreamAcceptsObservedPostMsg1UpperChain(t *testing.T) {
	cases := []struct {
		msgID   uint16
		bodyLen int
	}{
		{msgID: 194, bodyLen: 368},
		{msgID: 1286, bodyLen: 16},
		{msgID: 1287, bodyLen: 8},
		{msgID: 250, bodyLen: 64},
		{msgID: 279, bodyLen: 16},
		{msgID: 593, bodyLen: 8},
		{msgID: 3, bodyLen: 16},
	}
	for _, tc := range cases {
		upper, err := BuildChannelPacket(tc.msgID, make([]byte, tc.bodyLen), 7, DefaultChannelClassification)
		if err != nil {
			t.Fatalf("build post-msg1 upper %d: %v", tc.msgID, err)
		}
		packets, remaining, skipped, err := SplitLatestGameStream(upper, 1024)
		if err != nil {
			t.Fatalf("split post-msg1 upper %d: %v", tc.msgID, err)
		}
		if skipped != 0 || len(remaining) != 0 || len(packets) != 1 {
			t.Fatalf("split post-msg1 upper %d state packets=%d skipped=%d remaining=%d", tc.msgID, len(packets), skipped, len(remaining))
		}
		if packets[0].Kind != LatestGameStreamUpper {
			t.Fatalf("post-msg1 upper %d kind = %d, want upper", tc.msgID, packets[0].Kind)
		}
	}
}

func TestSplitLatestGameStreamAcceptsCodecUpperWithoutChecksum(t *testing.T) {
	raw := mustHex(t, "01b2021d00000038ab3cdc000015b101e6d58515bd9dbb3490ac32e75a")

	packets, remaining, skipped, err := SplitLatestGameStream(raw, 1024)
	if err != nil {
		t.Fatalf("split codec upper: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 || len(packets) != 1 {
		t.Fatalf("split state packets=%d skipped=%d remaining=%d", len(packets), skipped, len(remaining))
	}
	if packets[0].Kind != LatestGameStreamUpper {
		t.Fatalf("packet kind = %d, want upper", packets[0].Kind)
	}
}

func TestSplitLatestGameStreamCutsHookedShortCreateBeforeHeartbeat(t *testing.T) {
	create := mustHex(t, "010500250000009585a5c916000008000000b8d5b8d5b7a2b8f8000000000000ff00")
	heartbeat, err := BuildChannelPacket(1276, nil, 23, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build heartbeat upper: %v", err)
	}
	stream := append(append([]byte(nil), create...), heartbeat...)

	packets, remaining, skipped, err := SplitLatestGameStream(stream, 1024)
	if err != nil {
		t.Fatalf("split hooked create stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%x", skipped, remaining)
	}
	if len(packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(packets))
	}
	first, err := ParseChannelPacketUnchecked(packets[0].Data)
	if err != nil {
		t.Fatalf("parse create upper: %v", err)
	}
	second, err := ParseChannelPacketUnchecked(packets[1].Data)
	if err != nil {
		t.Fatalf("parse heartbeat upper: %v", err)
	}
	if first.Header.MsgID != 5 || len(first.Body) != 21 {
		t.Fatalf("first packet msg/body = %d/%d", first.Header.MsgID, len(first.Body))
	}
	if second.Header.MsgID != 1276 || len(second.Body) != 0 {
		t.Fatalf("second packet msg/body = %d/%d", second.Header.MsgID, len(second.Body))
	}
}

func TestSplitLatestGameStreamCutsCurrentHookedShortCreateBeforeHeartbeat(t *testing.T) {
	create := mustHex(t, "010500250000003514fb8014000106000000b7a2b7a2b7a2000000000000ff00")
	heartbeat, err := BuildChannelPacket(1276, nil, 21, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build heartbeat upper: %v", err)
	}
	stream := append(append([]byte(nil), create...), heartbeat...)

	packets, remaining, skipped, err := SplitLatestGameStream(stream, 1024)
	if err != nil {
		t.Fatalf("split current hooked create stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%x", skipped, remaining)
	}
	if len(packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(packets))
	}
	first, err := ParseChannelPacketUnchecked(packets[0].Data)
	if err != nil {
		t.Fatalf("parse create upper: %v", err)
	}
	second, err := ParseChannelPacketUnchecked(packets[1].Data)
	if err != nil {
		t.Fatalf("parse heartbeat upper: %v", err)
	}
	if first.Header.MsgID != 5 || len(first.Body) != 19 {
		t.Fatalf("first packet msg/body = %d/%d", first.Header.MsgID, len(first.Body))
	}
	if second.Header.MsgID != 1276 || len(second.Body) != 0 {
		t.Fatalf("second packet msg/body = %d/%d", second.Header.MsgID, len(second.Body))
	}
}

func TestSplitLatestGameStreamClassifiesExactLengthShortCreateAsUpper(t *testing.T) {
	body := mustHex(t, "0003000000363636000000000000ff00")
	create, err := BuildChannelPacket(5, body, 20, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build exact-length create upper: %v", err)
	}

	packets, remaining, skipped, err := SplitLatestGameStream(create, 1024)
	if err != nil {
		t.Fatalf("split exact-length create upper: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%x", skipped, remaining)
	}
	if len(packets) != 1 {
		t.Fatalf("packet count = %d, want 1", len(packets))
	}
	if packets[0].Kind != LatestGameStreamUpper {
		t.Fatalf("packet kind = %d, want upper", packets[0].Kind)
	}
	parsed, err := ParseChannelPacketUnchecked(packets[0].Data)
	if err != nil {
		t.Fatalf("parse exact-length create upper: %v", err)
	}
	if parsed.Header.MsgID != 5 || !bytes.Equal(parsed.Body, body) {
		t.Fatalf("create packet msg/body = %d/%x, want 5/%x", parsed.Header.MsgID, parsed.Body, body)
	}
}

func TestSplitLatestGameStreamCutsCurrentHookedSelectBeforeFollowingLegacyPacket(t *testing.T) {
	selectDungeon := mustHex(t, "0110002d000000a46efcb81800e91b00000000000000ffff00000000000000000000")
	changeTutorial := mustHex(t, "018f001d00000064d69b8b19004cfb5fc3405889ac4e528f72fe7249ea")
	stream := append(append([]byte(nil), selectDungeon...), changeTutorial...)

	packets, remaining, skipped, err := SplitLatestGameStream(stream, 1024)
	if err != nil {
		t.Fatalf("split hooked select stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%x", skipped, remaining)
	}
	if len(packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(packets))
	}
	first, err := ParseChannelPacketUnchecked(packets[0].Data)
	if err != nil {
		t.Fatalf("parse select upper: %v", err)
	}
	second, err := ParseLegacyGamePacket(packets[1].Data)
	if err != nil {
		t.Fatalf("parse following legacy packet: %v", err)
	}
	if first.Header.MsgID != 16 || len(first.Body) != 21 {
		t.Fatalf("first packet msg/body = %d/%d", first.Header.MsgID, len(first.Body))
	}
	if second.Header.Type != 143 || len(second.Body) != 16 {
		t.Fatalf("second packet type/body = %d/%d", second.Header.Type, len(second.Body))
	}
}

func TestSplitLatestGameStreamPrefersLegacyForUpperLikeCommand(t *testing.T) {
	packet := buildLegacyGamePacketForTest(1, 0x0301, 8, nil)

	packets, remaining, skipped, err := SplitLatestGameStream(packet, 1024)
	if err != nil {
		t.Fatalf("split stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%d", skipped, len(remaining))
	}
	if len(packets) != 1 {
		t.Fatalf("packet count = %d, want 1", len(packets))
	}
	if packets[0].Kind != LatestGameStreamLegacy {
		t.Fatalf("packet kind = %d, want legacy", packets[0].Kind)
	}
	parsed, err := ParseLegacyGamePacket(packets[0].Data)
	if err != nil {
		t.Fatalf("parse legacy packet: %v", err)
	}
	if parsed.Header.Type != 0x0301 {
		t.Fatalf("legacy type = 0x%04x, want 0x0301", parsed.Header.Type)
	}
}

func TestSplitLatestGameStreamKeepsLegacyGetUserInfoWithoutUpperBody(t *testing.T) {
	packet := buildLegacyGamePacketForTest(1, 8, 8, nil)

	packets, remaining, skipped, err := SplitLatestGameStream(packet, 1024)
	if err != nil {
		t.Fatalf("split stream: %v", err)
	}
	if skipped != 0 || len(remaining) != 0 {
		t.Fatalf("unexpected split state skipped=%d remaining=%d", skipped, len(remaining))
	}
	if len(packets) != 1 {
		t.Fatalf("packet count = %d, want 1", len(packets))
	}
	if packets[0].Kind != LatestGameStreamLegacy {
		t.Fatalf("packet kind = %d, want legacy", packets[0].Kind)
	}
}

func buildLegacyGamePacketForTest(cmd byte, typ uint16, seq uint16, body []byte) []byte {
	packet := make([]byte, LegacyGameHeaderSize+len(body))
	packet[0] = cmd
	packet[1] = byte(typ)
	packet[2] = byte(typ >> 8)
	length := uint32(len(packet))
	packet[3] = byte(length)
	packet[4] = byte(length >> 8)
	packet[5] = byte(length >> 16)
	packet[6] = byte(length >> 24)
	packet[11] = byte(seq)
	packet[12] = byte(seq >> 8)
	copy(packet[LegacyGameHeaderSize:], body)
	return packet
}
