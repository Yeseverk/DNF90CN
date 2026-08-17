package pet

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeHatchCreatureRequestExactCurrentBody(t *testing.T) {
	got, err := DecodeHatchCreatureRequest([]byte{listTypePet, 0x05, 0x00})
	if err != nil {
		t.Fatalf("DecodeHatchCreatureRequest: %v", err)
	}
	if got.ListType != listTypePet || got.SlotIndex != 5 {
		t.Fatalf("got = %+v", got)
	}
	for _, body := range [][]byte{
		{listTypePet, 5},
		{listTypePet, 5, 0, 1},
		{0, 5, 0},
		{listTypePet, 140, 0},
	} {
		if _, err := DecodeHatchCreatureRequest(body); err == nil {
			t.Fatalf("body % X unexpectedly accepted", body)
		}
	}
}

func TestBuildCreatureListBodyUsesTypedFieldsNotRawReplay(t *testing.T) {
	entry := dnfrepo.PetEntry{
		PetKey:      "123",
		CreatureKey: 123,
		ItemID:      63000,
		NameRaw:     []byte("abc"),
		Satiety:     100,
		Level:       3,
		Exp:         12,
		TailFlag:    1,
		RawEntry:    []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}
	body, err := buildCreatureListBody([]dnfrepo.PetEntry{entry})
	if err != nil {
		t.Fatalf("buildCreatureListBody: %v", err)
	}
	want := []byte{
		1,
		123, 0, 0, 0,
		100,
		0,
		12, 0, 0, 0,
		3,
		3, 0, 0, 0, 'a', 'b', 'c',
		1,
	}
	if string(body) != string(want) {
		t.Fatalf("body = % X\nwant = % X", body, want)
	}
}

func TestBuildCreatureListBodyModeOneAndValidation(t *testing.T) {
	body, err := buildCreatureListBody([]dnfrepo.PetEntry{{
		PetKey:       "9",
		CreatureKey:  9,
		ItemID:       63000,
		Satiety:      7,
		ModeFlag:     1,
		Mode1Field0A: 2,
		Mode1Field0B: 3,
		Level:        4,
	}})
	if err != nil {
		t.Fatalf("build mode1: %v", err)
	}
	if len(body) != 19 || body[6] != 1 || body[11] != 2 || body[12] != 3 || body[13] != 4 {
		t.Fatalf("mode1 body = % X", body)
	}
	invalid := []dnfrepo.PetEntry{{PetKey: "not-numeric", ItemID: 1, Satiety: 1, Level: 1}}
	if _, err := buildCreatureListBody(invalid); err == nil {
		t.Fatal("invalid key unexpectedly encoded")
	}
}

func TestBuildCreatureListBodyEnforcesCurrentReaderExclusiveNameLimit(t *testing.T) {
	accepted := dnfrepo.PetEntry{
		PetKey:      "9",
		CreatureKey: 9,
		ItemID:      63000,
		NameRaw:     []byte(strings.Repeat("a", 29)),
		Level:       1,
	}
	if _, err := buildCreatureListBody([]dnfrepo.PetEntry{accepted}); err != nil {
		t.Fatalf("29-byte name rejected: %v", err)
	}
	accepted.NameRaw = []byte(strings.Repeat("b", 30))
	if _, err := buildCreatureListBody([]dnfrepo.PetEntry{accepted}); !errors.Is(err, errCreatureNameTooLong) {
		t.Fatalf("30-byte name error = %v, want %v", err, errCreatureNameTooLong)
	}
}

