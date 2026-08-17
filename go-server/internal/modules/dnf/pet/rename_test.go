package pet

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeRenameCreatureRequestExactCurrentBody(t *testing.T) {
	got, err := DecodeRenameCreatureRequest(renameCreatureRequestBody(5, listTypePet, []byte("Mina")))
	if err != nil {
		t.Fatalf("DecodeRenameCreatureRequest: %v", err)
	}
	if got.SlotIndex != 5 || got.ListType != listTypePet || !bytes.Equal(got.NameRaw, []byte("Mina")) {
		t.Fatalf("request = %+v", got)
	}
	// GBK/codepage-encoded names must not be rejected as invalid UTF-8.
	gbkName := []byte{0xc7, 0xbf, 0xd6, 0xc6}
	gotGBK, err := DecodeRenameCreatureRequest(renameCreatureRequestBody(5, listTypePet, gbkName))
	if err != nil || !bytes.Equal(gotGBK.NameRaw, gbkName) {
		t.Fatalf("gbk rename request=%+v err=%v", gotGBK, err)
	}
	// The current-EXE validator rejects an empty name.
	if _, err := DecodeRenameCreatureRequest(renameCreatureRequestBody(0, listTypePet, nil)); err == nil {
		t.Fatalf("empty rename request unexpectedly accepted")
	}

	tooLong := bytes.Repeat([]byte{'x'}, maxCreatureRenameNameBytes+1)
	declaredShort := renameCreatureRequestBody(5, listTypePet, []byte("abc"))
	binary.LittleEndian.PutUint32(declaredShort[3:7], 2)
	for _, body := range [][]byte{
		{5, 0, listTypePet},
		renameCreatureRequestBody(5, 6, []byte("x")),
		renameCreatureRequestBody(140, listTypePet, []byte("x")),
		renameCreatureRequestBody(5, listTypePet, tooLong),
		renameCreatureRequestBody(5, listTypePet, []byte{'a', 0, 'b'}),
		declaredShort,
		append(renameCreatureRequestBody(5, listTypePet, []byte("x")), 0),
	} {
		if _, err := DecodeRenameCreatureRequest(body); err == nil {
			t.Fatalf("body % X unexpectedly accepted", body)
		}
	}
	// The nine literal characters rejected by the current-EXE validator.
	for _, forbidden := range []byte{'\'', ' ', '\t', '\\', '%', '<', '>', '"', '|'} {
		if _, err := DecodeRenameCreatureRequest(renameCreatureRequestBody(5, listTypePet, []byte{'a', forbidden, 'b'})); err == nil {
			t.Fatalf("name containing %q unexpectedly accepted", forbidden)
		}
	}
}

func TestOwnerRenameConsumesCardAndPersistsEquippedCreatureName(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedRenameCreature(t, ctx, repos, "77")
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}

	command := RenameCommand{SelectedCharacterID: 77, ListType: listTypePet, SlotIndex: 5, NameRaw: []byte("Mina")}
	first, err := owner.Rename(ctx, command)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if !first.Changed || first.CharacterID != "77" || first.PetKey != "37" || first.CreatureSerial != 37 ||
		first.ItemID != 63000 || first.ListType != listTypePet || first.SourceListType != currentMainInventoryListType ||
		first.SlotIndex != 5 || first.RemainingCount != 1 {
		t.Fatalf("first result = %+v", first)
	}
	record, found, err := repos.Pet.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load pet found=%t err=%v", found, err)
	}
	entry := record.Entries["37"]
	if entry.Name != "Mina" || !bytes.Equal(entry.NameRaw, []byte("Mina")) || entry.Level != 4 || entry.Exp != 12 || entry.Satiety != 80 {
		t.Fatalf("renamed entry = %+v", entry)
	}
	updatedAt := record.UpdatedAt
	inventory, _, _ := repos.Inventory.Load(ctx, "77")
	if stack := inventory.Slots["0:5"]; stack.ItemID != currentPetRenameCardItemID || stack.Count != 1 {
		t.Fatalf("rename card after first use = %+v", stack)
	}

	second, err := owner.Rename(ctx, command)
	if err != nil {
		t.Fatalf("second Rename: %v", err)
	}
	if second.Changed || second.RemainingCount != 0 {
		t.Fatalf("second result = %+v", second)
	}
	record, _, _ = repos.Pet.Load(ctx, "77")
	if !record.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("same-name use rewrote pet timestamp: before=%v after=%v", updatedAt, record.UpdatedAt)
	}
	inventory, _, _ = repos.Inventory.Load(ctx, "77")
	if _, found := inventory.Slots["0:5"]; found {
		t.Fatalf("exhausted rename card row still exists: %+v", inventory.Slots["0:5"])
	}
	if _, err := owner.Rename(ctx, command); !errors.Is(err, ErrPetRenameCardInvalid) {
		t.Fatalf("third Rename error = %v, want %v", err, ErrPetRenameCardInvalid)
	}
}

