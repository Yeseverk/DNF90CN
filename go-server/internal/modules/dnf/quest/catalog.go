package quest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"longheng.io/server/internal/modules/dnf/jobmap"
	"longheng.io/server/internal/modules/dnf/profession"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const DefaultList = "n_quest/quest.lst"

var ErrCatalogEmpty = errors.New("dnf quest catalog is empty")

var (
	ErrQuestDefinitionMissing         = errors.New("dnf quest definition is missing")
	ErrQuestNotAcceptable             = errors.New("dnf quest is not acceptable for this character state")
	ErrQuestInitialTriggerUnsupported = errors.New("dnf quest initial trigger rule is not implemented")
	ErrQuestAcceptEventItemsRequired  = errors.New("dnf quest acceptance requires event-item transaction")
	ErrQuestRewardUnsupported         = errors.New("dnf quest reward type is unsupported")
	ErrQuestRewardMalformed           = errors.New("dnf quest reward data is malformed")
	ErrQuestRewardSelectionInvalid    = errors.New("dnf quest reward selection is invalid")
)

// RewardItemRule preserves one exact PVF [reward int data] item rule. Job and
// grow-type filters are evaluated only after the selected character is known.
type RewardItemRule struct {
	ItemID       int64
	Count        int64
	HasJobFilter bool
	Job          int
	GrowType     int
}

// TargetCharacterRule preserves one PVF [target character] tuple:
// job tag, first-grow branch, and awakening stage.
type TargetCharacterRule struct {
	JobTag         string
	FirstGrowType  int
	AwakeningStage int
}

// MonsterRewardItemEntry preserves one PVF [monster reward item] 7-tuple:
// monster_code dungeon_id difficulty item_id count drop_rate max_stack.
// dungeon_id -1 and difficulty -1 are wildcards (86JP QuestDropProvider).
type MonsterRewardItemEntry struct {
	MonsterCode int64
	DungeonID   int64
	Difficulty  int64
	ItemID      int64
	Count       int64
	DropRate    int64
	MaxStack    int64
}

// EnemyRewardItemEntry preserves one PVF [enemy reward item] 8-tuple:
// enemy_code enemy_type dungeon_id difficulty item_id count drop_rate max_stack.
// enemy_type 2 = AI character, 3 = passive object (86JP QuestDropProvider).
type EnemyRewardItemEntry struct {
	EnemyCode  int64
	EnemyType  int64
	DungeonID  int64
	Difficulty int64
	ItemID     int64
	Count      int64
	DropRate   int64
	MaxStack   int64
}

type FinishRewardPlan struct {
	QuestID             int64
	PVFPath             string
	QuestLevel          int
	Difficulty          rune
	IgnoreLevel4Exp     bool
	HasGoldReward       bool
	GoldMultiple        int
	Items               []RewardItemRule
	RewardSelectionUsed bool
	ProfessionRequest   profession.Request
	Profession          profession.Transition
	HasProfession       bool
	ExpertJobType       byte
	HasExpertJob        bool
	HasSlotExpansion    bool
	SlotExpansionIndex  uint32
	SlotExpansionBit    byte
}

// Definition is the subset of a PVF quest definition required to decide
// whether the quest belongs in ENUM_NOTIPACKET_ACCEPTABLE_QUEST_LIST.
type Definition struct {
	ID                     int64
	Path                   string
	Grade                  string
	Type                   string
	SubType                int
	IntData                []int64
	CheckCount             []int64
	ConditionData          []int64
	DependGiveItemData     []int64
	HasDependGiveItem      bool
	CantGiveUp             bool
	LevelMin               int
	LevelMax               int
	Job                    string
	TargetCharacter        string
	TargetCharacterRules   []TargetCharacterRule
	GrowType               int
	HasGrowType            bool
	JobChangeQuest         int
	ExposedByNPC           int64
	IsEvent                bool
	HasCreatureRequirement bool
	HasExpertRequirement   bool
	MainQuestID            int64
	PreRequiredGroups      [][]int64
	PreRequiredAnswers     []int64
	CollisionQuests        []int64
	Difficulty             string
	IgnoreQuestLevel4Exp   bool
	NoExperience           bool
	RewardType             string
	RewardIntData          []int64
	HasGoldReward          bool
	GoldMultiple           int
	RewardItems            []RewardItemRule
	RewardSelectionItems   []RewardItemRule
	RewardParseError       string
	MonsterRewardItems     []MonsterRewardItemEntry
	EnemyRewardItems       []EnemyRewardItemEntry
}

type Catalog struct {
	ordered []Definition
	byID    map[int64]Definition
}

type Snapshot struct {
	Definitions int
	Epic        int
	Normal      int
}

type CharacterEligibility struct {
	Level    int
	Job      int
	GrowType int
}

type EligibilityResult struct {
	IDs         []int32
	EpicCount   int
	ActiveCount int
}

