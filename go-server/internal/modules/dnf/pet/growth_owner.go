package pet

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	equippedCreatureListType   byte  = 3
	equippedCreatureSlot       int16 = 26
	maxGrowthClearTokenBytes         = 256
	maxGrowthClearTokenHistory       = 128
)

var (
	ErrPetGrowthOwnerUnavailable = errors.New("pet growth owner unavailable")
	ErrPetGrowthTransaction      = errors.New("pet growth transaction unavailable")
	ErrPetGrowthTokenRequired    = errors.New("authoritative pet growth clear token is required")
	ErrPetGrowthEquippedMismatch = errors.New("equipped creature persistence is inconsistent")
)

// DungeonClearGrowthCommand is produced from an authoritative dungeon run.
// ClearToken must be server-owned run identity, never a request-provided value.
type DungeonClearGrowthCommand struct {
	SelectedCharacterID uint16
	ClearToken          string
	ConsumedFatigue     int
	// ApplyEvolution is intentionally opt-in. Current NoPack's incremental
	// experience route is proved, while the evolution wire is not. Online
	// callers must leave this false until that route is closed.
	ApplyEvolution bool
}

// PetElapsedCommand applies elapsed wall-clock state to the currently worn
// creature. Modifiers can only be populated by this package's typed PVF
// catalog (the field itself is private); the zero value means no artifacts.
type PetElapsedCommand struct {
	SelectedCharacterID uint16
	Elapsed             time.Duration
	Modifiers           PetSatietyModifiers
}

// PetGrowthPersistenceResult describes committed domain state and contains no
// current-client packet fields.
type PetGrowthPersistenceResult struct {
	CharacterID      string
	PetKey           string
	Equipped         bool
	Replayed         bool
	Changed          bool
	Before           PetGrowthState
	After            PetGrowthState
	ExperienceGained int64
	Evolution        PetEvolutionUpdate
	SatietyDelta     int
}

// GrowthOwner atomically owns creature experience, evolution and satiety.
type GrowthOwner struct {
	characterPets dnfrepo.CharacterPetUnitOfWork
	engine        *PetGrowthEngine
	now           func() time.Time
}

func NewGrowthOwner(repos dnfrepo.Group, engine *PetGrowthEngine) (*GrowthOwner, error) {
	if engine == nil {
		return nil, ErrPetGrowthOwnerUnavailable
	}
	if repos.CharacterPets == nil {
		return nil, ErrPetGrowthTransaction
	}
	return &GrowthOwner{characterPets: repos.CharacterPets, engine: engine, now: time.Now}, nil
}

// ApplyDungeonClear grants experience to exactly the persisted slot-26
// creature. The durable clear token is checked across every creature entry so
// switching the worn creature cannot replay the same run reward.
func (o *GrowthOwner) ApplyDungeonClear(ctx context.Context, cmd DungeonClearGrowthCommand) (PetGrowthPersistenceResult, error) {
	characterID, err := growthCharacterID(cmd.SelectedCharacterID)
	if err != nil {
		return PetGrowthPersistenceResult{}, err
	}
	token := strings.TrimSpace(cmd.ClearToken)
	if token == "" || token != cmd.ClearToken || len(token) > maxGrowthClearTokenBytes {
		return PetGrowthPersistenceResult{}, ErrPetGrowthTokenRequired
	}
	if cmd.ConsumedFatigue < 0 {
		return PetGrowthPersistenceResult{}, ErrPetGrowthStateInvalid
	}
	return o.withEquipped(ctx, characterID, func(
		petRecord *dnfrepo.PetRecord,
		petKey string,
		entry *dnfrepo.PetEntry,
		equipment *dnfrepo.EquipmentRecord,
		worn *dnfrepo.EquipmentEntry,
	) (PetGrowthPersistenceResult, error) {
		for _, candidate := range petRecord.Entries {
			if candidate.AppliedClearTokens[token] {
				state, stateErr := petGrowthState(*entry)
				if stateErr != nil {
					return PetGrowthPersistenceResult{}, stateErr
				}
				return PetGrowthPersistenceResult{
					CharacterID: characterID,
					PetKey:      petKey,
					Equipped:    true,
					Replayed:    true,
					Before:      state,
					After:       state,
				}, nil
			}
		}
		before, err := petGrowthState(*entry)
		if err != nil {
			return PetGrowthPersistenceResult{}, err
		}
		update, err := o.engine.applyDungeonClear(before, cmd.ConsumedFatigue, cmd.ApplyEvolution)
		if err != nil {
			return PetGrowthPersistenceResult{}, err
		}
		entry.Exp = update.After.Experience
		entry.Level = int64(update.After.Level)
		rememberGrowthClearToken(entry, token)
		if update.Evolution.Changed {
			if err := applyWornPetEvolution(entry, worn, update.Before.ItemID, update.After.ItemID); err != nil {
				return PetGrowthPersistenceResult{}, err
			}
			equipment.Entries[strconv.Itoa(int(equippedCreatureSlot))] = *worn
		}
		petRecord.Entries[petKey] = *entry
		return PetGrowthPersistenceResult{
			CharacterID:      characterID,
			PetKey:           petKey,
			Equipped:         true,
			Changed:          true,
			Before:           before,
			After:            update.After,
			ExperienceGained: update.ExperienceGained,
			Evolution:        update.Evolution,
		}, nil
	})
}

