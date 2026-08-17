package dnfbridge

import (
	"context"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentJoustRosterUsesProvedFloatMultiplierLayoutAndPoolUpdate(t *testing.T) {
	opening, err := mustTestJoustEventCatalog(t).Opening(321, dnfjoust.BettingLedger{})
	if err != nil {
		t.Fatal(err)
	}
	roster, err := buildCurrentJoustOpeningRoster(opening)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 90 || binary.LittleEndian.Uint16(roster[:2]) != 321 {
		t.Fatalf("roster len=%d prefix=%x", len(roster), roster[:2])
	}
	multipliers := make(map[uint32]struct{}, currentJoustRosterCount)
	for index, want := range opening.Riders {
		record := roster[2+index*currentJoustRosterRecordSize : 2+(index+1)*currentJoustRosterRecordSize]
		if record[0] != want.ID || record[1] != want.AttackType || record[10] != want.Status ||
			binary.LittleEndian.Uint16(record[6:8]) != want.Wins || binary.LittleEndian.Uint16(record[8:10]) != want.Losses {
			t.Fatalf("record[%d]=%x want=%+v", index, record, want)
		}
		bits := binary.LittleEndian.Uint32(record[2:6])
		if got := math.Float32frombits(bits); got != want.Multiplier || got <= 0 {
			t.Fatalf("record[%d] multiplier=%v want=%v", index, got, want.Multiplier)
		}
		multipliers[bits] = struct{}{}
	}
	if got := roster[2+(currentJoustRosterCount-1)*currentJoustRosterRecordSize+10]; got != dnfjoust.MysteryRiderStatus {
		t.Fatalf("mystery roster status=%d want=%d", got, dnfjoust.MysteryRiderStatus)
	}
	if len(multipliers) != currentJoustRosterCount {
		t.Fatalf("distinct multipliers=%d want=%d", len(multipliers), currentJoustRosterCount)
	}
	const personalSupport = uint32(37)
	pool, err := buildCurrentJoustPool(opening, personalSupport)
	if err != nil {
		t.Fatal(err)
	}
	if len(pool) != 46 || binary.LittleEndian.Uint16(pool[:2]) != opening.Number ||
		binary.LittleEndian.Uint32(pool[2:6]) != personalSupport {
		t.Fatalf("pool=%x opening=%+v", pool, opening)
	}
	for index, want := range opening.Riders {
		record := pool[6+index*5 : 6+(index+1)*5]
		if record[0] != want.ID || binary.LittleEndian.Uint32(record[1:5]) != want.Support {
			t.Fatalf("pool[%d]=%x want=%+v", index, record, want)
		}
	}
}

func TestCurrentJoustPoolRejectsPersonalSupportAbovePVFLimit(t *testing.T) {
	opening, err := mustTestJoustEventCatalog(t).Opening(321, dnfjoust.BettingLedger{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildCurrentJoustPool(opening, dnfjoust.MaximumBetPerRound+1); err == nil {
		t.Fatal("personal support above PVF max betting was accepted")
	}
}

func TestCurrentJoustInfoReturnsProvedZeroResult(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{}
	session := &gameSession{conn: connection, connID: "joust-info"}
	if err := handleCurrentJoustInfo(service, session, nil); err != nil {
		t.Fatal(err)
	}
	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketJoustInfo) {
		t.Fatalf("joust info header=%+v trailing=%x", packet.Header, trailing)
	}
	if len(packet.Body) != 5 || packet.Body[0] != 1 || binary.LittleEndian.Uint32(packet.Body[1:]) != 0 {
		t.Fatalf("joust info body=%x", packet.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("joust info replayed state packets=%x", trailing)
	}
}

func TestCurrentJoustInfoRefreshesRosterAndPersonalSupportAfterWindowReady(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	catalog := mustTestJoustEventCatalog(t)
	const (
		round           = uint16(4321)
		personalSupport = int64(37)
	)
	base, err := catalog.OpeningWithLedgers(round, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       90,
		Stats: map[string]int64{
			dnfjoust.RoundStat:   int64(round),
			dnfjoust.KnightStat:  int64(base.Riders[0].ID),
			dnfjoust.AmountStat:  personalSupport,
			dnfjoust.PendingStat: 1,
		},
	}); err != nil {
		t.Fatal(err)
	}
	queue := newFakeCurrentDungeonDeathTimerQueue()
	queue.now = time.Unix(7200*int64(round), 0).UTC().Add(10 * time.Minute)
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		joustCatalog:       catalog,
		gameplayTimers:     queue,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "joust-info-panel-refresh", selectedCharacterID: 19}
	if err := handleCurrentJoustInfo(service, session, nil); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketJoustInfo) || len(ack.Body) != 5 ||
		ack.Body[0] != 1 || binary.LittleEndian.Uint32(ack.Body[1:]) != 0 {
		t.Fatalf("info ACK=%+v body=%x", ack.Header, ack.Body)
	}
	roster, rest := splitGameServerUpperPacket(t, rest)
	if roster.Header.Classification != 0 || roster.Header.MsgID != currentJoustRosterPushMsgID ||
		binary.LittleEndian.Uint16(roster.Body[:2]) != round {
		t.Fatalf("roster header=%+v body=%x", roster.Header, roster.Body)
	}
	pool, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || pool.Header.Classification != 0 || pool.Header.MsgID != currentJoustPoolPushMsgID ||
		binary.LittleEndian.Uint16(pool.Body[:2]) != round ||
		binary.LittleEndian.Uint32(pool.Body[2:6]) != uint32(personalSupport) {
		t.Fatalf("pool header=%+v body=%x trailing=%x", pool.Header, pool.Body, trailing)
	}
}