type AcceptPlan struct {
	QuestID         int64
	Path            string
	Type            string
	InitTrigger     uint32
	LinkedSubQuests []AcceptLinkedSubQuestPlan
}

type AcceptLinkedSubQuestPlan struct {
	QuestID     int64
	Path        string
	Type        string
	InitTrigger uint32
}

func Load(ctx context.Context, index *dnfpvf.Index) (*Catalog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	entries := index.List(DefaultList)
	if len(entries) == 0 {
		return nil, ErrCatalogEmpty
	}
	catalog := &Catalog{
		ordered: make([]Definition, 0, len(entries)),
		byID:    make(map[int64]Definition, len(entries)),
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.ID <= 0 || entry.ID > math.MaxInt32 {
			continue
		}
		if _, exists := catalog.byID[entry.ID]; exists {
			continue
		}
		doc, ok := index.Document(entry.Path)
		if !ok {
			continue
		}
		definition := parseDefinition(entry, doc)
		catalog.ordered = append(catalog.ordered, definition)
		catalog.byID[definition.ID] = definition
	}
	if len(catalog.ordered) == 0 {
		return nil, ErrCatalogEmpty
	}
	return catalog, nil
}

func (c *Catalog) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	snapshot := Snapshot{Definitions: len(c.ordered)}
	for _, definition := range c.ordered {
		switch normalizeQuestTag(definition.Grade) {
		case "epic":
			snapshot.Epic++
		case "", "normal":
			snapshot.Normal++
		}
	}
	return snapshot
}

// ExpertJobTransitionQuestIDs returns every quest explicitly assigned to the
// PVF expert-job transition chain, preserving quest-list order.
func (c *Catalog) ExpertJobTransitionQuestIDs() []int64 {
	if c == nil {
		return nil
	}
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, definition := range c.ordered {
		if definition.ID <= 0 || definition.JobChangeQuest != 20 {
			continue
		}
		if _, exists := seen[definition.ID]; exists {
			continue
		}
		seen[definition.ID] = struct{}{}
		ids = append(ids, definition.ID)
	}
	return ids
}

// ExpertJobTransitionTerminalQuestIDs returns transition quests that are not
// prerequisites of another transition quest in the same PVF chain.
func (c *Catalog) ExpertJobTransitionTerminalQuestIDs() []int64 {
	ids := c.ExpertJobTransitionQuestIDs()
	if len(ids) == 0 {
		return nil
	}
	members := make(map[int64]struct{}, len(ids))
	for _, questID := range ids {
		members[questID] = struct{}{}
	}
	nonTerminal := make(map[int64]struct{})
	for _, questID := range ids {
		for _, successor := range c.Successors(questID) {
			if _, ok := members[successor.ID]; ok {
				nonTerminal[questID] = struct{}{}
				break
			}
		}
	}
	terminal := make([]int64, 0, len(ids))
	for _, questID := range ids {
		if _, exists := nonTerminal[questID]; !exists {
			terminal = append(terminal, questID)
		}
	}
	return terminal
}

func (c *Catalog) Find(id int64) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	definition, ok := c.byID[id]
	return cloneDefinition(definition), ok
}

// Successors returns the definitions that list questID in any prerequisite
// group, preserving catalog order. It is the static PVF chain step used to
// resolve the next quest after a completed one; it does not inspect character
// quest state.
func (c *Catalog) Successors(questID int64) []Definition {
	if c == nil {
		return nil
	}
	var successors []Definition
	for _, definition := range c.ordered {
		for _, group := range definition.PreRequiredGroups {
			for _, prerequisite := range group {
				if prerequisite == questID {
					successors = append(successors, cloneDefinition(definition))
					goto nextDefinition
				}
			}
		}
	nextDefinition:
	}
	return successors
}

func (c *Catalog) Acceptable(character CharacterEligibility, record dnfrepo.QuestRecord) EligibilityResult {
	return c.eligibility(character, record, false)
}

// QuestList returns the PVF-backed IDs carried by the current EXE's
// ENUM_NOTIPACKET_ACCEPTABLE_QUEST_LIST notification. The task manual's All
// view combines this definition list with the active-progress snapshot, so an
// already active quest must remain present here even though PlanAccept must
// continue to reject accepting it a second time.
func (c *Catalog) QuestList(character CharacterEligibility, record dnfrepo.QuestRecord) EligibilityResult {
	return c.eligibility(character, record, true)
}

func (c *Catalog) eligibility(character CharacterEligibility, record dnfrepo.QuestRecord, includeActive bool) EligibilityResult {
	if c == nil || !jobmap.Valid(character.Job) {
		return EligibilityResult{}
	}
	if character.Level <= 0 {
		character.Level = 1
	}
	completed, active := questStateSets(record)
	result := EligibilityResult{IDs: make([]int32, 0, 64)}
	for _, definition := range c.ordered {
		if !definitionAcceptable(definition, character, completed, active, includeActive) {
			continue
		}
		result.IDs = append(result.IDs, int32(definition.ID))
		if _, exists := active[definition.ID]; exists {
			result.ActiveCount++
		}
		if normalizeQuestTag(definition.Grade) == "epic" {
			result.EpicCount++
		}
	}
	return result
}