// rememberGrowthClearToken keeps a bounded, ordered idempotency window. Older
// rows that only contain the legacy map are normalized deterministically on
// their next successful clear, so existing JSON remains readable without
// allowing the entries blob to grow forever.
func rememberGrowthClearToken(entry *dnfrepo.PetEntry, token string) {
	if entry == nil || token == "" {
		return
	}
	active := make(map[string]bool, len(entry.AppliedClearTokens)+1)
	order := make([]string, 0, len(entry.AppliedClearTokens)+1)
	for _, candidate := range entry.AppliedClearTokenOrder {
		if candidate == "" || active[candidate] || !entry.AppliedClearTokens[candidate] {
			continue
		}
		active[candidate] = true
		order = append(order, candidate)
	}
	legacy := make([]string, 0, len(entry.AppliedClearTokens))
	for candidate, applied := range entry.AppliedClearTokens {
		if applied && candidate != "" && !active[candidate] {
			legacy = append(legacy, candidate)
		}
	}
	sort.Strings(legacy)
	for _, candidate := range legacy {
		active[candidate] = true
		order = append(order, candidate)
	}
	if !active[token] {
		active[token] = true
		order = append(order, token)
	}
	if overflow := len(order) - maxGrowthClearTokenHistory; overflow > 0 {
		order = append([]string(nil), order[overflow:]...)
		active = make(map[string]bool, len(order))
		for _, candidate := range order {
			active[candidate] = true
		}
	}
	entry.AppliedClearTokens = active
	entry.AppliedClearTokenOrder = order
}

func (o *GrowthOwner) ApplyDungeonElapsed(ctx context.Context, cmd PetElapsedCommand) (PetGrowthPersistenceResult, error) {
	return o.applySatiety(ctx, cmd, true)
}

func (o *GrowthOwner) ApplyTownElapsed(ctx context.Context, cmd PetElapsedCommand) (PetGrowthPersistenceResult, error) {
	return o.applySatiety(ctx, cmd, false)
}

