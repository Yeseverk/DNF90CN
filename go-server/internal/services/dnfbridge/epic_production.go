package dnfbridge

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentEpicProductionPVFPath            = "etc/epicproduction/epicproduction.etc"
	currentEpicProductionStartRequestSize   = 4
	currentEpicProductionStartIndex         = 0
	currentEpicProductionTargetStat         = "epic_production_target_item_id"
	currentEpicProductionChargeStat         = "epic_production_charge_point"
	currentEpicProductionWeeklyCountStat    = "epic_production_weekly_count"
	currentEpicProductionWeekStartStat      = "epic_production_week_start_unix"
	currentEpicProductionInfoMsgID          = 1355
	currentEpicProductionInfoStateSize      = 28
	currentEpicProductionProcessRowSize     = 8
	currentEpicProductionChangePrefixSize   = 6
	currentEpicProductionCompoundPrefixSize = 6
	currentEpicProductionCompoundRowSize    = 12
	currentEpicProductionGenericError       = 3
	currentEpicProductionAlreadyActive      = 18
	currentEpicProductionWeeklyLimitError   = 19
	currentEpicProductionCarryGroup1Stat    = "epic_production_material_carry_group_1"
	currentEpicProductionCarryGroup2Stat    = "epic_production_material_carry_group_2"
	currentEpicProductionAbilityPVFPath     = "etc/customability/epicproductionability.etc"
	currentEpicProductionAbilityRequestSize = 4
	currentEpicProductionAbilityTailOffset  = 0x72
	currentEpicProductionAbilityTailSize    = 5
	currentEpicProductionMaterialError      = 22
)

var (
	errCurrentEpicProductionInvalid       = errors.New("dnf epic production request is invalid")
	errCurrentEpicProductionAlreadyActive = errors.New("dnf epic production is already active")
	errCurrentEpicProductionMaterial      = errors.New("dnf epic production material is invalid")
	errCurrentEpicProductionWeeklyLimit   = errors.New("dnf epic production weekly limit reached")
)

type currentEpicProductionCatalyst struct {
	ItemID            uint32
	GroupKey          int32
	LevelLimit        uint32
	NeedItemIDs       map[uint32]struct{}
	NeedItemType      string
	NeedItemParts     string
	NeedItemGrade     string
	NeedItemMinLevel  uint32
	NeedItemMaxLevel  uint32
	NeedCount         uint32
	MustNeedItemID    uint32
	MustNeedItemCount uint32
	MinMakeCount      uint32
	MaxMakeCount      uint32
	MaxItemCountRate  uint32
	GetPoint          uint32
	IsMaterialPoint   bool
	IsSeal            bool
	AffectsTryCount   bool
}

type currentEpicProductionCatalog struct {
	targetsByJob              map[string]map[uint32]struct{}
	catalysts                 map[uint32]currentEpicProductionCatalyst
	changeMaterials           map[uint32]uint32
	materialPointsByRarity    map[int64]uint32
	maxItemCountMaxRate       uint32
	levelLimit                uint32
	weeklyLimit               uint32
	maxChargePoint            uint32
	processMaterialNeedCount  uint32
	requiredMaterialItemID    uint32
	requiredMaterialItemCount uint32
	bigChanceRate             uint32
	bigChanceMaxRate          uint32
	bigChanceMultiply         uint32
	minBigChanceLimit         uint32
	maxBigChanceLimit         uint32
}

type currentEpicProductionProcessMaterial struct {
	ItemID uint32
	Slot   int16
}

type currentEpicProductionProcessRequest struct {
	Materials []currentEpicProductionProcessMaterial
}

type currentEpicProductionProcessResult struct {
	ChargePoint uint32
	WeeklyCount uint32
	BigSuccess  bool
	Updates     []currentItemListEntry
}

type currentEpicProductionChangeRequest struct {
	TargetItemID uint32
	Materials    []currentEpicProductionProcessMaterial
}

type currentEpicProductionChangeResult struct {
	TargetItemID uint32
	Updates      []currentItemListEntry
}

type currentEpicProductionCompoundMaterial struct {
	ItemID uint32
	Slot   int16
	Count  uint32
}

type currentEpicProductionCompoundRequest struct {
	CatalystItemID uint32
	Materials      []currentEpicProductionCompoundMaterial
}

type currentEpicProductionCompoundPlan struct {
	Recipe           currentEpicProductionCatalyst
	OutputDefinition dungeonDropItemDefinition
	Contribution     uint64
}

type currentEpicProductionCompoundResult struct {
	CatalystItemID uint32
	OutputCount    uint32
	CarryValue     uint32
	GroupSelector  byte
	Updates        []currentItemListEntry
}

type currentEpicProductionAbilityMaterial struct {
	ItemID uint32
	Count  uint32
}

type currentEpicProductionAbilityRecipe struct {
	Category  byte
	Option    byte
	Materials []currentEpicProductionAbilityMaterial
}

type currentEpicProductionAbilityCatalog struct {
	Recipes map[byte]map[byte]currentEpicProductionAbilityRecipe
}

type currentEpicProductionAbilityRequest struct {
	Slot     int16
	Category byte
	Option   byte
}

type currentEpicProductionAbilityResult struct {
	Applied bool
	Updates []currentItemListEntry
}

func decodeCurrentEpicProductionStartRequest(body []byte) (uint32, error) {
	if len(body) != currentEpicProductionStartRequestSize {
		return 0, fmt.Errorf("%w: body length=%d want=%d", errCurrentEpicProductionInvalid, len(body), currentEpicProductionStartRequestSize)
	}
	targetItemID := binary.LittleEndian.Uint32(body)
	if targetItemID == 0 {
		return 0, fmt.Errorf("%w: target item id is zero", errCurrentEpicProductionInvalid)
	}
	return targetItemID, nil
}

