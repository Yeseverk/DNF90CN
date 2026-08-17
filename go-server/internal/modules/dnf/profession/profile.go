package profession

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
	"longheng.io/server/internal/modules/dnf/skillcmd"
)

const DefaultCharacterList = "character/character.lst"

var (
	ErrSourceRequired = errors.New("dnf profession PVF source is required")
	ErrProfileMissing = errors.New("dnf profession profile is missing")
	ErrSkillMissing   = errors.New("dnf profession grant skill is missing")
	ErrSkillGrant     = errors.New("dnf profession skill grant is malformed")
)

var (
	growTypeSectionRE  = regexp.MustCompile(`(?i)\[\s*growtype\s+(\d+)\s*\]`)
	awakeningSectionRE = regexp.MustCompile(`(?i)\[\s*awakening\s+(\d+)\s*\]`)
	integerRE          = regexp.MustCompile(`-?\d+`)
)

type Source interface {
	ReadText(relativePath string) (string, error)
}

type Grant struct {
	SkillID uint16 `json:"skill_id"`
	Level   int    `json:"level"`
}

type jobProfile struct {
	initial            []Grant
	grow               map[byte][]Grant
	growSupported      map[byte]bool
	awakening          map[byte]map[byte][]Grant
	awakeningSupported map[byte]map[byte]bool
	displayNames       map[byte]string
	awakeningNames     map[byte]map[byte]string
}

type Profiles struct {
	jobs               map[byte]jobProfile
	missingSkillGrants int
}

type Snapshot struct {
	Jobs            int
	InitialGrants   int
	ClassGrants     int
	AwakeningGrants int
	MissingSkills   int
}

func LoadProfiles(ctx context.Context, source Source, catalog *dnfskill.Table) (*Profiles, error) {
	if source == nil || catalog == nil {
		return nil, ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	listText, err := source.ReadText(DefaultCharacterList)
	if err != nil {
		return nil, err
	}
	doc, err := dnfpvf.Parse(DefaultCharacterList, listText)
	if err != nil {
		return nil, err
	}
	entries := dnfpvf.ParseList(doc)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrProfileMissing, DefaultCharacterList)
	}
	profiles := &Profiles{jobs: make(map[byte]jobProfile, len(entries))}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.ID < 0 || entry.ID > math.MaxUint8 || strings.TrimSpace(entry.Path) == "" {
			continue
		}
		job := byte(entry.ID)
		charPath := path.Clean(path.Join(path.Dir(DefaultCharacterList), strings.ReplaceAll(entry.Path, "\\", "/")))
		text, err := source.ReadText(charPath)
		if err != nil {
			return nil, fmt.Errorf("read profession profile job=%d path=%s: %w", job, charPath, err)
		}
		profile, err := parseJobProfile(text)
		if err != nil {
			return nil, fmt.Errorf("parse profession profile job=%d path=%s: %w", job, charPath, err)
		}
		profiles.missingSkillGrants += countMissingProfileSkills(catalog, job, profile)
		profiles.jobs[job] = profile
	}
	if len(profiles.jobs) == 0 {
		return nil, ErrProfileMissing
	}
	return profiles, nil
}

func (p *Profiles) Snapshot() Snapshot {
	if p == nil {
		return Snapshot{}
	}
	out := Snapshot{Jobs: len(p.jobs), MissingSkills: p.missingSkillGrants}
	for _, profile := range p.jobs {
		out.InitialGrants += len(profile.initial)
		for _, grants := range profile.grow {
			out.ClassGrants += len(grants)
		}
		for _, stages := range profile.awakening {
			for _, grants := range stages {
				out.AwakeningGrants += len(grants)
			}
		}
	}
	return out
}

func (p *Profiles) Initial(job byte) ([]Grant, bool) {
	profile, ok := p.profile(job)
	if !ok {
		return nil, false
	}
	return cloneGrants(profile.initial), true
}

// DisplayName returns the profession name declared by the current Script.pvf.
// The low grow-type nibble selects the class branch and the high nibble
// selects its awakening stage. No client-side profession table is guessed.
func (p *Profiles) DisplayName(job byte, growType byte) (string, bool) {
	profile, ok := p.profile(job)
	if !ok {
		return "", false
	}
	first, awakening := Decode(growType)
	if awakening > 0 {
		if name := strings.TrimSpace(profile.awakeningNames[byte(first)][byte(awakening)]); name != "" {
			return name, true
		}
	}
	name := strings.TrimSpace(profile.displayNames[byte(first)])
	return name, name != ""
}