func (o *GrowthOwner) applySatiety(ctx context.Context, cmd PetElapsedCommand, dungeon bool) (PetGrowthPersistenceResult, error) {
	characterID, err := growthCharacterID(cmd.SelectedCharacterID)
	if err != nil {
		return PetGrowthPersistenceResult{}, err
	}
	return o.withEquipped(ctx, characterID, func(
		petRecord *dnfrepo.PetRecord,
		petKey string,
		entry *dnfrepo.PetEntry,
		_ *dnfrepo.EquipmentRecord,
		_ *dnfrepo.EquipmentEntry,
	) (PetGrowthPersistenceResult, error) {
		before, err := petGrowthState(*entry)
		if err != nil {
			return PetGrowthPersistenceResult{}, err
		}
		var update PetSatietyUpdate
		if dungeon {
			update, err = o.engine.ApplyDungeonElapsed(before, cmd.Elapsed, cmd.Modifiers)
		} else {
			update, err = o.engine.ApplyTownElapsed(before, cmd.Elapsed)
		}
		if err != nil {
			return PetGrowthPersistenceResult{}, err
		}
		if update.Changed {
			entry.Satiety = byte(update.After.Satiety)
			entry.SatietyMicros = update.After.SatietyMicros
			petRecord.Entries[petKey] = *entry
		}
		return PetGrowthPersistenceResult{
			CharacterID:  characterID,
			PetKey:       petKey,
			Equipped:     true,
			Changed:      update.Changed,
			Before:       before,
			After:        update.After,
			SatietyDelta: update.SatietyDelta,
		}, nil
	})
}

type equippedGrowthMutation func(
	*dnfrepo.PetRecord,
	string,
	*dnfrepo.PetEntry,
	*dnfrepo.EquipmentRecord,
	*dnfrepo.EquipmentEntry,
) (PetGrowthPersistenceResult, error)