func parseCurrentEpicProductionCatalog(source initialEquipmentTextSource) (*currentEpicProductionCatalog, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: PVF source is nil", errCurrentEpicProductionInvalid)
	}
	text, err := source.ReadText(currentEpicProductionPVFPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", currentEpicProductionPVFPath, err)
	}
	document, err := dnfpvf.Parse(currentEpicProductionPVFPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", currentEpicProductionPVFPath, err)
	}

	targetsByJob := make(map[string]map[uint32]struct{})
	catalysts := make(map[uint32]currentEpicProductionCatalyst)
	inProductionItems := false
	inLiquidList := false
	currentJob := ""
	inCatalyst := false
	currentCatalyst := currentEpicProductionCatalyst{GroupKey: -1}
	commitCatalyst := func() {
		if currentCatalyst.ItemID != 0 && currentCatalyst.GetPoint != 0 && currentCatalyst.NeedCount != 0 &&
			currentCatalyst.MinMakeCount != 0 && currentCatalyst.MaxMakeCount >= currentCatalyst.MinMakeCount {
			catalysts[currentCatalyst.ItemID] = currentCatalyst
		}
		currentCatalyst = currentEpicProductionCatalyst{GroupKey: -1}
		inCatalyst = false
	}
	catalog := &currentEpicProductionCatalog{
		targetsByJob:           targetsByJob,
		catalysts:              catalysts,
		changeMaterials:        make(map[uint32]uint32),
		materialPointsByRarity: make(map[int64]uint32),
	}
	for _, section := range document.Sections {
		name := currentRentalSectionName(section.Name)
		tokens, ok := currentRentalSectionTokens(document, section)
		if !ok {
			return nil, fmt.Errorf("%w: invalid [%s] token range", errCurrentEpicProductionInvalid, section.Name)
		}
		uints := currentEpicProductionUintTokens(tokens)
		switch name {
		case "production item":
			inProductionItems = true
			currentJob = ""
			continue
		case "/production item":
			inProductionItems = false
			currentJob = ""
			continue
		case "liquid list":
			inLiquidList = true
			continue
		case "/liquid list":
			commitCatalyst()
			inLiquidList = false
			continue
		}
		if !inProductionItems && !inLiquidList {
			switch name {
			case "level limit":
				catalog.levelLimit = currentEpicProductionFirstUint(uints)
			case "weekly limit":
				catalog.weeklyLimit = currentEpicProductionFirstUint(uints)
			case "max charge point":
				catalog.maxChargePoint = currentEpicProductionFirstUint(uints)
			case "process material need count":
				catalog.processMaterialNeedCount = currentEpicProductionFirstUint(uints)
			case "must need item":
				if len(uints) >= 2 {
					catalog.requiredMaterialItemID = uints[0]
					catalog.requiredMaterialItemCount = uints[1]
				}
			case "need item type change":
				for index := 0; index+1 < len(uints); index += 2 {
					catalog.changeMaterials[uints[index]] = uints[index+1]
				}
			case "big chance rate":
				catalog.bigChanceRate = currentEpicProductionFirstUint(uints)
			case "big chance max rate":
				catalog.bigChanceMaxRate = currentEpicProductionFirstUint(uints)
			case "big chance multiply":
				catalog.bigChanceMultiply = currentEpicProductionFirstUint(uints)
			case "min big chance limit":
				catalog.minBigChanceLimit = currentEpicProductionFirstUint(uints)
			case "max big chance limit":
				catalog.maxBigChanceLimit = currentEpicProductionFirstUint(uints)
			case "max item count max rate":
				catalog.maxItemCountMaxRate = currentEpicProductionFirstUint(uints)
			}
		}
		if inLiquidList {
			switch name {
			case "item index":
				commitCatalyst()
				inCatalyst = true
				currentCatalyst.ItemID = currentEpicProductionFirstUint(uints)
				currentCatalyst.NeedItemIDs = make(map[uint32]struct{})
			case "/item index":
				commitCatalyst()
			case "material point list":
				for index := 0; index+1 < len(tokens); index += 2 {
					if tokens[index].Kind != dnfpvf.TokenInt || tokens[index+1].Kind != dnfpvf.TokenInt ||
						tokens[index].Int < 0 || tokens[index+1].Int <= 0 || tokens[index+1].Int > math.MaxUint32 {
						continue
					}
					catalog.materialPointsByRarity[tokens[index].Int] = uint32(tokens[index+1].Int)
				}
			case "max item count max rate":
				if !inCatalyst {
					catalog.maxItemCountMaxRate = currentEpicProductionFirstUint(uints)
				}
			case "group key":
				if !inCatalyst {
					break
				}
				for _, token := range tokens {
					if token.Kind == dnfpvf.TokenInt && token.Int >= 0 && token.Int <= math.MaxInt32 {
						currentCatalyst.GroupKey = int32(token.Int)
						break
					}
				}
			case "level limit":
				if inCatalyst {
					currentCatalyst.LevelLimit = currentEpicProductionFirstUint(uints)
				}
			case "need item list":
				if inCatalyst {
					for _, itemID := range uints {
						currentCatalyst.NeedItemIDs[itemID] = struct{}{}
					}
				}
			case "need item type":
				if inCatalyst {
					currentCatalyst.NeedItemType = currentEpicProductionFirstText(tokens)
				}
			case "need item parts":
				if inCatalyst {
					currentCatalyst.NeedItemParts = currentEpicProductionFirstText(tokens)
				}
			case "need item grade":
				if inCatalyst {
					currentCatalyst.NeedItemGrade = currentEpicProductionFirstText(tokens)
				}
			case "need item min level limit":
				if inCatalyst {
					currentCatalyst.NeedItemMinLevel = currentEpicProductionFirstUint(uints)
				}
			case "need item max level limit":
				if inCatalyst {
					currentCatalyst.NeedItemMaxLevel = currentEpicProductionFirstUint(uints)
				}
			case "need item count":
				if inCatalyst {
					currentCatalyst.NeedCount = currentEpicProductionFirstUint(uints)
				}
			case "must need item":
				if inCatalyst && len(uints) >= 2 {
					currentCatalyst.MustNeedItemID = uints[0]
					currentCatalyst.MustNeedItemCount = uints[1]
				}
			case "min make item count":
				if inCatalyst {
					currentCatalyst.MinMakeCount = currentEpicProductionFirstUint(uints)
				}
			case "max make item count":
				if inCatalyst {
					currentCatalyst.MaxMakeCount = currentEpicProductionFirstUint(uints)
				}
			case "max item count rate":
				if inCatalyst {
					currentCatalyst.MaxItemCountRate = currentEpicProductionFirstUint(uints)
				}
			case "get point":
				if inCatalyst {
					currentCatalyst.GetPoint = currentEpicProductionFirstUint(uints)
				}
			case "is material point":
				if inCatalyst {
					currentCatalyst.IsMaterialPoint = currentEpicProductionFirstUint(uints) != 0
				}
			case "is seal":
				if inCatalyst {
					currentCatalyst.IsSeal = currentEpicProductionFirstUint(uints) != 0
				}
			case "is affect try count":
				if inCatalyst {
					currentCatalyst.AffectsTryCount = currentEpicProductionFirstUint(uints) != 0
				}
			}
		}
		if !inProductionItems {
			continue
		}
		switch name {
		case "job":
			currentJob = ""
			for _, token := range tokens {
				if token.Kind == dnfpvf.TokenString || token.Kind == dnfpvf.TokenIdent {
					currentJob = currentRentalJobTag(token.Value)
					break
				}
			}
		case "indexes":
			if currentJob == "" {
				continue
			}
			if targetsByJob[currentJob] == nil {
				targetsByJob[currentJob] = make(map[uint32]struct{})
			}
			for _, itemID := range uints {
				targetsByJob[currentJob][itemID] = struct{}{}
			}
		}
	}
	if len(targetsByJob) == 0 {
		return nil, fmt.Errorf("%w: %s has no production targets", errCurrentEpicProductionInvalid, currentEpicProductionPVFPath)
	}
	if len(catalysts) == 0 || catalog.maxChargePoint == 0 || catalog.weeklyLimit == 0 ||
		catalog.processMaterialNeedCount == 0 || catalog.requiredMaterialItemID == 0 || catalog.requiredMaterialItemCount == 0 {
		return nil, fmt.Errorf("%w: %s has incomplete process configuration", errCurrentEpicProductionInvalid, currentEpicProductionPVFPath)
	}
	if catalog.bigChanceRate > catalog.bigChanceMaxRate || catalog.bigChanceMultiply == 0 || catalog.bigChanceMaxRate == 0 {
		return nil, fmt.Errorf("%w: %s has invalid big-success configuration", errCurrentEpicProductionInvalid, currentEpicProductionPVFPath)
	}
	return catalog, nil
}

func currentEpicProductionUintTokens(tokens []dnfpvf.Token) []uint32 {
	values := make([]uint32, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenInt && token.Int > 0 && token.Int <= math.MaxUint32 {
			values = append(values, uint32(token.Int))
		}
	}
	return values
}

func currentEpicProductionFirstUint(values []uint32) uint32 {
	if len(values) == 0 {
		return 0
	}
	return values[0]
}

func currentEpicProductionFirstText(tokens []dnfpvf.Token) string {
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenString || token.Kind == dnfpvf.TokenIdent {
			return strings.ToLower(strings.TrimSpace(token.Value))
		}
	}
	return ""
}

func (c *currentEpicProductionCatalog) allows(jobTag string, targetItemID uint32) bool {
	if c == nil || targetItemID == 0 {
		return false
	}
	_, ok := c.targetsByJob[currentRentalJobTag(jobTag)][targetItemID]
	return ok
}

func validateCurrentEpicProductionTarget(source initialEquipmentTextSource, items *pvfDungeonDropCatalog, catalog *currentEpicProductionCatalog, jobTag string, targetItemID uint32) (dungeonDropItemDefinition, error) {
	if source == nil || items == nil || catalog == nil || !catalog.allows(jobTag, targetItemID) {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: target item=%d is not configured for job=%s", errCurrentEpicProductionInvalid, targetItemID, jobTag)
	}
	definition, err := items.ResolveItem(targetItemID)
	if err != nil {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: resolve target item=%d: %v", errCurrentEpicProductionInvalid, targetItemID, err)
	}
	if definition.Kind != dungeonDropItemEquipment || currentRentalJobTag(definition.EquipmentType) != "[weapon]" {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: target item=%d is not a weapon", errCurrentEpicProductionInvalid, targetItemID)
	}
	text, err := source.ReadText(definition.PVFPath)
	if err != nil {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: read target item=%d path=%s: %v", errCurrentEpicProductionInvalid, targetItemID, definition.PVFPath, err)
	}
	document, err := dnfpvf.Parse(definition.PVFPath, text)
	if err != nil {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: parse target item=%d path=%s: %v", errCurrentEpicProductionInvalid, targetItemID, definition.PVFPath, err)
	}
	ability, found := document.Text("custom ability type")
	if !found || currentRentalJobTag(ability) != "epic production" {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: target item=%d lacks [custom ability type] epic production", errCurrentEpicProductionInvalid, targetItemID)
	}
	return definition, nil
}

func startCurrentEpicProduction(ctx context.Context, characters dnfrepo.CharacterRepository, accountID, characterID string, targetItemID uint32) (bool, error) {
	if characters == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(characterID) == "" || targetItemID == 0 {
		return false, errCurrentEpicProductionInvalid
	}
	character, found, err := characters.Load(ctx, characterID)
	if err != nil {
		return false, err
	}
	if !found || strings.TrimSpace(character.CharacterID) != strings.TrimSpace(characterID) || strings.TrimSpace(character.AccountID) != strings.TrimSpace(accountID) {
		return false, errCurrentEpicProductionInvalid
	}
	if character.Stats == nil {
		character.Stats = make(map[string]int64)
	}
	current := character.Stats[currentEpicProductionTargetStat]
	if current > 0 {
		if current == int64(targetItemID) {
			return false, nil
		}
		return false, fmt.Errorf("%w: current=%d requested=%d", errCurrentEpicProductionAlreadyActive, current, targetItemID)
	}
	character.Stats[currentEpicProductionTargetStat] = int64(targetItemID)
	if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
		return false, err
	}
	return true, nil
}

func buildCurrentEpicProductionStartSuccessBody(targetItemID uint32) []byte {
	var body packetWriter
	body.writeUint32(targetItemID)
	body.writeUint32(currentEpicProductionStartIndex)
	return body.bytes()
}

func (s *Service) handleCurrentEpicProductionStart(session *gameSession, body []byte) error {
	if session == nil {
		return errCurrentEpicProductionInvalid
	}
	targetItemID, err := decodeCurrentEpicProductionStartRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}
	if session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "reason", "selected_character_missing")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "reason", "character_repository_missing")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil || !found || strings.TrimSpace(character.AccountID) != strings.TrimSpace(s.accountID()) {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "reason", errCurrentEpicProductionInvalid)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}
	jobValue := numericCharacterStat(character.Job)
	if jobValue < 0 || jobValue > math.MaxUint8 {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "job", character.Job, "reason", "job_out_of_u8_range")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}
	productionCatalog, err := parseCurrentEpicProductionCatalog(archive)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}
	items, err := s.currentPVFItemCatalog()
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}
	jobTag, err := currentRentalCharacterJobTag(archive, byte(jobValue))
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "job", jobValue, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}
	definition, err := validateCurrentEpicProductionTarget(archive, items, productionCatalog, jobTag, targetItemID)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "job", jobValue, "job_tag", jobTag, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), currentEpicProductionGenericError)
	}

	changed, err := startCurrentEpicProduction(ctx, repositories.Character, s.accountID(), characterID, targetItemID)
	if err != nil {
		code := byte(currentEpicProductionGenericError)
		if errors.Is(err, errCurrentEpicProductionAlreadyActive) {
			code = currentEpicProductionAlreadyActive
		}
		s.logGameEvent(session, "game-upper-epic-production-start-blocked", "target_item_id", targetItemID, "character_id", characterID, "job", jobValue, "job_tag", jobTag, "result_code", code, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), code)
	}
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketEpicProductionStartFinish), buildCurrentEpicProductionStartSuccessBody(targetItemID)); err != nil {
		return err
	}
	s.logGameEvent(session, "game-upper-epic-production-start-applied",
		"target_item_id", targetItemID,
		"character_id", characterID,
		"job", jobValue,
		"job_tag", jobTag,
		"pvf_path", definition.PVFPath,
		"result_index", currentEpicProductionStartIndex,
		"state_changed", changed,
		"body_source", "current_exe_op1417_success_u8_true_u32_target_u32_valid_index")
	return nil
}

