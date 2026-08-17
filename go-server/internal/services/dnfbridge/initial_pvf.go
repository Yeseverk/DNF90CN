package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
)

const initialSPTablePath = "Etc/spTable.etc"

var initialNumberRE = regexp.MustCompile(`-?\d+`)

type characterPVFInitialization struct {
	Job           byte
	Level         int
	GrowType      byte
	CharacterPath string
	Stats         dnfcharstat.Vector
	HasStats      bool
	Equipment     []initialEquipmentEntry
	Skills        []initialSkillEntry
	SkillPoints   initialSkillPointState
}

type initialSkillEntry struct {
	SkillID int64
	Level   int
}

type initialSkillPointState struct {
	TotalSP     int
	RemainingSP int
	TotalTP     int
	RemainingTP int
	SyncedLevel int
}

func (s *Service) newCharacterPVFInitialization(ctx context.Context, record dnfrepo.CharacterRecord) (characterPVFInitialization, error) {
	job, ok := characterJobByte(record)
	if !ok {
		return characterPVFInitialization{}, nil
	}
	level := record.Level
	if level <= 0 {
		level = 1
	}
	growType := byte(numericCharacterStatValue(record, "grow_type"))
	init := characterPVFInitialization{
		Job:      job,
		Level:    level,
		GrowType: growType,
	}
	var errs []string

	if stats, ok := s.characterPVFStatsForUserInfo(ctx, nil, record, true); ok {
		init.Stats = stats
		init.HasStats = true
	}

	if equipment, err := s.initialCharacterEquipment(ctx, job); err != nil {
		errs = append(errs, "equipment: "+err.Error())
	} else {
		init.Equipment = cloneInitialEquipmentEntries(equipment)
	}

	if skills, err := s.initialCharacterSkills(ctx, job); err != nil {
		errs = append(errs, "skills: "+err.Error())
	} else {
		init.Skills = cloneInitialSkillEntries(skills)
	}

	if points, err := s.initialSkillPoints(ctx, level); err != nil {
		errs = append(errs, "skill_points: "+err.Error())
		init.SkillPoints = initialSkillPointState{SyncedLevel: level}
	} else {
		init.SkillPoints = points
	}
	init.SkillPoints.RemainingSP = init.SkillPoints.TotalSP
	init.SkillPoints.RemainingTP = init.SkillPoints.TotalTP
	if init.SkillPoints.SyncedLevel <= 0 {
		init.SkillPoints.SyncedLevel = level
	}

	if len(errs) > 0 {
		return init, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return init, nil
}

func buildCharacterPVFInitializationFromSource(ctx context.Context, source initialEquipmentTextSource, job byte, level int, growType byte) (characterPVFInitialization, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	init := characterPVFInitialization{Job: job, Level: level, GrowType: growType}
	if level <= 0 {
		init.Level = 1
	}
	listText, err := source.ReadText(initialEquipmentCharacterList)
	if err != nil {
		return init, err
	}
	charPath, ok, err := initialCharacterPVFPath(listText, job)
	if err != nil {
		return init, err
	}
	if ok {
		init.CharacterPath = initialPVFPath("character", charPath)
	}
	statTable, err := dnfcharstat.Load(ctx, source, dnfcharstat.Options{ListPath: initialEquipmentCharacterList})
	if err != nil {
		return init, err
	}
	stats, err := statTable.Compute(job, growType, init.Level)
	if err != nil {
		return init, err
	}
	init.Stats = stats
	init.HasStats = true
	equipment, err := parseInitialCharacterEquipmentFromSource(source, job)
	if err != nil {
		return init, err
	}
	init.Equipment = cloneInitialEquipmentEntries(equipment)
	skills, err := parseInitialCharacterSkillsFromSource(ctx, source, job)
	if err != nil {
		return init, err
	}
	init.Skills = cloneInitialSkillEntries(skills)
	points, err := parseInitialSkillPointsFromSource(source, init.Level)
	if err != nil {
		return init, err
	}
	points.RemainingSP = points.TotalSP
	points.RemainingTP = points.TotalTP
	init.SkillPoints = points
	return init, nil
}

func (s *Service) initialCharacterSkills(ctx context.Context, job byte) ([]initialSkillEntry, error) {
	if s == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.initialSkillsMu.Lock()
	if s.initialSkillsByJob == nil {
		s.initialSkillsByJob = make(map[byte][]initialSkillEntry)
	}
	if cached, ok := s.initialSkillsByJob[job]; ok {
		s.initialSkillsMu.Unlock()
		return cloneInitialSkillEntries(cached), nil
	}
	s.initialSkillsMu.Unlock()

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, err
	}
	catalog, err := s.loadSkillCatalog(ctx, archive)
	if err != nil {
		return nil, err
	}
	entries, err := parseInitialCharacterSkillsWithCatalog(archive, job, catalog)
	if err != nil {
		return nil, err
	}

	s.initialSkillsMu.Lock()
	s.initialSkillsByJob[job] = cloneInitialSkillEntries(entries)
	s.initialSkillsMu.Unlock()
	return cloneInitialSkillEntries(entries), nil
}

