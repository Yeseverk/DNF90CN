package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

const mysqlPetTable = "pets"

type mysqlPetStore struct {
	mysqlStoreBase
}

// Load reads one character creature record from MySQL.
func (s *mysqlPetStore) Load(ctx context.Context, characterID string) (repository.PetRecord, bool, error) {
	table, err := s.router.readTable(mysqlPetTable, characterID)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	entriesTable, err := s.router.readTable(mysqlPetEntriesTable, characterID)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	extraTable, err := s.router.readTable(mysqlPetExtraTable, characterID)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	tokensTable, err := s.router.readTable(mysqlPetTokensTable, characterID)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	artifactsTable, err := s.router.readTable(mysqlPetArtifactsTable, characterID)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	artifactExtraTable, err := s.router.readTable(mysqlPetArtifactExtraTable, characterID)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT character_id, equipped_key, town_display, updated_at FROM " + table + " WHERE character_id = ?")
	var record repository.PetRecord
	var updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, characterID).Scan(
		&record.CharacterID,
		&record.EquippedKey,
		&record.TownDisplay,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.PetRecord{}, ok, scanErr
	}
	record.Entries, err = loadPetEntries(ctx, s.router, entriesTable, extraTable, tokensTable, characterID)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	record.Artifacts, err = loadItemStackCollection(
		ctx, s.router.db, artifactsTable, artifactExtraTable, "character_id", characterID, "", false,
	)
	if err != nil {
		return repository.PetRecord{}, false, err
	}
	record.UpdatedAt = scanTime(updatedAt)
	return repository.ClonePet(record), true, nil
}

// Save writes the complete creature record.
func (s *mysqlPetStore) Save(ctx context.Context, record repository.PetRecord) error {
	return s.SaveFields(ctx, record, repository.AllPetFields()...)
}

// SaveFields writes selected creature fields.
func (s *mysqlPetStore) SaveFields(ctx context.Context, record repository.PetRecord, fields ...repository.PetField) error {
	characterID, err := requireRecordKey(repository.PetKey, record, "pet")
	if err != nil {
		return err
	}
	fields = repository.PetFields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	table, err := s.router.writeTable(mysqlPetTable, characterID)
	if err != nil {
		return err
	}
	entriesTable, err := s.router.writeTable(mysqlPetEntriesTable, characterID)
	if err != nil {
		return err
	}
	extraTable, err := s.router.writeTable(mysqlPetExtraTable, characterID)
	if err != nil {
		return err
	}
	tokensTable, err := s.router.writeTable(mysqlPetTokensTable, characterID)
	if err != nil {
		return err
	}
	artifactsTable, err := s.router.writeTable(mysqlPetArtifactsTable, characterID)
	if err != nil {
		return err
	}
	artifactExtraTable, err := s.router.writeTable(mysqlPetArtifactExtraTable, characterID)
	if err != nil {
		return err
	}
	columns := []string{"character_id", "updated_at"}
	args := []any{characterID, timeOrNow(record.UpdatedAt, s.router.now)}
	updates := []string{updateValue("updated_at")}
	saveEntries := false
	for _, field := range fields {
		switch field {
		case repository.PetFieldEntries:
			saveEntries = true
		case repository.PetFieldEquipped:
			columns = append(columns, "equipped_key")
			args = append(args, record.EquippedKey)
			updates = append(updates, updateValue("equipped_key"))
		case repository.PetFieldDisplay:
			columns = append(columns, "town_display")
			args = append(args, record.TownDisplay)
			updates = append(updates, updateValue("town_display"))
		}
	}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		if !saveEntries {
			return nil
		}
		if err := replacePetEntries(ctx, database, entriesTable, extraTable, tokensTable, characterID, record.Entries); err != nil {
			return err
		}
		return replaceItemStackCollection(
			ctx,
			database,
			artifactsTable,
			artifactExtraTable,
			"character_id",
			characterID,
			"",
			false,
			record.Artifacts,
		)
	})
}