func parseCurrentEpicProductionAbilityCatalog(source initialEquipmentTextSource) (*currentEpicProductionAbilityCatalog, error) {
	if source == nil {
		return nil, errCurrentEpicProductionInvalid
	}
	text, err := source.ReadText(currentEpicProductionAbilityPVFPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", currentEpicProductionAbilityPVFPath, err)
	}
	document, err := dnfpvf.Parse(currentEpicProductionAbilityPVFPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", currentEpicProductionAbilityPVFPath, err)
	}
	abilityType, found := document.Text("custom ability type")
	if !found || currentRentalJobTag(abilityType) != "epic production" {
		return nil, fmt.Errorf("%w: invalid custom ability type", errCurrentEpicProductionInvalid)
	}

	indexCategories := make(map[uint32]byte)
	inGroup := false
	groupCategory := byte(math.MaxUint8)
	for _, section := range document.Sections {
		name := currentRentalSectionName(section.Name)
		tokens, ok := currentRentalSectionTokens(document, section)
		if !ok {
			return nil, fmt.Errorf("%w: invalid [%s] token range", errCurrentEpicProductionInvalid, section.Name)
		}
		values := currentEpicProductionUintTokens(tokens)
		switch name {
		case "group":
			inGroup = true
			groupCategory = math.MaxUint8
			continue
		case "/group":
			inGroup = false
			groupCategory = math.MaxUint8
			continue
		}
		if !inGroup {
			continue
		}
		switch name {
		case "key":
			groupKey := uint64(0)
			if len(values) != 0 {
				groupKey = uint64(values[0])
			} else if text := currentEpicProductionFirstText(tokens); text != "" {
				groupKey, _ = strconv.ParseUint(strings.TrimSpace(text), 10, 8)
			}
			if groupKey == 0 || groupKey > 3 {
				return nil, fmt.Errorf("%w: ability group key=%d", errCurrentEpicProductionInvalid, groupKey)
			}
			groupCategory = byte(groupKey - 1)
		case "indexes":
			if len(values) == 0 {
				continue
			}
			if groupCategory == math.MaxUint8 {
				return nil, fmt.Errorf("%w: ability indexes precede group key", errCurrentEpicProductionInvalid)
			}
			for _, value := range values {
				if value == 0 || value > math.MaxUint8 {
					return nil, fmt.Errorf("%w: ability option=%d", errCurrentEpicProductionInvalid, value)
				}
				if _, duplicate := indexCategories[value]; duplicate {
					return nil, fmt.Errorf("%w: duplicate ability option=%d", errCurrentEpicProductionInvalid, value)
				}
				indexCategories[value] = groupCategory
			}
		}
	}

	catalog := &currentEpicProductionAbilityCatalog{Recipes: make(map[byte]map[byte]currentEpicProductionAbilityRecipe)}
	inInfo := false
	option := uint32(0)
	materials := make([]currentEpicProductionAbilityMaterial, 0, 3)
	commit := func() error {
		if option == 0 && len(materials) == 0 {
			return nil
		}
		category, ok := indexCategories[option]
		if !ok || option > math.MaxUint8 || len(materials) == 0 {
			return fmt.Errorf("%w: incomplete ability recipe option=%d", errCurrentEpicProductionInvalid, option)
		}
		if catalog.Recipes[category] == nil {
			catalog.Recipes[category] = make(map[byte]currentEpicProductionAbilityRecipe)
		}
		optionByte := byte(option)
		if _, duplicate := catalog.Recipes[category][optionByte]; duplicate {
			return fmt.Errorf("%w: duplicate ability recipe option=%d", errCurrentEpicProductionInvalid, option)
		}
		catalog.Recipes[category][optionByte] = currentEpicProductionAbilityRecipe{
			Category:  category,
			Option:    optionByte,
			Materials: append([]currentEpicProductionAbilityMaterial(nil), materials...),
		}
		return nil
	}
	for _, section := range document.Sections {
		name := currentRentalSectionName(section.Name)
		tokens, ok := currentRentalSectionTokens(document, section)
		if !ok {
			return nil, fmt.Errorf("%w: invalid [%s] token range", errCurrentEpicProductionInvalid, section.Name)
		}
		values := currentEpicProductionUintTokens(tokens)
		switch name {
		case "info":
			inInfo = true
			option = 0
			materials = materials[:0]
			continue
		case "/info":
			if inInfo {
				if err := commit(); err != nil {
					return nil, err
				}
			}
			inInfo = false
			continue
		}
		if !inInfo || len(values) == 0 {
			continue
		}
		switch name {
		case "index":
			option = values[0]
		case "list":
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("%w: ability option=%d material token count=%d", errCurrentEpicProductionInvalid, option, len(values))
			}
			seen := make(map[uint32]struct{}, len(values)/2)
			for index := 0; index < len(values); index += 2 {
				itemID, count := values[index], values[index+1]
				if itemID == 0 || count == 0 {
					return nil, fmt.Errorf("%w: ability option=%d invalid material", errCurrentEpicProductionInvalid, option)
				}
				if _, duplicate := seen[itemID]; duplicate {
					return nil, fmt.Errorf("%w: ability option=%d duplicate material=%d", errCurrentEpicProductionInvalid, option, itemID)
				}
				seen[itemID] = struct{}{}
				materials = append(materials, currentEpicProductionAbilityMaterial{ItemID: itemID, Count: count})
			}
		}
	}
	if len(catalog.Recipes) == 0 {
		return nil, fmt.Errorf("%w: no epic production ability recipes", errCurrentEpicProductionInvalid)
	}
	return catalog, nil
}

func decodeCurrentEpicProductionAbilityRequest(body []byte) (currentEpicProductionAbilityRequest, error) {
	if len(body) != currentEpicProductionAbilityRequestSize {
		return currentEpicProductionAbilityRequest{}, fmt.Errorf("%w: ability body length=%d", errCurrentEpicProductionInvalid, len(body))
	}
	slot := int16(binary.LittleEndian.Uint16(body[:2]))
	if slot < 0 || dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, slot) {
		return currentEpicProductionAbilityRequest{}, fmt.Errorf("%w: ability slot=%d", errCurrentEpicProductionInvalid, slot)
	}
	return currentEpicProductionAbilityRequest{Slot: slot, Category: body[2], Option: body[3]}, nil
}

func currentEpicProductionAbilityRecipeFor(
	catalog *currentEpicProductionAbilityCatalog,
	request currentEpicProductionAbilityRequest,
) (currentEpicProductionAbilityRecipe, error) {
	if catalog == nil || catalog.Recipes == nil {
		return currentEpicProductionAbilityRecipe{}, errCurrentEpicProductionInvalid
	}
	recipe, found := catalog.Recipes[request.Category][request.Option]
	if !found || recipe.Category != request.Category || recipe.Option != request.Option || len(recipe.Materials) == 0 {
		return currentEpicProductionAbilityRecipe{}, fmt.Errorf("%w: ability category=%d option=%d", errCurrentEpicProductionInvalid, request.Category, request.Option)
	}
	return recipe, nil
}

func currentEpicProductionConsumeAbilityMaterial(
	account *dnfrepo.AccountInventoryRecord,
	inventory *dnfrepo.InventoryRecord,
	material currentEpicProductionAbilityMaterial,
) ([]currentItemListEntry, bool, error) {
	if material.ItemID == 0 || material.Count == 0 {
		return nil, false, errCurrentEpicProductionMaterial
	}
	if fixedSlot, shared := currentDisjointWarehouseFixedSlots[material.ItemID]; shared {
		entry, err := currentEpicProductionConsumeAccountStack(account, fixedSlot, material.ItemID, material.Count)
		if err != nil {
			return nil, false, err
		}
		return []currentItemListEntry{entry}, true, nil
	}
	if inventory == nil || inventory.Slots == nil {
		return nil, false, errCurrentEpicProductionMaterial
	}
	slots := make([]int16, 0)
	var available uint64
	for key, stack := range inventory.Slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != dnfrepo.MainInventoryListType || slot < 0 || stack.ItemID != int64(material.ItemID) || stack.Count <= 0 || currentNPCShopItemLocked(stack) {
			continue
		}
		slots = append(slots, slot)
		available += uint64(stack.Count)
	}
	if available < uint64(material.Count) {
		return nil, false, fmt.Errorf("%w: ability material=%d have=%d need=%d", errCurrentEpicProductionMaterial, material.ItemID, available, material.Count)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })
	remaining := material.Count
	updates := make([]currentItemListEntry, 0, len(slots))
	for _, slot := range slots {
		stack := inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)]
		consume := uint32(stack.Count)
		if consume > remaining {
			consume = remaining
		}
		entry, err := currentEpicProductionConsumeStack(inventory, slot, material.ItemID, consume)
		if err != nil {
			return nil, false, err
		}
		updates = append(updates, entry)
		remaining -= consume
		if remaining == 0 {
			break
		}
	}
	return updates, false, nil
}