func TestPetHatchResponsesUseCurrentItemRowAndTypedCreatureList(t *testing.T) {
	responses := petHatchResponses(uint16(dnfenum.CmdPacketHatchCreature), HatchResult{
		CharacterID: "61",
		PetKey:      "123",
		ItemID:      63000,
		Changed:     true,
		PetInventory: map[string]dnfrepo.ItemStack{
			"7:5": {ItemID: 63000, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "123"}},
		},
		EntryCount: 1,
		Entries:    []dnfrepo.PetEntry{{PetKey: "123", CreatureKey: 123, ItemID: 63000, Satiety: 100, Level: 1}},
	})
	if len(responses) != 3 {
		t.Fatalf("response count = %d", len(responses))
	}
	if ack := responses[0]; ack.MsgID != uint16(dnfenum.CmdPacketHatchCreature) || ack.Classification != dnfproto.DefaultChannelClassification || string(ack.Body) != string([]byte{1}) {
		t.Fatalf("ack = %+v", ack)
	}
	refresh := responses[1]
	if refresh.MsgID != 0x000D || refresh.Classification != 0 || len(refresh.Body) != 3+0x77 || refresh.Body[0] != listTypePet || binary.LittleEndian.Uint16(refresh.Body[1:3]) != 1 {
		t.Fatalf("refresh header/body = %+v % X", refresh, refresh.Body)
	}
	row := refresh.Body[3:]
	if binary.LittleEndian.Uint16(row[0:2]) != 5 || binary.LittleEndian.Uint32(row[2:6]) != 63000 || binary.LittleEndian.Uint32(row[6:10]) != 123 {
		t.Fatalf("refresh row = % X", row)
	}
	if list := responses[2]; list.MsgID != 0x0069 || list.Classification != 0 || len(list.Body) != 17 || list.Body[0] != 1 || binary.LittleEndian.Uint32(list.Body[1:5]) != 123 {
		t.Fatalf("creature list = %+v body=% X", list, list.Body)
	}
}

func TestHandlerBlocksOldOp173C2SHatchRoute(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketHatchCreatureEgg),
		Body:                []byte{listTypePet, 5, 0},
		SelectedCharacterID: 61,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !got.Handled || got.ResponseAllowed || !strings.Contains(got.Reason, "op173") {
		t.Fatalf("result = %+v", got)
	}
}

func TestHandlerHatchUsesInjectedRuntimePVFResolver(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "61",
		Slots: map[string]dnfrepo.ItemStack{
			"7:5": {ItemID: 63006, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "61", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	resolverCalls := 0
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketHatchCreature),
		Body:                []byte{listTypePet, 5, 0},
		SelectedCharacterID: 61,
		Repositories:        repos,
		PetHatchResolver: func(itemID int64) (alignedcmd.PetHatchResolution, error) {
			resolverCalls++
			if itemID != 63006 {
				t.Fatalf("resolver item = %d", itemID)
			}
			return alignedcmd.PetHatchResolution{EggItemID: 63006, HatchedItemID: 63000, EggPVFPath: "creature/egg.equ", HatchedPVFPath: "creature/pet.equ", MinimumLevel: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resolverCalls != 1 || !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 3 {
		t.Fatalf("resolver_calls=%d result=%+v", resolverCalls, got)
	}
	inventory, found, err := repos.Inventory.Load(ctx, "61")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if stack := inventory.Slots["7:5"]; stack.ItemID != 63000 || stack.Count != 1 {
		t.Fatalf("hatched stack = %+v", stack)
	}
	petRecord, found, err := repos.Pet.Load(ctx, "61")
	if err != nil || !found || len(petRecord.Entries) != 1 {
		t.Fatalf("load pet found=%t entries=%d err=%v", found, len(petRecord.Entries), err)
	}
}

func TestHandlerRequestHatchedCreatureReturnsTypedList(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "66",
		Entries: map[string]dnfrepo.PetEntry{
			"123": {PetKey: "123", CreatureKey: 123, ItemID: 63000, Satiety: 100, Level: 2, Exp: 7, NameRaw: []byte("pet")},
		},
		EquippedKey: "123",
		TownDisplay: true,
	}); err != nil {
		t.Fatalf("save pet: %v", err)
	}
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRequestHatchedCreature),
		SelectedCharacterID: 66,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestHandlerRequestHatchedCreatureReturnsEmptyTypedList(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRequestHatchedCreature),
		SelectedCharacterID: 66,
		Repositories:        dnfrepomemory.NewMemoryGroup(),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestHatchCommandContainsNoClientItemCandidate(t *testing.T) {
	cmd := NewHatchCommand(alignedcmd.Request{AccountID: " acc ", SelectedCharacterID: 70}, "hatch_creature", HatchCreatureRequest{ListType: listTypePet, SlotIndex: 3})
	if cmd.AccountID != "acc" || cmd.SelectedCharacterID != 70 || cmd.ListType != listTypePet || cmd.SlotIndex != 3 {
		t.Fatalf("cmd = %+v", cmd)
	}
	if strings.Contains(cmd.String(), "candidate") || !strings.Contains(cmd.String(), "runtime PVF") {
		t.Fatalf("command summary = %s", cmd.String())
	}
}