func TestOwnerRenameRejectsCardAndEquippedCreatureMismatchWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*dnfrepo.InventoryRecord, *dnfrepo.EquipmentRecord, *dnfrepo.PetRecord)
		wantErr error
	}{
		{
			name: "wrong card item",
			mutate: func(inventory *dnfrepo.InventoryRecord, _ *dnfrepo.EquipmentRecord, _ *dnfrepo.PetRecord) {
				stack := inventory.Slots["0:5"]
				stack.ItemID++
				inventory.Slots["0:5"] = stack
			},
			wantErr: ErrPetRenameCardInvalid,
		},
		{
			name: "wrong card path",
			mutate: func(inventory *dnfrepo.InventoryRecord, _ *dnfrepo.EquipmentRecord, _ *dnfrepo.PetRecord) {
				stack := inventory.Slots["0:5"]
				stack.Extra["pvf_path"] = "stackable/cash/other.stk"
				inventory.Slots["0:5"] = stack
			},
			wantErr: ErrPetRenameCardInvalid,
		},
		{
			name: "missing equipped slot",
			mutate: func(_ *dnfrepo.InventoryRecord, equipment *dnfrepo.EquipmentRecord, _ *dnfrepo.PetRecord) {
				delete(equipment.Entries, "26")
			},
			wantErr: ErrPetEquippedAbsent,
		},
		{
			name: "missing equipped serial",
			mutate: func(_ *dnfrepo.InventoryRecord, equipment *dnfrepo.EquipmentRecord, _ *dnfrepo.PetRecord) {
				entry := equipment.Entries["26"]
				binary.LittleEndian.PutUint32(entry.RawEntry[24:28], 0)
				equipment.Entries["26"] = entry
			},
			wantErr: ErrPetCreatureSerialAbsent,
		},
		{
			name: "item mismatch",
			mutate: func(_ *dnfrepo.InventoryRecord, _ *dnfrepo.EquipmentRecord, record *dnfrepo.PetRecord) {
				entry := record.Entries["37"]
				entry.ItemID++
				record.Entries["37"] = entry
			},
			wantErr: ErrPetInventoryMismatch,
		},
		{
			name: "entry serial mismatch",
			mutate: func(_ *dnfrepo.InventoryRecord, _ *dnfrepo.EquipmentRecord, record *dnfrepo.PetRecord) {
				entry := record.Entries["37"]
				entry.CreatureKey = 38
				record.Entries["37"] = entry
			},
			wantErr: ErrPetInventoryMismatch,
		},
		{
			name: "equipped key mismatch",
			mutate: func(_ *dnfrepo.InventoryRecord, _ *dnfrepo.EquipmentRecord, record *dnfrepo.PetRecord) {
				record.EquippedKey = "38"
			},
			wantErr: ErrPetInventoryMismatch,
		},
		{
			name: "entry missing",
			mutate: func(_ *dnfrepo.InventoryRecord, _ *dnfrepo.EquipmentRecord, record *dnfrepo.PetRecord) {
				delete(record.Entries, "37")
			},
			wantErr: ErrPetInventoryMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			seedRenameCreature(t, ctx, repos, "77")
			inventory, _, _ := repos.Inventory.Load(ctx, "77")
			equipment, _, _ := repos.Equipment.Load(ctx, "77")
			record, _, _ := repos.Pet.Load(ctx, "77")
			test.mutate(&inventory, &equipment, &record)
			if err := repos.Inventory.Save(ctx, inventory); err != nil {
				t.Fatal(err)
			}
			if err := repos.Equipment.Save(ctx, equipment); err != nil {
				t.Fatal(err)
			}
			if err := repos.Pet.Save(ctx, record); err != nil {
				t.Fatal(err)
			}

			owner, _ := NewOwner(repos)
			_, err := owner.Rename(ctx, RenameCommand{SelectedCharacterID: 77, ListType: listTypePet, SlotIndex: 5, NameRaw: []byte("new")})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Rename error = %v, want %v", err, test.wantErr)
			}
			afterInventory, _, _ := repos.Inventory.Load(ctx, "77")
			if stack := afterInventory.Slots["0:5"]; stack.Count != 2 {
				t.Fatalf("failed rename consumed card: %+v", stack)
			}
			after, _, _ := repos.Pet.Load(ctx, "77")
			if entry, exists := after.Entries["37"]; exists && (entry.Name != "old" || !bytes.Equal(entry.NameRaw, []byte("old"))) {
				t.Fatalf("failed rename mutated entry = %+v", entry)
			}
		})
	}
}

