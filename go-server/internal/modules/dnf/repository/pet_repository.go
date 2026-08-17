package repository

import (
	"context"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// PetRepository stores hatched creature state for one character.
type PetRepository interface {
	db.Store[PetRecord]
}

// PetRecord is the durable creature list owned by a character.
type PetRecord struct {
	CharacterID string              `json:"character_id"`
	Entries     map[string]PetEntry `json:"entries,omitempty"`
	// Artifacts is keyed by the semantic PVF artifact kind (red/blue/green).
	// It deliberately does not use historical client equipment-slot numbers:
	// those numbers overlap ordinary equipment in the current EXE.
	Artifacts   map[string]ItemStack `json:"artifacts,omitempty"`
	EquippedKey string               `json:"equipped_key,omitempty"`
	TownDisplay bool                 `json:"town_display,omitempty"`
	UpdatedAt   time.Time            `json:"updated_at,omitempty"`
}

// PetEntry keeps one creature entry plus raw evidence needed by later USERINFO writers.
type PetEntry struct {
	PetKey          string `json:"pet_key"`
	CreatureKey     uint32 `json:"creature_key,omitempty"`
	ItemID          int64  `json:"item_id"`
	SourceListType  byte   `json:"source_list_type,omitempty"`
	SourceSlotIndex int16  `json:"source_slot_index,omitempty"`
	Name            string `json:"name,omitempty"`
	NameRaw         []byte `json:"name_raw,omitempty"`
	Satiety         byte   `json:"satiety,omitempty"`
	// SatietyMicros keeps the server-owned fractional gauge so repeated scene
	// transitions or artifact swaps cannot discard/duplicate partial elapsed
	// time. Zero is also the correct representation for an empty gauge; legacy
	// rows with Satiety>0 and no value are promoted from Satiety on first use.
	SatietyMicros int64 `json:"satiety_micros,omitempty"`
	ModeFlag      byte  `json:"mode_flag,omitempty"`
	Mode1Field0A  byte  `json:"mode1_field_0a,omitempty"`
	Mode1Field0B  byte  `json:"mode1_field_0b,omitempty"`
	Level         int64 `json:"level,omitempty"`
	Exp           int64 `json:"exp,omitempty"`
	TailFlag      byte  `json:"tail_flag,omitempty"`
	// AppliedClearTokens is durable domain idempotency evidence for creature
	// experience grants. Tokens are issued by the authoritative dungeon run;
	// packet/request values must never be inserted here directly.
	AppliedClearTokens     map[string]bool `json:"applied_clear_tokens,omitempty"`
	AppliedClearTokenOrder []string        `json:"applied_clear_token_order,omitempty"`
	// RawEntry is retained only so existing JSON rows remain readable. New
	// protocol builders must encode the typed fields above and never replay it.
	RawEntry []byte            `json:"raw_entry,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
}

// PetField identifies fields that can be saved independently.
type PetField string

const (
	PetFieldEntries  PetField = "entries"
	PetFieldEquipped PetField = "equipped"
	PetFieldDisplay  PetField = "display"
)

// SavePetFields saves selected pet fields, falling back to whole-record save when needed.
func SavePetFields(ctx context.Context, repo PetRepository, record PetRecord, fields ...PetField) error {
	return db.SaveFields(ctx, repo, record, PetFields.Normalize, fields...)
}

// ClonePet detaches mutable maps and raw bytes from the caller.
func ClonePet(record PetRecord) PetRecord {
	record.Artifacts = cloneItemMap(record.Artifacts)
	if len(record.Entries) == 0 {
		record.Entries = nil
		return record
	}
	out := make(map[string]PetEntry, len(record.Entries))
	for key, entry := range record.Entries {
		entry.NameRaw = append([]byte(nil), entry.NameRaw...)
		entry.RawEntry = append([]byte(nil), entry.RawEntry...)
		entry.Extra = cloneStringMap(entry.Extra)
		if len(entry.AppliedClearTokens) == 0 {
			entry.AppliedClearTokens = nil
		} else {
			tokens := make(map[string]bool, len(entry.AppliedClearTokens))
			for token, applied := range entry.AppliedClearTokens {
				tokens[token] = applied
			}
			entry.AppliedClearTokens = tokens
		}
		entry.AppliedClearTokenOrder = append([]string(nil), entry.AppliedClearTokenOrder...)
		out[key] = entry
	}
	record.Entries = out
	return record
}

func PetKey(record PetRecord) string {
	return strings.TrimSpace(record.CharacterID)
}
