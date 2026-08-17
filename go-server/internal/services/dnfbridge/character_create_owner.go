package dnfbridge

import (
	"context"
	"fmt"
	"time"

	dnfcharacterdata "longheng.io/server/internal/modules/dnf/characterdata"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) saveNewCharacter(ctx context.Context, repos dnfrepo.Group, record dnfrepo.CharacterRecord) error {
	now := record.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	init, initErr := s.newCharacterPVFInitialization(ctx, record)
	if initErr != nil {
		s.logPacketEvent("dnf-character-pvf-initialization-blocked",
			"character_id", record.CharacterID,
			"job", record.Job,
			"level", record.Level,
			"error", initErr)
		return fmt.Errorf("initialize new character from pvf: %w", initErr)
	}
	if init.HasStats {
		applyCharacterPVFStats(&record, init.Stats)
	}
	var equipment *dnfrepo.EquipmentRecord
	if built := initialEquipmentRecord(record.CharacterID, init.Equipment, now); len(built.Entries) > 0 {
		equipment = &built
	}
	skill := initialSkillRecord(record, init, now)
	settings := s.newCharacterCSharpDefaultSettings(ctx, record, now)
	creation := dnfcharacterdata.Creation{
		Account: &dnfrepo.AccountRecord{
			AccountID: record.AccountID,
			State:     "active",
			Metadata:  map[string]string{"source": "dnfbridge"},
			CreatedAt: record.CreatedAt,
			UpdatedAt: now,
		},
		Character: record,
		Inventory: ptrInventoryRecord(newCharacterInventoryRecord(record.CharacterID, now)),
		Equipment: equipment,
		Skill:     &skill,
		Settings:  settings,
	}
	if err := dnfcharacterdata.NewCreator(repos).Create(ctx, creation); err != nil {
		return fmt.Errorf("persist new character initialization: %w", err)
	}
	if len(init.Equipment) > 0 {
		s.logPacketEvent("dnf-character-initial-equipment-seeded",
			"character_id", record.CharacterID,
			"job", record.Job,
			"count", len(init.Equipment),
			"source", "pvf_create_equipment_list")
	}
	return nil
}

func ptrInventoryRecord(record dnfrepo.InventoryRecord) *dnfrepo.InventoryRecord {
	return &record
}