func TestHandlerRenameCreatureReturnsAckNativeNameNotificationAndDoesNotSwallowFailure(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedRenameCreature(t, ctx, repos, "77")

	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRenameCreature),
		Body:                renameCreatureRequestBody(5, listTypePet, []byte("Mina")),
		AccountID:           "account",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !result.Handled || !result.ResponseAllowed || result.Operation != "rename_creature" ||
		len(result.UpperResponses) != 2 ||
		len(result.ItemSlotRefreshes) != 1 ||
		result.ItemSlotRefreshes[0] != (alignedcmd.ItemSlotRefresh{ListType: currentMainInventoryListType, SlotIndex: 5}) ||
		len(result.PostActions) != 0 {
		t.Fatalf("result = %+v", result)
	}
	ack := result.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketRenameCreature) || ack.Classification != dnfproto.DefaultChannelClassification || !ack.AllowCodec || !bytes.Equal(ack.Body, []byte{5, 0, listTypePet}) {
		t.Fatalf("ack = %+v body=% X", ack, ack.Body)
	}
	notify := result.UpperResponses[1]
	wantNotify := []byte{77, 0, 4, 0, 0, 0, 'M', 'i', 'n', 'a'}
	if notify.MsgID != 101 || notify.Classification != 0 || !notify.AllowCodec || !bytes.Equal(notify.Body, wantNotify) {
		t.Fatalf("name notification = %+v body=% X want=% X", notify, notify.Body, wantNotify)
	}
	commitErr := errors.New("rename commit failed")
	failing := repos
	failing.CharacterPets = renameCommitFailureUOW{base: repos, err: commitErr}
	failed, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRenameCreature),
		Body:                renameCreatureRequestBody(5, listTypePet, []byte("Other")),
		SelectedCharacterID: 77,
		Repositories:        failing,
	})
	if err != nil {
		t.Fatalf("failed Handle returned Go error: %v", err)
	}
	if !failed.Handled || failed.ResponseAllowed || len(failed.UpperResponses) != 0 {
		t.Fatalf("commit failure was swallowed: %+v", failed)
	}
	record, _, _ := repos.Pet.Load(ctx, "77")
	if entry := record.Entries["37"]; entry.Name != "Mina" || !bytes.Equal(entry.NameRaw, []byte("Mina")) {
		t.Fatalf("commit failure mutated durable entry = %+v", entry)
	}
}