// PlanAccept reuses the same PVF/character/database eligibility decision used
// for the acceptable-quest snapshot and derives only initial-trigger rules
// that are represented in the typed catalog. Quests that grant event items at
// acceptance remain blocked until inventory and quest state share one unit of
// work.
func (c *Catalog) PlanAccept(character CharacterEligibility, record dnfrepo.QuestRecord, questID int64) (AcceptPlan, error) {
	definition, ok := c.Find(questID)
	if !ok {
		return AcceptPlan{}, ErrQuestDefinitionMissing
	}
	if !jobmap.Valid(character.Job) {
		return AcceptPlan{}, ErrQuestNotAcceptable
	}
	if character.Level <= 0 {
		character.Level = 1
	}
	completed, active := questStateSets(record)
	if !definitionAcceptable(definition, character, completed, active, false) {
		return AcceptPlan{}, ErrQuestNotAcceptable
	}
	if definition.HasDependGiveItem {
		return AcceptPlan{}, ErrQuestAcceptEventItemsRequired
	}
	// Keep the accepted task's trigger in the same domain state as the C#
	// QuestData.ComputeInitTrigger implementation.  This is deliberately a
	// PVF/state calculation only; it does not infer any packet fields.
	trigger := definitionInitialTrigger(definition, completed)
	return AcceptPlan{
		QuestID:         definition.ID,
		Path:            definition.Path,
		Type:            definition.Type,
		InitTrigger:     trigger,
		LinkedSubQuests: c.linkedSubQuestAcceptPlans(definition, character, completed, active),
	}, nil
}

// PlanFinishReward parses the detached PVF reward for the current character.
// Profession rewards remain requests until the settlement owner resolves them
// through the job profile and current grow_type inside the repository UoW. It
// does not allocate slots, mutate repositories, calculate EXP, or emit an ACK.
func (c *Catalog) PlanFinishReward(character CharacterEligibility, questID int64, rewardSelectIndex uint16, hasRewardSelect bool) (FinishRewardPlan, error) {
	definition, ok := c.Find(questID)
	if !ok {
		return FinishRewardPlan{}, ErrQuestDefinitionMissing
	}
	if !jobmap.Valid(character.Job) {
		return FinishRewardPlan{}, ErrQuestNotAcceptable
	}
	plan := FinishRewardPlan{
		QuestID:         definition.ID,
		PVFPath:         definition.Path,
		QuestLevel:      definition.LevelMin,
		Difficulty:      'G',
		IgnoreLevel4Exp: definition.IgnoreQuestLevel4Exp,
		HasGoldReward:   definition.HasGoldReward,
		GoldMultiple:    definition.GoldMultiple,
	}
	if difficulty := []rune(strings.TrimSpace(definition.Difficulty)); len(difficulty) == 1 {
		plan.Difficulty = difficulty[0]
	}
	rewardType := normalizeQuestTag(definition.RewardType)
	if rewardType == "" {
		if !emptyRewardDataValid(definition.RewardIntData) {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d empty reward type has data=%v", ErrQuestRewardMalformed, questID, definition.RewardIntData)
		}
		if hasRewardSelect {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d has no selectable reward", ErrQuestRewardSelectionInvalid, questID)
		}
		return plan, nil
	}
	if rewardType == "grow type" || rewardType == "awakening type" {
		request, err := profession.ParseReward(definition.JobChangeQuest, definition.RewardType, definition.RewardIntData)
		if err != nil {
			if errors.Is(err, profession.ErrRewardMalformed) {
				return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d: %v", ErrQuestRewardMalformed, questID, err)
			}
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d: %v", ErrQuestRewardUnsupported, questID, err)
		}
		plan.ProfessionRequest = request
		plan.HasProfession = true
		if hasRewardSelect {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d profession reward has no selection", ErrQuestRewardSelectionInvalid, questID)
		}
		return plan, nil
	}
	if rewardType == "expert job" {
		// The current PVF encodes the chosen auxiliary-profession type as one
		// positive byte (for example, quest 2710 supplies 3 for disjointer).
		// Current EXE op34 consumes that value under chain type 20.
		if len(definition.RewardIntData) != 1 || definition.RewardIntData[0] <= 0 || definition.RewardIntData[0] > math.MaxUint8 {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d expert job values=%v", ErrQuestRewardMalformed, questID, definition.RewardIntData)
		}
		if hasRewardSelect {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d expert job reward has no selection", ErrQuestRewardSelectionInvalid, questID)
		}
		plan.ExpertJobType = byte(definition.RewardIntData[0])
		plan.HasExpertJob = true
		return plan, nil
	}
	if rewardType == "slot expansion" {
		if hasRewardSelect {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d slot expansion reward has no selection", ErrQuestRewardSelectionInvalid, questID)
		}
		// The current runtime PVF stores the slot-expansion index in
		// [reward int data]: 0=support, 1=magic stone, 2=earring. The durable
		// character column and current global actor snapshots store the
		// corresponding bit.
		if len(definition.RewardIntData) != 1 || definition.RewardIntData[0] < 0 || definition.RewardIntData[0] > 2 {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d slot expansion values=%v", ErrQuestRewardMalformed, questID, definition.RewardIntData)
		}
		plan.HasSlotExpansion = true
		plan.SlotExpansionIndex = uint32(definition.RewardIntData[0])
		plan.SlotExpansionBit, _ = ExEquipSlotBitForPVFIndex(plan.SlotExpansionIndex)
		return plan, nil
	}
	if rewardType != "item" {
		return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d type=%q", ErrQuestRewardUnsupported, questID, definition.RewardType)
	}
	if definition.RewardParseError != "" {
		return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d: %s", ErrQuestRewardMalformed, questID, definition.RewardParseError)
	}
	for _, rule := range definition.RewardItems {
		if rewardRuleMatches(rule, character) {
			plan.Items = append(plan.Items, rule)
		}
	}
	selectable := make([]RewardItemRule, 0, len(definition.RewardSelectionItems))
	for _, rule := range definition.RewardSelectionItems {
		if rewardRuleMatches(rule, character) {
			selectable = append(selectable, rule)
		}
	}
	if len(selectable) == 0 {
		if hasRewardSelect {
			return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d has no selectable reward", ErrQuestRewardSelectionInvalid, questID)
		}
		return plan, nil
	}
	if !hasRewardSelect || int(rewardSelectIndex) >= len(selectable) {
		return FinishRewardPlan{}, fmt.Errorf("%w: quest=%d selected=%d choices=%d", ErrQuestRewardSelectionInvalid, questID, rewardSelectIndex, len(selectable))
	}
	plan.Items = append(plan.Items, selectable[int(rewardSelectIndex)])
	plan.RewardSelectionUsed = true
	return plan, nil
}

