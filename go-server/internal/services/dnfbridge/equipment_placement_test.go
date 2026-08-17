package dnfbridge

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	dnfequip "longheng.io/server/internal/modules/dnf/equip"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

type equipmentPlacementPVFSource map[string]string

func (s equipmentPlacementPVFSource) ReadText(relativePath string) (string, error) {
	for path, text := range s {
		if cleanInitialPVFPath(path) == cleanInitialPVFPath(relativePath) {
			return text, nil
		}
	}
	return "", errors.New("test PVF path missing")
}

func TestCurrentEquipmentPlacementValidatorAcceptsOnlyExactPVFSlot(t *testing.T) {
	validator := newEquipmentPlacementValidatorFixture(t)
	tests := []struct {
		name       string
		itemID     int64
		listType   byte
		sourceSlot int16
		targetSlot int16
		wantErr    error
	}{
		{name: "hat avatar exact", itemID: 9100, listType: 1, sourceSlot: 0, targetSlot: 0},
		{name: "aura avatar exact", itemID: 9109, listType: 1, sourceSlot: 21, targetSlot: 9},
		{name: "unlocked aura animation skin", itemID: 9109, listType: 1, sourceSlot: 21, targetSlot: 11},
		{name: "weapon avatar exact", itemID: 9110, listType: 1, sourceSlot: 77, targetSlot: 10},
		{name: "weapon exact from main", itemID: 9001, listType: 0, sourceSlot: 9, targetSlot: 12},
		{name: "title exact from cargo", itemID: 9002, listType: 2, sourceSlot: 5, targetSlot: 13},
		{name: "earring exact from main", itemID: 9025, listType: 0, sourceSlot: 11, targetSlot: 25},
		{name: "guild medal exact from dedicated bag", itemID: 9032, listType: 38, sourceSlot: 12, targetSlot: 32},
		{name: "guardian page cannot equip as medal", itemID: 9032, listType: 38, sourceSlot: 49, targetSlot: 32, wantErr: errEquipmentPlacementSourceUnknown},
		{name: "worn weapon remains weapon", itemID: 9001, listType: 3, sourceSlot: 12, targetSlot: 12},
		{name: "worn guild medal remains guild medal", itemID: 9032, listType: 3, sourceSlot: 32, targetSlot: 32},
		{name: "avatar same class wrong slot", itemID: 9100, listType: 1, sourceSlot: 0, targetSlot: 8, wantErr: errEquipmentPlacementSlotTypeMismatch},
		{name: "weapon same class wrong slot", itemID: 9001, listType: 0, sourceSlot: 9, targetSlot: 13, wantErr: errEquipmentPlacementSlotTypeMismatch},
		{name: "guild medal same class wrong slot", itemID: 9032, listType: 0, sourceSlot: 12, targetSlot: 31, wantErr: errEquipmentPlacementSlotTypeMismatch},
		{name: "avatar into normal", itemID: 9100, listType: 1, sourceSlot: 0, targetSlot: 12, wantErr: errEquipmentPlacementSlotClassMismatch},
		{name: "normal into avatar", itemID: 9001, listType: 0, sourceSlot: 9, targetSlot: 0, wantErr: errEquipmentPlacementSlotClassMismatch},
		{name: "worn avatar source contains normal item", itemID: 9001, listType: 3, sourceSlot: 11, targetSlot: 12, wantErr: errEquipmentPlacementSlotClassMismatch},
		{name: "worn normal source contains avatar item", itemID: 9100, listType: 3, sourceSlot: 12, targetSlot: 0, wantErr: errEquipmentPlacementSlotClassMismatch},
		{name: "worn unknown source slot", itemID: 9001, listType: 3, sourceSlot: 33, targetSlot: 12, wantErr: errEquipmentPlacementSourceUnknown},
		{name: "unknown source list", itemID: 9001, listType: 12, sourceSlot: 0, targetSlot: 12, wantErr: errEquipmentPlacementSourceUnknown},
		{name: "normal target below range", itemID: 9001, listType: 0, sourceSlot: 9, targetSlot: -1, wantErr: errEquipmentPlacementSlotClassMismatch},
		{name: "normal target above range", itemID: 9001, listType: 0, sourceSlot: 9, targetSlot: 33, wantErr: errEquipmentPlacementSlotClassMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validator.ValidateEquipmentPlacement(context.Background(), dnfequip.Placement{
				CharacterID:     "19",
				ItemID:          test.itemID,
				SourceListType:  test.listType,
				SourceSlotIndex: test.sourceSlot,
				TargetSlotIndex: test.targetSlot,
			})
			if test.wantErr == nil && err != nil {
				t.Fatalf("ValidateEquipmentPlacement error = %v", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("ValidateEquipmentPlacement error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCurrentEquipmentPlacementValidatorRejectsLockedAuraAnimationSkin(t *testing.T) {
	validator := newEquipmentPlacementValidatorFixture(t)
	character, found, err := validator.characters.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load character: found=%t err=%v", found, err)
	}
	delete(character.Stats, currentOpenAuraSkinSlotFlagStat)
	if err := validator.characters.Save(context.Background(), character); err != nil {
		t.Fatalf("save locked character: %v", err)
	}

	err = validator.ValidateEquipmentPlacement(context.Background(), dnfequip.Placement{
		CharacterID:     "19",
		ItemID:          9109,
		SourceListType:  currentEquipmentPlacementListAvatar,
		SourceSlotIndex: 21,
		TargetSlotIndex: 11,
	})
	if !errors.Is(err, errEquipmentPlacementAuraSkinLocked) {
		t.Fatalf("locked aura-skin error = %v, want %v", err, errEquipmentPlacementAuraSkinLocked)
	}
}

func TestCurrentEquipmentPlacementRuleForPVFTypeUsesCurrentEXESlots(t *testing.T) {
	tests := []struct {
		pvfType string
		slot    int16
		class   currentEquipmentPlacementClass
	}{
		{pvfType: "[hat avatar]", slot: 0, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[hair avatar]", slot: 1, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[face avatar]", slot: 2, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[coat avatar]", slot: 3, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[pants avatar]", slot: 4, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[shoes avatar]", slot: 5, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[breast avatar]", slot: 6, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[waist avatar]", slot: 7, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[skin avatar]", slot: 8, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[aurora avatar]", slot: 9, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[weapon avatar]", slot: 10, class: currentEquipmentPlacementClassAvatar},
		{pvfType: "[weapon]", slot: 12, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[title name]", slot: 13, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[coat]", slot: 14, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[shoulder]", slot: 15, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[pants]", slot: 16, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[shoes]", slot: 17, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[waist]", slot: 18, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[amulet]", slot: 19, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[wrist]", slot: 20, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[ring]", slot: 21, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[support]", slot: 22, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[magic stone]", slot: 23, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[support weapon]", slot: 24, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[earring]", slot: 25, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[flag]", slot: 32, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[name tag]", slot: 30, class: currentEquipmentPlacementClassNormal},
		{pvfType: "[artifact red]", slot: 27, class: currentEquipmentPlacementClassCreature},
		{pvfType: "[artifact blue]", slot: 28, class: currentEquipmentPlacementClassCreature},
		{pvfType: "[artifact green]", slot: 29, class: currentEquipmentPlacementClassCreature},
	}
	for _, test := range tests {
		t.Run(test.pvfType, func(t *testing.T) {
			rule, ok := currentEquipmentPlacementRuleForPVFType(" `" + test.pvfType + "` ")
			if !ok || rule.pvfType != test.pvfType || rule.targetSlot != test.slot || rule.class != test.class {
				t.Fatalf("rule = %+v ok=%t, want type=%q slot=%d class=%d", rule, ok, test.pvfType, test.slot, test.class)
			}
		})
	}

	// [creature] stays unsupported on purpose: target 26 is admitted by the
	// equip owner's proven short-circuit before this validator runs, so the
	// validator must not grow a second, unowned creature-body rule.
	for _, unsupported := range []string{"", "[creature]", "[charm]", "[unknown]"} {
		if rule, ok := currentEquipmentPlacementRuleForPVFType(unsupported); ok {
			t.Fatalf("unsupported type %q resolved to %+v", unsupported, rule)
		}
	}
}

func TestCurrentEquipmentPlacementValidatorRealPVFStarterSlots(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("DNFBRIDGE_REAL_PVF_SMOKE not set")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real PVF: %v", err)
	}
	byJob, err := parseInitialCharacterEquipmentAllFromSource(archive)
	if err != nil {
		t.Fatalf("parse real initial equipment: %v", err)
	}
	equipmentPaths, err := initialEquipmentPathMap(archive)
	if err != nil {
		t.Fatalf("parse real equipment paths: %v", err)
	}

	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:account-1",
		Job:         "11",
		Level:       90,
		Stats:       map[string]int64{currentOpenAuraSkinSlotFlagStat: 1},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	validator := &currentEquipmentPlacementValidator{
		selectedAccountID:   "dnf:account-1",
		selectedCharacterID: 19,
		characters:          repositories.Character,
		pvfSource:           archive,
		equipmentPaths:      equipmentPaths,
	}

	expected := map[int16]int16{11: 12, 13: 14, 15: 16}
	checked := 0
	for job, entries := range byJob {
		for _, entry := range entries {
			target, ok := expected[entry.Slot]
			if !ok {
				continue
			}
			placement := dnfequip.Placement{
				CharacterID:     "19",
				ItemID:          entry.ItemID,
				SourceListType:  currentEquipmentPlacementListMain,
				SourceSlotIndex: 9,
				TargetSlotIndex: target,
			}
			if err := validator.ValidateEquipmentPlacement(context.Background(), placement); err != nil {
				t.Fatalf("job=%d item=%d starter_slot=%d target=%d: %v", job, entry.ItemID, entry.Slot, target, err)
			}
			placement.TargetSlotIndex = currentEquipmentPlacementNormalSlotMax
			if target == currentEquipmentPlacementNormalSlotMax {
				placement.TargetSlotIndex--
			}
			if err := validator.ValidateEquipmentPlacement(context.Background(), placement); !errors.Is(err, errEquipmentPlacementSlotTypeMismatch) {
				t.Fatalf("job=%d item=%d wrong target error=%v", job, entry.ItemID, err)
			}
			checked++
		}
	}
	if checked != 48 {
		t.Fatalf("checked real PVF starter rows = %d, want 48", checked)
	}

	// Current runtime Script.pvf declares guild medals as [flag]. This is not
	// a synthetic type fixture: prove the production catalog reaches slot 32
	// and cannot be placed into a neighbouring ordinary slot.
	const guildMedalItemID int64 = 100380017
	if path := equipmentPaths[guildMedalItemID]; path == "" {
		t.Fatalf("real PVF guild medal %d missing from equipment/equipment.lst", guildMedalItemID)
	}
	guildMedal := dnfequip.Placement{
		CharacterID:     "19",
		ItemID:          guildMedalItemID,
		SourceListType:  currentEquipmentPlacementListGuildMedal,
		SourceSlotIndex: 0,
		TargetSlotIndex: 32,
	}
	if err := validator.ValidateEquipmentPlacement(context.Background(), guildMedal); err != nil {
		t.Fatalf("real PVF guild medal target 32: %v", err)
	}
	guildMedal.TargetSlotIndex = 31
	if err := validator.ValidateEquipmentPlacement(context.Background(), guildMedal); !errors.Is(err, errEquipmentPlacementSlotTypeMismatch) {
		t.Fatalf("real PVF guild medal target 31 error=%v, want %v", err, errEquipmentPlacementSlotTypeMismatch)
	}

	// Live current-client op19 evidence uses this runtime-PVF aura both as the
	// ordinary stat-bearing aura in slot 9 and as the unlocked animation skin
	// in slot 11. Pin both destinations against the real archive so a synthetic
	// fixture cannot hide a changed PVF category.
	const auraItemID int64 = 101590023
	aura := dnfequip.Placement{
		CharacterID:     "19",
		ItemID:          auraItemID,
		SourceListType:  currentEquipmentPlacementListAvatar,
		SourceSlotIndex: 21,
		TargetSlotIndex: 9,
	}
	if err := validator.ValidateEquipmentPlacement(context.Background(), aura); err != nil {
		t.Fatalf("real PVF aura target 9: %v", err)
	}
	aura.TargetSlotIndex = 11
	if err := validator.ValidateEquipmentPlacement(context.Background(), aura); err != nil {
		t.Fatalf("real PVF unlocked aura skin target 11: %v", err)
	}
}

func TestCurrentEquipmentPlacementValidatorRejectsUnownedCharacterAndUnknownPVFItem(t *testing.T) {
	validator := newEquipmentPlacementValidatorFixture(t)
	placement := dnfequip.Placement{
		CharacterID:     "20",
		ItemID:          9001,
		SourceListType:  0,
		SourceSlotIndex: 9,
		TargetSlotIndex: 11,
	}
	if err := validator.ValidateEquipmentPlacement(context.Background(), placement); !errors.Is(err, errEquipmentPlacementOwnerMismatch) {
		t.Fatalf("character mismatch error = %v", err)
	}

	placement.CharacterID = "19"
	placement.ItemID = 9999
	if err := validator.ValidateEquipmentPlacement(context.Background(), placement); !errors.Is(err, errEquipmentPlacementItemUnknown) {
		t.Fatalf("unknown item error = %v", err)
	}

	validator.equipmentPaths[9002] = "weapon/missing.equ"
	placement.ItemID = 9002
	if err := validator.ValidateEquipmentPlacement(context.Background(), placement); !errors.Is(err, errEquipmentPlacementPVFInvalid) {
		t.Fatalf("missing PVF error = %v", err)
	}

	validator.equipmentPaths[9003] = "special/missing-type.equ"
	validator.pvfSource.(equipmentPlacementPVFSource)["equipment/special/missing-type.equ"] = "[name]\n`missing type`\n"
	placement.ItemID = 9003
	if err := validator.ValidateEquipmentPlacement(context.Background(), placement); !errors.Is(err, errEquipmentPlacementPVFTypeUnknown) {
		t.Fatalf("missing PVF type error = %v", err)
	}

	validator.equipmentPaths[9004] = "special/charm.equ"
	validator.pvfSource.(equipmentPlacementPVFSource)["equipment/special/charm.equ"] = equipmentPlacementPVFDocument("[charm]")
	placement.ItemID = 9004
	if err := validator.ValidateEquipmentPlacement(context.Background(), placement); !errors.Is(err, errEquipmentPlacementPVFTypeUnknown) {
		t.Fatalf("unsupported PVF type error = %v", err)
	}
}

func TestCurrentEquipmentPlacementValidatorRejectsAccountOwnerMismatch(t *testing.T) {
	validator := newEquipmentPlacementValidatorFixture(t)
	validator.selectedAccountID = "dnf:other"
	err := validator.ValidateEquipmentPlacement(context.Background(), dnfequip.Placement{
		CharacterID:     "19",
		ItemID:          9001,
		SourceListType:  0,
		SourceSlotIndex: 9,
		TargetSlotIndex: 11,
	})
	if !errors.Is(err, errEquipmentPlacementOwnerMismatch) {
		t.Fatalf("account mismatch error = %v", err)
	}
}

func newEquipmentPlacementValidatorFixture(t *testing.T) *currentEquipmentPlacementValidator {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:account-1",
		Job:         "11",
		Level:       90,
		Stats:       map[string]int64{currentOpenAuraSkinSlotFlagStat: 1},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	return &currentEquipmentPlacementValidator{
		selectedAccountID:   "dnf:account-1",
		selectedCharacterID: 19,
		characters:          repositories.Character,
		pvfSource: equipmentPlacementPVFSource{
			"equipment/weapon/test.equ":        equipmentPlacementPVFDocument("[weapon]"),
			"equipment/title/test.equ":         equipmentPlacementPVFDocument("[title name]"),
			"equipment/earring/test.equ":       equipmentPlacementPVFDocument("[earring]"),
			"equipment/flag/test.equ":          equipmentPlacementPVFDocument("[flag]"),
			"equipment/avatar/hat/test.equ":    equipmentPlacementPVFDocument("[hat avatar]"),
			"equipment/avatar/aura/test.equ":   equipmentPlacementPVFDocument("[aurora avatar]"),
			"equipment/avatar/weapon/test.equ": equipmentPlacementPVFDocument("[weapon avatar]"),
		},
		equipmentPaths: map[int64]string{
			9001: "weapon/test.equ",
			9002: "title/test.equ",
			9025: "earring/test.equ",
			9032: "flag/test.equ",
			9100: "avatar/hat/test.equ",
			9109: "avatar/aura/test.equ",
			9110: "avatar/weapon/test.equ",
		},
	}
}

func equipmentPlacementPVFDocument(pvfType string) string {
	return "[name]\n`test equipment`\n[equipment type]\n`" + pvfType + "` 0\n"
}

func TestCurrentEquipmentPlacementValidatorOverEquipContractRaisesLevelGate(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:account-1",
		Job:         "11",
		Level:       10,
	}); err != nil {
		t.Fatal(err)
	}
	validator := &currentEquipmentPlacementValidator{
		selectedAccountID:   "dnf:account-1",
		selectedCharacterID: 19,
		characters:          repositories.Character,
		accounts:            repositories.Account,
		pvfSource: equipmentPlacementPVFSource{
			"equipment/weapon/test.equ": "[name]\n`test weapon`\n[equipment type]\n`[weapon]` 0\n[minimum level]\n15\n",
		},
		equipmentPaths: map[int64]string{9001: "weapon/test.equ"},
	}
	placement := dnfequip.Placement{
		CharacterID:     "19",
		ItemID:          9001,
		SourceListType:  currentEquipmentPlacementListMain,
		SourceSlotIndex: 9,
		TargetSlotIndex: 12,
	}

	// Level 10 vs PVF minimum 15: rejected without the contract.
	if err := validator.ValidateEquipmentPlacement(ctx, placement); !errors.Is(err, errEquipmentPlacementLevelInsufficient) {
		t.Fatalf("no-contract error = %v, want %v", err, errEquipmentPlacementLevelInsufficient)
	}

	// 霸王契约 (type 22): effective level 10+10=20 >= 15, accepted.
	future := time.Now().Add(24 * time.Hour).Unix()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "dnf:account-1",
		Metadata:  map[string]string{"premium_expire_22": strconv.FormatInt(future, 10)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateEquipmentPlacement(ctx, placement); err != nil {
		t.Fatalf("over-equip contract error = %v", err)
	}

	// An expired contract must not leak the bonus.
	past := time.Now().Add(-time.Hour).Unix()
	account, _, _ := repositories.Account.Load(ctx, "dnf:account-1")
	account.Metadata["premium_expire_22"] = strconv.FormatInt(past, 10)
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateEquipmentPlacement(ctx, placement); !errors.Is(err, errEquipmentPlacementLevelInsufficient) {
		t.Fatalf("expired-contract error = %v, want %v", err, errEquipmentPlacementLevelInsufficient)
	}
}
