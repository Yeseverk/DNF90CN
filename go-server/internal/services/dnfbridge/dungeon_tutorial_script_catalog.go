package dnfbridge

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const defaultDungeonCinematicList = "cinematic/cinematic.lst"

var (
	errDungeonTutorialScriptSourceRequired = errors.New("dnf tutorial script PVF source is required")
	errDungeonTutorialScriptListEmpty      = errors.New("dnf tutorial cinematic list is empty")
	errDungeonTutorialScriptTargetsEmpty   = errors.New("dnf tutorial cinematic destroy targets are empty")
)

type dungeonTutorialScriptEvidence struct {
	CinematicID           int64
	CinematicPath         string
	MapID                 int64
	MonsterIndex          int
	MonsterActorIndexes   []int
	DestroyMonsterIndexes []int
}

type dungeonCinematicMonsterUsage struct {
	MapID                 int64
	MonsterActorIndexes   []int
	DestroyMonsterIndexes []int
}

type dungeonTutorialBasicActionEvidence struct {
	MapID           int64
	MapPath         string
	ActionPath      string
	MonsterIndex    int
	MonsterID       int64
	BehaviorIndexes []int
}

type dungeonBasicActionDestroyUsage struct {
	MonsterIDs      []int64
	BehaviorIndexes []int
}

type dungeonTutorialScriptCatalogSnapshot struct {
	CinematicEntries         int
	CinematicsWithTargets    int
	MapsWithTargets          int
	MonsterTargets           int
	ReadFailures             int
	ParseFailures            int
	BasicActionEntries       int
	BasicActionsWithTargets  int
	BasicActionMaps          int
	BasicActionTargets       int
	BasicActionReadFailures  int
	BasicActionParseFailures int
}

// pvfDungeonTutorialScriptCatalog indexes explicit cinematic and map-action
// [DESTROY] operations. Runtime object keys are not stored here; the room binds
// each static monster index to a fresh key.
type pvfDungeonTutorialScriptCatalog struct {
	byMapID            map[int64]map[int][]dungeonTutorialScriptEvidence
	basicActionByMapID map[int64]map[int]dungeonTutorialBasicActionEvidence
	snapshot           dungeonTutorialScriptCatalogSnapshot
}

func newPVFDungeonTutorialScriptCatalog(source dnfpvf.Source) (*pvfDungeonTutorialScriptCatalog, error) {
	if source == nil {
		return nil, errDungeonTutorialScriptSourceRequired
	}
	listText, err := source.ReadText(defaultDungeonCinematicList)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", defaultDungeonCinematicList, err)
	}
	listDocument, err := dnfpvf.Parse(defaultDungeonCinematicList, listText)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", defaultDungeonCinematicList, err)
	}
	entries := dnfpvf.ParseList(listDocument)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", errDungeonTutorialScriptListEmpty, defaultDungeonCinematicList)
	}
	catalog := &pvfDungeonTutorialScriptCatalog{
		byMapID:            make(map[int64]map[int][]dungeonTutorialScriptEvidence),
		basicActionByMapID: make(map[int64]map[int]dungeonTutorialBasicActionEvidence),
	}
	catalog.snapshot.CinematicEntries = len(entries)
	for _, entry := range entries {
		docPath, text, readErr := readDungeonCinematicText(source, entry.Path)
		if readErr != nil {
			catalog.snapshot.ReadFailures++
			continue
		}
		document, parseErr := worldmap.ParseDocument(docPath, text)
		if parseErr != nil {
			catalog.snapshot.ParseFailures++
			continue
		}
		usage, ok := parseDungeonCinematicMonsterUsage(document)
		if !ok {
			continue
		}
		catalog.snapshot.CinematicsWithTargets++
		if catalog.byMapID[usage.MapID] == nil {
			catalog.byMapID[usage.MapID] = make(map[int][]dungeonTutorialScriptEvidence)
		}
		for _, monsterIndex := range usage.DestroyMonsterIndexes {
			candidates := catalog.byMapID[usage.MapID][monsterIndex]
			if len(candidates) == 0 {
				catalog.snapshot.MonsterTargets++
			}
			catalog.byMapID[usage.MapID][monsterIndex] = append(candidates, dungeonTutorialScriptEvidence{
				CinematicID:           entry.ID,
				CinematicPath:         docPath,
				MapID:                 usage.MapID,
				MonsterIndex:          monsterIndex,
				MonsterActorIndexes:   append([]int(nil), usage.MonsterActorIndexes...),
				DestroyMonsterIndexes: append([]int(nil), usage.DestroyMonsterIndexes...),
			})
		}
	}
	catalog.snapshot.MapsWithTargets = len(catalog.byMapID)
	if catalog.snapshot.MonsterTargets == 0 {
		return nil, fmt.Errorf("%w: entries=%d read_failures=%d parse_failures=%d",
			errDungeonTutorialScriptTargetsEmpty,
			catalog.snapshot.CinematicEntries,
			catalog.snapshot.ReadFailures,
			catalog.snapshot.ParseFailures,
		)
	}
	return catalog, nil
}