func (o *GrowthOwner) withEquipped(ctx context.Context, characterID string, mutate equippedGrowthMutation) (PetGrowthPersistenceResult, error) {
	if o == nil || o.engine == nil {
		return PetGrowthPersistenceResult{}, ErrPetGrowthOwnerUnavailable
	}
	if o.characterPets == nil {
		return PetGrowthPersistenceResult{}, ErrPetGrowthTransaction
	}
	var result PetGrowthPersistenceResult
	err := o.characterPets.WithinCharacterPets(ctx, characterID, func(
		_ dnfrepo.InventoryRepository,
		equipmentRepo dnfrepo.EquipmentRepository,
		petRepo dnfrepo.PetRepository,
	) error {
		petRecord, petFound, err := petRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		equipment, equipmentFound, err := equipmentRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !petFound && !equipmentFound {
			result = PetGrowthPersistenceResult{CharacterID: characterID}
			return nil
		}
		petRecord = dnfrepo.ClonePet(petRecord)
		petRecord.CharacterID = characterID
		equipment = dnfrepo.CloneEquipment(equipment)
		equipment.CharacterID = characterID
		petKey := strings.TrimSpace(petRecord.EquippedKey)
		worn, wornFound := equipment.Entries[strconv.Itoa(int(equippedCreatureSlot))]
		if petKey == "" && !wornFound {
			result = PetGrowthPersistenceResult{CharacterID: characterID}
			return nil
		}
		if petKey == "" {
			// A slot-26 row without PetRecord ownership is not automatically a
			// creature. Keep ordinary/unclassified equipment a no-op, but reject a
			// real creature row that lacks the matching durable pet key.
			if _, kindErr := o.engine.resolveCreature(worn.ItemID); errors.Is(kindErr, ErrPetPVFNotCreature) {
				result = PetGrowthPersistenceResult{CharacterID: characterID}
				return nil
			} else if kindErr != nil {
				return fmt.Errorf("%w: unclassified slot26 item=%d: %w", ErrPetGrowthEquippedMismatch, worn.ItemID, kindErr)
			}
			return fmt.Errorf("%w: slot26 creature item=%d has no equipped pet key", ErrPetGrowthEquippedMismatch, worn.ItemID)
		}
		if !wornFound {
			return ErrPetGrowthEquippedMismatch
		}
		entry, entryFound := petRecord.Entries[petKey]
		if !entryFound {
			return fmt.Errorf("%w: pet_key=%s missing", ErrPetGrowthEquippedMismatch, petKey)
		}
		if err := validateWornPetEntry(petKey, entry, worn); err != nil {
			return err
		}
		if _, err := o.engine.resolveCreature(entry.ItemID); err != nil {
			return fmt.Errorf("%w: pet_key=%s item=%d: %w", ErrPetGrowthEquippedMismatch, petKey, entry.ItemID, err)
		}
		result, err = mutate(&petRecord, petKey, &entry, &equipment, &worn)
		if err != nil {
			return err
		}
		if !result.Changed {
			return nil
		}
		now := o.now()
		petRecord.UpdatedAt = now
		if err := dnfrepo.SavePetFields(ctx, petRepo, petRecord, dnfrepo.PetFieldEntries); err != nil {
			return err
		}
		if result.Evolution.Changed {
			equipment.UpdatedAt = now
			if err := dnfrepo.SaveEquipmentFields(ctx, equipmentRepo, equipment, dnfrepo.EquipmentFieldEntries); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func growthCharacterID(selected uint16) (string, error) {
	if selected == 0 {
		return "", ErrCharacterRequired
	}
	return strconv.FormatUint(uint64(selected), 10), nil
}

func petGrowthState(entry dnfrepo.PetEntry) (PetGrowthState, error) {
	if entry.Level > math.MaxInt || entry.Exp > math.MaxInt64 {
		return PetGrowthState{}, ErrPetGrowthStateInvalid
	}
	state := PetGrowthState{
		ItemID:        entry.ItemID,
		Experience:    entry.Exp,
		Level:         int(entry.Level),
		Satiety:       int(entry.Satiety),
		SatietyMicros: entry.SatietyMicros,
	}
	if err := validatePetGrowthState(state); err != nil {
		return PetGrowthState{}, err
	}
	return state, nil
}

func validateWornPetEntry(petKey string, entry dnfrepo.PetEntry, worn dnfrepo.EquipmentEntry) error {
	if entry.SourceListType != equippedCreatureListType || entry.SourceSlotIndex != equippedCreatureSlot ||
		worn.SlotIndex != equippedCreatureSlot || entry.ItemID <= 0 || entry.ItemID > math.MaxUint32 || worn.ItemID != entry.ItemID ||
		entry.CreatureKey == 0 || strconv.FormatUint(uint64(entry.CreatureKey), 10) != petKey {
		return fmt.Errorf("%w: pet_key=%s item=%d worn_item=%d source=(%d,%d)", ErrPetGrowthEquippedMismatch, petKey, entry.ItemID, worn.ItemID, entry.SourceListType, entry.SourceSlotIndex)
	}
	if len(worn.RawEntry) < 28 || int16(worn.RawEntry[0]) != equippedCreatureSlot ||
		binary.LittleEndian.Uint32(worn.RawEntry[1:5]) != uint32(entry.ItemID) ||
		binary.LittleEndian.Uint32(worn.RawEntry[5:9]) != entry.CreatureKey ||
		binary.LittleEndian.Uint32(worn.RawEntry[24:28]) != entry.CreatureKey {
		return fmt.Errorf("%w: pet_key=%s invalid slot26 raw", ErrPetGrowthEquippedMismatch, petKey)
	}
	return nil
}

func applyWornPetEvolution(entry *dnfrepo.PetEntry, worn *dnfrepo.EquipmentEntry, beforeItemID, afterItemID int64) error {
	if entry == nil || worn == nil || beforeItemID <= 0 || afterItemID <= 0 || afterItemID > math.MaxUint32 ||
		entry.ItemID != beforeItemID || worn.ItemID != beforeItemID {
		return ErrPetGrowthEquippedMismatch
	}
	if err := validateWornPetEntry(entry.PetKey, *entry, *worn); err != nil {
		return err
	}
	entry.ItemID = afterItemID
	worn.ItemID = afterItemID
	worn.RawEntry = append([]byte(nil), worn.RawEntry...)
	binary.LittleEndian.PutUint32(worn.RawEntry[1:5], uint32(afterItemID))
	if worn.Extra != nil {
		cloned := make(map[string]string, len(worn.Extra))
		for key, value := range worn.Extra {
			cloned[key] = value
		}
		worn.Extra = cloned
		worn.Extra["raw_entry_hex"] = hex.EncodeToString(worn.RawEntry)
	}
	return nil
}
