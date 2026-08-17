package inventory

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeDamageFontRequestsUseVerifiedWireWidths(t *testing.T) {
	body := []byte{0x4c, 0x00, 0x00, 0, 0, 0, 0, 0xa2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	action, err := DecodeUseStackableActionRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if action.SourceSlotIndex != 76 || action.ListType != 0 || action.ActionIndex != 162 {
		t.Fatalf("action = %+v", action)
	}
	selection, err := DecodeSelectDamageFontRequest([]byte{0x88, 0x13})
	if err != nil {
		t.Fatal(err)
	}
	if selection.FontIndex != 5000 {
		t.Fatalf("font index = %d, want 5000", selection.FontIndex)
	}
}

func TestDamageFontUnlockAndSelectionAreRepositoryBacked(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{
		CharacterID: "77",
		Stats:       map[string]int64{DamageFontOwnershipStatKey(5000): now.Add(10 * 24 * time.Hour).Unix()},
	})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{
		slotKey(listTypeMain, 76): {ItemID: 10160911, Count: 2},
	}})

	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUseStackableAction),
		Body:                []byte{0x4c, 0, 0, 0, 0, 0, 0, 0xa2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		SelectedCharacterID: 77,
		Repositories:        repos,
		DamageFontNow:       now,
		DamageFontResolver: func(itemID int64) (alignedcmd.DamageFontResolution, error) {
			return alignedcmd.DamageFontResolution{
				Valid:          true,
				PVFPath:        "stackable/test.stk",
				ActionType:     "[add damage font skin]",
				FontIndex:      5000,
				ExpirationMode: alignedcmd.DamageFontExpirationPeriod,
				PeriodDays:     90,
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ResponseAllowed || len(result.UpperResponses) != 1 {
		t.Fatalf("result = %+v", result)
	}
	wantAck := []byte{1, 76, 0, 0, 0, 1, 0, 0, 0, 1}
	if !bytes.Equal(result.UpperResponses[0].Body, wantAck) {
		t.Fatalf("op515 ACK = %x, want %x", result.UpperResponses[0].Body, wantAck)
	}
	if len(result.ItemSlotRefreshes) != 1 || result.ItemSlotRefreshes[0].SlotIndex != 76 ||
		len(result.PostActions) != 1 || result.PostActions[0] != alignedcmd.PostActionRefreshSelectedDamageFontState {
		t.Fatalf("refresh result = %+v", result)
	}
	bag := loadTestInventory(t, ctx, repos, "77")
	if got := bag.Slots[slotKey(listTypeMain, 76)].Count; got != 1 {
		t.Fatalf("remaining count = %d, want 1", got)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	wantExpiry := now.Add(100 * 24 * time.Hour).Unix()
	if got := character.Stats[DamageFontOwnershipStatKey(5000)]; got != wantExpiry {
		t.Fatalf("expiry = %d, want %d", got, wantExpiry)
	}

	selectBody := make([]byte, 2)
	binary.LittleEndian.PutUint16(selectBody, 5000)
	selected, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSelectDamageFontSkin),
		Body:                selectBody,
		SelectedCharacterID: 77,
		Repositories:        repos,
		DamageFontNow:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := selected.UpperResponses[0].Body; !bytes.Equal(got, []byte{1, 0x88, 0x13}) {
		t.Fatalf("op1288 ACK = %x", got)
	}
	character, _, _ = repos.Character.Load(ctx, "77")
	if got := character.Stats[DamageFontSelectedStatKey]; got != 5000 {
		t.Fatalf("selected stat = %d", got)
	}
}

func TestDamageFontUnlockRollsBackInventoryWhenCharacterSaveFails(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77"})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{
		slotKey(listTypeMain, 76): {ItemID: 10160911, Count: 1},
	}})
	wantErr := errors.New("forced character save failure")
	repos.CharacterAssets = failingDamageFontAssetUOW{base: repos.CharacterAssets, saveErr: wantErr}
	owner, _ := NewOwner(repos)
	_, err := owner.UnlockDamageFont(ctx, Command{
		SelectedCharacterID: 77,
		SourceListType:      listTypeMain,
		SourceSlotIndex:     76,
		ActionIndex:         damageFontActionIndex,
	}, func(int64) (alignedcmd.DamageFontResolution, error) {
		return alignedcmd.DamageFontResolution{
			Valid:          true,
			ActionType:     "[add damage font skin]",
			FontIndex:      2,
			ExpirationMode: alignedcmd.DamageFontExpirationUnlimited,
		}, nil
	}, now)
	if !errors.Is(err, wantErr) {
		t.Fatalf("UnlockDamageFont error = %v, want %v", err, wantErr)
	}
	if got := loadTestInventory(t, ctx, repos, "77").Slots[slotKey(listTypeMain, 76)].Count; got != 1 {
		t.Fatalf("count after rollback = %d, want 1", got)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if _, ok := character.Stats[DamageFontOwnershipStatKey(2)]; ok {
		t.Fatalf("font ownership persisted after rollback: %+v", character.Stats)
	}
}

type failingDamageFontAssetUOW struct {
	base    dnfrepo.CharacterAssetUnitOfWork
	saveErr error
}

func (u failingDamageFontAssetUOW) WithinCharacterAssets(ctx context.Context, characterID string, apply func(dnfrepo.CharacterRepository, dnfrepo.InventoryRepository, dnfrepo.EquipmentRepository) error) error {
	return u.base.WithinCharacterAssets(ctx, characterID, func(characters dnfrepo.CharacterRepository, inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository) error {
		return apply(failingDamageFontCharacterRepo{CharacterRepository: characters, saveErr: u.saveErr}, inventory, equipment)
	})
}

type failingDamageFontCharacterRepo struct {
	dnfrepo.CharacterRepository
	saveErr error
}

func (r failingDamageFontCharacterRepo) Save(context.Context, dnfrepo.CharacterRecord) error {
	return r.saveErr
}

func TestDamageFontStateFiltersExpiredAndKeepsPermanent(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	selected, entries := DamageFontStateFromStats(map[string]int64{
		DamageFontSelectedStatKey:         2,
		DamageFontOwnershipStatKey(1):     0,
		DamageFontOwnershipStatKey(2):     now.Unix() - 1,
		DamageFontOwnershipStatKey(5000):  now.Unix() + 10,
		"damage_font_skin_bad_expires_at": 4,
	}, now)
	if selected != 0 {
		t.Fatalf("selected = %d, want 0 after expiry", selected)
	}
	if len(entries) != 2 || entries[0].FontIndex != 1 || entries[0].ExpiresAt != 0 || entries[1].FontIndex != 5000 {
		t.Fatalf("entries = %+v", entries)
	}
}
