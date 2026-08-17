// equipment_placement.go owns the bridge-side PVF and selected-character
// checks required before the equipment domain may persist a worn item.
package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfequip "longheng.io/server/internal/modules/dnf/equip"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	errEquipmentPlacementServiceRequired   = errors.New("equipment placement service is required")
	errEquipmentPlacementAccountRequired   = errors.New("equipment placement account is required")
	errEquipmentPlacementCharacterRequired = errors.New("equipment placement selected character is required")
	errEquipmentPlacementRepositoryMissing = errors.New("equipment placement character repository is missing")
	errEquipmentPlacementCharacterMissing  = errors.New("equipment placement selected character is missing")
	errEquipmentPlacementOwnerMismatch     = errors.New("equipment placement selected character owner mismatch")
	errEquipmentPlacementItemUnknown       = errors.New("equipment placement item is not in equipment/equipment.lst")
	errEquipmentPlacementPVFInvalid        = errors.New("equipment placement item PVF is unavailable")
	errEquipmentPlacementPVFTypeUnknown    = errors.New("equipment placement item PVF equipment type is unsupported")
	errEquipmentPlacementSourceUnknown     = errors.New("equipment placement source list is unsupported")
	errEquipmentPlacementSlotClassMismatch = errors.New("equipment placement target slot class mismatch")
	errEquipmentPlacementSlotTypeMismatch  = errors.New("equipment placement target slot does not match PVF equipment type")
	errEquipmentPlacementAuraSkinLocked    = errors.New("equipment placement aura skin slot is locked")
	errEquipmentPlacementLevelInsufficient = errors.New("equipment placement character level below PVF minimum level")
)

const (
	currentEquipmentPlacementListMain          byte = 0
	currentEquipmentPlacementListAvatar        byte = 1
	currentEquipmentPlacementListPersonalCargo byte = 2
	currentEquipmentPlacementListEquipment     byte = 3
	currentEquipmentPlacementListPet           byte = 7
	currentEquipmentPlacementListGuildMedal    byte = 38

	currentEquipmentPlacementAvatarSlotMin int16 = 0
	currentEquipmentPlacementAvatarSlotMax int16 = 11
	currentEquipmentPlacementNormalSlotMin int16 = 12
	currentEquipmentPlacementNormalSlotMax int16 = 32
)

type currentEquipmentPlacementClass byte

const (
	currentEquipmentPlacementClassUnknown currentEquipmentPlacementClass = iota
	currentEquipmentPlacementClassAvatar
	currentEquipmentPlacementClassNormal
	currentEquipmentPlacementClassCreature
)

type currentEquipmentPlacementRule struct {
	pvfType    string
	targetSlot int16
	class      currentEquipmentPlacementClass
	minLevel   int64
}

// currentEquipmentPlacementValidator is deliberately request-scoped. The
// selected account and character are frozen from the same game request that
// creates the equipment owner, so another character cannot be substituted
// between aligned-command routing and persistence.
type currentEquipmentPlacementValidator struct {
	selectedAccountID   string
	selectedCharacterID uint16
	characters          dnfrepo.CharacterRepository
	accounts            dnfrepo.AccountRepository
	pvfSource           initialEquipmentTextSource
	equipmentPaths      map[int64]string
}

// newCurrentEquipmentPlacementValidator returns the callback consumed by
// equip.NewOwnerWithPlacementValidator. Keeping this factory in dnfbridge lets
// the inventory handler remain independent of PVF archive implementation.
func (s *Service) newCurrentEquipmentPlacementValidator(
	ctx context.Context,
	accountID string,
	selectedCharacterID uint16,
	repositories dnfrepo.Group,
) (dnfequip.PlacementValidator, error) {
	if s == nil {
		return nil, errEquipmentPlacementServiceRequired
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, errEquipmentPlacementAccountRequired
	}
	if selectedCharacterID == 0 {
		return nil, errEquipmentPlacementCharacterRequired
	}
	if repositories.Character == nil {
		return nil, errEquipmentPlacementRepositoryMissing
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.preloadEquipmentStatIndex(ctx); err != nil {
		return nil, fmt.Errorf("prepare equipment placement PVF index: %w", err)
	}

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("prepare equipment placement PVF archive: %w", err)
	}

	s.equipmentStatsMu.Lock()
	paths := s.equipmentStatPaths
	s.equipmentStatsMu.Unlock()
	if len(paths) == 0 {
		return nil, errEquipmentPlacementItemUnknown
	}

	return &currentEquipmentPlacementValidator{
		selectedAccountID:   accountID,
		selectedCharacterID: selectedCharacterID,
		characters:          repositories.Character,
		accounts:            repositories.Account,
		pvfSource:           archive,
		equipmentPaths:      paths,
	}, nil
}

