package dnfbridge

import (
	"encoding/binary"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfinventory "longheng.io/server/internal/modules/dnf/inventory"
)

func TestDamageFontPVFDefinitionsSupportAllExpirationModes(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":          "",
		"equipment/equipment.lst":      "",
		"stackable/stackable.lst":      "1 `font/period.stk`\n2 `font/fixed.stk`\n3 `font/unlimited.stk`\n",
		"stackable/font/period.stk":    damageFontTestDocument(2, "`[period]` 90"),
		"stackable/font/fixed.stk":     damageFontTestDocument(5000, "`[date]` `2028-01-02 03:04:05`"),
		"stackable/font/unlimited.stk": damageFontTestDocument(5001, "`[unlimit]`"),
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	period, err := catalog.ResolveItem(1)
	if err != nil || period.DamageFontIndex != 2 || period.DamageFontExpirationMode != alignedcmd.DamageFontExpirationPeriod || period.DamageFontPeriodDays != 90 {
		t.Fatalf("period = %+v err=%v", period, err)
	}
	fixed, err := catalog.ResolveItem(2)
	if err != nil || fixed.DamageFontIndex != 5000 || fixed.DamageFontExpirationMode != alignedcmd.DamageFontExpirationFixed || fixed.DamageFontFixedExpiration.IsZero() {
		t.Fatalf("fixed = %+v err=%v", fixed, err)
	}
	unlimited, err := catalog.ResolveItem(3)
	if err != nil || unlimited.DamageFontIndex != 5001 || unlimited.DamageFontExpirationMode != alignedcmd.DamageFontExpirationUnlimited {
		t.Fatalf("unlimited = %+v err=%v", unlimited, err)
	}
}

func damageFontTestDocument(index int, expiration string) string {
	return "[action type]\n`[add damage font skin]`\n[/action type]\n[damage font info]\n[index]\n" +
		strconv.Itoa(index) + "\n[expiration info]\n" + expiration + "\n[/damage font info]\n"
}

func TestBuildCurrentDamageFontSkinListBodyMatchesVerifiedNoti1239Layout(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	body := buildCurrentDamageFontSkinListBody(map[string]int64{
		dnfinventory.DamageFontSelectedStatKey:        5000,
		dnfinventory.DamageFontOwnershipStatKey(2):    0,
		dnfinventory.DamageFontOwnershipStatKey(5000): now.Unix() + 90,
	}, now)
	if len(body) != 20 {
		t.Fatalf("body len = %d, want 20", len(body))
	}
	if got := binary.LittleEndian.Uint16(body[0:2]); got != 5000 {
		t.Fatalf("selected = %d", got)
	}
	if got := binary.LittleEndian.Uint16(body[2:4]); got != 2 {
		t.Fatalf("count = %d", got)
	}
	if got := binary.LittleEndian.Uint32(body[4:8]); got != 2 {
		t.Fatalf("entry0 index = %d", got)
	}
	if got := binary.LittleEndian.Uint32(body[12:16]); got != 5000 {
		t.Fatalf("entry1 index = %d", got)
	}
}