// FreeGrants returns the PVF free baseline for the selected class branch and
// cumulatively includes awakening stages one through awakeningStage.
func (p *Profiles) FreeGrants(job byte, firstGrowType byte, awakeningStage byte) ([]Grant, bool) {
	profile, ok := p.profile(job)
	if !ok {
		return nil, false
	}
	merged := make(map[uint16]int)
	mergeGrantLevels(merged, profile.initial)
	mergeGrantLevels(merged, profile.grow[firstGrowType])
	for stage := byte(1); stage <= awakeningStage; stage++ {
		mergeGrantLevels(merged, profile.awakening[firstGrowType][stage])
	}
	return sortedGrants(merged), true
}

func (p *Profiles) profile(job byte) (jobProfile, bool) {
	if p == nil {
		return jobProfile{}, false
	}
	profile, ok := p.jobs[job]
	return profile, ok
}

// ApplySkillTransition mutates only the detached SkillRecord. The caller owns
// the surrounding repository transaction and saves the returned fields.
func (p *Profiles) ApplySkillTransition(catalog *dnfskill.Table, job byte, characterLevel int, transition Transition, record dnfrepo.SkillRecord, points dnfrepo.SkillPointState) (dnfrepo.SkillRecord, error) {
	if p == nil || catalog == nil {
		return dnfrepo.SkillRecord{}, ErrSourceRequired
	}
	grants, ok := p.FreeGrants(job, transition.FirstGrowType, transition.AwakeningStage)
	if !ok {
		return dnfrepo.SkillRecord{}, fmt.Errorf("%w: job=%d", ErrProfileMissing, job)
	}
	grants = grantsAtCharacterLevel(catalog, job, characterLevel, grants)
	record = dnfrepo.CloneSkill(record)
	switch transition.Kind {
	case KindClassChange:
		record.Skills = grantStates(grants)
		record.Cooldowns = nil
		layout, err := skillcmd.BuildInitialSkillLayout(catalog, job, 0, record.Skills)
		if err != nil {
			return dnfrepo.SkillRecord{}, err
		}
		record.Layouts = map[int]dnfrepo.SkillLayout{0: layout}
		points.RemainingSP = points.TotalSP
		points.RemainingTP = points.TotalTP
		record.Points = points
	case KindAwakening:
		if record.Skills == nil {
			record.Skills = make(map[int64]dnfrepo.SkillState)
		}
		for _, grant := range grants {
			current := record.Skills[int64(grant.SkillID)]
			if current.Level < grant.Level {
				current.Level = grant.Level
			}
			current.Enabled = true
			record.Skills[int64(grant.SkillID)] = current
		}
		var existing dnfrepo.SkillLayout
		if record.Layouts != nil {
			existing = record.Layouts[0]
		}
		layout, err := skillcmd.BuildCurrentSkillLayout(catalog, job, 0, record.Skills, existing)
		if err != nil {
			return dnfrepo.SkillRecord{}, err
		}
		if record.Layouts == nil {
			record.Layouts = make(map[int]dnfrepo.SkillLayout)
		}
		record.Layouts[0] = layout
		record.Points = points
	default:
		return dnfrepo.SkillRecord{}, fmt.Errorf("%w: kind=%d", ErrTransitionInvalid, transition.Kind)
	}
	return record, nil
}

func grantsAtCharacterLevel(catalog *dnfskill.Table, job byte, characterLevel int, grants []Grant) []Grant {
	out := cloneGrants(grants)
	for index := range out {
		definition, ok := catalog.Find(job, out[index].SkillID)
		if !ok {
			continue
		}
		if fixedLevel := definition.FixedLevelForCharacter(characterLevel); fixedLevel > out[index].Level {
			out[index].Level = fixedLevel
		}
	}
	return out
}