func (s *Service) preloadSkillCatalog(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return fmt.Errorf("preload skill catalog pvf: %w", err)
	}
	catalog, err := s.loadSkillCatalog(ctx, archive)
	if err != nil {
		return fmt.Errorf("preload skill catalog: %w", err)
	}
	snapshot := catalog.Snapshot()
	s.logPacketEvent("dnf-skill-catalog-loaded", "jobs", snapshot.Jobs, "skills", snapshot.Skills)
	return nil
}

func (s *Service) loadSkillCatalog(ctx context.Context, source initialEquipmentTextSource) (*dnfskill.Table, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.initialSkillsMu.Lock()
	defer s.initialSkillsMu.Unlock()
	if s.skillCatalog != nil {
		return s.skillCatalog, nil
	}
	catalog, err := buildSkillCatalogFromSource(ctx, source)
	if err != nil {
		return nil, err
	}
	s.skillCatalog = catalog
	return catalog, nil
}

func buildSkillCatalogFromSource(ctx context.Context, source initialEquipmentTextSource) (*dnfskill.Table, error) {
	index, err := dnfpvf.Build(ctx, source, dnfpvf.BuildOptions{Lists: []string{dnfskill.DefaultList}})
	if err != nil {
		return nil, err
	}
	return dnfskill.Load(ctx, index, dnfskill.Options{})
}

func parseInitialCharacterSkillsFromSource(ctx context.Context, source initialEquipmentTextSource, job byte) ([]initialSkillEntry, error) {
	catalog, err := buildSkillCatalogFromSource(ctx, source)
	if err != nil {
		return nil, err
	}
	return parseInitialCharacterSkillsWithCatalog(source, job, catalog)
}

func parseInitialCharacterSkillsWithCatalog(source initialEquipmentTextSource, job byte, catalog *dnfskill.Table) ([]initialSkillEntry, error) {
	if source == nil {
		return nil, fmt.Errorf("initial skill pvf source is nil")
	}
	if catalog == nil {
		return nil, fmt.Errorf("initial skill catalog is nil")
	}
	characterList, err := source.ReadText(initialEquipmentCharacterList)
	if err != nil {
		return nil, err
	}
	characterPath, ok, err := initialCharacterPVFPath(characterList, job)
	if err != nil || !ok {
		return nil, err
	}
	characterText, _, err := readInitialPVFText(source, initialPVFPath("character", characterPath), characterPath)
	if err != nil {
		return nil, err
	}
	pairs, err := parseInitialSkillPairs(characterText)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	entries := make([]initialSkillEntry, 0, len(pairs))
	for _, pair := range pairs {
		if pair.skillID < 0 || pair.skillID > 0xffff {
			return nil, fmt.Errorf("initial skill id out of current EXE range: job=%d skill=%d", job, pair.skillID)
		}
		if _, ok := catalog.Find(job, uint16(pair.skillID)); !ok {
			return nil, fmt.Errorf("initial skill missing from job catalog: job=%d skill=%d", job, pair.skillID)
		}
		entries = append(entries, initialSkillEntry{
			SkillID: pair.skillID,
			Level:   pair.level,
		})
	}
	return entries, nil
}