func emptyRewardDataValid(values []int64) bool {
	for _, value := range values {
		if value != 0 {
			return false
		}
	}
	return true
}

func parseDefinition(entry dnfpvf.ListEntry, doc *dnfpvf.Document) Definition {
	definition := Definition{
		ID:           entry.ID,
		Path:         entry.Path,
		LevelMin:     1,
		LevelMax:     99,
		GrowType:     -1,
		ExposedByNPC: -1,
	}
	definition.Grade, _ = doc.Text("grade")
	definition.Type, _ = doc.Text("type")
	definition.Difficulty, _ = doc.Text("difficulty")
	definition.RewardType, _ = doc.Text("reward type")
	definition.RewardIntData = append([]int64(nil), doc.Ints("reward int data")...)
	if value, ok := doc.Int("gold multiple"); ok {
		definition.GoldMultiple = boundedInt(value)
	}
	if tokens, ok := doc.Section("ignore quest level 4 exp"); ok {
		definition.IgnoreQuestLevel4Exp = len(tokens) == 0
		for _, token := range tokens {
			if token.Kind == dnfpvf.TokenInt {
				definition.IgnoreQuestLevel4Exp = token.Int != 0
				break
			}
		}
	}
	for _, attribute := range doc.Texts("attribute") {
		if normalizeQuestTag(attribute) == "not give exp quest" {
			definition.NoExperience = true
			break
		}
	}
	if normalizeQuestTag(definition.RewardType) == "item" {
		definition.RewardItems, definition.HasGoldReward, definition.RewardParseError = parseRewardItemRules(doc, "reward int data")
		definition.RewardSelectionItems, definition.RewardParseError = parseRewardItemRulesAfter(definition.RewardParseError, doc, "reward selection int data")
	}
	if value, ok := doc.Int("sub type"); ok {
		definition.SubType = boundedInt(value)
	}
	definition.IntData = append([]int64(nil), doc.Ints("int data")...)
	definition.CheckCount = append([]int64(nil), doc.Ints("check count")...)
	definition.ConditionData = append([]int64(nil), doc.Ints("condition data")...)
	if _, ok := doc.Section("depend give item"); ok {
		definition.HasDependGiveItem = true
		definition.DependGiveItemData = append([]int64(nil), doc.Ints("depend give item")...)
	}
	_, definition.CantGiveUp = doc.Section("cant giveup")
	definition.Job = strings.Join(doc.Texts("job"), " ")
	definition.TargetCharacter = strings.Join(doc.Texts("target character"), " ")
	if tokens, ok := doc.Section("target character"); ok {
		definition.TargetCharacterRules = parseTargetCharacterRules(tokens)
	}
	if levels := doc.Ints("level"); len(levels) > 0 {
		definition.LevelMin = boundedInt(levels[0])
		if len(levels) > 1 {
			definition.LevelMax = boundedInt(levels[1])
		}
	}
	if value, ok := doc.Int("grow type"); ok {
		definition.GrowType = boundedInt(value)
		definition.HasGrowType = true
	}
	if value, ok := doc.Int("job change quest"); ok {
		definition.JobChangeQuest = boundedInt(value)
	}
	if value, ok := doc.Int("exposed by npc"); ok {
		definition.ExposedByNPC = value
	}
	if value, ok := doc.Int("main quest"); ok {
		definition.MainQuestID = value
	}
	if value, ok := doc.Int("event"); ok && value != 0 {
		definition.IsEvent = true
	}
	if value, ok := doc.Int("creature kind"); ok && value >= 0 {
		definition.HasCreatureRequirement = true
	}
	if values := doc.Ints("expertjob level"); len(values) >= 2 && values[0] >= 0 && values[1] >= 0 {
		definition.HasExpertRequirement = true
	}
	definition.PreRequiredGroups = sectionIntGroups(doc, "pre required quest")
	definition.PreRequiredAnswers = flattenIntGroups(sectionIntGroups(doc, "pre required quest answer"))
	definition.CollisionQuests = flattenIntGroups(sectionIntGroups(doc, "collision quest"))
	definition.MonsterRewardItems = parseMonsterRewardItems(doc)
	definition.EnemyRewardItems = parseEnemyRewardItems(doc)
	return definition
}

