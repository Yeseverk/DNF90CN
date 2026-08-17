package worldmap

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

type SectionValueKind string

const (
	SectionFlag     SectionValueKind = "flag"
	SectionIntegers SectionValueKind = "integers"
	SectionNumbers  SectionValueKind = "numbers"
	SectionTexts    SectionValueKind = "texts"
	SectionMixed    SectionValueKind = "mixed"
)

type ExtensionSection struct {
	Name       string           `json:"name"`
	Occurrence int              `json:"occurrence"`
	Scope      []string         `json:"scope,omitempty"`
	Block      bool             `json:"block,omitempty"`
	Kind       SectionValueKind `json:"kind"`
	Integers   []int64          `json:"integers,omitempty"`
	Numbers    []float64        `json:"numbers,omitempty"`
	Texts      []string         `json:"texts,omitempty"`
	Symbols    []string         `json:"symbols,omitempty"`
	Tokens     []dnfpvf.Token   `json:"tokens,omitempty"`
}

type OptionalNumber struct {
	Value float64 `json:"value"`
	Set   bool    `json:"set"`
}

type DungeonMetadata struct {
	Name                      string              `json:"name,omitempty"`
	Explain                   string              `json:"explain,omitempty"`
	CutsceneImage             Resource            `json:"cutscene_image"`
	MinimapImage              string              `json:"minimap_image,omitempty"`
	EnteringTitle             string              `json:"entering_title,omitempty"`
	MinimumRequiredLevel      OptionalInt         `json:"minimum_required_level"`
	BasisLevel                OptionalInt         `json:"basis_level"`
	ExperienceIncreasingPoint OptionalNumber      `json:"experience_increasing_point"`
	BackgroundPosition        OptionalInt         `json:"background_position"`
	DungeonType               string              `json:"dungeon_type,omitempty"`
	Champion                  []int64             `json:"champion,omitempty"`
	PathgateObjects           []int64             `json:"pathgate_objects,omitempty"`
	WorldMapPatternInfo       []RawSection        `json:"worldmap_pattern_info,omitempty"`
	WorldMapInfo              []RawSection        `json:"worldmap_info,omitempty"`
	Difficulty                OptionalInt         `json:"difficulty"`
	DifficultyLevels          []int64             `json:"difficulty_levels,omitempty"`
	DesignatedDifficulties    []int64             `json:"designated_difficulties,omitempty"`
	TutorialDungeon           OptionalInt         `json:"tutorial_dungeon"`
	NoFatigue                 bool                `json:"no_fatigue,omitempty"`
	NamedMonsters             []int64             `json:"named_monsters,omitempty"`
	RecommendedLevels         []int64             `json:"recommended_levels,omitempty"`
	LimitPartyCount           OptionalInt         `json:"limit_party_count"`
	IntegerValues             map[string]int64    `json:"integer_values,omitempty"`
	NumberValues              map[string]float64  `json:"number_values,omitempty"`
	Flags                     map[string]bool     `json:"flags,omitempty"`
	TextValues                map[string][]string `json:"text_values,omitempty"`
}

type MazePoint struct {
	X      int64   `json:"x"`
	Y      int64   `json:"y"`
	Params []int64 `json:"params,omitempty"`
}

type MapSpecification struct {
	Type              string  `json:"type"`
	Coordinate        Point   `json:"coordinate"`
	MapIDs            []int64 `json:"map_ids"`
	SectionOccurrence int     `json:"section_occurrence"`
}

type RidableObject struct {
	MazeCoordinate  *Point             `json:"maze_coordinate,omitempty"`
	ObjectIndex     OptionalInt        `json:"object_index"`
	Team            string             `json:"team,omitempty"`
	Position        *Point             `json:"position,omitempty"`
	SourceSections  []RawSection       `json:"source_sections,omitempty"`
	UnknownSections []RawSection       `json:"unknown_sections,omitempty"`
	Extensions      []ExtensionSection `json:"extensions,omitempty"`
}

type RandomizedObjectScript struct {
	SelectCount     OptionalInt        `json:"select_count"`
	Regenerate      OptionalInt        `json:"regenerate"`
	MinimapIcon     OptionalInt        `json:"minimap_icon"`
	Objects         []RidableObject    `json:"objects,omitempty"`
	SourceSections  []RawSection       `json:"source_sections,omitempty"`
	UnknownSections []RawSection       `json:"unknown_sections,omitempty"`
	Extensions      []ExtensionSection `json:"extensions,omitempty"`
}

type ClearCondition struct {
	Type     string `json:"type"`
	TargetID int64  `json:"target_id"`
	Count    int64  `json:"count"`
}

type Maze struct {
	Index                    int                      `json:"index"`
	Width                    OptionalInt              `json:"width"`
	Height                   OptionalInt              `json:"height"`
	Greed                    string                   `json:"greed,omitempty"`
	MapSpecifications        []MapSpecification       `json:"map_specifications,omitempty"`
	Start                    *MazePoint               `json:"start,omitempty"`
	Boss                     *MazePoint               `json:"boss,omitempty"`
	HitCount                 []int64                  `json:"hit_count,omitempty"`
	SealDoorAppearRate       OptionalInt              `json:"seal_door_appear_rate"`
	SealDoorMapIndex         OptionalInt              `json:"seal_door_map_index"`
	SealDoorPosition         []int64                  `json:"seal_door_position,omitempty"`
	QuestConnection          []int64                  `json:"quest_connection,omitempty"`
	BossMapSpecifications    []RawSection             `json:"boss_map_specifications,omitempty"`
	LayeredMapSpecifications []RawSection             `json:"layered_map_specifications,omitempty"`
	BossSpecifications       []MapSpecification       `json:"boss_specifications,omitempty"`
	LayeredSpecifications    []MapSpecification       `json:"layered_specifications,omitempty"`
	RandomizedObjects        []RandomizedObjectScript `json:"randomized_objects,omitempty"`
	ClearConditions          []ClearCondition         `json:"clear_conditions,omitempty"`
	SourceSections           []RawSection             `json:"source_sections,omitempty"`
	OpaqueSections           []RawSection             `json:"opaque_sections,omitempty"`
	UnknownSections          []RawSection             `json:"unknown_sections,omitempty"`
	Extensions               []ExtensionSection       `json:"extensions,omitempty"`
	Diagnostics              []Diagnostic             `json:"diagnostics,omitempty"`
}

type Dungeon struct {
	ID              int64              `json:"id"`
	Path            string             `json:"path"`
	Metadata        DungeonMetadata    `json:"metadata"`
	Mazes           []Maze             `json:"mazes,omitempty"`
	SourceSections  []RawSection       `json:"source_sections,omitempty"`
	OpaqueSections  []RawSection       `json:"opaque_sections,omitempty"`
	UnknownSections []RawSection       `json:"unknown_sections,omitempty"`
	Extensions      []ExtensionSection `json:"extensions,omitempty"`
	Diagnostics     []Diagnostic       `json:"diagnostics,omitempty"`
}
