package dnfbridge

import (
	"os"
	"strings"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealPVFHistoricalGiftPackagesArePurchasableAndResolvable(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify historical gift packages")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}

	type expected struct {
		price uint32
		group string
	}
	wanted := make(map[uint32]expected, 228)
	addRange := func(first, last, price uint32, group string) {
		for itemID := first; itemID <= last; itemID++ {
			wanted[itemID] = expected{price: price, group: group}
		}
	}
	addRange(490701336, 490701387, 19900, "2017_national_day_dream")
	addRange(490701388, 490701439, 33800, "2017_national_day_romance")
	addRange(490701876, 490701927, 19900, "2018_spring_guide")
	addRange(490701928, 490701979, 39900, "2018_spring_guardian")
	addRange(10007252, 10007261, 25900, "2014_sao_true")
	addRange(10007262, 10007271, 23900, "2014_sao")

	found := make(map[uint32]currentCeraShopProduct, len(wanted))
	minCommodityID := uint32(^uint32(0))
	var maxCommodityID uint32
	for _, product := range catalog.products {
		want, ok := wanted[product.ItemID]
		if !ok {
			continue
		}
		if product.Section != "package" || product.Count != 1 || product.CeraPrice != want.price {
			t.Fatalf("group=%s item=%d product=%+v", want.group, product.ItemID, product)
		}
		if previous, duplicate := found[product.ItemID]; duplicate {
			t.Fatalf("item=%d is listed by duplicate commodities %d and %d", product.ItemID, previous.CommodityID, product.CommodityID)
		}
		found[product.ItemID] = product
		if product.CommodityID < minCommodityID {
			minCommodityID = product.CommodityID
		}
		if product.CommodityID > maxCommodityID {
			maxCommodityID = product.CommodityID
		}
	}
	if len(found) != len(wanted) {
		t.Fatalf("historical package products=%d, want %d", len(found), len(wanted))
	}
	// 108514 already occurs elsewhere in the real Cera-shop script, so the
	// patcher skips it instead of aliasing a client-side commodity record.
	if minCommodityID != 108515 || maxCommodityID != 108742 {
		t.Fatalf("historical package commodity range=%d..%d, want collision-free package-domain range 108515..108742", minCommodityID, maxCommodityID)
	}

	now := time.Now().UTC()
	groups := make(map[string]int)
	for itemID, want := range wanted {
		definition, err := resolveCurrentCeraPackageDefinition(catalog.items, itemID)
		if err != nil {
			t.Fatalf("group=%s item=%d resolve Cera package: %v", want.group, itemID, err)
		}
		if len(definition.Rewards) == 0 || len(definition.AvatarItemIDs) == 0 {
			t.Fatalf("group=%s item=%d rewards=%d avatars=%d", want.group, itemID, len(definition.Rewards), len(definition.AvatarItemIDs))
		}
		if !definition.Source.ExpirationDate.IsZero() && !now.Before(definition.Source.ExpirationDate) {
			t.Fatalf("group=%s item=%d source expiration=%s", want.group, itemID, definition.Source.ExpirationDate)
		}
		if want.group == "2014_sao" || want.group == "2014_sao_true" {
			for _, reward := range definition.Rewards {
				if !reward.Definition.ExpirationDate.IsZero() && !now.Before(reward.Definition.ExpirationDate) {
					t.Fatalf("group=%s item=%d reward=%d expiration=%s", want.group, itemID, reward.Definition.ItemID, reward.Definition.ExpirationDate)
				}
			}
		}
		groups[want.group]++
	}
	for group, count := range map[string]int{
		"2017_national_day_dream":   52,
		"2017_national_day_romance": 52,
		"2018_spring_guide":         52,
		"2018_spring_guardian":      52,
		"2014_sao":                  10,
		"2014_sao_true":             10,
	} {
		if groups[group] != count {
			t.Fatalf("group=%s count=%d, want %d", group, groups[group], count)
		}
	}
}

func TestRealPVFNationalDayAuraRewardsGrantWithDefaultSockets(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify National Day aura sockets")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatalf("load real item catalog: %v", err)
	}

	booster, err := resolveCurrentBoosterDefinition(catalog, 490701319, currentBoosterRequestSelection)
	if err != nil {
		t.Fatalf("resolve National Day aura box: %v", err)
	}
	auraItems := make(map[uint32][]byte)
	for _, candidate := range booster.Selection {
		definition, resolveErr := catalog.ResolveItem(candidate.ItemID)
		if resolveErr != nil {
			t.Fatalf("resolve National Day aura candidate=%d: %v", candidate.ItemID, resolveErr)
		}
		document, readErr := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
		if readErr != nil {
			t.Fatalf("read National Day aura candidate=%d: %v", candidate.ItemID, readErr)
		}
		name, _ := document.Text("name")
		equipmentType, _ := document.Text("equipment type")
		t.Logf("candidate=%d name=%q kind=%s equipment_type=%q path=%s", candidate.ItemID, strings.TrimSpace(name), definition.Kind, equipmentType, definition.PVFPath)
		if normalizeEquipmentPlacementPVFType(equipmentType) != "[aurora avatar]" {
			continue
		}
		text, readErr := catalog.source.ReadText(definition.PVFPath)
		if readErr != nil {
			t.Fatalf("read National Day aura=%d text: %v", candidate.ItemID, readErr)
		}
		socketTypes := currentParseAvatarDefaultSocketTypes(text)
		if len(socketTypes) == 0 {
			t.Fatalf("aura=%d path=%s has no PVF default sockets", candidate.ItemID, definition.PVFPath)
		}
		placement, placementErr := currentBoosterPlacement(definition)
		if placementErr != nil || placement != currentBoosterRewardAvatar {
			t.Fatalf("aura=%d booster placement=%d err=%v", candidate.ItemID, placement, placementErr)
		}
		inventory := dnfrepo.InventoryRecord{CharacterID: "19", Slots: make(map[string]dnfrepo.ItemStack)}
		keys, grantErr := grantCurrentBoosterReward(
			&inventory,
			catalog.source,
			currentBoosterReward{
				Definition: definition,
				Count:      candidate.Count,
				Option:     candidate.Option,
				Placement:  placement,
			},
		)
		if grantErr != nil {
			t.Fatalf("grant National Day aura=%d: %v", candidate.ItemID, grantErr)
		}
		if len(keys) != 1 {
			t.Fatalf("aura=%d granted keys=%v", candidate.ItemID, keys)
		}
		granted := inventory.Slots[keys[0]]
		grantedSockets := currentAvatarSocketData(granted.Extra)
		if currentAvatarSocketOpenCount(grantedSockets) != len(socketTypes) {
			t.Fatalf("aura=%d granted sockets=%x want_types=%x", candidate.ItemID, grantedSockets, socketTypes)
		}
		for index, want := range socketTypes {
			if got := currentAvatarSocketType(grantedSockets, byte(index)); got != want {
				t.Fatalf("aura=%d granted socket[%d]=0x%02x want=0x%02x", candidate.ItemID, index, got, want)
			}
		}
		t.Logf("aura=%d name=%q sockets=%x socket_num=%d", candidate.ItemID, strings.TrimSpace(name), socketTypes, currentResolveAvatarSocketNum(text))
		auraItems[candidate.ItemID] = append([]byte(nil), socketTypes...)
	}
	if len(auraItems) != 2 || len(auraItems[101590068]) != 2 || len(auraItems[101590069]) != 2 {
		t.Fatalf("National Day aura defaults=%v", auraItems)
	}
}
