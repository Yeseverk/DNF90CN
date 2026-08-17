package dnfbridge

import (
	"context"
	"strconv"

	dnfcharacterdata "longheng.io/server/internal/modules/dnf/characterdata"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) resolveSelectedCharacter(
	ctx context.Context,
	session *gameSession,
	request []byte,
) (uint16, string, dnfrepo.CharacterRecord, bool, int) {
	parsed := parseSelectCharacterRequest(request)
	slot := parsed.slot
	if !parsed.clear && len(request) > 0 {
		s.logGameEvent(session, "game-upper-select-character-request-opaque",
			"body_len", len(request),
			"parsed_slot", slot,
			"source", parsed.source)
	} else if parsed.source == "nopack_key4_slot" || parsed.source == "nopack_plain_slot" {
		s.logGameEvent(session, "game-upper-select-character-request-decoded",
			"body_len", len(request),
			"slot", slot,
			"source", parsed.source)
	}
	if repos, ok := s.repositoryGroup(); ok && repos.Character != nil {
		characters, err := s.listCharacters(ctx, repos, s.accountIDForSession(session))
		if err != nil {
			s.logGameEvent(session, "game-upper-select-character-list-failed", "slot", slot, "error", err)
		} else if len(characters) > 0 {
			if selected, selectedBy, ok := selectCharacterRecord(characters, parsed); ok {
				if repos.Equipment != nil {
					s.attachEquipmentSummary(ctx, repos, &selected)
				}
				// listCharacters is a roster projection. Restore the complete
				// structured map before persisting level-stat repairs so a
				// partial row cannot erase unrelated character state.
				charIDStr := strconv.FormatUint(uint64(numericCharacterID(selected)), 10)
				if fullChar, found, err := repos.Character.Load(ctx, charIDStr); err == nil && found && fullChar.Stats != nil {
					selected.Stats = fullChar.Stats
				}
				if stats, ok := s.characterPVFStatsForUserInfo(ctx, session, selected, true); ok {
					needsRepair := characterPVFStatsNeedRepair(selected, stats)
					applyCharacterPVFStats(&selected, stats)
					if needsRepair {
						if err := repairSelectedCharacterPVFStats(ctx, repos.Character, selected); err != nil {
							s.logGameEvent(session, "game-upper-select-character-stat-repair-failed",
								"character_id", selected.CharacterID,
								"level", selected.Level,
								"error", err)
						}
					}
				}
				charID := uint16(numericCharacterID(selected))
				if charID == 0 {
					charID = 1
				}
				charName := selected.Name
				if charName == "" {
					charName = csharpSelectCharacterName(charID)
				}
				s.logGameEvent(session, "game-upper-select-character-resolved",
					"request_slot", slot,
					"record_slot", selected.Slot,
					"char_id", charID,
					"char_name", charName,
					"job", selected.Job,
					"level", selected.Level,
					"source", parsed.source,
					"selected_by", selectedBy,
					"count", len(characters))
				return charID, charName, selected, true, selected.Slot
			}
			s.logGameEvent(session, "game-upper-select-character-unresolved",
				"request_slot", slot,
				"source", parsed.source,
				"count", len(characters),
				"reason", "no_verified_slot_or_char_id")
		}
	}

	charID := parsed.charID
	return charID, csharpSelectCharacterName(charID), dnfrepo.CharacterRecord{}, false, slot
}

func selectCharacterRecord(
	characters []dnfrepo.CharacterRecord,
	parsed selectCharacterRequest,
) (dnfrepo.CharacterRecord, string, bool) {
	if parsed.slot >= 0 && parsed.slot < defaultCharacterSlots {
		for _, character := range characters {
			if character.Slot == parsed.slot {
				return character, "slot", true
			}
		}
	}
	if parsed.charID != 0 && selectRequestHasAuthoritativeCharID(parsed) {
		for _, character := range characters {
			if numericCharacterID(character) == int(parsed.charID) {
				return character, "char_id", true
			}
		}
	}
	return dnfrepo.CharacterRecord{}, "", false
}

func selectRequestHasAuthoritativeCharID(parsed selectCharacterRequest) bool {
	switch parsed.source {
	case "unknown", "opaque", "fallback", "nopack_key4_slot", "nopack_plain_slot":
		return false
	default:
		return true
	}
}

func csharpSelectCharacterName(charID uint16) string {
	if charID == 0 {
		return ""
	}
	return "dnf_" + strconv.Itoa(int(charID))
}

func (s *Service) selectedCharacterForEnter(
	ctx context.Context,
	session *gameSession,
) (uint16, string, dnfrepo.CharacterRecord, bool) {
	if session == nil {
		return 0, "", dnfrepo.CharacterRecord{}, false
	}
	return s.characterForEnter(ctx, session, session.selectedCharacterID)
}

func (s *Service) characterForEnter(
	ctx context.Context,
	session *gameSession,
	characterID uint16,
) (uint16, string, dnfrepo.CharacterRecord, bool) {
	if repos, ok := s.repositoryGroup(); ok && repos.Character != nil {
		if characterID != 0 {
			characterKey := strconv.Itoa(int(characterID))
			aggregate, found, err := dnfcharacterdata.NewLoader(repos).Load(ctx, characterKey, dnfcharacterdata.PartEquipment)
			if err != nil {
				s.logGameEvent(session, "game-upper-enter-select-dungeon-aggregate-load-failed", "character_id", characterID, "error", err)
			} else if found {
				loaded := aggregate.Character
				if aggregate.Presence.Equipment {
					if summary := equipmentRosterSummary(aggregate.Equipment); len(summary) > 0 {
						loaded.Roster.Entry.EquipSummary = summary
					}
				}
				s.attachEquipmentSummary(ctx, repos, &loaded)
				charName := loaded.Name
				if charName == "" {
					charName = csharpSelectCharacterName(characterID)
				}
				return characterID, charName, loaded, true
			}
			loaded, found, err := repos.Character.Load(ctx, characterKey)
			if err != nil {
				s.logGameEvent(session, "game-upper-enter-select-dungeon-load-failed", "character_id", characterID, "error", err)
			} else if found {
				s.attachEquipmentSummary(ctx, repos, &loaded)
				charName := loaded.Name
				if charName == "" {
					charName = csharpSelectCharacterName(characterID)
				}
				return characterID, charName, loaded, true
			}
		}
	}
	return characterID, csharpSelectCharacterName(characterID), dnfrepo.CharacterRecord{}, false
}
