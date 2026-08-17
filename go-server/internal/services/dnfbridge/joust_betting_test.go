package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeCurrentJoustBettingCapturedBusinessBody(t *testing.T) {
	request, err := decodeCurrentJoustBettingRequest([]byte{1, 7, 0, 0, 0, 1, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if request.Knight != 1 || request.SourceSlot != 7 || request.Amount != 1 {
		t.Fatalf("request=%+v", request)
	}
}

func TestCurrentJoustBettingCommitsCapturedRequestAndRefreshesInventory(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       90,
		Stats:       map[string]int64{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:7": {ItemID: dnfjoust.PermanentCrystalID, Count: 10},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		pvfItemCatalog:     mustTestJoustCatalog(t),
		joustCatalog:       mustTestJoustEventCatalog(t),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	queue := newFakeCurrentDungeonDeathTimerQueue()
	queue.now = time.Unix(7200*4321, 0).UTC().Add(10 * time.Minute)
	service.gameplayTimers = queue
	connection := &bufferConn{}
	session := &gameSession{conn: connection, connID: "joust-betting", selectedCharacterID: 19}
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketJoustBetting),
		[]byte{1, 7, 0, 0, 0, 3, 0, 0, 0},
	); err != nil {
		t.Fatal(err)
	}

	character, found, err := repositories.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	expectedRound := dnfjoust.RoundNumberAt(queue.Now())
	if character.Stats[dnfjoust.RoundStat] != int64(expectedRound) ||
		character.Stats[dnfjoust.KnightStat] != 1 ||
		character.Stats[dnfjoust.AmountStat] != 3 ||
		character.Stats[dnfjoust.PendingStat] != 1 {
		t.Fatalf("joust ledger=%v", character.Stats)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if inventory.Slots["0:7"].Count != 7 {
		t.Fatalf("source=%+v", inventory.Slots["0:7"])
	}
	foundReward := false
	for _, stack := range inventory.Slots {
		if stack.ItemID == dnfjoust.ParticipationItemID && stack.Count == 3 {
			foundReward = true
		}
	}
	if !foundReward {
		t.Fatalf("participation reward missing inventory=%v", inventory.Slots)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketJoustBetting) ||
		len(ack.Body) != 5 || ack.Body[0] != 1 || binary.LittleEndian.Uint32(ack.Body[1:]) != 0 {
		t.Fatalf("ack header=%+v body=%x", ack.Header, ack.Body)
	}
	refresh, rest := splitGameServerUpperPacket(t, rest)
	if refresh.Header.Classification != 0 ||
		refresh.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) {
		t.Fatalf("refresh header=%+v", refresh.Header)
	}
	roster, rest := splitGameServerUpperPacket(t, rest)
	if roster.Header.Classification != 0 || roster.Header.MsgID != currentJoustRosterPushMsgID || len(roster.Body) != 90 {
		t.Fatalf("roster header=%+v len=%d", roster.Header, len(roster.Body))
	}
	pool, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || pool.Header.Classification != 0 || pool.Header.MsgID != currentJoustPoolPushMsgID || len(pool.Body) != 46 {
		t.Fatalf("pool header=%+v len=%d trailing=%x", pool.Header, len(pool.Body), trailing)
	}
	if binary.LittleEndian.Uint16(roster.Body[:2]) != expectedRound || binary.LittleEndian.Uint16(pool.Body[:2]) != expectedRound {
		t.Fatalf("roster round=%d pool round=%d want=%d", binary.LittleEndian.Uint16(roster.Body[:2]), binary.LittleEndian.Uint16(pool.Body[:2]), expectedRound)
	}
	if personalSupport := binary.LittleEndian.Uint32(pool.Body[2:6]); personalSupport != 3 {
		t.Fatalf("pool personal support=%d want=3", personalSupport)
	}
	for index := 0; index < currentJoustRosterCount; index++ {
		record := roster.Body[2+index*currentJoustRosterRecordSize:]
		if multiplier := math.Float32frombits(binary.LittleEndian.Uint32(record[2:6])); multiplier <= 0 {
			t.Fatalf("roster[%d] multiplier=%v record=%x", index, multiplier, record[:currentJoustRosterRecordSize])
		}
	}
}

func TestCurrentJoustOpeningAggregatesEveryCharacterLedgerOnAccount(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	catalog := mustTestJoustEventCatalog(t)
	round := uint16(4321)
	base, err := catalog.OpeningWithLedgers(round, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, amount := range []int64{3, 7} {
		characterID := fmt.Sprintf("%d", 19+index)
		if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
			CharacterID: characterID,
			AccountID:   "account-1",
			Level:       90,
			Stats: map[string]int64{
				dnfjoust.RoundStat:   int64(round),
				dnfjoust.KnightStat:  int64(base.Riders[index].ID),
				dnfjoust.AmountStat:  amount,
				dnfjoust.PendingStat: int64(index % 2), // settled ledgers still belong to the public pool
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	queue := newFakeCurrentDungeonDeathTimerQueue()
	queue.now = time.Unix(7200*int64(round), 0).UTC().Add(10 * time.Minute)
	service := &Service{
		options:            options{accountID: "account-1"},
		joustCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		gameplayTimers:     queue,
	}
	opening, err := service.currentJoustOpening(ctx, &gameSession{selectedCharacterID: 19}, queue.Now())
	if err != nil {
		t.Fatal(err)
	}
	if opening.TotalSupport != base.TotalSupport+10 || opening.Riders[0].Support != base.Riders[0].Support+3 || opening.Riders[1].Support != base.Riders[1].Support+7 {
		t.Fatalf("base=%+v opening=%+v", base, opening)
	}
}

func TestCurrentJoustOpeningAggregatesSplitSupportForOneCharacter(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	catalog := mustTestJoustEventCatalog(t)
	round := uint16(4321)
	base, err := catalog.OpeningWithLedgers(round, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       90,
		Stats: map[string]int64{
			dnfjoust.RoundStat:                           int64(round),
			dnfjoust.KnightStat:                          int64(base.Riders[1].ID),
			dnfjoust.AmountStat:                          10,
			dnfjoust.KnightAmountStat(base.Riders[0].ID): 3,
			dnfjoust.KnightAmountStat(base.Riders[1].ID): 7,
			dnfjoust.PendingStat:                         1,
		},
	}); err != nil {
		t.Fatal(err)
	}
	queue := newFakeCurrentDungeonDeathTimerQueue()
	queue.now = time.Unix(7200*int64(round), 0).UTC().Add(10 * time.Minute)
	service := &Service{
		options:            options{accountID: "account-1"},
		joustCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		gameplayTimers:     queue,
	}
	opening, err := service.currentJoustOpening(ctx, &gameSession{selectedCharacterID: 19}, queue.Now())
	if err != nil || opening.TotalSupport != base.TotalSupport+10 ||
		opening.Riders[0].Support != base.Riders[0].Support+3 ||
		opening.Riders[1].Support != base.Riders[1].Support+7 {
		t.Fatalf("base=%+v opening=%+v err=%v", base, opening, err)
	}
}

func mustTestJoustCatalog(t *testing.T) *pvfDungeonDropCatalog {
	t.Helper()
	catalog, err := newPVFDungeonDropCatalog(dungeonDropCatalogTestSource{
		"monster/monster.lst":     "",
		"equipment/equipment.lst": "",
		"stackable/stackable.lst": "490005585 `490005001/chn_490005585.stk`\n490005593 `490005001/chn_490005593.stk`\n",
		"stackable/490005001/chn_490005585.stk": "[name] `竞猜硬币`\n" +
			"[stackable type] `[material]`\n[stack limit] 10000000\n[expiration date]\n`2028-08-16 06:00:00`\n",
		"stackable/490005001/chn_490005593.stk": "[name] `骑士马战爆竹`\n" +
			"[stackable type] `[random reward item]`\n[stack limit] 1000\n",
	})
	if err != nil {
		t.Fatalf("build joust catalog: %v", err)
	}
	return catalog
}

func mustTestJoustEventCatalog(t *testing.T) *dnfjoust.Catalog {
	t.Helper()
	catalog, err := dnfjoust.LoadCatalog(context.Background(), dungeonDropCatalogTestSource{
		dnfjoust.EventPVFPath: testCurrentJoustPVFText(),
	})
	if err != nil {
		t.Fatalf("build joust event catalog: %v", err)
	}
	return catalog
}

func testCurrentJoustPVFText() string {
	var text strings.Builder
	text.WriteString("[min level]\n50\n[max betting]\n10000\n[reward]\n490005585\n")
	text.WriteString("[betting reward]\n490005593\n[material]\n490005585 490005586\n[/material]\n[knight info]\n")
	attackTypes := [...]byte{1, 1, 0, 0, 1, 27, 27, 0, 1, 0, 28, 28}
	for index, attackType := range attackTypes {
		fmt.Fprintf(&text, "[knight]\n[index]\n%d\n[attack type]\n%d\n[knight name]\n`rider-%d`\n[win]\n", index, attackType, index)
		for value := 0; value < 28; value++ {
			fmt.Fprintf(&text, "%d ", []int{10, 25, 40, 55, 70, 85, 100}[value%7])
		}
		text.WriteString("\n[/win]\n[loss]\n")
		for value := 0; value < 28; value++ {
			fmt.Fprintf(&text, "%d ", []int{10, 25, 40, 55, 70, 85, 100}[value%7])
		}
		text.WriteString("\n[/loss]\n[/knight]\n")
	}
	text.WriteString("[/knight info]\n")
	return text.String()
}