func TestCurrentJoustHistoryReturnsFixedEmptyArray(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{}
	session := &gameSession{conn: connection, connID: "joust-history"}
	if err := handleCurrentJoustHistory(service, session, nil); err != nil {
		t.Fatal(err)
	}
	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketJoustMatchHistory) {
		t.Fatalf("joust history header=%+v trailing=%x", packet.Header, trailing)
	}
	if len(packet.Body) != 1+4+currentJoustHistoryRecordCount*7 || packet.Body[0] != 1 ||
		binary.LittleEndian.Uint32(packet.Body[1:5]) != 0 {
		t.Fatalf("joust history body len=%d prefix=%x", len(packet.Body), packet.Body[:5])
	}
	for index, value := range packet.Body[5:] {
		if value != 0 {
			t.Fatalf("joust history byte[%d]=%d want=0", index, value)
		}
	}
}

func TestCurrentJoustHistoryReturnsOnlySettledChampionAndFloatMultiplier(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	connection := &bufferConn{}
	queue := newFakeCurrentDungeonDeathTimerQueue()
	queue.now = time.Unix(7200*4321, 0).UTC().Add(10 * time.Minute)
	catalog := mustTestJoustEventCatalog(t)
	tournament, err := catalog.TournamentFor(4320)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		joustCatalog:       catalog,
		gameplayTimers:     queue,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.persistCurrentJoustHistoryRecord(ctx, "account-1", currentJoustHistoryRecord{
		Round: 4320, Winner: tournament.Champion(), Multiplier: 7.5,
	}, queue.now); err != nil {
		t.Fatal(err)
	}
	session := &gameSession{conn: connection, connID: "joust-history-complete"}
	if err := handleCurrentJoustHistory(service, session, nil); err != nil {
		t.Fatal(err)
	}
	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || len(packet.Body) != 1+4+currentJoustHistoryRecordCount*7 {
		t.Fatalf("history len=%d trailing=%x", len(packet.Body), trailing)
	}
	first := packet.Body[5:12]
	if binary.LittleEndian.Uint16(first[:2]) != 4320 || first[2] != tournament.Champion() {
		t.Fatalf("history first=%x", first)
	}
	if multiplier := math.Float32frombits(binary.LittleEndian.Uint32(first[3:7])); multiplier != 7.5 {
		t.Fatalf("history multiplier=%v record=%x", multiplier, first)
	}
	for index, value := range packet.Body[12:] {
		if value != 0 {
			t.Fatalf("history trailing byte[%d]=%d want=0", index, value)
		}
	}
}

func TestCurrentJoustMatchSnapshotUsesProvedSevenDWORDLayout(t *testing.T) {
	catalog := mustTestJoustEventCatalog(t)
	tournament, err := catalog.TournamentFor(321)
	if err != nil {
		t.Fatal(err)
	}
	body, err := buildCurrentJoustMatchSnapshot(321, 2, tournament)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 31 || binary.LittleEndian.Uint16(body[:2]) != 321 || body[2] != 2 {
		t.Fatalf("match body=%x", body)
	}
	for index, want := range tournament.Matches {
		record := body[3+index*4 : 7+index*4]
		if record[0] != want.Winner || record[1] != want.WinnerAction || record[2] != want.Loser || record[3] != want.LoserAction {
			t.Fatalf("match[%d]=%x want=%+v", index, record, want)
		}
	}
}

