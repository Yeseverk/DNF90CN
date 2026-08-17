package inventory

import (
	"bytes"
	"context"
	"encoding/binary"
	"reflect"
	"strconv"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestHandlerWearRoundTripKeepsOrdinaryEquipmentEndpointsNative(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	const (
		characterID uint16 = 701
		bagSlot     int16  = 31
		wornSlot    int16  = 12
		itemID      int64  = 101000069
	)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "701",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, bagSlot): {
				ItemID:   itemID,
				Count:    1,
				RawEntry: make([]byte, currentItemListEntrySize),
			},
		},
	})
	saveWearRoundTripEquipment(t, ctx, repos, "701")
	placement := func(context.Context, alignedcmd.EquipmentPlacementRequest) error { return nil }
	wearHandler := NewHandler()
	wantWearRefresh := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeMain, bagSlot, int32(itemID),
		listTypeEquipment, wornSlot, 0,
		placement, wantWearRefresh)
	requireWearRoundTripInventoryItem(t, ctx, repos, "701", listTypeMain, bagSlot, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "701", wornSlot, itemID, true)

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeMain, bagSlot, 0,
		listTypeEquipment, wornSlot, int32(itemID),
		placement, wantWearRefresh)
	requireWearRoundTripInventoryItem(t, ctx, repos, "701", listTypeMain, bagSlot, itemID, true)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "701", wornSlot, 0, false)

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeMain, bagSlot, int32(itemID),
		listTypeEquipment, wornSlot, 0,
		placement, wantWearRefresh)
	requireWearRoundTripInventoryItem(t, ctx, repos, "701", listTypeMain, bagSlot, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "701", wornSlot, itemID, true)
}

func TestHandlerConsecutiveUnequipRedirectsStaleClientDestinationToNextEmptySlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	const (
		characterID uint16 = 707
		firstBag    int16  = 31
		secondBag   int16  = 32
		firstWorn   int16  = 12
		secondWorn  int16  = 13
		firstItem   int64  = 101000069
		secondItem  int64  = 101000070
	)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "707",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, firstBag): {
				ItemID:   firstItem,
				Count:    1,
				RawEntry: make([]byte, currentItemListEntrySize),
			},
			slotKey(listTypeMain, secondBag): {
				ItemID:   secondItem,
				Count:    1,
				RawEntry: make([]byte, currentItemListEntrySize),
			},
		},
	})
	saveWearRoundTripEquipment(t, ctx, repos, "707")
	placement := func(context.Context, alignedcmd.EquipmentPlacementRequest) error { return nil }
	wearHandler := NewHandler()
	wantWearRefresh := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeMain, firstBag, int32(firstItem),
		listTypeEquipment, firstWorn, 0,
		placement, wantWearRefresh)
	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeMain, secondBag, int32(secondItem),
		listTypeEquipment, secondWorn, 0,
		placement, wantWearRefresh)

	// The client still reports slot 31 as empty for both clicks. The first
	// unequip commits there; the second must inspect durable occupancy and
	// redirect to slot 32 instead of failing or overwriting the first item.
	firstACK := runCurrentEXEUnequipWithStaleBagSlot(
		t, ctx, wearHandler, repos, characterID,
		listTypeMain, firstBag, listTypeEquipment, firstWorn, int32(firstItem),
		placement, wantWearRefresh,
	)
	secondACK := runCurrentEXEUnequipWithStaleBagSlot(
		t, ctx, wearHandler, repos, characterID,
		listTypeMain, firstBag, listTypeEquipment, secondWorn, int32(secondItem),
		placement, wantWearRefresh,
	)
	if got := int16(binary.LittleEndian.Uint16(firstACK[2:4])); got != firstBag {
		t.Fatalf("first unequip ACK main slot=%d, want %d", got, firstBag)
	}
	if got := int16(binary.LittleEndian.Uint16(secondACK[2:4])); got != secondBag {
		t.Fatalf("second unequip ACK main slot=%d, want redirected %d", got, secondBag)
	}
	requireWearRoundTripInventoryItem(t, ctx, repos, "707", listTypeMain, firstBag, firstItem, true)
	requireWearRoundTripInventoryItem(t, ctx, repos, "707", listTypeMain, secondBag, secondItem, true)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "707", firstWorn, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "707", secondWorn, 0, false)
}