type initialSkillPair struct {
	skillID int64
	level   int
}

func parseInitialSkillPairs(characterText string) ([]initialSkillPair, error) {
	doc, err := dnfpvf.Parse("character/current.chr", characterText)
	if err != nil {
		return nil, err
	}
	insideInitial := false
	for _, section := range doc.Sections {
		name := strings.ToLower(strings.TrimSpace(section.Name))
		switch name {
		case "initial value":
			insideInitial = true
			continue
		case "/initial value":
			return nil, nil
		case "skill":
			if !insideInitial {
				continue
			}
		}
		if !insideInitial || name != "skill" || section.Start < 0 || section.End > len(doc.Tokens) {
			continue
		}
		values := make([]int64, 0, section.End-section.Start)
		for _, token := range doc.Tokens[section.Start:section.End] {
			if token.Kind == dnfpvf.TokenInt {
				values = append(values, token.Int)
			}
		}
		out := make([]initialSkillPair, 0, len(values)/2)
		for idx := 0; idx+1 < len(values); idx += 2 {
			if values[idx] <= 0 || values[idx+1] <= 0 {
				continue
			}
			out = append(out, initialSkillPair{skillID: values[idx], level: int(values[idx+1])})
		}
		return out, nil
	}
	return nil, nil
}

func (s *Service) initialSkillPoints(ctx context.Context, level int) (initialSkillPointState, error) {
	if s == nil {
		return initialSkillPointState{SyncedLevel: level}, nil
	}
	if level <= 0 {
		level = 1
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return initialSkillPointState{}, err
	}
	s.initialSkillsMu.Lock()
	if s.initialSPTable != nil {
		points := skillPointsFromPVFTables(s.initialSPTable, s.initialTPTable, level)
		s.initialSkillsMu.Unlock()
		return points, nil
	}
	s.initialSkillsMu.Unlock()

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return initialSkillPointState{SyncedLevel: level}, err
	}
	spTable, tpTable, err := parseInitialSkillPointTablesFromSource(archive)
	if err != nil {
		return initialSkillPointState{SyncedLevel: level}, err
	}
	s.initialSkillsMu.Lock()
	s.initialSPTable = cloneSPTable(spTable)
	s.initialTPTable = cloneSPTable(tpTable)
	s.initialSkillsMu.Unlock()
	return skillPointsFromPVFTables(spTable, tpTable, level), nil
}

func parseInitialSkillPointsFromSource(source initialEquipmentTextSource, level int) (initialSkillPointState, error) {
	spTable, tpTable, err := parseInitialSkillPointTablesFromSource(source)
	if err != nil {
		return initialSkillPointState{}, err
	}
	return skillPointsFromPVFTables(spTable, tpTable, level), nil
}

func parseInitialSkillPointTablesFromSource(source initialEquipmentTextSource) (map[int]int, map[int]int, error) {
	text, _, err := readInitialPVFText(source, initialSPTablePath, strings.ToLower(initialSPTablePath), "etc/spTable.etc", "etc/sptable.etc")
	if err != nil {
		return nil, nil, err
	}
	spTable, err := parseInitialSkillPointSection(text, "sp table")
	if err != nil {
		return nil, nil, err
	}
	tpTable, err := parseInitialSkillPointSection(text, "tp table")
	if err != nil {
		return nil, nil, err
	}
	return spTable, tpTable, nil
}