func parseRewardItemRulesAfter(previous string, doc *dnfpvf.Document, section string) ([]RewardItemRule, string) {
	rules, _, parseError := parseRewardItemRules(doc, section)
	if previous != "" {
		return rules, previous
	}
	return rules, parseError
}

func parseRewardItemRules(doc *dnfpvf.Document, section string) ([]RewardItemRule, bool, string) {
	tokens, ok := doc.Section(section)
	if !ok || len(tokens) == 0 {
		return nil, false, ""
	}
	rules := make([]RewardItemRule, 0, len(tokens)/2)
	hasGoldReward := false
	for index := 0; index < len(tokens); {
		if tokens[index].Kind != dnfpvf.TokenInt {
			return nil, false, fmt.Sprintf("[%s] token %d must be item id, got kind=%s raw=%q", section, index, tokens[index].Kind, tokens[index].Raw)
		}
		rule := RewardItemRule{ItemID: tokens[index].Int, Job: -1, GrowType: -1}
		index++
		if index < len(tokens) && rewardJobMarker(tokens[index]) {
			if index+3 >= len(tokens) || tokens[index+1].Kind != dnfpvf.TokenInt || tokens[index+2].Kind != dnfpvf.TokenInt || tokens[index+3].Kind != dnfpvf.TokenInt {
				return nil, false, fmt.Sprintf("[%s] item %d has incomplete [job] tuple", section, rule.ItemID)
			}
			rule.HasJobFilter = true
			rule.Job = boundedInt(tokens[index+1].Int)
			rule.GrowType = boundedInt(tokens[index+2].Int)
			rule.Count = tokens[index+3].Int
			index += 4
		} else {
			if index >= len(tokens) || tokens[index].Kind != dnfpvf.TokenInt {
				return nil, false, fmt.Sprintf("[%s] item %d is missing count", section, rule.ItemID)
			}
			rule.Count = tokens[index].Int
			index++
		}
		if rule.ItemID == 0 {
			hasGoldReward = hasGoldReward || rule.Count > 0
			continue
		}
		if rule.ItemID < 0 || rule.Count <= 0 || rule.Count > math.MaxUint32 {
			return nil, false, fmt.Sprintf("[%s] invalid item/count %d/%d", section, rule.ItemID, rule.Count)
		}
		rules = append(rules, rule)
	}
	return rules, hasGoldReward, ""
}

func rewardJobMarker(token dnfpvf.Token) bool {
	if token.Kind != dnfpvf.TokenString && token.Kind != dnfpvf.TokenIdent {
		return false
	}
	return normalizeQuestTag(token.Value) == "job"
}

// parseMonsterRewardItems parses the PVF [monster reward item] section.
// Each entry is a 7-tuple: monster_code dungeon_id difficulty item_id count
// drop_rate max_stack. dungeon_id -1 and difficulty -1 are wildcards.
func parseMonsterRewardItems(doc *dnfpvf.Document) []MonsterRewardItemEntry {
	tokens, ok := doc.Section("monster reward item")
	if !ok || len(tokens) == 0 {
		return nil
	}
	ints := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenInt {
			ints = append(ints, token.Int)
		}
	}
	entries := make([]MonsterRewardItemEntry, 0, len(ints)/7)
	for i := 0; i+6 < len(ints); i += 7 {
		entries = append(entries, MonsterRewardItemEntry{
			MonsterCode: ints[i],
			DungeonID:   ints[i+1],
			Difficulty:  ints[i+2],
			ItemID:      ints[i+3],
			Count:       ints[i+4],
			DropRate:    ints[i+5],
			MaxStack:    ints[i+6],
		})
	}
	return entries
}