func TestHandlerConsecutiveAvatarUnequipRedirectsWithinAvatarPage(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	const (
		characterID uint16 = 708
		firstBag    int16  = 2
		secondBag   int16  = 3
		firstWorn   int16  = 3
		secondWorn  int16  = 4
		firstItem   int64  = 112520227
		secondItem  int64  = 112530222
	)
	avatarStack := func(itemID int64) dnfrepo.ItemStack {
		return dnfrepo.ItemStack{
			ItemID:   itemID,
			Count:    0,
			RawEntry: make([]byte, currentItemListEntrySize),
			Extra: map[string]string{
				"item_kind":       "avatar",
				"amount_or_count": "0",
			},
		}
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "708",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeAvatar, firstBag):  avatarStack(firstItem),
			slotKey(listTypeAvatar, secondBag): avatarStack(secondItem),
		},
	})
	saveWearRoundTripEquipment(t, ctx, repos, "708")
	placement := func(context.Context, alignedcmd.EquipmentPlacementRequest) error { return nil }
	wearHandler := NewHandler()
	wantWearRefresh := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeAvatar, firstBag, int32(firstItem),
		listTypeActorWornAlt, firstWorn, 0,
		placement, wantWearRefresh)
	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeAvatar, secondBag, int32(secondItem),
		listTypeActorWornAlt, secondWorn, 0,
		placement, wantWearRefresh)

	firstACK := runCurrentEXEUnequipWithStaleBagSlot(
		t, ctx, wearHandler, repos, characterID,
		listTypeAvatar, firstBag, listTypeActorWornAlt, firstWorn, int32(firstItem),
		placement, wantWearRefresh,
	)
	secondACK := runCurrentEXEUnequipWithStaleBagSlot(
		t, ctx, wearHandler, repos, characterID,
		listTypeAvatar, firstBag, listTypeActorWornAlt, secondWorn, int32(secondItem),
		placement, wantWearRefresh,
	)
	if got := int16(binary.LittleEndian.Uint16(firstACK[2:4])); got != firstBag {
		t.Fatalf("first avatar unequip ACK slot=%d, want %d", got, firstBag)
	}
	if got := int16(binary.LittleEndian.Uint16(secondACK[2:4])); got != secondBag {
		t.Fatalf("second avatar unequip ACK slot=%d, want redirected %d", got, secondBag)
	}
	first := requireWearRoundTripInventoryItem(t, ctx, repos, "708", listTypeAvatar, firstBag, firstItem, true)
	second := requireWearRoundTripInventoryItem(t, ctx, repos, "708", listTypeAvatar, secondBag, secondItem, true)
	if first.Count != 0 || second.Count != 0 {
		t.Fatalf("redirected avatars lost permanent-item amount first=%+v second=%+v", first, second)
	}
	requireWearRoundTripEquipmentItem(t, ctx, repos, "708", firstWorn, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "708", secondWorn, 0, false)
}

func TestHandlerWearRoundTripKeepsAvatarAndActorEndpoint17Native(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	const (
		characterID uint16 = 702
		avatarSlot  int16  = 2
		wornSlot    int16  = 3
		itemID      int64  = 112520227
	)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "702",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeAvatar, avatarSlot): {
				ItemID:   itemID,
				Count:    0,
				RawEntry: make([]byte, currentItemListEntrySize),
				Extra: map[string]string{
					"item_kind":       "avatar",
					"amount_or_count": "0",
				},
			},
		},
	})
	saveWearRoundTripEquipment(t, ctx, repos, "702")
	placement := func(context.Context, alignedcmd.EquipmentPlacementRequest) error { return nil }
	wearHandler := NewHandler()
	wantWearRefresh := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeAvatar, avatarSlot, int32(itemID),
		listTypeActorWornAlt, wornSlot, 0,
		placement, wantWearRefresh)
	requireWearRoundTripInventoryItem(t, ctx, repos, "702", listTypeAvatar, avatarSlot, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "702", wornSlot, itemID, true)

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeAvatar, avatarSlot, 0,
		listTypeActorWornAlt, wornSlot, int32(itemID),
		placement, wantWearRefresh)
	avatar := requireWearRoundTripInventoryItem(t, ctx, repos, "702", listTypeAvatar, avatarSlot, itemID, true)
	if avatar.Count != 0 || avatar.Extra["amount_or_count"] != "0" {
		t.Fatalf("unequipped avatar lost permanent-item amount: %+v", avatar)
	}
	requireWearRoundTripEquipmentItem(t, ctx, repos, "702", wornSlot, 0, false)

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypeAvatar, avatarSlot, int32(itemID),
		listTypeActorWornAlt, wornSlot, 0,
		placement, wantWearRefresh)
	requireWearRoundTripInventoryItem(t, ctx, repos, "702", listTypeAvatar, avatarSlot, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "702", wornSlot, itemID, true)
}