func (c *pvfDungeonTutorialScriptCatalog) indexBasicActionDestroyTargets(
	source dnfpvf.Source,
	table *worldmap.Table,
) {
	if c == nil || source == nil || table == nil {
		return
	}
	tutorialDungeonIDs := make(map[int64]struct{})
	for _, dungeon := range table.Dungeons() {
		if dungeon.Metadata.TutorialDungeon.Set && dungeon.Metadata.TutorialDungeon.Value == 1 {
			tutorialDungeonIDs[dungeon.ID] = struct{}{}
		}
	}
	for _, mapValue := range table.Maps() {
		if strings.TrimSpace(mapValue.BasicAction) == "" || !mapOwnedByDungeonSet(mapValue, tutorialDungeonIDs) {
			continue
		}
		c.snapshot.BasicActionEntries++
		actionPath := resolveDungeonBasicActionPath(mapValue.Path, mapValue.BasicAction)
		text, err := source.ReadText(actionPath)
		if err != nil {
			c.snapshot.BasicActionReadFailures++
			continue
		}
		document, err := worldmap.ParseDocument(actionPath, text)
		if err != nil {
			c.snapshot.BasicActionParseFailures++
			continue
		}
		usage, ok := parseDungeonBasicActionDestroyUsage(document)
		if !ok {
			continue
		}
		c.snapshot.BasicActionsWithTargets++
		if c.basicActionByMapID[mapValue.ID] == nil {
			c.basicActionByMapID[mapValue.ID] = make(map[int]dungeonTutorialBasicActionEvidence)
		}
		monsterIDs := make(map[int64]struct{}, len(usage.MonsterIDs))
		for _, monsterID := range usage.MonsterIDs {
			monsterIDs[monsterID] = struct{}{}
		}
		for monsterIndex, monster := range mapValue.Monsters {
			if _, matched := monsterIDs[monster.MonsterID]; !matched {
				continue
			}
			if _, exists := c.basicActionByMapID[mapValue.ID][monsterIndex]; exists {
				continue
			}
			c.basicActionByMapID[mapValue.ID][monsterIndex] = dungeonTutorialBasicActionEvidence{
				MapID:           mapValue.ID,
				MapPath:         mapValue.Path,
				ActionPath:      actionPath,
				MonsterIndex:    monsterIndex,
				MonsterID:       monster.MonsterID,
				BehaviorIndexes: append([]int(nil), usage.BehaviorIndexes...),
			}
			c.snapshot.BasicActionTargets++
		}
	}
	c.snapshot.BasicActionMaps = len(c.basicActionByMapID)
}

func (c *pvfDungeonTutorialScriptCatalog) BasicActionMonsterDestroyTargets(
	mapID int64,
) []dungeonTutorialBasicActionEvidence {
	if c == nil || mapID <= 0 {
		return nil
	}
	byIndex := c.basicActionByMapID[mapID]
	if len(byIndex) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(byIndex))
	for monsterIndex := range byIndex {
		indexes = append(indexes, monsterIndex)
	}
	sort.Ints(indexes)
	result := make([]dungeonTutorialBasicActionEvidence, 0, len(indexes))
	for _, monsterIndex := range indexes {
		evidence := byIndex[monsterIndex]
		evidence.BehaviorIndexes = append([]int(nil), evidence.BehaviorIndexes...)
		result = append(result, evidence)
	}
	return result
}