// parseEnemyRewardItems parses the PVF [enemy reward item] section.
// Each entry is an 8-tuple: enemy_code enemy_type dungeon_id difficulty
// item_id count drop_rate max_stack. enemy_type 2 = AI character,
// 3 = passive object.
func parseEnemyRewardItems(doc *dnfpvf.Document) []EnemyRewardItemEntry {
	tokens, ok := doc.Section("enemy reward item")
	if !ok || len(tokens) == 0 {
		return nil
	}
	ints := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenInt {
			ints = append(ints, token.Int)
		}
	}
	entries := make([]EnemyRewardItemEntry, 0, len(ints)/8)
	for i := 0; i+7 < len(ints); i += 8 {
		entries = append(entries, EnemyRewardItemEntry{
			EnemyCode:  ints[i],
			EnemyType:  ints[i+1],
			DungeonID:  ints[i+2],
			Difficulty: ints[i+3],
			ItemID:     ints[i+4],
			Count:      ints[i+5],
			DropRate:   ints[i+6],
			MaxStack:   ints[i+7],
		})
	}
	return entries
}

func parseTargetCharacterRules(tokens []dnfpvf.Token) []TargetCharacterRule {
	rules := make([]TargetCharacterRule, 0, len(tokens)/3)
	for index := 0; index+2 < len(tokens); {
		jobToken := tokens[index]
		if (jobToken.Kind != dnfpvf.TokenString && jobToken.Kind != dnfpvf.TokenIdent) ||
			tokens[index+1].Kind != dnfpvf.TokenInt || tokens[index+2].Kind != dnfpvf.TokenInt {
			index++
			continue
		}
		rules = append(rules, TargetCharacterRule{
			JobTag: jobToken.Value, FirstGrowType: boundedInt(tokens[index+1].Int), AwakeningStage: boundedInt(tokens[index+2].Int),
		})
		index += 3
	}
	return rules
}

func rewardRuleMatches(rule RewardItemRule, character CharacterEligibility) bool {
	if rule.ItemID <= 0 || rule.Count <= 0 {
		return false
	}
	if !rule.HasJobFilter {
		return true
	}
	return rule.Job == character.Job && (rule.GrowType < 0 || rule.GrowType == character.GrowType&0x0f)
}

// definitionInitialTrigger ports the C# QuestData.ComputeInitTrigger domain
// rules.  The current EXE accepts the resulting u32 through the existing
// typed accept ACK; C# packet layouts are not used here.
//
// C# intentionally uses one as the fallback for a recognised-but-not-yet
// specialised quest type.  Returning a fake "unsupported" error after the
// task was advertised in the acceptable-quest list made the second main-line
// task (3146) impossible to accept.
func definitionInitialTrigger(definition Definition, completed map[int64]struct{}) uint32 {
	tag := normalizeQuestTag(definition.Type)
	switch tag {
	case "seek n meet npc":
		return seekAndMeetNPCInitialTrigger(definition.IntData)
	case "quest clear", "clear quest":
		return questClearInitialTrigger(definition.IntData, completed)
	case "condition under clear", "clear map":
		return triggerFromIntData(definition.IntData, 4)
	case "condition under clear2":
		return triggerFromIntData(definition.IntData, 5)
	case "normal clear":
		return packTriggerChannels(1, 1, 0)
	case "hunt monster":
		return triggerFromIntData(definition.IntData, 4)
	case "hunt enemy":
		return triggerFromIntData(definition.IntData, 5)
	}
	if csharpTypeOneQuest(tag) && definition.SubType == 6 && len(definition.IntData) >= 3 && definition.IntData[2] > 0 {
		return boundedTriggerChannel(definition.IntData[2])
	}
	return 1
}

func (c *Catalog) linkedSubQuestAcceptPlans(definition Definition, character CharacterEligibility, completed, active map[int64]struct{}) []AcceptLinkedSubQuestPlan {
	if c == nil {
		return nil
	}
	tag := normalizeQuestTag(definition.Type)
	if tag != "quest clear" && tag != "clear quest" {
		return nil
	}
	seen := make(map[int64]struct{}, len(definition.IntData))
	plans := make([]AcceptLinkedSubQuestPlan, 0, len(definition.IntData))
	for _, childID := range definition.IntData {
		if childID <= 0 {
			continue
		}
		if _, duplicate := seen[childID]; duplicate {
			continue
		}
		seen[childID] = struct{}{}
		if _, exists := completed[childID]; exists {
			continue
		}
		if _, exists := active[childID]; exists {
			continue
		}
		child, ok := c.byID[childID]
		if !ok || child.MainQuestID != definition.ID || child.HasDependGiveItem || !shouldAutoCompleteNoRewardSubQuest(child) {
			continue
		}
		if !definitionAcceptable(child, character, completed, active, false) {
			continue
		}
		active[childID] = struct{}{}
		plans = append(plans, AcceptLinkedSubQuestPlan{
			QuestID:     child.ID,
			Path:        child.Path,
			Type:        child.Type,
			InitTrigger: definitionInitialTrigger(child, completed),
		})
	}
	return plans
}