func TestHandlerWearRoundTripKeepsCreatureSlot26Native(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	const (
		characterID uint16 = 703
		petSlot     int16  = 24
		wornSlot    int16  = 26
		itemID      int64  = 400990199
		creatureKey int32  = 37
	)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "703",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, petSlot): {
				ItemID:   itemID,
				Count:    1,
				RawEntry: make([]byte, currentItemListEntrySize),
				Extra: map[string]string{
					"creature_serial_or_handle": "37",
					"creature_key":              "37",
					"item_kind":                 "equipment",
					"equipment_type":            "[creature]",
				},
			},
		},
	})
	saveWearRoundTripEquipment(t, ctx, repos, "703")
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "703",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     uint32(creatureKey),
				ItemID:          itemID,
				SourceListType:  listTypePet,
				SourceSlotIndex: petSlot,
				Level:           1,
				Satiety:         100,
			},
		},
	}); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}
	wearHandler := NewHandler()
	wantCreatureState := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedCreatureState,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypePet, petSlot, creatureKey,
		listTypeActorWornAlt, wornSlot, 0,
		nil, wantCreatureState)
	requireWearRoundTripInventoryItem(t, ctx, repos, "703", listTypePet, petSlot, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "703", wornSlot, itemID, true)
	requireWearRoundTripPetState(t, ctx, repos, "703", "37", listTypeEquipment, wornSlot, true)

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypePet, petSlot, 0,
		listTypeActorWornAlt, wornSlot, creatureKey,
		nil, wantCreatureState)
	requireWearRoundTripInventoryItem(t, ctx, repos, "703", listTypePet, petSlot, itemID, true)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "703", wornSlot, 0, false)
	requireWearRoundTripPetState(t, ctx, repos, "703", "37", listTypePet, petSlot, false)

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypePet, petSlot, creatureKey,
		listTypeActorWornAlt, wornSlot, 0,
		nil, wantCreatureState)
	requireWearRoundTripInventoryItem(t, ctx, repos, "703", listTypePet, petSlot, 0, false)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "703", wornSlot, itemID, true)
	requireWearRoundTripPetState(t, ctx, repos, "703", "37", listTypeEquipment, wornSlot, true)
}