func TestOwnerRenameConcurrentUseConsumesOneAvailableCardExactlyOnce(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedRenameCreature(t, ctx, repos, "77")
	inventory, _, _ := repos.Inventory.Load(ctx, "77")
	card := inventory.Slots["0:5"]
	card.Count = 1
	inventory.Slots["0:5"] = card
	if err := repos.Inventory.Save(ctx, inventory); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repos)
	command := RenameCommand{SelectedCharacterID: 77, ListType: listTypePet, SlotIndex: 5, NameRaw: []byte("Mina")}

	const workers = 32
	results := make(chan RenameResult, workers)
	errorsCh := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := owner.Rename(ctx, command)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errorsCh)
	failures := 0
	for err := range errorsCh {
		if !errors.Is(err, ErrPetRenameCardInvalid) {
			t.Fatalf("concurrent Rename: %v", err)
		}
		failures++
	}
	changed := 0
	successes := 0
	for result := range results {
		successes++
		if result.Changed {
			changed++
		}
	}
	if successes != 1 || changed != 1 || failures != workers-1 {
		t.Fatalf("successes=%d changed=%d failures=%d, want 1/1/%d", successes, changed, failures, workers-1)
	}
	record, _, _ := repos.Pet.Load(ctx, "77")
	if entry := record.Entries["37"]; entry.Name != "Mina" || !bytes.Equal(entry.NameRaw, []byte("Mina")) {
		t.Fatalf("final entry = %+v", entry)
	}
	inventory, _, _ = repos.Inventory.Load(ctx, "77")
	if _, found := inventory.Slots["0:5"]; found {
		t.Fatalf("consumed card row still exists: %+v", inventory.Slots["0:5"])
	}
}

type renameCommitFailureUOW struct {
	base dnfrepo.Group
	err  error
}

func (u renameCommitFailureUOW) WithinCharacterPets(
	ctx context.Context,
	characterID string,
	apply func(dnfrepo.InventoryRepository, dnfrepo.EquipmentRepository, dnfrepo.PetRepository) error,
) error {
	staged := dnfrepomemory.NewMemoryGroup()
	if record, found, err := u.base.Inventory.Load(ctx, characterID); err != nil {
		return err
	} else if found {
		if err := staged.Inventory.Save(ctx, record); err != nil {
			return err
		}
	}
	if record, found, err := u.base.Equipment.Load(ctx, characterID); err != nil {
		return err
	} else if found {
		if err := staged.Equipment.Save(ctx, record); err != nil {
			return err
		}
	}
	if record, found, err := u.base.Pet.Load(ctx, characterID); err != nil {
		return err
	} else if found {
		if err := staged.Pet.Save(ctx, record); err != nil {
			return err
		}
	}
	if err := apply(staged.Inventory, staged.Equipment, staged.Pet); err != nil {
		return err
	}
	return u.err
}

func seedRenameCreature(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID string) {
	t.Helper()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: characterID,
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {
				ItemID: currentPetRenameCardItemID,
				Count:  2,
				Extra: map[string]string{
					"item_kind":      "stackable",
					"pvf_path":       currentPetRenameCardPVFPath,
					"stackable_type": "[creature]",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	rawEquipment := make([]byte, 0x77)
	binary.LittleEndian.PutUint32(rawEquipment[24:28], 37)
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: characterID,
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {
				SlotIndex: currentEquippedCreatureSlot,
				ItemID:    63000,
				RawEntry:  rawEquipment,
			},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: characterID,
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     37,
				ItemID:          63000,
				SourceListType:  listTypePet,
				SourceSlotIndex: 5,
				Name:            "old",
				NameRaw:         []byte("old"),
				Satiety:         80,
				Level:           4,
				Exp:             12,
			},
		},
		EquippedKey: "37",
	}); err != nil {
		t.Fatalf("save pet: %v", err)
	}
}

func renameCreatureRequestBody(slot int16, listType byte, name []byte) []byte {
	body := make([]byte, 7+len(name))
	binary.LittleEndian.PutUint16(body[0:2], uint16(slot))
	body[2] = listType
	binary.LittleEndian.PutUint32(body[3:7], uint32(len(name)))
	copy(body[7:], name)
	return body
}