func TestCurrentJoustQuarterFinalBoundaryPushesOneStateThenOneBracket(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{joustCatalog: mustTestJoustEventCatalog(t)}
	session := &gameSession{conn: connection, connID: "joust-quarter-final"}
	timeline := dnfjoust.Timeline{Round: 321, Phase: dnfjoust.PhaseQuarterFinal, State: 2, Stage: 0}
	if err := service.pushCurrentJoustBoundarySnapshot(context.Background(), session, timeline); err != nil {
		t.Fatal(err)
	}
	state, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if state.Header.MsgID != currentJoustStatePushMsgID || !reflect.DeepEqual(state.Body, []byte{0x41, 0x01, 0x02}) {
		t.Fatalf("state header=%+v body=%x", state.Header, state.Body)
	}
	bracket, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || bracket.Header.MsgID != currentJoustMatchPushMsgID || len(bracket.Body) != 31 || bracket.Body[2] != 0 {
		t.Fatalf("bracket header=%+v body=%x trailing=%x", bracket.Header, bracket.Body, trailing)
	}
}

func TestCurrentJoustSemiFinalBoundaryRestartsNativeBattleAnimation(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{joustCatalog: mustTestJoustEventCatalog(t)}
	session := &gameSession{conn: connection, connID: "joust-semi-final"}
	timeline := dnfjoust.Timeline{Round: 321, Phase: dnfjoust.PhaseSemiFinal, State: 2, Stage: 1}
	if err := service.pushCurrentJoustBoundarySnapshot(context.Background(), session, timeline); err != nil {
		t.Fatal(err)
	}
	state, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if state.Header.MsgID != currentJoustStatePushMsgID || !reflect.DeepEqual(state.Body, []byte{0x41, 0x01, 0x02}) {
		t.Fatalf("state header=%+v body=%x", state.Header, state.Body)
	}
	bracket, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || bracket.Header.MsgID != currentJoustMatchPushMsgID || len(bracket.Body) != 31 || bracket.Body[2] != 1 {
		t.Fatalf("bracket header=%+v body=%x trailing=%x", bracket.Header, bracket.Body, trailing)
	}
}

func TestCurrentJoustQueriesRejectUnprovedBodies(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler func(*Service, *gameSession, []byte) error
	}{
		{name: "info", handler: handleCurrentJoustInfo},
		{name: "history", handler: handleCurrentJoustHistory},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := &bufferConn{}
			if err := test.handler(&Service{}, &gameSession{conn: connection}, []byte{1}); err != nil {
				t.Fatal(err)
			}
			if connection.write.Len() != 0 {
				t.Fatalf("unproved request wrote %d bytes", connection.write.Len())
			}
		})
	}
}

func TestCurrentJoustLegacyQueriesStripOnlyExactOpaquePrefix(t *testing.T) {
	for _, opcode := range []uint16{
		uint16(dnfenum.CmdPacketJoustInfo),
		uint16(dnfenum.CmdPacketJoustMatchHistory),
	} {
		t.Run(dnfenum.CmdPacketName(opcode), func(t *testing.T) {
			observed := []byte{0x7b, 0x12, 0xb1, 0x03, 0x0d, 0xd1, 0xf2, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00}
			if got := normalizeLegacyGameBody(opcode, observed); len(got) != 0 {
				t.Fatalf("normalized observed body=%x want empty", got)
			}
			for _, body := range [][]byte{observed[:12], append(append([]byte(nil), observed...), 0)} {
				got := normalizeLegacyGameBody(opcode, body)
				if !reflect.DeepEqual(got, body) {
					t.Fatalf("normalized boundary body=%x want=%x", got, body)
				}
			}
		})
	}
}

func TestCurrentJoustLegacyBettingStripsCapturedPrefixAndKeepsBusinessFields(t *testing.T) {
	body := []byte{
		0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc0, 0xef, 0x66, 0x57, 0x50,
		0x01, 0x07, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
	}
	want := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketJoustBetting), body); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized betting body=%x want=%x", got, want)
	}
	for _, boundary := range [][]byte{body[:21], append(append([]byte(nil), body...), 0)} {
		if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketJoustBetting), boundary); !reflect.DeepEqual(got, boundary) {
			t.Fatalf("normalized boundary=%x want unchanged=%x", got, boundary)
		}
	}
}