// PlanTransition applies one detached quest reward to the transaction's
// current packed grow_type. Section presence in character/*.chr is the source
// of truth, including branch-zero awakenings whose free-grant lists are empty.
func (p *Profiles) PlanTransition(job byte, current byte, request Request) (Transition, error) {
	profile, ok := p.profile(job)
	if !ok {
		return Transition{}, fmt.Errorf("%w: job=%d", ErrProfileMissing, job)
	}
	first, awakening := Decode(current)
	switch request.Kind {
	case KindClassChange:
		if request.ChainType != 1 || request.GrowNumber == 0 || first != 0 || awakening != 0 {
			return Transition{}, fmt.Errorf("%w: class change current=0x%02x target=%d", ErrTransitionOutOfStep, current, request.GrowNumber)
		}
		if !profile.growSupported[request.GrowNumber] {
			return Transition{}, fmt.Errorf("%w: job=%d class=%d", ErrTransitionInvalid, job, request.GrowNumber)
		}
		return Transition{
			Kind: KindClassChange, ChainType: request.ChainType, GrowNumber: request.GrowNumber,
			PreviousGrowType: current, NewGrowType: request.GrowNumber,
			FirstGrowType: request.GrowNumber, AwakeningStage: 0,
		}, nil
	case KindAwakening:
		target := request.GrowNumber
		if request.ChainType != 2 || target == 0 || target > 2 || target != awakening+1 {
			return Transition{}, fmt.Errorf("%w: awakening current=0x%02x target=%d", ErrTransitionOutOfStep, current, target)
		}
		if !profile.awakeningSupported[first][target] {
			return Transition{}, fmt.Errorf("%w: job=%d first=%d awakening=%d", ErrTransitionInvalid, job, first, target)
		}
		packed, err := Encode(first, target)
		if err != nil {
			return Transition{}, err
		}
		return Transition{
			Kind: KindAwakening, ChainType: request.ChainType, GrowNumber: target,
			PreviousGrowType: current, NewGrowType: packed,
			FirstGrowType: first, AwakeningStage: target,
		}, nil
	default:
		return Transition{}, fmt.Errorf("%w: kind=%d", ErrTransitionInvalid, request.Kind)
	}
}

func parseJobProfile(text string) (jobProfile, error) {
	profile := jobProfile{
		grow:               make(map[byte][]Grant),
		growSupported:      make(map[byte]bool),
		awakening:          make(map[byte]map[byte][]Grant),
		awakeningSupported: make(map[byte]map[byte]bool),
		displayNames:       make(map[byte]string),
		awakeningNames:     make(map[byte]map[byte]string),
	}
	doc, err := dnfpvf.Parse("character/profile.chr", text)
	if err != nil {
		return jobProfile{}, err
	}
	for index, name := range doc.Texts("growtype name") {
		if index > math.MaxUint8 {
			break
		}
		if name = strings.TrimSpace(name); name != "" && !strings.HasPrefix(name, "//") {
			profile.displayNames[byte(index)] = name
		}
	}
	growMatches := growTypeSectionRE.FindAllStringSubmatchIndex(text, -1)
	initialEnd := len(text)
	if len(growMatches) > 0 {
		initialEnd = growMatches[0][0]
	}
	profile.initial = parseGrantSection(text[:initialEnd], "skill")
	for index, match := range growMatches {
		n, err := strconv.Atoi(text[match[2]:match[3]])
		if err != nil || n < 1 || n > 16 {
			continue
		}
		firstGrowType := byte(n - 1)
		profile.growSupported[firstGrowType] = true
		segmentEnd := len(text)
		if index+1 < len(growMatches) {
			segmentEnd = growMatches[index+1][0]
		}
		segment := text[match[1]:segmentEnd]
		segmentDoc, parseErr := dnfpvf.Parse("character/profile-grow.chr", segment)
		if parseErr != nil {
			return jobProfile{}, parseErr
		}
		for awakeningIndex, name := range segmentDoc.Texts("awakening name") {
			stage := awakeningIndex + 1
			if stage > math.MaxUint8 {
				break
			}
			name = strings.TrimSpace(name)
			if name == "" || strings.HasPrefix(name, "//") {
				continue
			}
			if profile.awakeningNames[firstGrowType] == nil {
				profile.awakeningNames[firstGrowType] = make(map[byte]string)
			}
			profile.awakeningNames[firstGrowType][byte(stage)] = name
		}
		awakeningMatches := awakeningSectionRE.FindAllStringSubmatchIndex(segment, -1)
		growEnd := len(segment)
		if len(awakeningMatches) > 0 {
			growEnd = awakeningMatches[0][0]
		}
		if grants := parseGrantSection(segment[:growEnd], "skill"); n >= 2 && len(grants) > 0 {
			profile.grow[firstGrowType] = grants
		}
		for stageIndex, awakeningMatch := range awakeningMatches {
			stageValue, err := strconv.Atoi(segment[awakeningMatch[2]:awakeningMatch[3]])
			if err != nil || stageValue < 1 || stageValue > 15 {
				continue
			}
			if profile.awakeningSupported[firstGrowType] == nil {
				profile.awakeningSupported[firstGrowType] = make(map[byte]bool)
			}
			profile.awakeningSupported[firstGrowType][byte(stageValue)] = true
			awakeningEnd := len(segment)
			if stageIndex+1 < len(awakeningMatches) {
				awakeningEnd = awakeningMatches[stageIndex+1][0]
			}
			grants := parseGrantSection(segment[awakeningMatch[1]:awakeningEnd], "awakening skill")
			if len(grants) == 0 {
				continue
			}
			if profile.awakening[firstGrowType] == nil {
				profile.awakening[firstGrowType] = make(map[byte][]Grant)
			}
			profile.awakening[firstGrowType][byte(stageValue)] = grants
		}
	}
	if len(profile.initial) == 0 {
		return jobProfile{}, fmt.Errorf("%w: missing [initial value] [skill] grants", ErrSkillGrant)
	}
	return profile, nil
}