func TestHandlerLaterCreatureUnequipRedirectsWithinPetBodyPage(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	const (
		characterID uint16 = 709
		firstBag    int16  = 24
		secondBag   int16  = 25
		wornSlot    int16  = 26
		firstItem   int64  = 400990199
		secondItem  int64  = 400990200
		firstKey    int32  = 37
		secondKey   int32  = 38
	)
	petStack := func(itemID int64, key int32) dnfrepo.ItemStack {
		return dnfrepo.ItemStack{
			ItemID:   itemID,
			Count:    1,
			RawEntry: make([]byte, currentItemListEntrySize),
			Extra: map[string]string{
				"creature_serial_or_handle": strconv.Itoa(int(key)),
				"creature_key":              strconv.Itoa(int(key)),
				"item_kind":                 "equipment",
				"equipment_type":            "[creature]",
			},
		}
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "709",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, firstBag):  petStack(firstItem, firstKey),
			slotKey(listTypePet, secondBag): petStack(secondItem, secondKey),
		},
	})
	saveWearRoundTripEquipment(t, ctx, repos, "709")
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "709",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     uint32(firstKey),
				ItemID:          firstItem,
				SourceListType:  listTypePet,
				SourceSlotIndex: firstBag,
				Level:           1,
				Satiety:         100,
			},
			"38": {
				PetKey:          "38",
				CreatureKey:     uint32(secondKey),
				ItemID:          secondItem,
				SourceListType:  listTypePet,
				SourceSlotIndex: secondBag,
				Level:           1,
				Satiety:         100,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	wearHandler := NewHandler()
	wantCreatureState := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedCreatureState,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypePet, firstBag, firstKey,
		listTypeActorWornAlt, wornSlot, 0,
		nil, wantCreatureState)
	firstACK := runCurrentEXEUnequipWithStaleBagSlot(
		t, ctx, wearHandler, repos, characterID,
		listTypePet, firstBag, listTypeActorWornAlt, wornSlot, firstKey,
		nil, wantCreatureState,
	)
	if got := int16(binary.LittleEndian.Uint16(firstACK[2:4])); got != firstBag {
		t.Fatalf("first creature unequip ACK slot=%d, want %d", got, firstBag)
	}

	runCurrentEXEWearStep(t, ctx, wearHandler, repos, characterID,
		listTypePet, secondBag, secondKey,
		listTypeActorWornAlt, wornSlot, 0,
		nil, wantCreatureState)
	secondACK := runCurrentEXEUnequipWithStaleBagSlot(
		t, ctx, wearHandler, repos, characterID,
		listTypePet, firstBag, listTypeActorWornAlt, wornSlot, secondKey,
		nil, wantCreatureState,
	)
	if got := int16(binary.LittleEndian.Uint16(secondACK[2:4])); got != secondBag {
		t.Fatalf("second creature unequip ACK slot=%d, want redirected %d", got, secondBag)
	}
	requireWearRoundTripInventoryItem(t, ctx, repos, "709", listTypePet, firstBag, firstItem, true)
	requireWearRoundTripInventoryItem(t, ctx, repos, "709", listTypePet, secondBag, secondItem, true)
	requireWearRoundTripEquipmentItem(t, ctx, repos, "709", wornSlot, 0, false)
	requireWearRoundTripPetState(t, ctx, repos, "709", "37", listTypePet, firstBag, false)
	requireWearRoundTripPetState(t, ctx, repos, "709", "38", listTypePet, secondBag, false)
}

func TestHandlerWearRoundTripKeepsAllPetArtifactSlotsNative(t *testing.T) {
	tests := []struct {
		name       string
		character  uint16
		sourceSlot int16
		wornList   byte
		wornSlot   int16
		itemID     int64
		kind       string
	}{
		{name: "red_slot27_list3", character: 704, sourceSlot: 140, wornList: listTypeEquipment, wornSlot: 27, itemID: 2747155, kind: "red"},
		{name: "blue_slot28_list17", character: 705, sourceSlot: 141, wornList: listTypeActorWornAlt, wornSlot: 28, itemID: 2747156, kind: "blue"},
		{name: "green_slot29_list17", character: 706, sourceSlot: 142, wornList: listTypeActorWornAlt, wornSlot: 29, itemID: 2747157, kind: "green"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			characterID := strconv.Itoa(int(test.character))
			saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
				CharacterID: characterID,
				Slots: map[string]dnfrepo.ItemStack{
					slotKey(listTypePet, test.sourceSlot): {
						ItemID:   test.itemID,
						Count:    1,
						RawEntry: make([]byte, currentItemListEntrySize),
						Extra: map[string]string{
							"item_kind": "equipment",
						},
					},
				},
			})
			saveWearRoundTripEquipment(t, ctx, repos, characterID)
			if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{CharacterID: characterID}); err != nil {
				t.Fatalf("Save pet error = %v", err)
			}
			placement := func(context.Context, alignedcmd.EquipmentPlacementRequest) error { return nil }
			wearHandler := NewHandler()
			wantArtifactRefresh := []alignedcmd.PostAction{
				alignedcmd.PostActionRefreshSelectedItemContainers,
			}

			runCurrentEXEWearStep(t, ctx, wearHandler, repos, test.character,
				listTypePet, test.sourceSlot, int32(test.itemID),
				test.wornList, test.wornSlot, 0,
				placement, wantArtifactRefresh)
			requireWearRoundTripInventoryItem(t, ctx, repos, characterID, listTypePet, test.sourceSlot, 0, false)
			requireWearRoundTripEquipmentItem(t, ctx, repos, characterID, test.wornSlot, test.itemID, true)
			requireWearRoundTripArtifactState(t, ctx, repos, characterID, test.kind, test.itemID, true)

			runCurrentEXEWearStep(t, ctx, wearHandler, repos, test.character,
				listTypePet, test.sourceSlot, 0,
				test.wornList, test.wornSlot, int32(test.itemID),
				placement, wantArtifactRefresh)
			requireWearRoundTripInventoryItem(t, ctx, repos, characterID, listTypePet, test.sourceSlot, test.itemID, true)
			requireWearRoundTripEquipmentItem(t, ctx, repos, characterID, test.wornSlot, 0, false)
			requireWearRoundTripArtifactState(t, ctx, repos, characterID, test.kind, 0, false)

			runCurrentEXEWearStep(t, ctx, wearHandler, repos, test.character,
				listTypePet, test.sourceSlot, int32(test.itemID),
				test.wornList, test.wornSlot, 0,
				placement, wantArtifactRefresh)
			requireWearRoundTripInventoryItem(t, ctx, repos, characterID, listTypePet, test.sourceSlot, 0, false)
			requireWearRoundTripEquipmentItem(t, ctx, repos, characterID, test.wornSlot, test.itemID, true)
			requireWearRoundTripArtifactState(t, ctx, repos, characterID, test.kind, test.itemID, true)
		})
	}
}