func seekAndMeetNPCInitialTrigger(values []int64) uint32 {
	if len(values) < 3 {
		return 1
	}
	itemCount := values[1]
	if itemCount <= 0 {
		itemCount = 1
	}
	meetNPCCount := int64(0)
	if values[2] > 0 {
		meetNPCCount = 1
	}
	return packTriggerChannels(itemCount, meetNPCCount, 0)
}

func questClearInitialTrigger(values []int64, completed map[int64]struct{}) uint32 {
	missing := int64(0)
	for _, questID := range values {
		if questID > 0 {
			if _, exists := completed[questID]; !exists {
				missing++
			}
		}
	}
	if missing == 0 {
		return 1
	}
	return boundedTriggerChannel(missing)
}

func csharpTypeOneQuest(tag string) bool {
	switch tag {
	case "seeking", "hunt monster", "meet npc", "hunt enemy", "use item", "get item", "get score",
		"clear quest", "quest clear", "custom quest", "send chatting", "check life", "amplify item",
		"disjoint item", "equipped item", "check time", "use fortune coin", "meet secret npc",
		"turn gold card", "ui click", "seek n meet npc", "assault count", "mobile":
		return true
	default:
		return false
	}
}

func triggerFromIntData(values []int64, stride int) uint32 {
	if len(values) == 0 || stride <= 0 {
		return 1
	}
	counts := make([]int64, 0, 3)
	for offset := stride - 1; offset < len(values) && len(counts) < 3; offset += stride {
		counts = append(counts, values[offset])
	}
	if len(counts) == 0 {
		return 1
	}
	return packTriggerChannels(counts...)
}

func packTriggerChannels(values ...int64) uint32 {
	var packed uint32
	for index, value := range values {
		if index == 3 {
			break
		}
		packed |= boundedTriggerChannel(value) << (index * 9)
	}
	return packed
}

func boundedTriggerChannel(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value > 0x1ff {
		return 0x1ff
	}
	return uint32(value)
}