func (s *Service) commitCurrentEpicProductionAbility(
	ctx context.Context,
	session *gameSession,
	recipe currentEpicProductionAbilityRecipe,
	request currentEpicProductionAbilityRequest,
	now time.Time,
) (currentEpicProductionAbilityResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || recipe.Category != request.Category || recipe.Option != request.Option {
		return currentEpicProductionAbilityResult{}, errCurrentEpicProductionInvalid
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.AccountAssets == nil {
		return currentEpicProductionAbilityResult{}, errCurrentEpicProductionInvalid
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	accountID := strings.TrimSpace(s.accountID())
	if accountID == "" {
		return currentEpicProductionAbilityResult{}, errCurrentEpicProductionInvalid
	}
	var result currentEpicProductionAbilityResult
	err := repositories.AccountAssets.WithinAccountCharacterAssets(
		ctx,
		accountID,
		characterID,
		func(
			accounts dnfrepo.AccountInventoryRepository,
			characters dnfrepo.CharacterRepository,
			inventories dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			character, found, err := characters.Load(ctx, characterID)
			if err != nil {
				return err
			}
			if !found || character.CharacterID != characterID || strings.TrimSpace(character.AccountID) != accountID || character.Stats == nil {
				return errCurrentEpicProductionInvalid
			}
			targetItemID := character.Stats[currentEpicProductionTargetStat]
			if targetItemID <= 0 || targetItemID > math.MaxUint32 {
				return errCurrentEpicProductionInvalid
			}
			inventory, found, err := inventories.Load(ctx, characterID)
			if err != nil {
				return err
			}
			if !found || inventory.CharacterID != characterID || inventory.Slots == nil {
				return errCurrentEpicProductionMaterial
			}
			account, found, err := accounts.Load(ctx, accountID)
			if err != nil {
				return err
			}
			if !found {
				account = dnfrepo.AccountInventoryRecord{AccountID: accountID, Slots: make(map[string]dnfrepo.ItemStack)}
			}
			if account.AccountID != accountID {
				return errCurrentEpicProductionInvalid
			}
			if account.Slots == nil {
				account.Slots = make(map[string]dnfrepo.ItemStack)
			}
			inventory = dnfrepo.CloneInventory(inventory)
			account = dnfrepo.CloneAccountInventory(account)

			targetKey := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, request.Slot)
			target, found := inventory.Slots[targetKey]
			if !found || target.ItemID != targetItemID || target.Count <= 0 || currentNPCShopItemLocked(target) {
				return fmt.Errorf("%w: ability target slot=%d item=%d want=%d", errCurrentEpicProductionInvalid, request.Slot, target.ItemID, targetItemID)
			}
			targetEntry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, request.Slot, target)
			tail := append([]byte(nil), targetEntry.data[currentEpicProductionAbilityTailOffset:currentEpicProductionAbilityTailOffset+currentEpicProductionAbilityTailSize]...)
			if int(request.Category) >= len(tail) {
				return errCurrentEpicProductionInvalid
			}
			if tail[request.Category] == request.Option {
				result = currentEpicProductionAbilityResult{Applied: false}
				return nil
			}

			updates := make([]currentItemListEntry, 0, len(recipe.Materials)+1)
			accountChanged := false
			for _, material := range recipe.Materials {
				rows, shared, err := currentEpicProductionConsumeAbilityMaterial(&account, &inventory, material)
				if err != nil {
					return err
				}
				updates = append(updates, rows...)
				accountChanged = accountChanged || shared
			}

			tail[request.Category] = request.Option
			if target.Extra == nil {
				target.Extra = make(map[string]string)
			}
			target.Extra["tail_data_72"] = hex.EncodeToString(tail)
			target.Extra[fmt.Sprintf("epic_production_ability_%d", request.Category)] = strconv.Itoa(int(request.Option))
			targetEntry = currentItemListEntryFromStack(dnfrepo.MainInventoryListType, request.Slot, target)
			target.RawEntry = append([]byte(nil), targetEntry.data[:]...)
			inventory.Slots[targetKey] = target
			updates = append(updates, targetEntry)

			if accountChanged {
				account.UpdatedAt = now.UTC()
				if err := accounts.Save(ctx, account); err != nil {
					return err
				}
			}
			inventory.UpdatedAt = now.UTC()
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
			sortCurrentItemListEntries(updates)
			result = currentEpicProductionAbilityResult{Applied: true, Updates: updates}
			return nil
		},
	)
	if err != nil {
		return currentEpicProductionAbilityResult{}, err
	}
	return result, nil
}

func (s *Service) handleCurrentEpicProductionAbility(session *gameSession, body []byte) error {
	request, err := decodeCurrentEpicProductionAbilityRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-ability-blocked", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionAbilityOption), currentEpicProductionGenericError)
	}
	s.initialEquipmentMu.Lock()
	archive, archiveErr := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if archiveErr != nil {
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionAbilityOption), currentEpicProductionGenericError)
	}
	catalog, err := parseCurrentEpicProductionAbilityCatalog(archive)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-ability-blocked", "slot", request.Slot, "category", request.Category, "option", request.Option, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionAbilityOption), currentEpicProductionGenericError)
	}
	recipe, err := currentEpicProductionAbilityRecipeFor(catalog, request)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-ability-blocked", "slot", request.Slot, "category", request.Category, "option", request.Option, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionAbilityOption), currentEpicProductionGenericError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentEpicProductionAbility(ctx, session, recipe, request, time.Now().UTC())
	if err != nil {
		code := byte(currentEpicProductionGenericError)
		if errors.Is(err, errCurrentEpicProductionMaterial) {
			code = currentEpicProductionMaterialError
		}
		s.logGameEvent(session, "game-upper-epic-production-ability-blocked", "slot", request.Slot, "category", request.Category, "option", request.Option, "result_code", code, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionAbilityOption), code)
	}
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketEpicProductionAbilityOption), nil); err != nil {
		return err
	}
	if len(result.Updates) != 0 {
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, result.Updates), 0); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-upper-epic-production-ability-applied",
		"character_id", session.selectedCharacterID,
		"slot", request.Slot,
		"category", request.Category,
		"option", request.Option,
		"material_types", len(recipe.Materials),
		"state_changed", result.Applied,
		"body_source", "current_exe_op1421_success_u8_true_no_additional_body_raw72_ability_state")
	return nil
}

func decodeCurrentEpicProductionProcessRequest(body []byte) (currentEpicProductionProcessRequest, error) {
	if len(body) < 2 {
		return currentEpicProductionProcessRequest{}, fmt.Errorf("%w: process body length=%d", errCurrentEpicProductionInvalid, len(body))
	}
	count := int(binary.LittleEndian.Uint16(body[:2]))
	if count <= 0 || count > 16 || len(body) != 2+count*currentEpicProductionProcessRowSize {
		return currentEpicProductionProcessRequest{}, fmt.Errorf("%w: process count=%d body length=%d", errCurrentEpicProductionInvalid, count, len(body))
	}
	request := currentEpicProductionProcessRequest{Materials: make([]currentEpicProductionProcessMaterial, 0, count)}
	seenSlots := make(map[int16]struct{}, count)
	for index, offset := 0, 2; index < count; index, offset = index+1, offset+currentEpicProductionProcessRowSize {
		rawSlot := binary.LittleEndian.Uint32(body[offset+4 : offset+8])
		material := currentEpicProductionProcessMaterial{
			ItemID: binary.LittleEndian.Uint32(body[offset : offset+4]),
			Slot:   int16(rawSlot),
		}
		if material.ItemID == 0 || rawSlot > math.MaxInt16 || material.Slot < 0 || dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, material.Slot) {
			return currentEpicProductionProcessRequest{}, fmt.Errorf("%w: material index=%d item=%d slot=%d", errCurrentEpicProductionInvalid, index, material.ItemID, material.Slot)
		}
		if _, duplicate := seenSlots[material.Slot]; duplicate {
			return currentEpicProductionProcessRequest{}, fmt.Errorf("%w: duplicate material slot=%d", errCurrentEpicProductionInvalid, material.Slot)
		}
		seenSlots[material.Slot] = struct{}{}
		request.Materials = append(request.Materials, material)
	}
	return request, nil
}

func decodeCurrentEpicProductionChangeRequest(body []byte) (currentEpicProductionChangeRequest, error) {
	if len(body) < currentEpicProductionChangePrefixSize {
		return currentEpicProductionChangeRequest{}, fmt.Errorf("%w: change body length=%d", errCurrentEpicProductionInvalid, len(body))
	}
	targetItemID := binary.LittleEndian.Uint32(body[:4])
	count := int(binary.LittleEndian.Uint16(body[4:6]))
	if targetItemID == 0 || count <= 0 || count > 16 || len(body) != currentEpicProductionChangePrefixSize+count*currentEpicProductionProcessRowSize {
		return currentEpicProductionChangeRequest{}, fmt.Errorf("%w: change target=%d count=%d body length=%d", errCurrentEpicProductionInvalid, targetItemID, count, len(body))
	}
	request := currentEpicProductionChangeRequest{
		TargetItemID: targetItemID,
		Materials:    make([]currentEpicProductionProcessMaterial, 0, count),
	}
	seenSlots := make(map[int16]struct{}, count)
	for index, offset := 0, currentEpicProductionChangePrefixSize; index < count; index, offset = index+1, offset+currentEpicProductionProcessRowSize {
		rawSlot := binary.LittleEndian.Uint32(body[offset+4 : offset+8])
		material := currentEpicProductionProcessMaterial{
			ItemID: binary.LittleEndian.Uint32(body[offset : offset+4]),
			Slot:   int16(rawSlot),
		}
		if material.ItemID == 0 || rawSlot > math.MaxInt16 || material.Slot < 0 {
			return currentEpicProductionChangeRequest{}, fmt.Errorf("%w: change material index=%d item=%d slot=%d", errCurrentEpicProductionInvalid, index, material.ItemID, material.Slot)
		}
		if _, duplicate := seenSlots[material.Slot]; duplicate {
			return currentEpicProductionChangeRequest{}, fmt.Errorf("%w: duplicate change material slot=%d", errCurrentEpicProductionInvalid, material.Slot)
		}
		seenSlots[material.Slot] = struct{}{}
		request.Materials = append(request.Materials, material)
	}
	return request, nil
}