func runCurrentEXEUnequipWithStaleBagSlot(
	t *testing.T,
	ctx context.Context,
	wearHandler alignedcmd.Handler,
	repos dnfrepo.Group,
	characterID uint16,
	inventoryList byte,
	staleBagSlot int16,
	wornList byte,
	wornSlot int16,
	wornInstance int32,
	placement alignedcmd.EquipmentPlacementValidator,
	wantPostActions []alignedcmd.PostAction,
) []byte {
	t.Helper()
	result, err := wearHandler.Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketMoveItemspace),
		Body: currentMoveItemspaceBody(
			inventoryList,
			staleBagSlot,
			0,
			1,
			wornList,
			wornSlot,
			wornInstance,
			0,
			-1,
		),
		SelectedCharacterID: characterID,
		Repositories:        repos,
		EquipmentPlacement:  placement,
	})
	if err != nil {
		t.Fatalf("Handle consecutive unequip error = %v", err)
	}
	if !result.Handled || !result.ResponseAllowed || len(result.UpperResponses) != 1 {
		t.Fatalf("consecutive unequip result = %+v", result)
	}
	response := result.UpperResponses[0]
	if len(response.Body) != 11 || response.Body[0] != 1 ||
		response.Body[1] != inventoryList ||
		response.Body[8] != wornList ||
		int16(binary.LittleEndian.Uint16(response.Body[9:11])) != wornSlot {
		t.Fatalf("consecutive unequip ACK = % X", response.Body)
	}
	if !reflect.DeepEqual(result.PostActions, wantPostActions) {
		t.Fatalf("consecutive unequip post actions = %v, want %v", result.PostActions, wantPostActions)
	}
	return append([]byte(nil), response.Body...)
}

func runCurrentEXEWearStep(
	t *testing.T,
	ctx context.Context,
	wearHandler alignedcmd.Handler,
	repos dnfrepo.Group,
	characterID uint16,
	sourceList byte,
	sourceSlot int16,
	sourceInstance int32,
	wornList byte,
	wornSlot int16,
	wornInstance int32,
	placement alignedcmd.EquipmentPlacementValidator,
	wantPostActions []alignedcmd.PostAction,
) {
	t.Helper()
	result, err := wearHandler.Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(sourceList, sourceSlot, sourceInstance, 1, wornList, wornSlot, wornInstance, 0, -1),
		SelectedCharacterID: characterID,
		Repositories:        repos,
		EquipmentPlacement:  placement,
	})
	if err != nil {
		t.Fatalf("Handle wear step error = %v", err)
	}
	if !result.Handled || !result.ResponseAllowed || result.Operation != "move_itemspace" {
		t.Fatalf("wear step result = %+v", result)
	}
	if len(result.UpperResponses) != 1 {
		t.Fatalf("wear step responses = %+v", result.UpperResponses)
	}
	response := result.UpperResponses[0]
	if response.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) ||
		response.Classification != dnfproto.DefaultChannelClassification ||
		!response.AllowCodec {
		t.Fatalf("wear step response metadata = %+v", response)
	}
	wantACK := make([]byte, 11)
	wantACK[0] = 1
	wantACK[1] = sourceList
	binary.LittleEndian.PutUint16(wantACK[2:4], uint16(sourceSlot))
	binary.LittleEndian.PutUint32(wantACK[4:8], 1)
	wantACK[8] = wornList
	binary.LittleEndian.PutUint16(wantACK[9:11], uint16(wornSlot))
	if !bytes.Equal(response.Body, wantACK) {
		t.Fatalf("wear step ACK = % X, want % X", response.Body, wantACK)
	}
	if len(result.ItemSlotRefreshes) != 0 {
		t.Fatalf("wear step emitted duplicate item slot refreshes = %v", result.ItemSlotRefreshes)
	}
	if !reflect.DeepEqual(result.PostActions, wantPostActions) {
		t.Fatalf("wear step post actions = %v, want %v", result.PostActions, wantPostActions)
	}
}