func definitionAcceptable(definition Definition, character CharacterEligibility, completed, active map[int64]struct{}, includeActive bool) bool {
	if definition.ExposedByNPC == 0 || definition.IsEvent || definition.HasCreatureRequirement || definition.HasExpertRequirement {
		return false
	}
	grade := normalizeQuestTag(definition.Grade)
	if !selectableGrade(grade) || grade == "training" {
		return false
	}
	if character.Level < definition.LevelMin || character.Level > definition.LevelMax {
		return false
	}
	if !definitionMatchesCharacter(definition, character) {
		return false
	}
	if _, exists := active[definition.ID]; exists && !includeActive {
		return false
	}
	if _, exists := completed[definition.ID]; exists && !repeatableGrade(grade) {
		return false
	}
	if len(definition.PreRequiredGroups) > 0 {
		matched := false
		for _, group := range definition.PreRequiredGroups {
			if completedAll(group, completed) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	// Answer-dependent branches require a durable answer flag. QuestRecord does
	// not yet expose such a typed field, so do not guess a branch from completion.
	if len(definition.PreRequiredAnswers) > 0 {
		return false
	}
	for _, collision := range definition.CollisionQuests {
		if collision <= 0 {
			continue
		}
		if _, exists := completed[collision]; exists {
			return false
		}
	}
	return true
}

// definitionMatchesCharacter is the shared PVF job/grow-type predicate for
// ordinary acceptance and server-owned main-quest skip planning. It contains
// no level, prerequisite, collision, event, or quest-state policy.
func definitionMatchesCharacter(definition Definition, character CharacterEligibility) bool {
	if job := strings.TrimSpace(definition.Job); job != "" && normalizeQuestTag(job) != "all" && !jobmap.MatchesQuestTag(job, character.Job) {
		return false
	}
	if len(definition.TargetCharacterRules) != 0 && definition.JobChangeQuest >= 1 && definition.JobChangeQuest <= 3 {
		matched := false
		firstGrowType := character.GrowType & 0x0f
		awakeningStage := (character.GrowType >> 4) & 0x0f
		for _, rule := range definition.TargetCharacterRules {
			if jobmap.MatchesQuestTag(rule.JobTag, character.Job) && rule.FirstGrowType == firstGrowType && rule.AwakeningStage == awakeningStage {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	} else if target := strings.TrimSpace(definition.TargetCharacter); target != "" && !jobmap.MatchesQuestTag(target, character.Job) {
		return false
	}
	if definition.HasGrowType && definition.GrowType >= 0 {
		switch definition.JobChangeQuest {
		case 1, 10, 20:
		case 2, 3:
			if definition.GrowType != character.GrowType&0x0f {
				return false
			}
		default:
			if definition.GrowType != character.GrowType {
				return false
			}
		}
	}
	return true
}

func questStateSets(record dnfrepo.QuestRecord) (map[int64]struct{}, map[int64]struct{}) {
	completed := make(map[int64]struct{})
	active := make(map[int64]struct{})
	collect := func(states map[int64]dnfrepo.QuestState) {
		for id, state := range states {
			if id <= 0 {
				continue
			}
			if isCompletedQuestStatus(state.Status) {
				completed[id] = struct{}{}
				continue
			}
			if isActiveQuestStatus(state.Status) {
				active[id] = struct{}{}
			}
		}
	}
	collect(record.States)
	collect(record.Progress)
	return completed, active
}

func isCompletedQuestStatus(status string) bool {
	switch normalizeStatus(status) {
	case "complete", "completed", "cleared", "finished", "done":
		return true
	default:
		return false
	}
}

func normalizeStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func selectableGrade(grade string) bool {
	switch grade {
	case "", "normal", "side", "sub", "epic", "realization", "training", "achievement", "daily", "daily random", "normaly repeat", "special daily", "common unique", "system":
		return true
	default:
		return strings.HasPrefix(grade, "daily") || strings.HasPrefix(grade, "special daily")
	}
}

func repeatableGrade(grade string) bool {
	return grade == "daily" || grade == "normaly repeat" || grade == "special daily" || strings.HasPrefix(grade, "special daily")
}

func normalizeQuestTag(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) >= 2 && value[0] == '[' && value[len(value)-1] == ']' {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func sectionIntGroups(doc *dnfpvf.Document, name string) [][]int64 {
	if doc == nil {
		return nil
	}
	want := normalizeQuestTag(name)
	groups := make([][]int64, 0, 1)
	for _, section := range doc.Sections {
		if normalizeQuestTag(section.Name) != want || section.Start < 0 || section.End > len(doc.Tokens) || section.Start > section.End {
			continue
		}
		group := make([]int64, 0, section.End-section.Start)
		for _, token := range doc.Tokens[section.Start:section.End] {
			if token.Kind == dnfpvf.TokenInt {
				group = append(group, token.Int)
			}
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func flattenIntGroups(groups [][]int64) []int64 {
	var values []int64
	for _, group := range groups {
		values = append(values, group...)
	}
	return values
}

func completedAll(ids []int64, completed map[int64]struct{}) bool {
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := completed[id]; !exists {
			return false
		}
	}
	return true
}

func boundedInt(value int64) int {
	if value > int64(math.MaxInt) {
		return math.MaxInt
	}
	if value < int64(math.MinInt) {
		return math.MinInt
	}
	return int(value)
}

func cloneDefinition(definition Definition) Definition {
	definition.IntData = append([]int64(nil), definition.IntData...)
	definition.CheckCount = append([]int64(nil), definition.CheckCount...)
	definition.ConditionData = append([]int64(nil), definition.ConditionData...)
	definition.DependGiveItemData = append([]int64(nil), definition.DependGiveItemData...)
	definition.PreRequiredGroups = append([][]int64(nil), definition.PreRequiredGroups...)
	for index := range definition.PreRequiredGroups {
		definition.PreRequiredGroups[index] = append([]int64(nil), definition.PreRequiredGroups[index]...)
	}
	definition.PreRequiredAnswers = append([]int64(nil), definition.PreRequiredAnswers...)
	definition.CollisionQuests = append([]int64(nil), definition.CollisionQuests...)
	definition.RewardIntData = append([]int64(nil), definition.RewardIntData...)
	definition.RewardItems = append([]RewardItemRule(nil), definition.RewardItems...)
	definition.RewardSelectionItems = append([]RewardItemRule(nil), definition.RewardSelectionItems...)
	definition.TargetCharacterRules = append([]TargetCharacterRule(nil), definition.TargetCharacterRules...)
	definition.MonsterRewardItems = append([]MonsterRewardItemEntry(nil), definition.MonsterRewardItems...)
	definition.EnemyRewardItems = append([]EnemyRewardItemEntry(nil), definition.EnemyRewardItems...)
	return definition
}

func SortedIDs(result EligibilityResult) []int32 {
	ids := append([]int32(nil), result.IDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// ExEquipSlotStat is the durable current-client bitset selected by the runtime
// PVF [slot expansion] reward index. Current Script.pvf uses indices 0, 1, and
// 2, but the current NoPack UI bits are deliberately not contiguous: its
// support/magic-stone/earring checks use 0x01, 0x02, and 0x10 respectively.
const (
	ExEquipSlotSupport    byte = 0x01
	ExEquipSlotMagicStone byte = 0x02
	ExEquipSlotEarring    byte = 0x10
	ExEquipSlotAll        byte = ExEquipSlotSupport | ExEquipSlotMagicStone | ExEquipSlotEarring
)

func ExEquipSlotBitForPVFIndex(index uint32) (byte, bool) {
	switch index {
	case 0:
		return ExEquipSlotSupport, true
	case 1:
		return ExEquipSlotMagicStone, true
	case 2:
		return ExEquipSlotEarring, true
	default:
		return 0, false
	}
}