func decodeCurrentEpicProductionCompoundRequest(body []byte) (currentEpicProductionCompoundRequest, error) {
	if len(body) < currentEpicProductionCompoundPrefixSize {
		return currentEpicProductionCompoundRequest{}, fmt.Errorf("%w: compound body length=%d", errCurrentEpicProductionInvalid, len(body))
	}
	catalystItemID := binary.LittleEndian.Uint32(body[:4])
	count := int(binary.LittleEndian.Uint16(body[4:6]))
	if catalystItemID == 0 || count <= 0 || count > 64 ||
		len(body) != currentEpicProductionCompoundPrefixSize+count*currentEpicProductionCompoundRowSize {
		return currentEpicProductionCompoundRequest{}, fmt.Errorf("%w: compound catalyst=%d count=%d body length=%d", errCurrentEpicProductionInvalid, catalystItemID, count, len(body))
	}
	request := currentEpicProductionCompoundRequest{
		CatalystItemID: catalystItemID,
		Materials:      make([]currentEpicProductionCompoundMaterial, 0, count),
	}
	seenSlots := make(map[int16]struct{}, count)
	for index, offset := 0, currentEpicProductionCompoundPrefixSize; index < count; index, offset = index+1, offset+currentEpicProductionCompoundRowSize {
		rawSlot := binary.LittleEndian.Uint32(body[offset+4 : offset+8])
		material := currentEpicProductionCompoundMaterial{
			ItemID: binary.LittleEndian.Uint32(body[offset : offset+4]),
			Slot:   int16(rawSlot),
			Count:  binary.LittleEndian.Uint32(body[offset+8 : offset+12]),
		}
		if material.ItemID == 0 || material.Count == 0 || material.Count > math.MaxInt32 ||
			rawSlot > math.MaxInt16 || material.Slot < 0 {
			return currentEpicProductionCompoundRequest{}, fmt.Errorf("%w: compound material index=%d item=%d slot=%d count=%d", errCurrentEpicProductionInvalid, index, material.ItemID, material.Slot, material.Count)
		}
		if _, duplicate := seenSlots[material.Slot]; duplicate {
			return currentEpicProductionCompoundRequest{}, fmt.Errorf("%w: duplicate compound slot=%d", errCurrentEpicProductionInvalid, material.Slot)
		}
		seenSlots[material.Slot] = struct{}{}
		request.Materials = append(request.Materials, material)
	}
	return request, nil
}

func currentEpicProductionItemRarity(source initialEquipmentTextSource, definition dungeonDropItemDefinition) (int64, error) {
	if source == nil || definition.ItemID == 0 || definition.PVFPath == "" {
		return 0, errCurrentEpicProductionMaterial
	}
	text, err := source.ReadText(definition.PVFPath)
	if err != nil {
		return 0, fmt.Errorf("%w: read item=%d path=%s: %v", errCurrentEpicProductionMaterial, definition.ItemID, definition.PVFPath, err)
	}
	document, err := dnfpvf.Parse(definition.PVFPath, text)
	if err != nil {
		return 0, fmt.Errorf("%w: parse item=%d path=%s: %v", errCurrentEpicProductionMaterial, definition.ItemID, definition.PVFPath, err)
	}
	rarity, found := document.Int("rarity")
	if !found || rarity < 0 {
		return 0, fmt.Errorf("%w: item=%d has no valid rarity", errCurrentEpicProductionMaterial, definition.ItemID)
	}
	return rarity, nil
}

func validateCurrentEpicProductionCompoundPlan(
	source initialEquipmentTextSource,
	items *pvfDungeonDropCatalog,
	catalog *currentEpicProductionCatalog,
	request currentEpicProductionCompoundRequest,
) (currentEpicProductionCompoundPlan, error) {
	if source == nil || items == nil || catalog == nil || len(request.Materials) == 0 {
		return currentEpicProductionCompoundPlan{}, errCurrentEpicProductionMaterial
	}
	recipe, found := catalog.catalysts[request.CatalystItemID]
	if !found || recipe.ItemID == 0 || recipe.GroupKey < 0 || recipe.NeedCount == 0 ||
		recipe.MinMakeCount == 0 || recipe.MaxMakeCount < recipe.MinMakeCount {
		return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: catalyst=%d has no complete PVF recipe", errCurrentEpicProductionMaterial, request.CatalystItemID)
	}
	// The current implementation accepts recipes whose exact input IDs are
	// enumerated by this PVF. Type-only equipment recipes require their seal,
	// level and part predicates to be resolved separately.
	if len(recipe.NeedItemIDs) == 0 {
		return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: catalyst=%d uses a type-only recipe", errCurrentEpicProductionMaterial, request.CatalystItemID)
	}
	outputDefinition, err := items.ResolveItem(request.CatalystItemID)
	if err != nil || outputDefinition.Kind != dungeonDropItemStackable {
		return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: resolve catalyst=%d: %v", errCurrentEpicProductionMaterial, request.CatalystItemID, err)
	}
	var contribution uint64
	for _, material := range request.Materials {
		if _, allowed := recipe.NeedItemIDs[material.ItemID]; !allowed {
			return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: catalyst=%d rejects item=%d", errCurrentEpicProductionMaterial, request.CatalystItemID, material.ItemID)
		}
		definition, resolveErr := items.ResolveItem(material.ItemID)
		if resolveErr != nil {
			return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: resolve material=%d: %v", errCurrentEpicProductionMaterial, material.ItemID, resolveErr)
		}
		if recipe.NeedItemType == "material" &&
			(definition.Kind != dungeonDropItemStackable || normalizeDungeonDropStackableType(definition.StackableType) != "[material]") {
			return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: item=%d is not PVF material", errCurrentEpicProductionMaterial, material.ItemID)
		}
		unitValue := uint64(1)
		if recipe.IsMaterialPoint {
			rarity, rarityErr := currentEpicProductionItemRarity(source, definition)
			if rarityErr != nil {
				return currentEpicProductionCompoundPlan{}, rarityErr
			}
			points := catalog.materialPointsByRarity[rarity]
			if points == 0 {
				return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: item=%d rarity=%d has no material point", errCurrentEpicProductionMaterial, material.ItemID, rarity)
			}
			unitValue = uint64(points)
		}
		if uint64(material.Count) > math.MaxUint64/unitValue || contribution > math.MaxUint64-uint64(material.Count)*unitValue {
			return currentEpicProductionCompoundPlan{}, errCurrentEpicProductionMaterial
		}
		contribution += uint64(material.Count) * unitValue
		if fixedSlot, shared := currentDisjointWarehouseFixedSlots[material.ItemID]; shared {
			if material.Slot != fixedSlot {
				return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: shared material=%d slot=%d want=%d", errCurrentEpicProductionMaterial, material.ItemID, material.Slot, fixedSlot)
			}
		} else if dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, material.Slot) {
			return currentEpicProductionCompoundPlan{}, fmt.Errorf("%w: ordinary material=%d uses shared slot=%d", errCurrentEpicProductionMaterial, material.ItemID, material.Slot)
		}
	}
	if contribution == 0 {
		return currentEpicProductionCompoundPlan{}, errCurrentEpicProductionMaterial
	}
	return currentEpicProductionCompoundPlan{Recipe: recipe, OutputDefinition: outputDefinition, Contribution: contribution}, nil
}

func currentEpicProductionCarryStat(group int32) (string, byte) {
	switch group {
	case 1:
		return currentEpicProductionCarryGroup1Stat, 1
	case 2:
		return currentEpicProductionCarryGroup2Stat, 2
	default:
		return "", 0
	}
}

func validateCurrentEpicProductionChangeMaterials(catalog *currentEpicProductionCatalog, request currentEpicProductionChangeRequest) error {
	if catalog == nil || len(catalog.changeMaterials) == 0 || len(request.Materials) != len(catalog.changeMaterials) {
		return errCurrentEpicProductionMaterial
	}
	seenItems := make(map[uint32]struct{}, len(request.Materials))
	for _, material := range request.Materials {
		if _, duplicate := seenItems[material.ItemID]; duplicate {
			return fmt.Errorf("%w: duplicate change material item=%d", errCurrentEpicProductionMaterial, material.ItemID)
		}
		seenItems[material.ItemID] = struct{}{}
		if catalog.changeMaterials[material.ItemID] == 0 {
			return fmt.Errorf("%w: unexpected change material item=%d", errCurrentEpicProductionMaterial, material.ItemID)
		}
		if sharedSlot, shared := currentDisjointWarehouseFixedSlots[material.ItemID]; shared {
			if material.Slot != sharedSlot {
				return fmt.Errorf("%w: shared change material item=%d slot=%d want=%d", errCurrentEpicProductionMaterial, material.ItemID, material.Slot, sharedSlot)
			}
		} else if dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, material.Slot) {
			return fmt.Errorf("%w: ordinary change material item=%d uses shared slot=%d", errCurrentEpicProductionMaterial, material.ItemID, material.Slot)
		}
	}
	return nil
}

func currentEpicProductionWeekStart(now time.Time) int64 {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	daysFromMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -daysFromMonday).Unix()
}

func validateCurrentEpicProductionMaterials(catalog *currentEpicProductionCatalog, request currentEpicProductionProcessRequest) (uint32, error) {
	if catalog == nil || len(request.Materials) == 0 || catalog.processMaterialNeedCount == 0 {
		return 0, errCurrentEpicProductionMaterial
	}
	groups := make(map[int32]struct{}, len(request.Materials))
	var points uint64
	for _, material := range request.Materials {
		catalyst, ok := catalog.catalysts[material.ItemID]
		if !ok || catalyst.GetPoint == 0 || catalyst.GroupKey < 0 {
			return 0, fmt.Errorf("%w: item=%d", errCurrentEpicProductionMaterial, material.ItemID)
		}
		if _, duplicate := groups[catalyst.GroupKey]; duplicate {
			return 0, fmt.Errorf("%w: duplicate catalyst group=%d", errCurrentEpicProductionMaterial, catalyst.GroupKey)
		}
		groups[catalyst.GroupKey] = struct{}{}
		points += uint64(catalyst.GetPoint)
	}
	if points == 0 || points > math.MaxUint32 {
		return 0, fmt.Errorf("%w: charge points=%d", errCurrentEpicProductionMaterial, points)
	}
	return uint32(points), nil
}