func parseGrantSection(text string, name string) []Grant {
	open := regexp.MustCompile(`(?i)\[\s*` + regexp.QuoteMeta(name) + `\s*\]`).FindStringIndex(text)
	if open == nil {
		return nil
	}
	close := regexp.MustCompile(`(?i)\[\s*/\s*` + regexp.QuoteMeta(name) + `\s*\]`).FindStringIndex(text[open[1]:])
	bodyEnd := len(text)
	if close != nil {
		bodyEnd = open[1] + close[0]
	}
	values := integerRE.FindAllString(text[open[1]:bodyEnd], -1)
	levels := make(map[uint16]int)
	for index := 0; index+1 < len(values); index += 2 {
		skillID, skillErr := strconv.ParseInt(values[index], 10, 64)
		level, levelErr := strconv.Atoi(values[index+1])
		if skillErr != nil || levelErr != nil || skillID <= 0 || skillID > math.MaxUint16 || level <= 0 {
			continue
		}
		id := uint16(skillID)
		if level > levels[id] {
			levels[id] = level
		}
	}
	return sortedGrants(levels)
}

func countMissingProfileSkills(catalog *dnfskill.Table, job byte, profile jobProfile) int {
	missing := make(map[uint16]struct{})
	check := func(grants []Grant) {
		for _, grant := range grants {
			if _, ok := catalog.Find(job, grant.SkillID); !ok {
				missing[grant.SkillID] = struct{}{}
			}
		}
	}
	check(profile.initial)
	for _, grants := range profile.grow {
		check(grants)
	}
	for _, stages := range profile.awakening {
		for _, grants := range stages {
			check(grants)
		}
	}
	return len(missing)
}

func grantStates(grants []Grant) map[int64]dnfrepo.SkillState {
	states := make(map[int64]dnfrepo.SkillState, len(grants))
	for _, grant := range grants {
		states[int64(grant.SkillID)] = dnfrepo.SkillState{Level: grant.Level, Enabled: true}
	}
	return states
}

func mergeGrantLevels(target map[uint16]int, grants []Grant) {
	for _, grant := range grants {
		if grant.Level > target[grant.SkillID] {
			target[grant.SkillID] = grant.Level
		}
	}
}

func sortedGrants(levels map[uint16]int) []Grant {
	ids := make([]int, 0, len(levels))
	for id := range levels {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	out := make([]Grant, 0, len(ids))
	for _, id := range ids {
		out = append(out, Grant{SkillID: uint16(id), Level: levels[uint16(id)]})
	}
	return out
}

func cloneGrants(grants []Grant) []Grant {
	return append([]Grant(nil), grants...)
}