func mapOwnedByDungeonSet(mapValue worldmap.Map, dungeonIDs map[int64]struct{}) bool {
	if mapValue.DungeonID.Set {
		if _, ok := dungeonIDs[mapValue.DungeonID.Value]; ok {
			return true
		}
	}
	for _, dungeonID := range mapValue.DungeonIDs {
		if _, ok := dungeonIDs[dungeonID]; ok {
			return true
		}
	}
	return false
}

func resolveDungeonBasicActionPath(mapPath, actionPath string) string {
	mapPath = strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(mapPath, "\\", "/")), "/")
	actionPath = strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(actionPath, "\\", "/")), "/")
	if actionPath == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(actionPath), "map/") {
		return path.Clean(actionPath)
	}
	mapDirectory := path.Dir(mapPath)
	if mapDirectory == "." || mapDirectory == "" {
		return path.Clean(actionPath)
	}
	if strings.HasPrefix(strings.ToLower(actionPath), strings.ToLower(mapDirectory)+"/") {
		return path.Clean(actionPath)
	}
	return path.Join(mapDirectory, actionPath)
}

func parseDungeonBasicActionDestroyUsage(document *dnfpvf.Document) (dungeonBasicActionDestroyUsage, bool) {
	if document == nil {
		return dungeonBasicActionDestroyUsage{}, false
	}
	type actionTrigger struct {
		monsterIDs   []int64
		behaviorRefs []int
	}
	triggers := make([]actionTrigger, 0)
	destroyBehaviors := make(map[int]struct{})
	inTrigger := false
	inBehavior := false
	behaviorIndex := -1
	monsterSelector := false
	doBehavior := false
	current := actionTrigger{}
	for _, section := range document.Sections {
		key := strings.ToLower(strings.TrimSpace(section.Name))
		switch key {
		case "trigger":
			inTrigger = true
			monsterSelector = false
			doBehavior = false
			current = actionTrigger{}
		case "/trigger":
			if inTrigger {
				triggers = append(triggers, current)
			}
			inTrigger = false
			monsterSelector = false
			doBehavior = false
		case "behavior":
			inBehavior = true
			behaviorIndex++
		case "/behavior":
			inBehavior = false
		case "monster":
			if inTrigger {
				monsterSelector = true
			}
		case "is index":
			if inTrigger && monsterSelector {
				current.monsterIDs = append(current.monsterIDs, dungeonSectionInts(document, section)...)
			}
		case "do behavior":
			if inTrigger {
				doBehavior = true
			}
		case "me", "checkup object":
			if inTrigger && doBehavior {
				for _, value := range dungeonSectionInts(document, section) {
					if value >= 0 {
						current.behaviorRefs = append(current.behaviorRefs, int(value))
					}
				}
			}
		case "destroy":
			if inBehavior && behaviorIndex >= 0 {
				destroyBehaviors[behaviorIndex] = struct{}{}
			}
		}
	}
	monsterIDs := make(map[int64]struct{})
	matchedBehaviors := make(map[int]struct{})
	for _, trigger := range triggers {
		matched := false
		for _, behaviorRef := range trigger.behaviorRefs {
			if _, ok := destroyBehaviors[behaviorRef]; ok {
				matched = true
				matchedBehaviors[behaviorRef] = struct{}{}
			}
		}
		if !matched {
			continue
		}
		for _, monsterID := range trigger.monsterIDs {
			if monsterID > 0 {
				monsterIDs[monsterID] = struct{}{}
			}
		}
	}
	usage := dungeonBasicActionDestroyUsage{
		MonsterIDs:      sortedDungeonInt64Values(monsterIDs),
		BehaviorIndexes: sortedDungeonIntValues(matchedBehaviors),
	}
	return usage, len(usage.MonsterIDs) != 0
}