func currentEpicProductionBigSuccess(catalog *currentEpicProductionCatalog) bool {
	if catalog == nil || catalog.bigChanceRate == 0 || catalog.bigChanceMaxRate == 0 {
		return false
	}
	var random [4]byte
	if _, err := cryptorand.Read(random[:]); err != nil {
		return false
	}
	return uint64(binary.LittleEndian.Uint32(random[:]))*uint64(catalog.bigChanceMaxRate) <
		uint64(catalog.bigChanceRate)*(uint64(math.MaxUint32)+1)
}

func currentEpicProductionConsumeStack(inventory *dnfrepo.InventoryRecord, slot int16, itemID uint32, count uint32) (currentItemListEntry, error) {
	if inventory == nil || inventory.Slots == nil || count == 0 {
		return currentItemListEntry{}, errCurrentEpicProductionMaterial
	}
	key := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)
	stack, found := inventory.Slots[key]
	if !found || stack.ItemID != int64(itemID) || stack.Count < int64(count) || currentNPCShopItemLocked(stack) {
		return currentItemListEntry{}, fmt.Errorf("%w: item=%d slot=%d need=%d", errCurrentEpicProductionMaterial, itemID, slot, count)
	}
	stack.Count -= int64(count)
	if stack.Count == 0 {
		delete(inventory.Slots, key)
		var empty currentItemListEntry
		empty.patchCore(slot, 0, 0)
		return empty, nil
	}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	inventory.Slots[key] = stack
	return entry, nil
}

func currentEpicProductionConsumeAccountStack(account *dnfrepo.AccountInventoryRecord, slot int16, itemID uint32, count uint32) (currentItemListEntry, error) {
	if account == nil || account.Slots == nil || count == 0 || !dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, slot) {
		return currentItemListEntry{}, errCurrentEpicProductionMaterial
	}
	key := dnfrepo.AccountSharedInventorySlotKey(slot)
	stack, found := account.Slots[key]
	if !found || stack.ItemID != int64(itemID) || stack.Count < int64(count) || currentNPCShopItemLocked(stack) {
		return currentItemListEntry{}, fmt.Errorf("%w: account item=%d slot=%d need=%d", errCurrentEpicProductionMaterial, itemID, slot, count)
	}
	stack.Count -= int64(count)
	if stack.Count == 0 {
		delete(account.Slots, key)
		var empty currentItemListEntry
		empty.patchCore(slot, 0, 0)
		return empty, nil
	}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	account.Slots[key] = stack
	return entry, nil
}

func currentEpicProductionFindMaterialSlot(inventory dnfrepo.InventoryRecord, itemID uint32, count uint32) (int16, bool) {
	slots := make([]int, 0)
	for key, stack := range inventory.Slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if ok && listType == dnfrepo.MainInventoryListType && slot >= 0 && stack.ItemID == int64(itemID) && stack.Count >= int64(count) && !currentNPCShopItemLocked(stack) {
			slots = append(slots, int(slot))
		}
	}
	if len(slots) == 0 {
		return 0, false
	}
	sort.Ints(slots)
	return int16(slots[0]), true
}

func (s *Service) commitCurrentEpicProductionChange(
	ctx context.Context,
	session *gameSession,
	catalog *currentEpicProductionCatalog,
	jobTag string,
	request currentEpicProductionChangeRequest,
	now time.Time,
) (currentEpicProductionChangeResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || catalog == nil ||
		!catalog.allows(jobTag, request.TargetItemID) {
		return currentEpicProductionChangeResult{}, errCurrentEpicProductionInvalid
	}
	if err := validateCurrentEpicProductionChangeMaterials(catalog, request); err != nil {
		return currentEpicProductionChangeResult{}, err
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.AccountAssets == nil {
		return currentEpicProductionChangeResult{}, errCurrentEpicProductionInvalid
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	accountID := strings.TrimSpace(s.accountID())
	if accountID == "" {
		return currentEpicProductionChangeResult{}, errCurrentEpicProductionInvalid
	}
	var result currentEpicProductionChangeResult
	err := repositories.AccountAssets.WithinAccountCharacterAssets(
		ctx,
		accountID,
		characterID,
		func(
			accounts dnfrepo.AccountInventoryRepository,
			characters dnfrepo.CharacterRepository,
			inventories dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			character, found, loadErr := characters.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !found || character.CharacterID != characterID || strings.TrimSpace(character.AccountID) != accountID ||
				character.Stats == nil || character.Stats[currentEpicProductionTargetStat] <= 0 ||
				character.Stats[currentEpicProductionTargetStat] == int64(request.TargetItemID) {
				return errCurrentEpicProductionInvalid
			}
			account, found, loadErr := accounts.Load(ctx, accountID)
			if loadErr != nil {
				return loadErr
			}
			if !found || account.AccountID != accountID || account.Slots == nil {
				return errCurrentEpicProductionMaterial
			}
			inventory, found, loadErr := inventories.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !found || inventory.CharacterID != characterID || inventory.Slots == nil {
				return fmt.Errorf("%w: change inventory missing character=%s", errCurrentEpicProductionMaterial, characterID)
			}

			character = dnfrepo.CloneCharacter(character)
			account = dnfrepo.CloneAccountInventory(account)
			inventory = dnfrepo.CloneInventory(inventory)
			updates := make([]currentItemListEntry, 0, len(request.Materials))
			accountChanged := false
			inventoryChanged := false
			for _, material := range request.Materials {
				need := catalog.changeMaterials[material.ItemID]
				var entry currentItemListEntry
				var consumeErr error
				if dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, material.Slot) {
					entry, consumeErr = currentEpicProductionConsumeAccountStack(&account, material.Slot, material.ItemID, need)
					accountChanged = consumeErr == nil
				} else {
					entry, consumeErr = currentEpicProductionConsumeStack(&inventory, material.Slot, material.ItemID, need)
					inventoryChanged = consumeErr == nil
				}
				if consumeErr != nil {
					return consumeErr
				}
				updates = append(updates, entry)
			}

			character.Stats[currentEpicProductionTargetStat] = int64(request.TargetItemID)
			character.UpdatedAt = now.UTC()
			if saveErr := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); saveErr != nil {
				return saveErr
			}
			if accountChanged {
				account.UpdatedAt = now.UTC()
				if saveErr := accounts.Save(ctx, account); saveErr != nil {
					return saveErr
				}
			}
			if inventoryChanged {
				inventory.UpdatedAt = now.UTC()
				if saveErr := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); saveErr != nil {
					return saveErr
				}
			}
			sortCurrentItemListEntries(updates)
			result = currentEpicProductionChangeResult{TargetItemID: request.TargetItemID, Updates: updates}
			return nil
		},
	)
	if err != nil {
		return currentEpicProductionChangeResult{}, err
	}
	return result, nil
}

func buildCurrentEpicProductionChangeSuccessBody(targetItemID uint32) []byte {
	var body packetWriter
	body.writeUint32(targetItemID)
	return body.bytes()
}

func (s *Service) handleCurrentEpicProductionChange(session *gameSession, body []byte) error {
	if session == nil {
		return errCurrentEpicProductionInvalid
	}
	request, err := decodeCurrentEpicProductionChangeRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}
	if session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "reason", "selected_character_missing")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.AccountAssets == nil {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "reason", "account_asset_repository_missing")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil || !found || strings.TrimSpace(character.AccountID) != strings.TrimSpace(s.accountID()) {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "reason", errCurrentEpicProductionInvalid)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}
	jobValue := numericCharacterStat(character.Job)
	if jobValue < 0 || jobValue > math.MaxUint8 {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "job", character.Job, "reason", "job_out_of_u8_range")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}

	s.initialEquipmentMu.Lock()
	archive, archiveErr := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if archiveErr != nil {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "reason", archiveErr)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}
	catalog, err := parseCurrentEpicProductionCatalog(archive)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}
	items, err := s.currentPVFItemCatalog()
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}
	jobTag, err := currentRentalCharacterJobTag(archive, byte(jobValue))
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "job", jobValue, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}
	definition, err := validateCurrentEpicProductionTarget(archive, items, catalog, jobTag, request.TargetItemID)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "job", jobValue, "job_tag", jobTag, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), currentEpicProductionGenericError)
	}

	result, err := s.commitCurrentEpicProductionChange(ctx, session, catalog, jobTag, request, time.Now().UTC())
	if err != nil {
		code := byte(currentEpicProductionGenericError)
		if errors.Is(err, errCurrentEpicProductionMaterial) {
			code = 4
		}
		s.logGameEvent(session, "game-upper-epic-production-change-blocked", "target_item_id", request.TargetItemID, "character_id", characterID, "material_count", len(request.Materials), "result_code", code, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), code)
	}
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketEpicProductionChangeItem), buildCurrentEpicProductionChangeSuccessBody(result.TargetItemID)); err != nil {
		return err
	}
	if len(result.Updates) != 0 {
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, result.Updates), 0); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-upper-epic-production-change-applied",
		"target_item_id", result.TargetItemID,
		"character_id", characterID,
		"job", jobValue,
		"job_tag", jobTag,
		"pvf_path", definition.PVFPath,
		"material_count", len(request.Materials),
		"body_source", "current_exe_op1418_success_u8_true_u32_target_item_id")
	return nil
}

