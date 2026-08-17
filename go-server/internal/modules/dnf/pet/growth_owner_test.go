package pet

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestGrowthOwnerDungeonClearPersistsOnceAndAtomicallyEvolvesWornCreature(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner := newGrowthTestOwner(t, repos)
	seedGrowthTestCreature(t, ctx, repos, 77, 37, 10, 1, 1, 100)

	result, err := owner.ApplyDungeonClear(ctx, DungeonClearGrowthCommand{
		SelectedCharacterID: 77,
		ClearToken:          "run-77-room-final",
		ConsumedFatigue:     1,
		ApplyEvolution:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Replayed || result.ExperienceGained != 1 ||
		result.After.ItemID != 11 || result.After.Level != 2 || result.After.Experience != 2 || !result.Evolution.Changed {
		t.Fatalf("result=%+v", result)
	}

	petRecord, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load pet ok=%t err=%v", ok, err)
	}
	entry := petRecord.Entries["37"]
	if entry.ItemID != 11 || entry.Level != 2 || entry.Exp != 2 || entry.Satiety != 100 ||
		string(entry.NameRaw) != "Mina" || entry.CreatureKey != 37 || !entry.AppliedClearTokens["run-77-room-final"] {
		t.Fatalf("entry=%+v", entry)
	}
	equipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load equipment ok=%t err=%v", ok, err)
	}
	worn := equipment.Entries["26"]
	if worn.ItemID != 11 || binary.LittleEndian.Uint32(worn.RawEntry[1:5]) != 11 ||
		binary.LittleEndian.Uint32(worn.RawEntry[5:9]) != 37 ||
		binary.LittleEndian.Uint32(worn.RawEntry[24:28]) != 37 || worn.Extra["preserve"] != "yes" {
		t.Fatalf("worn=%+v raw=%x", worn, worn.RawEntry)
	}
	expectedRaw := make([]byte, 48)
	expectedRaw[0] = byte(equippedCreatureSlot)
	binary.LittleEndian.PutUint32(expectedRaw[1:5], 11)
	binary.LittleEndian.PutUint32(expectedRaw[5:9], 37)
	binary.LittleEndian.PutUint32(expectedRaw[24:28], 37)
	if !reflect.DeepEqual(worn.RawEntry, expectedRaw) {
		t.Fatalf("evolution changed raw outside item field: got=%x want=%x", worn.RawEntry, expectedRaw)
	}

	replay, err := owner.ApplyDungeonClear(ctx, DungeonClearGrowthCommand{
		SelectedCharacterID: 77,
		ClearToken:          "run-77-room-final",
		ConsumedFatigue:     99,
		ApplyEvolution:      true,
	})
	if err != nil || !replay.Replayed || replay.Changed || replay.After.Experience != 2 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestRememberGrowthClearTokenBoundsAndMigratesLegacyMap(t *testing.T) {
	entry := dnfrepo.PetEntry{
		AppliedClearTokens: map[string]bool{
			"legacy-b": true,
			"legacy-a": true,
			"ignored":  false,
		},
	}
	rememberGrowthClearToken(&entry, "run-000")
	if got, want := entry.AppliedClearTokenOrder[:3], []string{"legacy-a", "legacy-b", "run-000"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy order = %v, want %v", got, want)
	}
	for index := 1; index <= maxGrowthClearTokenHistory+8; index++ {
		rememberGrowthClearToken(&entry, fmt.Sprintf("run-%03d", index))
	}
	if got := len(entry.AppliedClearTokenOrder); got != maxGrowthClearTokenHistory {
		t.Fatalf("order length = %d, want %d", got, maxGrowthClearTokenHistory)
	}
	if got := len(entry.AppliedClearTokens); got != maxGrowthClearTokenHistory {
		t.Fatalf("token map length = %d, want %d", got, maxGrowthClearTokenHistory)
	}
	if entry.AppliedClearTokens["legacy-a"] || entry.AppliedClearTokens["run-000"] {
		t.Fatalf("old tokens were not evicted: %#v", entry.AppliedClearTokens)
	}
	last := fmt.Sprintf("run-%03d", maxGrowthClearTokenHistory+8)
	if !entry.AppliedClearTokens[last] || entry.AppliedClearTokenOrder[len(entry.AppliedClearTokenOrder)-1] != last {
		t.Fatalf("newest token missing: last=%q map=%v", last, entry.AppliedClearTokens[last])
	}
}

func TestGrowthOwnerSameClearTokenIsRaceIdempotent(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner := newGrowthTestOwner(t, repos)
	seedGrowthTestCreature(t, ctx, repos, 78, 38, 10, 1, 1, 100)

	const callers = 32
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	changed := make(chan bool, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := owner.ApplyDungeonClear(ctx, DungeonClearGrowthCommand{
				SelectedCharacterID: 78,
				ClearToken:          "authoritative-run-78",
				ConsumedFatigue:     1,
				ApplyEvolution:      true,
			})
			errs <- err
			changed <- result.Changed
		}()
	}
	wg.Wait()
	close(errs)
	close(changed)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	writes := 0
	for applied := range changed {
		if applied {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("changed writes=%d want=1", writes)
	}
	record, _, _ := repos.Pet.Load(ctx, "78")
	if got := record.Entries["38"]; got.Exp != 2 || got.Level != 2 || got.ItemID != 11 || len(got.AppliedClearTokens) != 1 {
		t.Fatalf("entry=%+v", got)
	}
}

func TestGrowthOwnerOnlinePolicyCommitsLevelWithoutUnprovedEvolution(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner := newGrowthTestOwner(t, repos)
	seedGrowthTestCreature(t, ctx, repos, 88, 48, 10, 1, 1, 100)

	result, err := owner.ApplyDungeonClear(ctx, DungeonClearGrowthCommand{
		SelectedCharacterID: 88,
		ClearToken:          "online-current-exe-room",
		ConsumedFatigue:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.ExperienceGained != 1 || result.After.Experience != 2 || result.After.Level != 2 ||
		result.After.ItemID != 10 || result.Evolution.Changed || result.Evolution.QuestEligible {
		t.Fatalf("result=%+v", result)
	}
	record, _, _ := repos.Pet.Load(ctx, "88")
	equipment, _, _ := repos.Equipment.Load(ctx, "88")
	if record.Entries["48"].ItemID != 10 || equipment.Entries["26"].ItemID != 10 {
		t.Fatalf("unproved evolution leaked pet=%+v equipment=%+v", record.Entries["48"], equipment.Entries["26"])
	}
}

func TestGrowthOwnerOnlinePolicyDoesNotRequireEvolutionIdentity(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	source := petGrowthTestSource(false)
	delete(source, petCreatureListPath)
	catalog, err := NewPVFCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewGrowthOwner(repos, engine)
	if err != nil {
		t.Fatal(err)
	}
	seedGrowthTestCreature(t, ctx, repos, 89, 49, 10, 1, 1, 100)

	result, err := owner.ApplyDungeonClear(ctx, DungeonClearGrowthCommand{
		SelectedCharacterID: 89,
		ClearToken:          "online-current-exe-unmapped-evolution",
		ConsumedFatigue:     1,
		ApplyEvolution:      false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.ExperienceGained != 1 || result.After.Experience != 2 ||
		result.After.Level != 2 || result.After.ItemID != 10 ||
		result.Evolution.Changed || result.Evolution.QuestEligible {
		t.Fatalf("result=%+v", result)
	}
	record, _, _ := repos.Pet.Load(ctx, "89")
	if got := record.Entries["49"]; got.Exp != 2 || got.Level != 2 ||
		got.ItemID != 10 || !got.AppliedClearTokens["online-current-exe-unmapped-evolution"] {
		t.Fatalf("entry=%+v", got)
	}
}

func TestGrowthOwnerPersistsDungeonConsumptionAndTownRecovery(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner := newGrowthTestOwner(t, repos)
	seedGrowthTestCreature(t, ctx, repos, 79, 39, 10, 1, 0, 10)

	consumed, err := owner.ApplyDungeonElapsed(ctx, PetElapsedCommand{
		SelectedCharacterID: 79,
		Elapsed:             2 * time.Minute,
	})
	if err != nil || !consumed.Changed || consumed.SatietyDelta != -2 || consumed.After.Satiety != 8 {
		t.Fatalf("consumed=%+v err=%v", consumed, err)
	}
	recovered, err := owner.ApplyTownElapsed(ctx, PetElapsedCommand{
		SelectedCharacterID: 79,
		Elapsed:             12 * time.Minute,
	})
	if err != nil || !recovered.Changed || recovered.SatietyDelta != 2 || recovered.After.Satiety != 10 {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	record, _, _ := repos.Pet.Load(ctx, "79")
	if got := record.Entries["39"]; got.Satiety != 10 || got.ItemID != 10 || got.Exp != 0 || got.Level != 1 {
		t.Fatalf("entry=%+v", got)
	}
}

func TestGrowthOwnerFailsClosedOnWornProjectionAndLateEvolutionSave(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner := newGrowthTestOwner(t, repos)
	seedGrowthTestCreature(t, ctx, repos, 80, 40, 10, 1, 1, 100)

	equipment, _, _ := repos.Equipment.Load(ctx, "80")
	broken := equipment.Entries["26"]
	broken.ItemID = 999
	equipment.Entries["26"] = broken
	if err := repos.Equipment.Save(ctx, equipment); err != nil {
		t.Fatal(err)
	}
	_, err := owner.ApplyDungeonClear(ctx, DungeonClearGrowthCommand{SelectedCharacterID: 80, ClearToken: "broken", ConsumedFatigue: 1})
	if !errors.Is(err, ErrPetGrowthEquippedMismatch) {
		t.Fatalf("err=%v", err)
	}
	record, _, _ := repos.Pet.Load(ctx, "80")
	if got := record.Entries["40"]; got.Exp != 1 || len(got.AppliedClearTokens) != 0 {
		t.Fatalf("mutated entry=%+v", got)
	}

	seedGrowthTestCreature(t, ctx, repos, 81, 41, 10, 1, 1, 100)
	failing := repos
	failing.CharacterPets = failEquipmentPetUOW{base: repos.CharacterPets}
	failingOwner := newGrowthTestOwner(t, failing)
	_, err = failingOwner.ApplyDungeonClear(ctx, DungeonClearGrowthCommand{SelectedCharacterID: 81, ClearToken: "late-fail", ConsumedFatigue: 1, ApplyEvolution: true})
	if !errors.Is(err, errGrowthEquipmentSave) {
		t.Fatalf("err=%v", err)
	}
	record, _, _ = repos.Pet.Load(ctx, "81")
	if got := record.Entries["41"]; got.Exp != 1 || got.Level != 1 || got.ItemID != 10 || len(got.AppliedClearTokens) != 0 {
		t.Fatalf("pet rollback failed: %+v", got)
	}
	equipment, _, _ = repos.Equipment.Load(ctx, "81")
	if got := equipment.Entries["26"]; got.ItemID != 10 || binary.LittleEndian.Uint32(got.RawEntry[1:5]) != 10 {
		t.Fatalf("equipment rollback failed: %+v", got)
	}
}

func TestGrowthOwnerRejectsOrdinarySlot26ItemAndMissingCreatureSerialAtOffset24(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner := newGrowthTestOwner(t, repos)
	ordinaryRaw := make([]byte, 28)
	ordinaryRaw[0] = byte(equippedCreatureSlot)
	binary.LittleEndian.PutUint32(ordinaryRaw[1:5], 30)
	binary.LittleEndian.PutUint32(ordinaryRaw[5:9], 777)
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "84",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: equippedCreatureSlot, ItemID: 30, RawEntry: ordinaryRaw},
		},
	}); err != nil {
		t.Fatal(err)
	}
	noPet, err := owner.ApplyTownElapsed(ctx, PetElapsedCommand{SelectedCharacterID: 84, Elapsed: time.Minute})
	if err != nil || noPet.Equipped || noPet.Changed || noPet.CharacterID != "84" {
		t.Fatalf("ordinary support weapon no-op=%+v err=%v", noPet, err)
	}

	// Even a forged/stale PetRecord and a raw +5 instance that agree with the
	// pet key cannot turn a real PVF [support weapon] into a creature.
	seedGrowthTestCreature(t, ctx, repos, 82, 42, 30, 1, 0, 100)
	_, err = owner.ApplyTownElapsed(ctx, PetElapsedCommand{SelectedCharacterID: 82, Elapsed: time.Minute})
	if !errors.Is(err, ErrPetGrowthEquippedMismatch) || !errors.Is(err, ErrPetPVFNotCreature) {
		t.Fatalf("support weapon error=%v", err)
	}
	record, _, _ := repos.Pet.Load(ctx, "82")
	if got := record.Entries["42"]; got.Satiety != 100 {
		t.Fatalf("support weapon path mutated pet=%+v", got)
	}

	seedGrowthTestCreature(t, ctx, repos, 83, 43, 10, 1, 0, 100)
	equipment, _, _ := repos.Equipment.Load(ctx, "83")
	worn := equipment.Entries["26"]
	binary.LittleEndian.PutUint32(worn.RawEntry[24:28], 0)
	equipment.Entries["26"] = worn
	if err := repos.Equipment.Save(ctx, equipment); err != nil {
		t.Fatal(err)
	}
	_, err = owner.ApplyTownElapsed(ctx, PetElapsedCommand{SelectedCharacterID: 83, Elapsed: time.Minute})
	if !errors.Is(err, ErrPetGrowthEquippedMismatch) {
		t.Fatalf("missing +24 creature serial error=%v", err)
	}
	record, _, _ = repos.Pet.Load(ctx, "83")
	if got := record.Entries["43"]; got.Satiety != 100 {
		t.Fatalf("invalid +24 path mutated pet=%+v", got)
	}
}

func newGrowthTestOwner(t *testing.T, repos dnfrepo.Group) *GrowthOwner {
	t.Helper()
	engine, err := NewPetGrowthEngine(newPetGrowthTestCatalog(t, false))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewGrowthOwner(repos, engine)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func seedGrowthTestCreature(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID uint16, serial uint32, itemID, level, exp int64, satiety byte) {
	t.Helper()
	key := stringID(characterID)
	petKey := uint32ID(serial)
	raw := make([]byte, 48)
	raw[0] = byte(equippedCreatureSlot)
	binary.LittleEndian.PutUint32(raw[1:5], uint32(itemID))
	binary.LittleEndian.PutUint32(raw[5:9], serial)
	binary.LittleEndian.PutUint32(raw[24:28], serial)
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: key,
		EquippedKey: petKey,
		TownDisplay: true,
		Entries: map[string]dnfrepo.PetEntry{
			petKey: {
				PetKey:          petKey,
				CreatureKey:     serial,
				ItemID:          itemID,
				SourceListType:  equippedCreatureListType,
				SourceSlotIndex: equippedCreatureSlot,
				NameRaw:         []byte("Mina"),
				Satiety:         satiety,
				Level:           level,
				Exp:             exp,
				Extra:           map[string]string{"preserve": "pet"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: key,
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: equippedCreatureSlot, ItemID: itemID, RawEntry: raw, Extra: map[string]string{"preserve": "yes"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func stringID(value uint16) string { return uint32ID(uint32(value)) }

func uint32ID(value uint32) string {
	if value == 0 {
		return "0"
	}
	var buf [10]byte
	index := len(buf)
	for value > 0 {
		index--
		buf[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[index:])
}

var errGrowthEquipmentSave = errors.New("test equipment save failure")

type failEquipmentPetUOW struct {
	base dnfrepo.CharacterPetUnitOfWork
}

func (u failEquipmentPetUOW) WithinCharacterPets(ctx context.Context, characterID string, apply func(dnfrepo.InventoryRepository, dnfrepo.EquipmentRepository, dnfrepo.PetRepository) error) error {
	return u.base.WithinCharacterPets(ctx, characterID, func(inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository, pets dnfrepo.PetRepository) error {
		return apply(inventory, failEquipmentRepository{EquipmentRepository: equipment}, pets)
	})
}

type failEquipmentRepository struct{ dnfrepo.EquipmentRepository }

func (failEquipmentRepository) Save(context.Context, dnfrepo.EquipmentRecord) error {
	return errGrowthEquipmentSave
}