func dungeonSectionInts(document *dnfpvf.Document, section dnfpvf.Section) []int64 {
	if document == nil || section.Start < 0 || section.End < section.Start || section.End > len(document.Tokens) {
		return nil
	}
	values := make([]int64, 0)
	for _, token := range document.Tokens[section.Start:section.End] {
		if token.Kind == dnfpvf.TokenInt {
			values = append(values, token.Int)
		}
	}
	return values
}

func sortedDungeonIntValues(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func sortedDungeonInt64Values(values map[int64]struct{}) []int64 {
	result := make([]int64, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (c *pvfDungeonTutorialScriptCatalog) FindMonsterDestroy(
	mapID int64,
	monsterIndex int,
) (dungeonTutorialScriptEvidence, bool) {
	if c == nil || mapID <= 0 || monsterIndex < 0 {
		return dungeonTutorialScriptEvidence{}, false
	}
	byIndex := c.byMapID[mapID]
	if byIndex == nil {
		return dungeonTutorialScriptEvidence{}, false
	}
	candidates := byIndex[monsterIndex]
	if len(candidates) == 0 {
		return dungeonTutorialScriptEvidence{}, false
	}
	return cloneDungeonTutorialScriptEvidence(candidates[0]), true
}

// HasMonsterDestroyTargets reports whether any explicit CMT [DESTROY]
// operation owns a static monster in the map. A tutorial final with such a
// target must wait for the client's op117 path instead of being completed by
// the ordinary all-hostiles-dead branch.
func (c *pvfDungeonTutorialScriptCatalog) HasMonsterDestroyTargets(mapID int64) bool {
	if c == nil || mapID <= 0 {
		return false
	}
	return len(c.byMapID[mapID]) != 0
}

// FindMonsterDestroyCovering returns one explicit destroy-target cinematic
// whose own monster-actor set covers every supplied remaining static monster
// index. Candidate actor sets are never merged across cinematics.
func (c *pvfDungeonTutorialScriptCatalog) FindMonsterDestroyCovering(
	mapID int64,
	monsterIndex int,
	remainingMonsterIndexes []int,
) (dungeonTutorialScriptEvidence, bool) {
	if c == nil || mapID <= 0 || monsterIndex < 0 {
		return dungeonTutorialScriptEvidence{}, false
	}
	byIndex := c.byMapID[mapID]
	if byIndex == nil {
		return dungeonTutorialScriptEvidence{}, false
	}
	for _, evidence := range byIndex[monsterIndex] {
		if cinematicMonsterIndexesCover(evidence.MonsterActorIndexes, remainingMonsterIndexes) {
			return cloneDungeonTutorialScriptEvidence(evidence), true
		}
	}
	return dungeonTutorialScriptEvidence{}, false
}

func (c *pvfDungeonTutorialScriptCatalog) Snapshot() dungeonTutorialScriptCatalogSnapshot {
	if c == nil {
		return dungeonTutorialScriptCatalogSnapshot{}
	}
	return c.snapshot
}

func readDungeonCinematicText(source dnfpvf.Source, listedPath string) (string, string, error) {
	clean := strings.TrimSpace(strings.ReplaceAll(listedPath, "\\", "/"))
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" {
		return "", "", dnfpvf.ErrPathRequired
	}
	candidates := []string{clean}
	if !strings.HasPrefix(strings.ToLower(clean), "cinematic/") {
		candidates = append([]string{path.Join("cinematic", clean)}, candidates...)
	}
	var lastErr error
	for _, candidate := range candidates {
		text, err := source.ReadText(candidate)
		if err == nil {
			return candidate, text, nil
		}
		lastErr = err
	}
	return "", "", lastErr
}

func parseDungeonCinematicMonsterUsage(document *dnfpvf.Document) (dungeonCinematicMonsterUsage, bool) {
	if document == nil {
		return dungeonCinematicMonsterUsage{}, false
	}
	var mapID int64
	seenScene := false
	inBehavior := false
	subjectCaptured := false
	subjectMonster := false
	subjectIndex := 0
	subjectIndexSet := false
	inActor := false
	actorIsSubject := false
	actorTypeSeen := false
	actorMonster := false
	actorIndex := 0
	actorIndexSet := false
	monsterActors := make(map[int]struct{})
	destroyTargets := make(map[int]struct{})
	for _, section := range document.Sections {
		key := strings.ToLower(strings.TrimSpace(section.Name))
		if !seenScene && key == "map" {
			if value, ok := cinematicSectionFirstInt(document, section); ok {
				mapID = value
			}
		}
		switch key {
		case "scene":
			seenScene = true
		case "behavior":
			inBehavior = true
			subjectCaptured = false
			subjectMonster = false
			subjectIndex = 0
			subjectIndexSet = false
			inActor = false
			actorIsSubject = false
			actorTypeSeen = false
			actorMonster = false
			actorIndex = 0
			actorIndexSet = false
		case "/behavior":
			inBehavior = false
			inActor = false
			actorIsSubject = false
		case "actor":
			if inBehavior {
				inActor = true
				actorIsSubject = !subjectCaptured
				actorTypeSeen = false
				actorMonster = false
				actorIndex = 0
				actorIndexSet = false
			}
		case "/actor":
			if inBehavior && inActor {
				if actorTypeSeen && actorMonster && actorIndexSet {
					monsterActors[actorIndex] = struct{}{}
				}
				if actorIsSubject {
					subjectCaptured = true
					subjectMonster = actorTypeSeen && actorMonster
					subjectIndex = actorIndex
					subjectIndexSet = actorIndexSet
				}
			}
			inActor = false
			actorIsSubject = false
		case "type":
			if inBehavior && inActor {
				actorTypeSeen = true
			}
		case "monster":
			if inBehavior && inActor && actorTypeSeen {
				actorMonster = true
			}
		case "index":
			if inBehavior && inActor && !actorIndexSet {
				if value, ok := cinematicSectionFirstInt(document, section); ok && value >= 0 {
					actorIndex = int(value)
					actorIndexSet = true
				}
			}
		case "destroy":
			if inBehavior && subjectCaptured && subjectMonster && subjectIndexSet {
				destroyTargets[subjectIndex] = struct{}{}
			}
		}
	}
	usage := dungeonCinematicMonsterUsage{
		MapID:                 mapID,
		MonsterActorIndexes:   sortedDungeonCinematicMonsterIndexes(monsterActors),
		DestroyMonsterIndexes: sortedDungeonCinematicMonsterIndexes(destroyTargets),
	}
	if usage.MapID <= 0 || len(usage.DestroyMonsterIndexes) == 0 {
		return usage, false
	}
	return usage, true
}

func parseDungeonCinematicDestroyTargets(document *dnfpvf.Document) (int64, []int, bool) {
	usage, ok := parseDungeonCinematicMonsterUsage(document)
	return usage.MapID, append([]int(nil), usage.DestroyMonsterIndexes...), ok
}

func sortedDungeonCinematicMonsterIndexes(indexes map[int]struct{}) []int {
	values := make([]int, 0, len(indexes))
	for index := range indexes {
		values = append(values, index)
	}
	sort.Ints(values)
	return values
}

func cinematicMonsterIndexesCover(actorIndexes, remainingMonsterIndexes []int) bool {
	actors := make(map[int]struct{}, len(actorIndexes))
	for _, index := range actorIndexes {
		actors[index] = struct{}{}
	}
	for _, index := range remainingMonsterIndexes {
		if index < 0 {
			return false
		}
		if _, ok := actors[index]; !ok {
			return false
		}
	}
	return true
}

func cloneDungeonTutorialScriptEvidence(in dungeonTutorialScriptEvidence) dungeonTutorialScriptEvidence {
	in.MonsterActorIndexes = append([]int(nil), in.MonsterActorIndexes...)
	in.DestroyMonsterIndexes = append([]int(nil), in.DestroyMonsterIndexes...)
	return in
}

func cinematicSectionFirstInt(document *dnfpvf.Document, section dnfpvf.Section) (int64, bool) {
	if document == nil || section.Start < 0 || section.End < section.Start || section.End > len(document.Tokens) {
		return 0, false
	}
	for _, token := range document.Tokens[section.Start:section.End] {
		if token.Kind == dnfpvf.TokenInt {
			return token.Int, true
		}
	}
	return 0, false
}
