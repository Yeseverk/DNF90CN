package dnfbridge

import (
	"testing"
	"time"
)

func newMagicBoxTestCatalog(t *testing.T) (*pvfDungeonDropCatalog, dungeonDropCatalogTestSource) {
	t.Helper()
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                       "",
		"equipment/equipment.lst":                   "",
		"stackable/stackable.lst":                   "24 `cash/creature/creature_food.stk`\n10007368 `ect/chn_random/amazingbox.stk`\n10007367 `ect/chn_random/hammer.stk`\n2001 `booster/boosterbox.stk`\n2002 `other/plain.stk`\n2600014 `potion/ptn.stk`\n",
		"stackable/ect/chn_random/amazingbox.stk":   "[name] `Amazing Box`\n[stackable type] `[random upgradable legacy]`\n[RANDOMBOX]\n[int data]\n2600014 2 0 2600014 18000 2 0 2600014 5000 2 0 2600014 500 2 0 2600014 2 2 0\n[/int data]\n[int data]\n2682272 1 0 2682272 50000 1 0\n[/int data]\n[sealing removal item]\n2 10007367 1 10007368 1\n[/sealing removal item]\n[/RANDOMBOX]\n",
		"stackable/ect/chn_random/hammer.stk":       "[name] `Hammer`\n[stackable type] `[material]`\n",
		"stackable/booster/boosterbox.stk":          "[name] `Booster Box`\n[stackable type] `[booster]`\n[booster info]\n[etc]\n2 3001 100 1 3002 50 2\n[/etc]\n[/booster info]\n[need material] `10007367 1`\n",
		"stackable/other/plain.stk":                 "[name] `Plain`\n[stackable type] `[material]`\n",
		"stackable/potion/ptn.stk":                  "[name] `Potion`\n[stackable type] `[consumption]`\n[stack limit] 100\n[usable period] 30\n[attach type] `[sealing]`\n",
		"stackable/cash/creature/creature_food.stk": "[name] `Creature Food`\n[stackable type] `[feed]`\n[stack limit] 1000\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	return catalog, source
}

func TestResolveCurrentMagicBoxPetFeedTargetsPetConsumableSegment(t *testing.T) {
	catalog, source := newMagicBoxTestCatalog(t)
	item, err := resolveCurrentMagicBoxRewardItem(catalog, source, 24)
	if err != nil {
		t.Fatal(err)
	}
	if item.ItemID != 24 || item.TargetListType != currentPetInventoryListType ||
		item.SlotStart != currentCeraShopPetConsumableSlotStart ||
		item.SlotEnd != currentCeraShopPetConsumableSlotEnd ||
		item.StackLimit != 1000 {
		t.Fatalf("pet feed reward=%+v", item)
	}
}

func TestResolveCurrentMagicBoxRandomLegacy(t *testing.T) {
	catalog, source := newMagicBoxTestCatalog(t)
	resolution, err := resolveCurrentMagicBox(catalog, source, 10007368)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if resolution.Kind != "random" {
		t.Fatalf("kind = %q, want random", resolution.Kind)
	}
	if len(resolution.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(resolution.Groups))
	}
	first := resolution.Groups[0]
	if first.DrawCount != 1 || len(first.Entries) != 4 {
		t.Fatalf("group[0] = %+v, want 4 entries DrawCount 1", first)
	}
	if first.Entries[0].ItemID != 2600014 || first.Entries[0].Weight != 18000 || first.Entries[0].Count != 2 {
		t.Fatalf("group[0] entry[0] = %+v", first.Entries[0])
	}
	if first.Entries[3].Weight != 2 {
		t.Fatalf("group[0] entry[3] = %+v, want weight 2 preserved", first.Entries[3])
	}
	second := resolution.Groups[1]
	// Seven ints: exactly one quad entry after the skipped leading triple.
	if len(second.Entries) != 1 || second.Entries[0].ItemID != 2682272 || second.Entries[0].Weight != 50000 || second.Entries[0].Count != 1 {
		t.Fatalf("group[1] = %+v", second)
	}
	if resolution.MaterialItemID != 10007367 || resolution.MaterialCountPerUse != 1 {
		t.Fatalf("material = %d x%d, want 10007367 x1", resolution.MaterialItemID, resolution.MaterialCountPerUse)
	}
	if resolution.BoxPVFPath != "stackable/ect/chn_random/amazingbox.stk" {
		t.Fatalf("path = %q", resolution.BoxPVFPath)
	}
}

func TestResolveCurrentMagicBoxBoosterFamily(t *testing.T) {
	catalog, source := newMagicBoxTestCatalog(t)
	resolution, err := resolveCurrentMagicBox(catalog, source, 2001)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if resolution.Kind != "random" || len(resolution.Groups) != 1 {
		t.Fatalf("resolution = %+v", resolution)
	}
	group := resolution.Groups[0]
	if group.DrawCount != 2 {
		t.Fatalf("draw count = %d, want leading DrawCount 2", group.DrawCount)
	}
	if len(group.Entries) != 2 || group.Entries[0].ItemID != 3001 || group.Entries[0].Count != 1 || group.Entries[1].ItemID != 3002 || group.Entries[1].Count != 2 {
		t.Fatalf("entries = %+v", group.Entries)
	}
	if resolution.MaterialItemID != 10007367 || resolution.MaterialCountPerUse != 1 {
		t.Fatalf("material = %d x%d, want [need material] fallback", resolution.MaterialItemID, resolution.MaterialCountPerUse)
	}
}

func TestResolveCurrentMagicBoxUnsupportedAndUnknown(t *testing.T) {
	catalog, source := newMagicBoxTestCatalog(t)
	resolution, err := resolveCurrentMagicBox(catalog, source, 2002)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if resolution.Kind != "" {
		t.Fatalf("plain stackable kind = %q, want empty", resolution.Kind)
	}
	resolution, err = resolveCurrentMagicBox(catalog, source, 9999)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if resolution.Kind != "" {
		t.Fatalf("unknown item kind = %q, want empty", resolution.Kind)
	}
}

func TestResolveCurrentMagicBoxRewardItemMetadata(t *testing.T) {
	catalog, source := newMagicBoxTestCatalog(t)
	before := time.Now().UTC()
	item, err := resolveCurrentMagicBoxRewardItem(catalog, source, 2600014)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if item.ItemID != 2600014 || item.Kind != "stackable" || item.StackLimit != 100 || !item.Seal || item.UsablePeriodDays != 30 {
		t.Fatalf("reward item = %+v", item)
	}
	minExpire := time.Unix(before.Unix()+30*86400, 0).UTC()
	maxExpire := time.Unix(time.Now().UTC().Unix()+30*86400, 0).UTC()
	if item.ExpireAt.Before(minExpire) || item.ExpireAt.After(maxExpire) {
		t.Fatalf("reward expiration=%s outside %s..%s", item.ExpireAt, minExpire, maxExpire)
	}
	if item.SlotStart != 65 || item.SlotEnd != 120 {
		t.Fatalf("slot range = %d-%d, want 65-120", item.SlotStart, item.SlotEnd)
	}
	if item.PVFPath != "stackable/potion/ptn.stk" {
		t.Fatalf("path = %q", item.PVFPath)
	}
}