func (s *Service) commitCurrentEpicProductionCompound(
	ctx context.Context,
	session *gameSession,
	plan currentEpicProductionCompoundPlan,
	request currentEpicProductionCompoundRequest,
	now time.Time,
) (currentEpicProductionCompoundResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || plan.Recipe.ItemID != request.CatalystItemID ||
		plan.Contribution == 0 || len(request.Materials) == 0 {
		return currentEpicProductionCompoundResult{}, errCurrentEpicProductionInvalid
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.AccountAssets == nil {
		return currentEpicProductionCompoundResult{}, errCurrentEpicProductionInvalid
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	accountID := strings.TrimSpace(s.accountID())
	if accountID == "" {
		return currentEpicProductionCompoundResult{}, errCurrentEpicProductionInvalid
	}
	var result currentEpicProductionCompoundResult
	err := repositories.AccountAssets.WithinAccountCharacterAssets(
		ctx,
		accountID,
		characterID,
		func(
			accounts dnfrepo.AccountInventoryRepository,
			characters dnfrepo.CharacterRepository,
			inventories dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			character, found, loadErr := characters.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !found || character.CharacterID != characterID || strings.TrimSpace(character.AccountID) != accountID ||
				character.Level < int(plan.Recipe.LevelLimit) {
				return errCurrentEpicProductionInvalid
			}
			inventory, found, loadErr := inventories.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !found || inventory.CharacterID != characterID {
				return fmt.Errorf("%w: compound inventory missing character=%s", errCurrentEpicProductionMaterial, characterID)
			}
			if inventory.Slots == nil {
				inventory.Slots = make(map[string]dnfrepo.ItemStack)
			}
			account, accountFound, loadErr := accounts.Load(ctx, accountID)
			if loadErr != nil {
				return loadErr
			}
			if !accountFound {
				account = dnfrepo.AccountInventoryRecord{AccountID: accountID, Slots: make(map[string]dnfrepo.ItemStack)}
			}
			if account.AccountID != accountID {
				return fmt.Errorf("%w: compound account mismatch got=%s want=%s", errCurrentEpicProductionMaterial, account.AccountID, accountID)
			}
			if account.Slots == nil {
				account.Slots = make(map[string]dnfrepo.ItemStack)
			}

			character = dnfrepo.CloneCharacter(character)
			inventory = dnfrepo.CloneInventory(inventory)
			account = dnfrepo.CloneAccountInventory(account)
			if character.Stats == nil {
				character.Stats = make(map[string]int64)
			}
			updates := make(map[int16]currentItemListEntry, len(request.Materials)+2)
			accountChanged := false
			for _, material := range request.Materials {
				var entry currentItemListEntry
				var consumeErr error
				if dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, material.Slot) {
					entry, consumeErr = currentEpicProductionConsumeAccountStack(&account, material.Slot, material.ItemID, material.Count)
					if consumeErr == nil {
						accountChanged = true
					}
				} else {
					entry, consumeErr = currentEpicProductionConsumeStack(&inventory, material.Slot, material.ItemID, material.Count)
				}
				if consumeErr != nil {
					return consumeErr
				}
				updates[material.Slot] = entry
			}

			if plan.Recipe.MustNeedItemID != 0 && plan.Recipe.MustNeedItemCount != 0 {
				if fixedSlot, shared := currentDisjointWarehouseFixedSlots[plan.Recipe.MustNeedItemID]; shared {
					entry, consumeErr := currentEpicProductionConsumeAccountStack(&account, fixedSlot, plan.Recipe.MustNeedItemID, plan.Recipe.MustNeedItemCount)
					if consumeErr != nil {
						return consumeErr
					}
					accountChanged = true
					updates[fixedSlot] = entry
				} else {
					slot, found := currentEpicProductionFindMaterialSlot(inventory, plan.Recipe.MustNeedItemID, plan.Recipe.MustNeedItemCount)
					if !found {
						return fmt.Errorf("%w: compound must item=%d need=%d", errCurrentEpicProductionMaterial, plan.Recipe.MustNeedItemID, plan.Recipe.MustNeedItemCount)
					}
					entry, consumeErr := currentEpicProductionConsumeStack(&inventory, slot, plan.Recipe.MustNeedItemID, plan.Recipe.MustNeedItemCount)
					if consumeErr != nil {
						return consumeErr
					}
					updates[slot] = entry
				}
			}

			carryStat, groupSelector := currentEpicProductionCarryStat(plan.Recipe.GroupKey)
			carry := uint64(0)
			if carryStat != "" {
				stored := character.Stats[carryStat]
				if stored < 0 {
					return errCurrentEpicProductionInvalid
				}
				carry = uint64(stored)
			}
			if carry > math.MaxUint64-plan.Contribution {
				return errCurrentEpicProductionInvalid
			}
			total := carry + plan.Contribution
			units := total / uint64(plan.Recipe.NeedCount)
			if units == 0 || units > math.MaxUint32 {
				return fmt.Errorf("%w: compound points=%d need=%d", errCurrentEpicProductionMaterial, total, plan.Recipe.NeedCount)
			}
			output64 := units * uint64(plan.Recipe.MinMakeCount)
			// The client preview derives the whole batch result from the
			// accumulated points divided by [need item count], then applies the
			// per-unit make multiplier. [max make item count] is not a cap on
			// the whole request.
			if output64 == 0 || output64 > math.MaxUint32 {
				return fmt.Errorf("%w: compound output=%d", errCurrentEpicProductionMaterial, output64)
			}
			carryValue := uint32(0)
			if carryStat != "" {
				carryValue = uint32(total % uint64(plan.Recipe.NeedCount))
				character.Stats[carryStat] = int64(carryValue)
				character.UpdatedAt = now.UTC()
			}

			grantedSlots, grantErr := grantCurrentCeraShopProduct(&inventory, plan.OutputDefinition, uint32(output64))
			if grantErr != nil {
				return grantErr
			}
			for _, rawSlot := range grantedSlots {
				slot := int16(rawSlot)
				stack, exists := inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)]
				if !exists || stack.ItemID != int64(request.CatalystItemID) || stack.Count <= 0 {
					return fmt.Errorf("%w: compound grant slot=%d missing=%t item=%d count=%d", errCurrentEpicProductionMaterial, slot, !exists, stack.ItemID, stack.Count)
				}
				entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
				stack.RawEntry = append([]byte(nil), entry.data[:]...)
				inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)] = stack
				updates[slot] = entry
			}

			if carryStat != "" {
				if saveErr := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); saveErr != nil {
					return saveErr
				}
			}
			if accountChanged {
				account.UpdatedAt = now.UTC()
				if saveErr := accounts.Save(ctx, account); saveErr != nil {
					return saveErr
				}
			}
			inventory.UpdatedAt = now.UTC()
			if saveErr := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); saveErr != nil {
				return saveErr
			}
			rows := make([]currentItemListEntry, 0, len(updates))
			for _, entry := range updates {
				rows = append(rows, entry)
			}
			sortCurrentItemListEntries(rows)
			result = currentEpicProductionCompoundResult{
				CatalystItemID: request.CatalystItemID,
				OutputCount:    uint32(output64),
				CarryValue:     carryValue,
				GroupSelector:  groupSelector,
				Updates:        rows,
			}
			return nil
		},
	)
	if err != nil {
		return currentEpicProductionCompoundResult{}, err
	}
	return result, nil
}

func buildCurrentEpicProductionCompoundSuccessBody(result currentEpicProductionCompoundResult) []byte {
	var body packetWriter
	body.writeUint32(result.OutputCount)
	body.writeByte(result.GroupSelector)
	body.writeUint32(result.CarryValue)
	return body.bytes()
}

func (s *Service) handleCurrentEpicProductionCompound(session *gameSession, body []byte) error {
	request, err := decodeCurrentEpicProductionCompoundRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-compound-blocked", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionMaterialCompound), currentEpicProductionGenericError)
	}
	s.initialEquipmentMu.Lock()
	archive, archiveErr := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if archiveErr != nil {
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionMaterialCompound), currentEpicProductionGenericError)
	}
	catalog, err := parseCurrentEpicProductionCatalog(archive)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-compound-blocked", "catalyst_item_id", request.CatalystItemID, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionMaterialCompound), currentEpicProductionGenericError)
	}
	items, err := s.currentPVFItemCatalog()
	if err != nil {
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionMaterialCompound), currentEpicProductionGenericError)
	}
	plan, err := validateCurrentEpicProductionCompoundPlan(archive, items, catalog, request)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-compound-blocked", "catalyst_item_id", request.CatalystItemID, "material_count", len(request.Materials), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionMaterialCompound), 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentEpicProductionCompound(ctx, session, plan, request, time.Now().UTC())
	if err != nil {
		code := byte(currentEpicProductionGenericError)
		if errors.Is(err, errCurrentEpicProductionMaterial) || errors.Is(err, errCurrentCeraShopProductUnavailable) || errors.Is(err, errDungeonPickupInventoryFull) {
			code = 4
		}
		s.logGameEvent(session, "game-upper-epic-production-compound-blocked", "catalyst_item_id", request.CatalystItemID, "material_count", len(request.Materials), "result_code", code, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionMaterialCompound), code)
	}
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketEpicProductionMaterialCompound), buildCurrentEpicProductionCompoundSuccessBody(result)); err != nil {
		return err
	}
	if len(result.Updates) != 0 {
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, result.Updates), 0); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-upper-epic-production-compound-applied",
		"character_id", session.selectedCharacterID,
		"catalyst_item_id", result.CatalystItemID,
		"material_count", len(request.Materials),
		"output_count", result.OutputCount,
		"group_selector", result.GroupSelector,
		"carry_value", result.CarryValue,
		"body_source", "current_exe_op1420_success_u8_true_u32_output_count_u8_group_u32_carry")
	return nil
}