func (v *currentEquipmentPlacementValidator) ValidateEquipmentPlacement(ctx context.Context, placement dnfequip.Placement) error {
	if v == nil || v.characters == nil {
		return errEquipmentPlacementRepositoryMissing
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	characterID := strconv.FormatUint(uint64(v.selectedCharacterID), 10)
	if strings.TrimSpace(placement.CharacterID) != characterID {
		return fmt.Errorf("%w: selected=%s placement=%q", errEquipmentPlacementOwnerMismatch, characterID, placement.CharacterID)
	}
	character, found, err := v.characters.Load(ctx, characterID)
	if err != nil {
		return fmt.Errorf("load equipment placement character %s: %w", characterID, err)
	}
	if !found {
		return fmt.Errorf("%w: character=%s", errEquipmentPlacementCharacterMissing, characterID)
	}
	if strings.TrimSpace(character.CharacterID) != characterID ||
		strings.TrimSpace(character.AccountID) != v.selectedAccountID {
		return fmt.Errorf(
			"%w: selected_account=%q record_account=%q selected_character=%s record_character=%q",
			errEquipmentPlacementOwnerMismatch,
			v.selectedAccountID,
			character.AccountID,
			characterID,
			character.CharacterID,
		)
	}

	rule, err := v.validatePVFItem(placement.ItemID)
	if err != nil {
		return err
	}
	sourceClass, err := currentEquipmentPlacementClassForSource(placement.SourceListType, placement.SourceSlotIndex)
	if err != nil {
		return err
	}
	if sourceClass != rule.class || !currentEquipmentPlacementTargetMatches(rule.class, placement.TargetSlotIndex) {
		return fmt.Errorf(
			"%w: pvf_type=%q pvf_class=%d source_class=%d source=(%d,%d) target=%d",
			errEquipmentPlacementSlotClassMismatch,
			rule.pvfType,
			rule.class,
			sourceClass,
			placement.SourceListType,
			placement.SourceSlotIndex,
			placement.TargetSlotIndex,
		)
	}
	targetMatchesPVFType := placement.TargetSlotIndex == rule.targetSlot
	if !targetMatchesPVFType {
		// The current client uses worn avatar slot 9 for the aura whose stats
		// remain active and slot 11 for the separately selected aura animation.
		// A live op19 from the unlocked avatar panel proves the skin placement
		// as list 1 -> list 3/slot 11 with the same PVF [aurora avatar] type.
		// Keep the ordinary slot-9 rule, but admit slot 11 only for the durable
		// aura-skin entitlement committed by op863.
		if rule.pvfType == "[aurora avatar]" && placement.TargetSlotIndex == 11 {
			if character.Stats == nil || character.Stats[currentOpenAuraSkinSlotFlagStat] == 0 {
				return fmt.Errorf(
					"%w: pvf_type=%q target=%d character=%s",
					errEquipmentPlacementAuraSkinLocked,
					rule.pvfType,
					placement.TargetSlotIndex,
					characterID,
				)
			}
			targetMatchesPVFType = true
		}
	}
	if !targetMatchesPVFType {
		return fmt.Errorf(
			"%w: pvf_type=%q expected=%d target=%d source=(%d,%d)",
			errEquipmentPlacementSlotTypeMismatch,
			rule.pvfType,
			rule.targetSlot,
			placement.TargetSlotIndex,
			placement.SourceListType,
			placement.SourceSlotIndex,
		)
	}
	// 霸王契约 (premium type 22): normal equipment can be worn ten character
	// levels early. Without the contract the PVF [minimum level] still gates;
	// the client already enforces it locally, so this only rejects traffic the
	// current EXE would never legitimately send.
	if rule.class == currentEquipmentPlacementClassNormal && rule.minLevel > 0 {
		effectiveLevel := int64(character.Level)
		if v.premiumActive(ctx, premium.TypeOverEquip) {
			effectiveLevel += 10
		}
		if effectiveLevel < rule.minLevel {
			return fmt.Errorf(
				"%w: item=%d min_level=%d character_level=%d effective=%d",
				errEquipmentPlacementLevelInsufficient,
				placement.ItemID,
				rule.minLevel,
				character.Level,
				effectiveLevel,
			)
		}
	}
	return nil
}

// premiumActive reports whether the selected account currently holds an
// active premium contract of the given type. A missing account repository or
// record reads as inactive (fail closed for the bonus only, never for the
// base placement rule).
func (v *currentEquipmentPlacementValidator) premiumActive(ctx context.Context, premiumType int64) bool {
	if v == nil || v.accounts == nil {
		return false
	}
	account, found, err := v.accounts.Load(ctx, v.selectedAccountID)
	if err != nil || !found {
		return false
	}
	return premium.Active(account, premiumType, time.Now().UTC())
}

func (v *currentEquipmentPlacementValidator) validatePVFItem(itemID int64) (currentEquipmentPlacementRule, error) {
	if v == nil || itemID <= 0 {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d", errEquipmentPlacementItemUnknown, itemID)
	}
	refPath := strings.TrimSpace(v.equipmentPaths[itemID])
	if refPath == "" {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d", errEquipmentPlacementItemUnknown, itemID)
	}
	if v.pvfSource == nil {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d path=%q", errEquipmentPlacementPVFInvalid, itemID, refPath)
	}
	text, actualPath, err := readInitialPVFText(v.pvfSource, initialPVFPath("equipment", refPath), refPath)
	if err != nil {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d path=%q: %v", errEquipmentPlacementPVFInvalid, itemID, refPath, err)
	}
	doc, err := dnfpvf.Parse(actualPath, text)
	if err != nil {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d path=%q: %v", errEquipmentPlacementPVFInvalid, itemID, actualPath, err)
	}
	pvfType, ok := doc.Text("equipment type")
	if !ok {
		return currentEquipmentPlacementRule{}, fmt.Errorf(
			"%w: item=%d path=%q missing [equipment type] text token",
			errEquipmentPlacementPVFTypeUnknown,
			itemID,
			actualPath,
		)
	}
	rule, ok := currentEquipmentPlacementRuleForPVFType(pvfType)
	if !ok {
		return currentEquipmentPlacementRule{}, fmt.Errorf(
			"%w: item=%d path=%q type=%q",
			errEquipmentPlacementPVFTypeUnknown,
			itemID,
			actualPath,
			strings.TrimSpace(pvfType),
		)
	}
	if minLevel, found := doc.Int("minimum level"); found && minLevel > 0 {
		rule.minLevel = minLevel
	}
	return rule, nil
}

// currentEquipmentPlacementRuleForPVFType maps the real PVF equipment-type
// token to the current EXE's 33-slot actor-equipment table. These are category
// mappings, never item-id exceptions. Current EXE cache evidence closes avatar
// slots 0..8 and normal slots 12,14..25; the same-EXE appearance path closes
// aura/weapon-avatar 9/10 and title 13. Aura animation replacement reuses an
// [aurora avatar] in slot 11, gated by the durable op863 entitlement; it is an
// alternate target rather than a second PVF category. Current Script.pvf documents the
// guild-medal category as [flag], and the current EXE's 33-slot equipment
// table owns its item at slot 32. The current EXE's string-to-target table
// (sub_35FC900) plus the forward/inverse subtype converters (sub_21D9C10,
// sub_21D9DA0) close the creature family: [creature] 26, [artifact red] 27,
// [artifact blue] 28, [artifact green] 29. Slot 11 and slots 30/31 remain
// otherwise unsupported until their current-EXE item-table categories are proved.
func currentEquipmentPlacementRuleForPVFType(raw string) (currentEquipmentPlacementRule, bool) {
	pvfType := normalizeEquipmentPlacementPVFType(raw)
	var target int16
	var class = currentEquipmentPlacementClassNormal
	switch pvfType {
	case "[hat avatar]":
		target = 0
	case "[hair avatar]":
		target = 1
	case "[face avatar]":
		target = 2
	case "[coat avatar]":
		target = 3
	case "[pants avatar]":
		target = 4
	case "[shoes avatar]":
		target = 5
	case "[breast avatar]":
		target = 6
	case "[waist avatar]":
		target = 7
	case "[skin avatar]":
		target = 8
	case "[aurora avatar]":
		target = 9
	case "[weapon avatar]":
		target = 10
	case "[weapon]":
		target = 12
	case "[title name]":
		target = 13
	case "[coat]":
		target = 14
	case "[shoulder]":
		target = 15
	case "[pants]":
		target = 16
	case "[shoes]":
		target = 17
	case "[waist]":
		target = 18
	case "[amulet]":
		target = 19
	case "[wrist]":
		target = 20
	case "[ring]":
		target = 21
	case "[support]":
		target = 22
	case "[magic stone]":
		target = 23
	case "[support weapon]":
		target = 24
	case "[earring]":
		target = 25
	case "[artifact red]":
		target = 27
		class = currentEquipmentPlacementClassCreature
	case "[artifact blue]":
		target = 28
		class = currentEquipmentPlacementClassCreature
	case "[artifact green]":
		target = 29
		class = currentEquipmentPlacementClassCreature
	case "[flag]":
		target = 32
	case "[name tag]":
		target = 30
	default:
		return currentEquipmentPlacementRule{}, false
	}
	if target <= currentEquipmentPlacementAvatarSlotMax {
		class = currentEquipmentPlacementClassAvatar
	}
	return currentEquipmentPlacementRule{pvfType: pvfType, targetSlot: target, class: class}, true
}

func normalizeEquipmentPlacementPVFType(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimSpace(strings.Trim(raw, "`"))
	return raw
}

func currentEquipmentPlacementClassForSource(listType byte, slot int16) (currentEquipmentPlacementClass, error) {
	switch listType {
	case currentEquipmentPlacementListAvatar:
		return currentEquipmentPlacementClassAvatar, nil
	case currentEquipmentPlacementListPet:
		return currentEquipmentPlacementClassCreature, nil
	case currentEquipmentPlacementListGuildMedal:
		if !currentGuildMedalPageContains(slot) {
			return currentEquipmentPlacementClassUnknown, fmt.Errorf(
				"%w: guild-medal page slot=%d, want=%d..%d",
				errEquipmentPlacementSourceUnknown,
				slot,
				currentGuildMedalPageSlotStart,
				currentGuildMedalPageSlotEnd,
			)
		}
		return currentEquipmentPlacementClassNormal, nil
	case currentEquipmentPlacementListMain, currentEquipmentPlacementListPersonalCargo:
		return currentEquipmentPlacementClassNormal, nil
	case currentEquipmentPlacementListEquipment:
		switch {
		case slot >= currentEquipmentPlacementAvatarSlotMin && slot <= currentEquipmentPlacementAvatarSlotMax:
			return currentEquipmentPlacementClassAvatar, nil
		case slot >= currentEquipmentPlacementNormalSlotMin && slot <= currentEquipmentPlacementNormalSlotMax:
			return currentEquipmentPlacementClassNormal, nil
		default:
			return currentEquipmentPlacementClassUnknown, fmt.Errorf(
				"%w: equipment source slot=%d",
				errEquipmentPlacementSourceUnknown,
				slot,
			)
		}
	default:
		return currentEquipmentPlacementClassUnknown, fmt.Errorf(
			"%w: list=%d slot=%d",
			errEquipmentPlacementSourceUnknown,
			listType,
			slot,
		)
	}
}

func currentEquipmentPlacementTargetMatches(class currentEquipmentPlacementClass, target int16) bool {
	switch class {
	case currentEquipmentPlacementClassAvatar:
		return target >= currentEquipmentPlacementAvatarSlotMin && target <= currentEquipmentPlacementAvatarSlotMax
	case currentEquipmentPlacementClassNormal:
		return target >= currentEquipmentPlacementNormalSlotMin && target <= currentEquipmentPlacementNormalSlotMax
	case currentEquipmentPlacementClassCreature:
		return target >= 26 && target <= 29
	default:
		return false
	}
}

var _ dnfequip.PlacementValidator = (*currentEquipmentPlacementValidator)(nil)