func saveWearRoundTripEquipment(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID string) {
	t.Helper()
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: characterID,
		Entries:     map[string]dnfrepo.EquipmentEntry{},
	}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}
}

func requireWearRoundTripInventoryItem(
	t *testing.T,
	ctx context.Context,
	repos dnfrepo.Group,
	characterID string,
	listType byte,
	slot int16,
	itemID int64,
	want bool,
) dnfrepo.ItemStack {
	t.Helper()
	stack, found := loadTestInventory(t, ctx, repos, characterID).Slots[slotKey(listType, slot)]
	if found != want {
		t.Fatalf("inventory (%d,%d) found = %t, want %t; stack=%+v", listType, slot, found, want, stack)
	}
	if want && stack.ItemID != itemID {
		t.Fatalf("inventory (%d,%d) item = %d, want %d; stack=%+v", listType, slot, stack.ItemID, itemID, stack)
	}
	return stack
}

func requireWearRoundTripEquipmentItem(
	t *testing.T,
	ctx context.Context,
	repos dnfrepo.Group,
	characterID string,
	slot int16,
	itemID int64,
	want bool,
) dnfrepo.EquipmentEntry {
	t.Helper()
	record, found, err := repos.Equipment.Load(ctx, characterID)
	if err != nil || !found {
		t.Fatalf("Load equipment found=%t err=%v", found, err)
	}
	entry, occupied := record.Entries[wearRoundTripEquipmentKey(slot)]
	if occupied != want {
		t.Fatalf("equipment slot %d occupied = %t, want %t; entry=%+v", slot, occupied, want, entry)
	}
	if want && (entry.ItemID != itemID || entry.SlotIndex != slot) {
		t.Fatalf("equipment slot %d entry = %+v, want item %d", slot, entry, itemID)
	}
	return entry
}

func requireWearRoundTripPetState(
	t *testing.T,
	ctx context.Context,
	repos dnfrepo.Group,
	characterID string,
	petKey string,
	listType byte,
	slot int16,
	equipped bool,
) {
	t.Helper()
	record, found, err := repos.Pet.Load(ctx, characterID)
	if err != nil || !found {
		t.Fatalf("Load pet found=%t err=%v", found, err)
	}
	entry, found := record.Entries[petKey]
	if !found || entry.SourceListType != listType || entry.SourceSlotIndex != slot {
		t.Fatalf("pet entry %q = %+v found=%t, want endpoint=(%d,%d)", petKey, entry, found, listType, slot)
	}
	if equipped && record.EquippedKey != petKey {
		t.Fatalf("equipped pet key = %q, want %q", record.EquippedKey, petKey)
	}
	if !equipped && record.EquippedKey != "" {
		t.Fatalf("equipped pet key = %q, want empty", record.EquippedKey)
	}
}

func requireWearRoundTripArtifactState(
	t *testing.T,
	ctx context.Context,
	repos dnfrepo.Group,
	characterID string,
	kind string,
	itemID int64,
	want bool,
) {
	t.Helper()
	record, found, err := repos.Pet.Load(ctx, characterID)
	if err != nil || !found {
		t.Fatalf("Load pet found=%t err=%v", found, err)
	}
	stack, occupied := record.Artifacts[kind]
	if occupied != want {
		t.Fatalf("pet artifact %q occupied=%t, want %t; stack=%+v", kind, occupied, want, stack)
	}
	if want && stack.ItemID != itemID {
		t.Fatalf("pet artifact %q item=%d, want %d", kind, stack.ItemID, itemID)
	}
}

func wearRoundTripEquipmentKey(slot int16) string {
	return strconv.Itoa(int(slot))
}
