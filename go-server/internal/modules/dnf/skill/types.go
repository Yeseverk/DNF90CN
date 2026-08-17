// This file defines the immutable, job-scoped PVF skill catalog.
package skill

import "errors"

const DefaultList = "skill/skilllist.lst"

var (
	ErrIndexRequired = errors.New("dnf skill index is required")
	ErrListEmpty     = errors.New("dnf skill list is empty")
	ErrDocMissing    = errors.New("dnf skill document is missing")
	ErrJobOutOfRange = errors.New("dnf skill job id is out of range")
	ErrIDOutOfRange  = errors.New("dnf skill id is out of range")
	ErrPrerequisite  = errors.New("dnf skill prerequisite is invalid")
)

type Options struct {
	ListPath string
}

// Key identifies a skill in the namespace used by the current EXE and PVF.
// Skill IDs are only unique within one job list.
type Key struct {
	Job byte   `json:"job"`
	ID  uint16 `json:"id"`
}

type Prerequisite struct {
	SkillID uint16 `json:"skill_id"`
	Level   int    `json:"level"`
}

type Skill struct {
	Job                 byte               `json:"job"`
	ID                  uint16             `json:"id"`
	Path                string             `json:"path"`
	Name                string             `json:"name"`
	Kind                string             `json:"kind,omitempty"`
	Active              bool               `json:"active,omitempty"`
	RequiredLevel       int                `json:"required_level,omitempty"`
	MaximumLevel        int                `json:"maximum_level,omitempty"`
	FixedLevelSkill     bool               `json:"fixed_level_skill,omitempty"`
	FixedLevelBase      int                `json:"fixed_level_base,omitempty"`
	FixedLevelInterval  int                `json:"fixed_level_interval,omitempty"`
	FixedLevelIncrement int                `json:"fixed_level_increment,omitempty"`
	SkillClass          int                `json:"skill_class,omitempty"`
	GrowTypes           []int              `json:"grow_types,omitempty"`
	SecondGrowTypes     []int              `json:"second_grow_types,omitempty"`
	FeatureSkillType    int                `json:"feature_skill_type,omitempty"`
	Prerequisites       []Prerequisite     `json:"prerequisites,omitempty"`
	PurchaseCost        []int              `json:"purchase_cost,omitempty"`
	SpecialPurchaseCost []int              `json:"special_purchase_cost,omitempty"`
	CoolTime            float64            `json:"cool_time,omitempty"`
	MPCost              int64              `json:"mp_cost,omitempty"`
	CastTime            float64            `json:"cast_time,omitempty"`
	Command             string             `json:"command,omitempty"`
	Icon                string             `json:"icon,omitempty"`
	Scalars             map[string]float64 `json:"scalars,omitempty"`
}

func (s Skill) Key() Key {
	return Key{Job: s.Job, ID: s.ID}
}

func (s Skill) SupportsGrowType(growType int) bool {
	if len(s.GrowTypes) == 0 {
		return true
	}
	for _, candidate := range s.GrowTypes {
		if candidate == growType {
			return true
		}
	}
	return false
}

// SupportsCharacterGrowth applies both PVF fitness dimensions to the packed
// character grow_type byte. Low four bits select the class branch and high
// four bits select the cumulative awakening stage.
func (s Skill) SupportsCharacterGrowth(growType int) bool {
	first := growType & 0x0f
	awakening := (growType >> 4) & 0x0f
	if !s.SupportsGrowType(first) {
		return false
	}
	if len(s.SecondGrowTypes) == 0 {
		return true
	}
	for _, minimumStage := range s.SecondGrowTypes {
		if minimumStage >= 0 && awakening >= minimumStage {
			return true
		}
	}
	return false
}

func (s Skill) IsTPSkill() bool {
	return s.FeatureSkillType != 0 || len(s.SpecialPurchaseCost) != 0
}

// FixedLevelForCharacter derives the free PVF level of an automatically
// growing skill. These levels are part of the profession baseline and never
// consume SP/TP.
func (s Skill) FixedLevelForCharacter(characterLevel int) int {
	if !s.FixedLevelSkill || characterLevel < s.RequiredLevel {
		return 0
	}
	interval := s.FixedLevelInterval
	if interval <= 0 {
		interval = 1
	}
	increment := s.FixedLevelIncrement
	if increment <= 0 {
		increment = 1
	}
	level := s.FixedLevelBase + (characterLevel-s.RequiredLevel)/interval*increment
	if level < 0 {
		return 0
	}
	if s.MaximumLevel > 0 && level > s.MaximumLevel {
		return s.MaximumLevel
	}
	return level
}

func (s Skill) LevelCost(level int) int {
	costs := s.PurchaseCost
	if s.IsTPSkill() {
		costs = s.SpecialPurchaseCost
	}
	return costAtLevel(costs, level)
}

func costAtLevel(costs []int, level int) int {
	if len(costs) == 0 || level < 0 {
		return 0
	}
	if level >= len(costs) {
		return costs[len(costs)-1]
	}
	return costs[level]
}

type jobPathKey struct {
	Job  byte
	Path string
}

type Table struct {
	skills []Skill
	byKey  map[Key]int
	byPath map[jobPathKey]int
	byJob  map[byte][]int
}

type Snapshot struct {
	Jobs   int `json:"jobs"`
	Skills int `json:"skills"`
}

// Skills returns a deep copy of the complete catalog.
func (t *Table) Skills() []Skill {
	if t == nil || len(t.skills) == 0 {
		return nil
	}
	out := make([]Skill, len(t.skills))
	for idx, definition := range t.skills {
		out[idx] = cloneSkill(definition)
	}
	return out
}

// SkillsForJob returns a deep copy of one job's definitions.
func (t *Table) SkillsForJob(job byte) []Skill {
	if t == nil || len(t.byJob[job]) == 0 {
		return nil
	}
	indexes := t.byJob[job]
	out := make([]Skill, 0, len(indexes))
	for _, idx := range indexes {
		out = append(out, cloneSkill(t.skills[idx]))
	}
	return out
}

// Find resolves a skill by its job-scoped PVF key.
func (t *Table) Find(job byte, id uint16) (Skill, bool) {
	if t == nil {
		return Skill{}, false
	}
	idx, ok := t.byKey[Key{Job: job, ID: id}]
	if !ok {
		return Skill{}, false
	}
	return cloneSkill(t.skills[idx]), true
}

// FindPath resolves a skill path inside one job namespace.
func (t *Table) FindPath(job byte, skillPath string) (Skill, bool) {
	if t == nil {
		return Skill{}, false
	}
	idx, ok := t.byPath[jobPathKey{Job: job, Path: pathKey(skillPath)}]
	if !ok {
		return Skill{}, false
	}
	return cloneSkill(t.skills[idx]), true
}

func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{Jobs: len(t.byJob), Skills: len(t.skills)}
}

func cloneSkill(definition Skill) Skill {
	definition.GrowTypes = append([]int(nil), definition.GrowTypes...)
	definition.SecondGrowTypes = append([]int(nil), definition.SecondGrowTypes...)
	definition.Prerequisites = append([]Prerequisite(nil), definition.Prerequisites...)
	definition.PurchaseCost = append([]int(nil), definition.PurchaseCost...)
	definition.SpecialPurchaseCost = append([]int(nil), definition.SpecialPurchaseCost...)
	if len(definition.Scalars) > 0 {
		scalars := make(map[string]float64, len(definition.Scalars))
		for key, value := range definition.Scalars {
			scalars[key] = value
		}
		definition.Scalars = scalars
	}
	return definition
}