func parseInitialSkillPointSection(text, section string) (map[int]int, error) {
	lower := strings.ToLower(text)
	marker := "[" + strings.ToLower(section) + "]"
	start := strings.Index(lower, marker)
	if start < 0 {
		return nil, fmt.Errorf("%s missing [%s]", initialSPTablePath, section)
	}
	start += len(marker)
	end := len(text)
	if endRel := strings.Index(lower[start:], "[/"+strings.ToLower(section)+"]"); endRel >= 0 {
		end = start + endRel
	}
	nums := initialNumberRE.FindAllString(text[start:end], -1)
	out := make(map[int]int, len(nums)/2)
	for idx := 0; idx+1 < len(nums); idx += 2 {
		level, errLevel := strconv.Atoi(nums[idx])
		sp, errSP := strconv.Atoi(nums[idx+1])
		if errLevel != nil || errSP != nil || level <= 0 || sp < 0 {
			continue
		}
		out[level] = sp
	}
	return out, nil
}

func skillPointsFromPVFTables(spTable, tpTable map[int]int, level int) initialSkillPointState {
	if level <= 0 {
		level = 1
	}
	totalSP := 0
	totalTP := 0
	for current := 1; current <= level; current++ {
		totalSP += spTable[current]
		totalTP += tpTable[current]
	}
	return initialSkillPointState{
		TotalSP:     totalSP,
		RemainingSP: totalSP,
		TotalTP:     totalTP,
		RemainingTP: totalTP,
		SyncedLevel: level,
	}
}

func (s *Service) currentPVFSkillPointTarget(
	ctx context.Context,
	character dnfrepo.CharacterRecord,
) (dnfrepo.SkillPointState, error) {
	points, err := s.initialSkillPoints(ctx, character.Level)
	if err != nil {
		return dnfrepo.SkillPointState{}, err
	}
	bonusSP := numericCharacterStatValue(character, "bonus_sp")
	bonusTP := numericCharacterStatValue(character, "bonus_tp")
	if bonusSP < 0 || bonusTP < 0 || bonusSP > int64(math.MaxInt) || bonusTP > int64(math.MaxInt) ||
		points.TotalSP > math.MaxInt-int(bonusSP) || points.TotalTP > math.MaxInt-int(bonusTP) {
		return dnfrepo.SkillPointState{}, fmt.Errorf(
			"invalid persisted bonus SP/TP for character %s: sp=%d tp=%d",
			character.CharacterID,
			bonusSP,
			bonusTP,
		)
	}
	totalSP := points.TotalSP + int(bonusSP)
	totalTP := points.TotalTP + int(bonusTP)
	return dnfrepo.SkillPointState{
		TotalSP:     totalSP,
		RemainingSP: totalSP,
		TotalTP:     totalTP,
		RemainingTP: totalTP,
		SyncedLevel: points.SyncedLevel,
	}, nil
}

func initialSkillRecord(record dnfrepo.CharacterRecord, init characterPVFInitialization, now time.Time) dnfrepo.SkillRecord {
	skills := make(map[int64]dnfrepo.SkillState, len(init.Skills))
	for _, entry := range init.Skills {
		if entry.SkillID <= 0 || entry.Level <= 0 {
			continue
		}
		skills[entry.SkillID] = dnfrepo.SkillState{
			Level:   entry.Level,
			Enabled: true,
		}
	}
	return dnfrepo.SkillRecord{
		CharacterID: record.CharacterID,
		Skills:      skills,
		Points: dnfrepo.SkillPointState{
			TotalSP:     init.SkillPoints.TotalSP,
			RemainingSP: init.SkillPoints.RemainingSP,
			TotalTP:     init.SkillPoints.TotalTP,
			RemainingTP: init.SkillPoints.RemainingTP,
			SyncedLevel: init.SkillPoints.SyncedLevel,
		},
		Cooldowns: map[int64]time.Time{},
		UpdatedAt: now,
	}
}

func characterJobByte(record dnfrepo.CharacterRecord) (byte, bool) {
	job := numericCharacterStat(record.Job)
	if job < 0 || job > 0xff {
		return 0, false
	}
	return byte(job), true
}

func cloneInitialSkillEntries(entries []initialSkillEntry) []initialSkillEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]initialSkillEntry, len(entries))
	copy(out, entries)
	return out
}

func cloneSPTable(table map[int]int) map[int]int {
	if len(table) == 0 {
		return nil
	}
	out := make(map[int]int, len(table))
	for key, value := range table {
		out[key] = value
	}
	return out
}
