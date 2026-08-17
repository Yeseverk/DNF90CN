package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseCurrentCeraShopMultiBonusReadsRepeatedBonusItemBlocks(t *testing.T) {
	source := bridgePVFSource{
		"etc/chn_cerashop_multibonus.etc": "[one plus one bonus item info ipg]\n" +
			"[bonus item]\n102925 10007368 1 10007367 1\n[/bonus item]\n" +
			"[bonus item]\n102930 10007368 100 10007367 100\n[/bonus item]\n" +
			"[bonus item]\n104426 490701730 100\n[/bonus item]\n" +
			"[/one plus one bonus item info ipg]\n",
	}
	bonusByCommodity, err := parseCurrentCeraShopMultiBonus(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(bonusByCommodity) != 3 {
		t.Fatalf("bonus map=%+v", bonusByCommodity)
	}
	bonuses := bonusByCommodity[102930]
	if len(bonuses) != 2 || bonuses[0] != (currentCeraShopBonusItem{ItemID: 10007368, Count: 100}) ||
		bonuses[1] != (currentCeraShopBonusItem{ItemID: 10007367, Count: 100}) {
		t.Fatalf("commodity 102930 bonuses=%+v", bonuses)
	}
	single := bonusByCommodity[104426]
	if len(single) != 1 || single[0] != (currentCeraShopBonusItem{ItemID: 490701730, Count: 100}) {
		t.Fatalf("commodity 104426 bonuses=%+v", single)
	}
}

func TestParseCurrentCeraShopMultiBonusToleratesMissingDocument(t *testing.T) {
	bonusByCommodity, err := parseCurrentCeraShopMultiBonus(bridgePVFSource{})
	if err != nil || len(bonusByCommodity) != 0 {
		t.Fatalf("missing document bonus=%+v err=%v", bonusByCommodity, err)
	}
}

func TestParseCurrentCeraShopMultiBonusRejectsMalformedRows(t *testing.T) {
	for name, document := range map[string]string{
		"even value count": "[bonus item]\n102930 10007368\n[/bonus item]\n",
		"zero item":        "[bonus item]\n102930 0 100\n[/bonus item]\n",
		"zero count":       "[bonus item]\n102930 10007368 0\n[/bonus item]\n",
		"zero commodity":   "[bonus item]\n0 10007368 100\n[/bonus item]\n",
		"duplicate":        "[bonus item]\n102930 10007368 1\n[/bonus item]\n[bonus item]\n102930 10007368 2\n[/bonus item]\n",
	} {
		source := bridgePVFSource{"etc/chn_cerashop_multibonus.etc": document}
		if _, err := parseCurrentCeraShopMultiBonus(source); err == nil {
			t.Fatalf("%s: malformed document accepted", name)
		}
	}
}

func mustCurrentCeraShopMultiBonusTestCatalog(t *testing.T) *pvfCeraShopCatalog {
	t.Helper()
	source := bridgePVFSource{
		"etc/cerashop.etc": "[item]\n100050 37 3 0 0 80 `test potion` 0 0\n[/item]\n" +
			"[premium]\n[/premium]\n[creature]\n[/creature]\n[coin]\n[/coin]\n[material]\n[/material]\n[recoveryitem]\n[/recoveryitem]\n",
		"etc/chn_cerashop_multibonus.etc": "[one plus one bonus item info ipg]\n" +
			"[bonus item]\n100050 45 2 46 1\n[/bonus item]\n" +
			"[/one plus one bonus item info ipg]\n",
		"monster/monster.lst":                  "",
		"stackable/stackable.lst":              "37 `test_potion.stk`\n45 `cash/test_bonus_box.stk`\n46 `cash/test_bonus_hammer.stk`\n",
		"equipment/equipment.lst":              "",
		"stackable/test_potion.stk":            "[stackable type]\n`[waste]`\n[stack limit]\n1000\n",
		"stackable/cash/test_bonus_box.stk":    "[stackable type]\n`[etc]`\n[stack limit]\n1000\n",
		"stackable/cash/test_bonus_hammer.stk": "[stackable type]\n`[etc]`\n[stack limit]\n1000\n",
	}
	catalog, err := newPVFCeraShopCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.bonusByCommodity[100050]) != 2 {
		t.Fatalf("catalog bonus=%+v", catalog.bonusByCommodity)
	}
	return catalog
}

func TestCurrentCeraShopPurchaseGrantsOnePlusOneBonusItems(t *testing.T) {
	catalog := mustCurrentCeraShopMultiBonusTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 200)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1", Stats: map[string]int64{"cera": 200}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 19}, currentCeraShopTestRequestBody(100050)); err != nil {
		t.Fatal(err)
	}

	ack, _ := splitGameServerUpperPacket(t, connection.write.Bytes())
	wantAck := buildCurrentCeraShopPurchaseSuccessBodyWithCount(100050, 3,
		currentCeraShopBonusItem{ItemID: 45, Count: 2},
		currentCeraShopBonusItem{ItemID: 46, Count: 1},
	)
	if !bytes.Equal(ack.Body, wantAck) {
		t.Fatalf("ack body=%x want=%x", ack.Body, wantAck)
	}
	// The bonus rows sit behind the u16 row count at the ack tail.
	tail := ack.Body[len(ack.Body)-18:]
	if binary.LittleEndian.Uint16(tail[0:2]) != 2 ||
		binary.LittleEndian.Uint32(tail[2:6]) != 45 || binary.LittleEndian.Uint32(tail[6:10]) != 2 ||
		binary.LittleEndian.Uint32(tail[10:14]) != 46 || binary.LittleEndian.Uint32(tail[14:18]) != 1 {
		t.Fatalf("ack bonus rows=%x", tail)
	}

	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 120 {
		t.Fatalf("account cera=%d, want 120", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	counts := make(map[int64]int64)
	bonusMarked := make(map[int64]string)
	for _, stack := range inventory.Slots {
		counts[stack.ItemID] += stack.Count
		bonusMarked[stack.ItemID] = stack.Extra["last_cera_shop_bonus"]
	}
	if counts[37] != 3 || counts[45] != 2 || counts[46] != 1 {
		t.Fatalf("inventory item counts=%+v slots=%+v", counts, inventory.Slots)
	}
	if bonusMarked[45] != "one_plus_one_ipg" || bonusMarked[46] != "one_plus_one_ipg" || bonusMarked[37] != "" {
		t.Fatalf("bonus provenance=%+v", bonusMarked)
	}
}

func TestRealPVFCeraShopMultiBonusMatchesObservedLoveLetterRows(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real multi-bonus parsing")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}
	for commodity, want := range map[uint32][]currentCeraShopBonusItem{
		102925: {{ItemID: 10007368, Count: 1}, {ItemID: 10007367, Count: 1}},
		102929: {{ItemID: 10007368, Count: 60}, {ItemID: 10007367, Count: 60}},
		102930: {{ItemID: 10007368, Count: 100}, {ItemID: 10007367, Count: 100}},
	} {
		got := catalog.bonusByCommodity[commodity]
		if len(got) != len(want) {
			t.Fatalf("commodity=%d bonuses=%+v want=%+v", commodity, got, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("commodity=%d bonuses=%+v want=%+v", commodity, got, want)
			}
		}
		if _, err := catalog.items.ResolveItem(want[0].ItemID); err != nil {
			t.Fatalf("commodity=%d bonus item=%d unresolvable: %v", commodity, want[0].ItemID, err)
		}
	}
}