func (s *Service) commitCurrentEpicProductionProcess(
	ctx context.Context,
	session *gameSession,
	catalog *currentEpicProductionCatalog,
	request currentEpicProductionProcessRequest,
	now time.Time,
) (currentEpicProductionProcessResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || catalog == nil {
		return currentEpicProductionProcessResult{}, errCurrentEpicProductionInvalid
	}
	basePoints, err := validateCurrentEpicProductionMaterials(catalog, request)
	if err != nil {
		return currentEpicProductionProcessResult{}, err
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterAssets == nil {
		return currentEpicProductionProcessResult{}, errCurrentEpicProductionInvalid
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	accountID := strings.TrimSpace(s.accountID())
	weekStart := currentEpicProductionWeekStart(now)
	var result currentEpicProductionProcessResult
	err = repositories.CharacterAssets.WithinCharacterAssets(ctx, characterID, func(
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		character, found, loadErr := characters.Load(ctx, characterID)
		if loadErr != nil {
			return loadErr
		}
		if !found || character.CharacterID != characterID || accountID == "" || strings.TrimSpace(character.AccountID) != accountID {
			return errCurrentEpicProductionInvalid
		}
		if character.Level < int(catalog.levelLimit) || character.Stats == nil || character.Stats[currentEpicProductionTargetStat] <= 0 {
			return errCurrentEpicProductionInvalid
		}
		inventory, found, loadErr := inventories.Load(ctx, characterID)
		if loadErr != nil {
			return loadErr
		}
		if !found || inventory.CharacterID != characterID || inventory.Slots == nil {
			return errCurrentEpicProductionMaterial
		}
		character = dnfrepo.CloneCharacter(character)
		inventory = dnfrepo.CloneInventory(inventory)
		weeklyCount := character.Stats[currentEpicProductionWeeklyCountStat]
		if character.Stats[currentEpicProductionWeekStartStat] != weekStart {
			weeklyCount = 0
		}
		if weeklyCount < 0 || weeklyCount >= int64(catalog.weeklyLimit) {
			return errCurrentEpicProductionWeeklyLimit
		}

		updates := make([]currentItemListEntry, 0, len(request.Materials)+1)
		for _, material := range request.Materials {
			entry, consumeErr := currentEpicProductionConsumeStack(&inventory, material.Slot, material.ItemID, catalog.processMaterialNeedCount)
			if consumeErr != nil {
				return consumeErr
			}
			updates = append(updates, entry)
		}
		requiredSlot, found := currentEpicProductionFindMaterialSlot(inventory, catalog.requiredMaterialItemID, catalog.requiredMaterialItemCount)
		if !found {
			return fmt.Errorf("%w: required item=%d need=%d", errCurrentEpicProductionMaterial, catalog.requiredMaterialItemID, catalog.requiredMaterialItemCount)
		}
		requiredEntry, consumeErr := currentEpicProductionConsumeStack(&inventory, requiredSlot, catalog.requiredMaterialItemID, catalog.requiredMaterialItemCount)
		if consumeErr != nil {
			return consumeErr
		}
		updates = append(updates, requiredEntry)

		bigSuccess := currentEpicProductionBigSuccess(catalog)
		chargeGain := uint64(basePoints)
		if bigSuccess {
			chargeGain *= uint64(catalog.bigChanceMultiply)
		}
		if chargeGain > math.MaxInt64 {
			return errCurrentEpicProductionInvalid
		}
		charge := character.Stats[currentEpicProductionChargeStat]
		if charge < 0 {
			return errCurrentEpicProductionInvalid
		}
		charge += int64(chargeGain)
		if charge > int64(catalog.maxChargePoint) {
			charge = int64(catalog.maxChargePoint)
		}
		character.Stats[currentEpicProductionChargeStat] = charge
		character.Stats[currentEpicProductionWeeklyCountStat] = weeklyCount + 1
		character.Stats[currentEpicProductionWeekStartStat] = weekStart
		character.UpdatedAt = now.UTC()
		inventory.UpdatedAt = now.UTC()
		if saveErr := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); saveErr != nil {
			return saveErr
		}
		if saveErr := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); saveErr != nil {
			return saveErr
		}
		sortCurrentItemListEntries(updates)
		result = currentEpicProductionProcessResult{
			ChargePoint: uint32(charge),
			WeeklyCount: uint32(weeklyCount + 1),
			BigSuccess:  bigSuccess,
			Updates:     updates,
		}
		return nil
	})
	if err != nil {
		return currentEpicProductionProcessResult{}, err
	}
	return result, nil
}

func buildCurrentEpicProductionProcessSuccessBody(result currentEpicProductionProcessResult) []byte {
	var body packetWriter
	body.writeUint32(result.ChargePoint)
	if result.BigSuccess {
		body.writeByte(1)
	} else {
		body.writeByte(0)
	}
	body.writeByte(0)
	return body.bytes()
}

func (s *Service) handleCurrentEpicProductionProcess(session *gameSession, body []byte) error {
	request, err := decodeCurrentEpicProductionProcessRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-process-blocked", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionProcess), currentEpicProductionGenericError)
	}
	s.initialEquipmentMu.Lock()
	archive, archiveErr := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if archiveErr != nil {
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionProcess), currentEpicProductionGenericError)
	}
	catalog, err := parseCurrentEpicProductionCatalog(archive)
	if err != nil {
		s.logGameEvent(session, "game-upper-epic-production-process-blocked", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionProcess), currentEpicProductionGenericError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentEpicProductionProcess(ctx, session, catalog, request, time.Now().UTC())
	if err != nil {
		code := byte(currentEpicProductionGenericError)
		if errors.Is(err, errCurrentEpicProductionWeeklyLimit) {
			code = currentEpicProductionWeeklyLimitError
		}
		s.logGameEvent(session, "game-upper-epic-production-process-blocked", "body_len", len(body), "material_count", len(request.Materials), "result_code", code, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketEpicProductionProcess), code)
	}
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketEpicProductionProcess), buildCurrentEpicProductionProcessSuccessBody(result)); err != nil {
		return err
	}
	if len(result.Updates) != 0 {
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, result.Updates), 0); err != nil {
			return err
		}
	}
	if err := s.sendSelectedCurrentEpicProductionInfo(session, "epic_production_process_success"); err != nil {
		return err
	}
	s.logGameEvent(session, "game-upper-epic-production-process-applied",
		"character_id", session.selectedCharacterID,
		"material_count", len(request.Materials),
		"charge_point", result.ChargePoint,
		"weekly_count", result.WeeklyCount,
		"big_success", result.BigSuccess,
		"body_source", "current_exe_op1419_success_u8_true_u32_charge_u8_state_u8_ui_flag")
	return nil
}

func buildCurrentEpicProductionInfoBody(characterID uint16, targetItemID, chargePoint, remainingWeeklyCount, carryGroup1, carryGroup2 uint32) []byte {
	var body packetWriter
	body.writeUint32(uint32(characterID))
	state := make([]byte, currentEpicProductionInfoStateSize)
	binary.LittleEndian.PutUint32(state[0:4], remainingWeeklyCount)
	binary.LittleEndian.PutUint32(state[4:8], targetItemID)
	binary.LittleEndian.PutUint32(state[8:12], chargePoint)
	binary.LittleEndian.PutUint32(state[16:20], carryGroup1)
	binary.LittleEndian.PutUint32(state[20:24], carryGroup2)
	body.writeBytes(state)
	return body.bytes()
}

func currentEpicProductionRemainingWeeklyCount(usedCount, storedWeekStart int64, weeklyLimit uint32, now time.Time) uint32 {
	if weeklyLimit == 0 {
		return 0
	}
	if storedWeekStart != currentEpicProductionWeekStart(now.UTC()) || usedCount < 0 {
		usedCount = 0
	}
	if usedCount >= int64(weeklyLimit) {
		return 0
	}
	return weeklyLimit - uint32(usedCount)
}

func (s *Service) sendSelectedCurrentEpicProductionInfo(session *gameSession, source string) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil || !found || strings.TrimSpace(character.AccountID) != strings.TrimSpace(s.accountID()) || character.Stats == nil {
		return err
	}
	target := character.Stats[currentEpicProductionTargetStat]
	if target <= 0 || target > math.MaxUint32 {
		return nil
	}
	charge := character.Stats[currentEpicProductionChargeStat]
	if charge < 0 {
		charge = 0
	}
	if charge > math.MaxUint32 {
		charge = math.MaxUint32
	}
	s.initialEquipmentMu.Lock()
	archive, archiveErr := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if archiveErr != nil {
		return archiveErr
	}
	catalog, catalogErr := parseCurrentEpicProductionCatalog(archive)
	if catalogErr != nil {
		return catalogErr
	}
	usedWeeklyCount := character.Stats[currentEpicProductionWeeklyCountStat]
	storedWeekStart := character.Stats[currentEpicProductionWeekStartStat]
	now := time.Now().UTC()
	remainingWeeklyCount := currentEpicProductionRemainingWeeklyCount(usedWeeklyCount, storedWeekStart, catalog.weeklyLimit, now)
	if storedWeekStart != currentEpicProductionWeekStart(now) || usedWeeklyCount < 0 {
		usedWeeklyCount = 0
	}
	carryGroup1 := character.Stats[currentEpicProductionCarryGroup1Stat]
	if carryGroup1 < 0 || carryGroup1 > math.MaxUint32 {
		carryGroup1 = 0
	}
	carryGroup2 := character.Stats[currentEpicProductionCarryGroup2Stat]
	if carryGroup2 < 0 || carryGroup2 > math.MaxUint32 {
		carryGroup2 = 0
	}
	body := buildCurrentEpicProductionInfoBody(session.selectedCharacterID, uint32(target), uint32(charge), remainingWeeklyCount, uint32(carryGroup1), uint32(carryGroup2))
	s.logGameEvent(session, "game-upper-epic-production-info-send",
		"source", source,
		"character_id", session.selectedCharacterID,
		"msg_id", currentEpicProductionInfoMsgID,
		"target_item_id", target,
		"charge_point", charge,
		"weekly_limit", catalog.weeklyLimit,
		"weekly_used_count", usedWeeklyCount,
		"weekly_remaining_count", remainingWeeklyCount,
		"carry_group_1", carryGroup1,
		"carry_group_2", carryGroup2,
		"body_len", len(body),
		"body_source", "current_exe_HandleEpicProductionInfoNotice_u32_prefix_plus_28_byte_state")
	return s.sendGameUpperRawClass(session, currentEpicProductionInfoMsgID, body, 0)
}