func loadPetEntries(
	ctx context.Context,
	router mysqlRouter,
	entriesTable, extraTable, tokensTable, characterID string,
) (map[string]repository.PetEntry, error) {
	query := router.selectQuery("SELECT pet_key, creature_key, item_id, source_list_type, source_slot_index, pet_name, name_raw, satiety, satiety_micros, mode_flag, mode1_field_0a, mode1_field_0b, pet_level, pet_exp, tail_flag, raw_entry FROM " + entriesTable + " WHERE character_id = ? ORDER BY pet_key")
	rows, err := router.db.QueryContext(ctx, query, characterID)
	if err != nil {
		return nil, err
	}
	var entries map[string]repository.PetEntry
	for rows.Next() {
		var key string
		var entry repository.PetEntry
		var nameRaw, rawEntry []byte
		if err := rows.Scan(
			&key,
			&entry.CreatureKey,
			&entry.ItemID,
			&entry.SourceListType,
			&entry.SourceSlotIndex,
			&entry.Name,
			&nameRaw,
			&entry.Satiety,
			&entry.SatietyMicros,
			&entry.ModeFlag,
			&entry.Mode1Field0A,
			&entry.Mode1Field0B,
			&entry.Level,
			&entry.Exp,
			&entry.TailFlag,
			&rawEntry,
		); err != nil {
			rows.Close()
			return nil, err
		}
		entry.PetKey = key
		entry.NameRaw = append([]byte(nil), nameRaw...)
		entry.RawEntry = append([]byte(nil), rawEntry...)
		if entries == nil {
			entries = make(map[string]repository.PetEntry)
		}
		entries[key] = entry
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	extras, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT entry_key, extra_key, extra_value FROM "+extraTable+" WHERE character_id = ? ORDER BY entry_key, extra_key"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	for extras.Next() {
		var petKey, key, value string
		if err := extras.Scan(&petKey, &key, &value); err != nil {
			extras.Close()
			return nil, err
		}
		entry, ok := entries[petKey]
		if !ok {
			continue
		}
		if entry.Extra == nil {
			entry.Extra = make(map[string]string)
		}
		entry.Extra[key] = value
		entries[petKey] = entry
	}
	if err := extras.Err(); err != nil {
		extras.Close()
		return nil, err
	}
	if err := extras.Close(); err != nil {
		return nil, err
	}

	tokens, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT pet_key, token, applied FROM "+tokensTable+" WHERE character_id = ? ORDER BY pet_key, token_order"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	defer tokens.Close()
	for tokens.Next() {
		var petKey, token string
		var applied bool
		if err := tokens.Scan(&petKey, &token, &applied); err != nil {
			return nil, err
		}
		entry, ok := entries[petKey]
		if !ok {
			continue
		}
		if entry.AppliedClearTokens == nil {
			entry.AppliedClearTokens = make(map[string]bool)
		}
		entry.AppliedClearTokens[token] = applied
		entry.AppliedClearTokenOrder = append(entry.AppliedClearTokenOrder, token)
		entries[petKey] = entry
	}
	if err := tokens.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func replacePetEntries(
	ctx context.Context,
	database SQLDB,
	entriesTable, extraTable, tokensTable, characterID string,
	entries map[string]repository.PetEntry,
) error {
	if _, err := database.ExecContext(ctx, "DELETE FROM "+tokensTable+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM "+extraTable+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM "+entriesTable+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	entryQuery := "INSERT INTO " + entriesTable + " (character_id, pet_key, creature_key, item_id, source_list_type, source_slot_index, pet_name, name_raw, satiety, satiety_micros, mode_flag, mode1_field_0a, mode1_field_0b, pet_level, pet_exp, tail_flag, raw_entry) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	extraQuery := "INSERT INTO " + extraTable + " (character_id, entry_key, extra_key, extra_value) VALUES (?, ?, ?, ?)"
	tokenQuery := "INSERT INTO " + tokensTable + " (character_id, pet_key, token_order, token, applied) VALUES (?, ?, ?, ?, ?)"
	for _, petKey := range sortedStringKeys(entries) {
		entry := entries[petKey]
		if _, err := database.ExecContext(
			ctx,
			entryQuery,
			characterID,
			petKey,
			entry.CreatureKey,
			entry.ItemID,
			entry.SourceListType,
			entry.SourceSlotIndex,
			entry.Name,
			entry.NameRaw,
			entry.Satiety,
			entry.SatietyMicros,
			entry.ModeFlag,
			entry.Mode1Field0A,
			entry.Mode1Field0B,
			entry.Level,
			entry.Exp,
			entry.TailFlag,
			entry.RawEntry,
		); err != nil {
			return err
		}
		for _, key := range sortedStringKeys(entry.Extra) {
			if _, err := database.ExecContext(ctx, extraQuery, characterID, petKey, key, entry.Extra[key]); err != nil {
				return err
			}
		}
		tokenOrder := make([]string, 0, len(entry.AppliedClearTokens))
		seen := make(map[string]struct{}, len(entry.AppliedClearTokens))
		for _, token := range entry.AppliedClearTokenOrder {
			if _, ok := entry.AppliedClearTokens[token]; !ok {
				continue
			}
			if _, duplicate := seen[token]; duplicate {
				continue
			}
			seen[token] = struct{}{}
			tokenOrder = append(tokenOrder, token)
		}
		for _, token := range sortedStringKeys(entry.AppliedClearTokens) {
			if _, ok := seen[token]; ok {
				continue
			}
			tokenOrder = append(tokenOrder, token)
		}
		for index, token := range tokenOrder {
			if _, err := database.ExecContext(ctx, tokenQuery, characterID, petKey, index, token, entry.AppliedClearTokens[token]); err != nil {
				return err
			}
		}
	}
	return nil
}

const petEntriesEnvelopeVersion = 1

// petEntriesEnvelope extends the existing entries_json column without a
// schema migration. Legacy rows are a plain map of creature key to PetEntry;
// rows with equipped artifacts use this versioned envelope instead.
type petEntriesEnvelope struct {
	Version   int                             `json:"version"`
	Creatures map[string]repository.PetEntry  `json:"creatures,omitempty"`
	Artifacts map[string]repository.ItemStack `json:"artifacts,omitempty"`
}

func petEntriesJSONArg(record repository.PetRecord) (any, error) {
	if len(record.Artifacts) == 0 {
		return jsonArg(record.Entries)
	}
	return jsonArg(petEntriesEnvelope{
		Version:   petEntriesEnvelopeVersion,
		Creatures: record.Entries,
		Artifacts: record.Artifacts,
	})
}

func scanPetEntries(value sql.NullString, record *repository.PetRecord) error {
	if record == nil {
		return fmt.Errorf("pet record is nil")
	}
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}

	var shape map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value.String), &shape); err != nil {
		return err
	}
	_, hasVersion := shape["version"]
	_, hasCreatures := shape["creatures"]
	_, hasArtifacts := shape["artifacts"]
	if !hasVersion && !hasCreatures && !hasArtifacts {
		return json.Unmarshal([]byte(value.String), &record.Entries)
	}

	var envelope petEntriesEnvelope
	if err := json.Unmarshal([]byte(value.String), &envelope); err != nil {
		return err
	}
	if envelope.Version != petEntriesEnvelopeVersion {
		return fmt.Errorf("unsupported pet entries envelope version %d", envelope.Version)
	}
	record.Entries = envelope.Creatures
	record.Artifacts = envelope.Artifacts
	return nil
}
