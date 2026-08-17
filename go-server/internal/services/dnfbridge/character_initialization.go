package dnfbridge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	dnfcharacterdata "longheng.io/server/internal/modules/dnf/characterdata"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// ensureCharacterInitializationSnapshot is the Go equivalent of 86JP's
// EnsureDatabase: a legacy character gets a structured, durable initial
// snapshot exactly once.  Existing records, including intentionally empty
// inventories/equipment, are authoritative and never receive a reseed.
func (s *Service) ensureCharacterInitializationSnapshot(
	ctx context.Context,
	repos dnfrepo.Group,
	record dnfrepo.CharacterRecord,
) (dnfcharacterdata.InitializationResult, error) {
	characterID := strings.TrimSpace(record.CharacterID)
	if characterID == "" {
		return dnfcharacterdata.InitializationResult{}, nil
	}
	presence, err := characterInitializationPresenceForRecord(ctx, repos, record)
	if err != nil {
		return dnfcharacterdata.InitializationResult{}, err
	}
	if presence.complete() {
		return dnfcharacterdata.InitializationResult{}, nil
	}

	now := time.Now().UTC()
	initialization := dnfcharacterdata.Initialization{CharacterID: characterID}
	if !presence.inventory {
		inventory := newCharacterInventoryRecord(characterID, now)
		initialization.Inventory = &inventory
	}

	job, hasJob := characterJobByte(record)
	if !presence.equipment {
		if !hasJob {
			return dnfcharacterdata.InitializationResult{}, fmt.Errorf("character %s has invalid job %q for initial equipment", characterID, record.Job)
		}
		entries, loadErr := s.initialCharacterEquipment(ctx, job)
		if loadErr != nil {
			return dnfcharacterdata.InitializationResult{}, fmt.Errorf("load current PVF initial equipment: %w", loadErr)
		}
		equipment := initialEquipmentRecord(characterID, entries, now)
		// An empty record is meaningful: it prevents future login from treating
		// an intentionally item-less job as an uninitialized character.
		initialization.Equipment = &equipment
	}

	if !presence.skill {
		if !hasJob {
			return dnfcharacterdata.InitializationResult{}, fmt.Errorf("character %s has invalid job %q for initial skills", characterID, record.Job)
		}
		skills, loadErr := s.initialCharacterSkills(ctx, job)
		if loadErr != nil {
			return dnfcharacterdata.InitializationResult{}, fmt.Errorf("load current PVF initial skills: %w", loadErr)
		}
		level := record.Level
		if level <= 0 {
			level = newCharacterInitialLevel
		}
		points, loadErr := s.initialSkillPoints(ctx, level)
		if loadErr != nil {
			return dnfcharacterdata.InitializationResult{}, fmt.Errorf("load current PVF initial skill points: %w", loadErr)
		}
		points.RemainingSP = points.TotalSP
		points.RemainingTP = points.TotalTP
		if points.SyncedLevel <= 0 {
			points.SyncedLevel = level
		}
		skill := initialSkillRecord(record, characterPVFInitialization{
			Skills:      skills,
			SkillPoints: points,
		}, now)
		initialization.Skill = &skill
	}

	if len(presence.settings) > 0 {
		for _, setting := range s.newCharacterCSharpDefaultSettings(ctx, record, now) {
			if !presence.settings[setting.Scope] {
				initialization.Settings = append(initialization.Settings, setting)
			}
		}
	}

	result, err := dnfcharacterdata.NewInitializer(repos).Ensure(ctx, initialization)
	if err != nil {
		return dnfcharacterdata.InitializationResult{}, err
	}
	if result.Changed() {
		s.logPacketEvent("dnf-character-initialization-backfilled",
			"character_id", characterID,
			"inventory", result.Inventory,
			"equipment", result.Equipment,
			"skill", result.Skill,
			"settings", strings.Join(sortedInitializationScopes(result.Settings), ","),
			"source", "86jp_missing_only_structured_snapshot")
	}
	return result, nil
}

type characterInitializationPresence struct {
	inventory bool
	equipment bool
	skill     bool
	settings  map[string]bool
}

func (p characterInitializationPresence) complete() bool {
	if !p.inventory || !p.equipment || !p.skill || len(p.settings) == 0 {
		return false
	}
	for _, found := range p.settings {
		if !found {
			return false
		}
	}
	return true
}

func characterInitializationPresenceForRecord(
	ctx context.Context,
	repos dnfrepo.Group,
	record dnfrepo.CharacterRecord,
) (characterInitializationPresence, error) {
	characterID := strings.TrimSpace(record.CharacterID)
	if characterID == "" {
		return characterInitializationPresence{}, dnfcharacterdata.ErrCharacterIDRequired
	}
	if repos.Inventory == nil || repos.Equipment == nil || repos.Skill == nil || repos.Settings == nil {
		return characterInitializationPresence{}, fmt.Errorf("character initialization repository is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	presence := characterInitializationPresence{settings: make(map[string]bool, 3)}
	var err error
	_, presence.inventory, err = repos.Inventory.Load(ctx, characterID)
	if err != nil {
		return characterInitializationPresence{}, fmt.Errorf("load character inventory initialization state: %w", err)
	}
	_, presence.equipment, err = repos.Equipment.Load(ctx, characterID)
	if err != nil {
		return characterInitializationPresence{}, fmt.Errorf("load character equipment initialization state: %w", err)
	}
	_, presence.skill, err = repos.Skill.Load(ctx, characterID)
	if err != nil {
		return characterInitializationPresence{}, fmt.Errorf("load character skill initialization state: %w", err)
	}

	// These scopes identify the complete 86JP-compatible structured snapshot;
	// only their absence is repaired. Values in an existing scope are untouched.
	for _, scope := range []string{
		newCharacterContainerStateSettingsScope(characterID),
		newCharacterInitBodiesSettingsScope(characterID),
		newCharacterHotkeySettingsScope(characterID),
	} {
		_, found, loadErr := repos.Settings.Load(ctx, scope)
		if loadErr != nil {
			return characterInitializationPresence{}, fmt.Errorf("load character setting initialization state %q: %w", scope, loadErr)
		}
		presence.settings[scope] = found
	}
	return presence, nil
}

func sortedInitializationScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	out := append([]string(nil), scopes...)
	sort.Strings(out)
	return out
}
