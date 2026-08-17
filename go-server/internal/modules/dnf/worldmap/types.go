package worldmap

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

const (
	DefaultMapList      = "map/map.lst"
	DefaultWorldMapList = "worldmap/worldmap.lst"
	DefaultDungeonList  = "dungeon/dungeon.lst"
)

type OptionalInt struct {
	Value int64 `json:"value"`
	Set   bool  `json:"set"`
}

type RawSection struct {
	Name       string         `json:"name"`
	Occurrence int            `json:"occurrence"`
	Scope      []string       `json:"scope,omitempty"`
	Block      bool           `json:"block,omitempty"`
	Tokens     []dnfpvf.Token `json:"tokens,omitempty"`
}

type Diagnostic struct {
	Section    string `json:"section"`
	Occurrence int    `json:"occurrence"`
	Message    string `json:"message"`
}

type Point struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}

type Point3 struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int64 `json:"z"`
}

type Portal struct {
	Slot     int         `json:"slot"`
	Position *Point      `json:"position,omitempty"`
	ObjectID OptionalInt `json:"object_id"`
}

type MovableArea struct {
	X      int64    `json:"x"`
	Y      int64    `json:"y"`
	Width  int64    `json:"width"`
	Height int64    `json:"height"`
	Params [2]int64 `json:"params"`
}

type TileFile struct {
	Index int64  `json:"index"`
	Path  string `json:"path"`
}

type MapSummon struct {
	Key             OptionalInt        `json:"key"`
	Position        []int64            `json:"position,omitempty"`
	Type            string             `json:"type,omitempty"`
	ItemCount       OptionalInt        `json:"item_count"`
	Index           OptionalInt        `json:"index"`
	LifeTime        OptionalInt        `json:"life_time"`
	Info            []ExtensionSection `json:"info,omitempty"`
	SourceSections  []RawSection       `json:"source_sections,omitempty"`
	UnknownSections []RawSection       `json:"unknown_sections,omitempty"`
	Extensions      []ExtensionSection `json:"extensions,omitempty"`
}

type BackgroundAnimation struct {
	Filename string `json:"filename,omitempty"`
	Layer    string `json:"layer,omitempty"`
	Order    string `json:"order,omitempty"`
}

type AnimationObject struct {
	Filename string `json:"filename"`
	Layer    string `json:"layer"`
	Position Point3 `json:"position"`
}

type PassiveObject struct {
	ObjectID int64 `json:"object_id"`
	X        int64 `json:"x"`
	Y        int64 `json:"y"`
	Flags    int64 `json:"flags"`
}

type SpecialObjectSpawn struct {
	Kind   string   `json:"kind"`
	Code   int64    `json:"code"`
	Level  int64    `json:"level"`
	Params [3]int64 `json:"params"`
}

type HellPartyEntry struct {
	GroupID int64 `json:"group_id"`
	Rate    int64 `json:"rate"`
	Order   int64 `json:"order"`
}

type SpecialPassiveObject struct {
	PassiveObject
	Spawns    []SpecialObjectSpawn `json:"spawns,omitempty"`
	HellParty []HellPartyEntry     `json:"hell_party,omitempty"`
}

type MonsterSpawn struct {
	MonsterID       int64  `json:"monster_id"`
	Level           int64  `json:"level"`
	AutoLevel       int64  `json:"auto_level"`
	Position        Point3 `json:"position"`
	RandomDropCount int64  `json:"random_drop_count"`
	FixedDropCount  int64  `json:"fixed_drop_count"`
	Placement       string `json:"placement"`
	Rank            string `json:"rank"`
	SuffixMarker    string `json:"suffix_marker,omitempty"`
}

type NPCSpawn struct {
	NPCID     int64   `json:"npc_id"`
	Direction string  `json:"direction,omitempty"`
	Position  Point3  `json:"position"`
	Params    []int64 `json:"params,omitempty"`
}

type AICharacter struct {
	Code      int64   `json:"code"`
	Position  Point   `json:"position"`
	Direction int64   `json:"direction"`
	Faction   string  `json:"faction"`
	AIType    string  `json:"ai_type"`
	Params    []int64 `json:"params,omitempty"`
}

type MapFlags struct {
	BlockUseStackableItem         bool `json:"block_use_stackable_item,omitempty"`
	BlockUseActiveSkill           bool `json:"block_use_active_skill,omitempty"`
	VisibleOnDungeonClear         bool `json:"visible_on_dungeon_clear,omitempty"`
	LoopYAxis                     bool `json:"loop_y_axis,omitempty"`
	AllDeadCasePassable           bool `json:"all_dead_case_passable,omitempty"`
	DisableItemEscapeStuck        bool `json:"disable_item_escape_stuck,omitempty"`
	DisableCharacterEscapeStuck   bool `json:"disable_character_escape_stuck,omitempty"`
	CannotUseCoinMap              bool `json:"cannot_use_coin_map,omitempty"`
	NoRevivalTimerLimit           bool `json:"no_revival_timer_limit,omitempty"`
	IgnoreDiehard                 bool `json:"ignore_diehard,omitempty"`
	DisableRebirth                bool `json:"disable_rebirth,omitempty"`
	PreservePlayerCorpse          bool `json:"preserve_player_corpse,omitempty"`
	CannotUseResolutionChangeZoom bool `json:"cannot_use_resolution_change_zoom,omitempty"`
	CenterFixedCamera             bool `json:"center_fixed_camera,omitempty"`
	ForceDrawPattern              bool `json:"force_draw_pattern,omitempty"`
	IsRevival                     bool `json:"is_revival,omitempty"`
	IsMovieEnd                    bool `json:"is_movie_end,omitempty"`
	QuestStartMap                 bool `json:"quest_start_map,omitempty"`
	HideMonster                   bool `json:"hide_monster,omitempty"`
	ShowDust                      bool `json:"show_dust,omitempty"`
}

type Map struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
	Name string `json:"name,omitempty"`

	PlayerNumber      []int64     `json:"player_number,omitempty"`
	PVPStartArea      []int64     `json:"pvp_start_area,omitempty"`
	DungeonID         OptionalInt `json:"dungeon_id"`
	DungeonIDs        []int64     `json:"dungeon_ids,omitempty"`
	Type              string      `json:"type,omitempty"`
	Greed             string      `json:"greed,omitempty"`
	Tiles             []string    `json:"tiles,omitempty"`
	TileFiles         []TileFile  `json:"tile_files,omitempty"`
	TileMapRows       [][]int64   `json:"tile_map_rows,omitempty"`
	FarSightScroll    OptionalInt `json:"far_sight_scroll"`
	MiddleSightScroll OptionalInt `json:"middle_sight_scroll"`
	NearSightScroll   OptionalInt `json:"near_sight_scroll"`

	BackgroundAnimations  []BackgroundAnimation  `json:"background_animations,omitempty"`
	PathgatePositions     []Point                `json:"pathgate_positions,omitempty"`
	PathgateObjects       []int64                `json:"pathgate_objects,omitempty"`
	Portals               []Portal               `json:"portals,omitempty"`
	Sounds                []string               `json:"sounds,omitempty"`
	AnimationObjects      []AnimationObject      `json:"animation_objects,omitempty"`
	PassiveObjects        []PassiveObject        `json:"passive_objects,omitempty"`
	SpecialPassiveObjects []SpecialPassiveObject `json:"special_passive_objects,omitempty"`
	Monsters              []MonsterSpawn         `json:"monsters,omitempty"`
	EventMonsterPositions []Point3               `json:"event_monster_positions,omitempty"`
	NPCs                  []NPCSpawn             `json:"npcs,omitempty"`
	AICharacters          []AICharacter          `json:"ai_characters,omitempty"`
	TownMovableAreas      []MovableArea          `json:"town_movable_areas,omitempty"`
	DungeonMovableAreas   []MovableArea          `json:"dungeon_movable_areas,omitempty"`
	Summons               []MapSummon            `json:"summons,omitempty"`

	FixChampion            OptionalInt `json:"fix_champion"`
	HeroesModeMapIndex     OptionalInt `json:"heroes_mode_map_index"`
	BackgroundCorrection   OptionalInt `json:"background_correction"`
	BackgroundPos          OptionalInt `json:"background_pos"`
	ForegroundPatternAlpha OptionalInt `json:"foreground_pattern_alpha"`
	APCRandomPoint         OptionalInt `json:"apc_random_point"`
	MonsterLock            OptionalInt `json:"monster_lock"`
	DrawMonsterCount       OptionalInt `json:"draw_monster_count"`
	SortBottom             OptionalInt `json:"sort_bottom"`
	AddGravity             OptionalInt `json:"add_gravity"`
	JumpPowerRate          OptionalInt `json:"jump_power_rate"`
	Flags                  MapFlags    `json:"flags"`

	DungeonStartArea       []int64     `json:"dungeon_start_area,omitempty"`
	ScreenPosition         []int64     `json:"screen_position,omitempty"`
	MonsterTeam            []int64     `json:"monster_team,omitempty"`
	PVPPracticeStartArea   []int64     `json:"pvp_practice_start_area,omitempty"`
	VirtualMovableArea     []int64     `json:"virtual_movable_area,omitempty"`
	OpeningBGM             []string    `json:"opening_bgm,omitempty"`
	MapLoadingImagePath    string      `json:"map_loading_image_path,omitempty"`
	BasicAction            string      `json:"basic_action,omitempty"`
	AbsoluteStartPath      []string    `json:"absolute_start_path,omitempty"`
	RandomStartMap         OptionalInt `json:"random_start_map"`
	PathgateRecognizeRange OptionalInt `json:"pathgate_recognize_range"`

	SourceSections  []RawSection       `json:"source_sections,omitempty"`
	OpaqueSections  []RawSection       `json:"opaque_sections,omitempty"`
	UnknownSections []RawSection       `json:"unknown_sections,omitempty"`
	Extensions      []ExtensionSection `json:"extensions,omitempty"`
	Diagnostics     []Diagnostic       `json:"diagnostics,omitempty"`
}

type DungeonEntry struct {
	DungeonID      int64 `json:"dungeon_id"`
	QuestID        int64 `json:"quest_id"`
	InProgressOnly bool  `json:"in_progress_only,omitempty"`
}

type TicketItem struct {
	ItemID int64 `json:"item_id"`
	Count  int64 `json:"count"`
}

type Resource struct {
	Path   string  `json:"path,omitempty"`
	Params []int64 `json:"params,omitempty"`
}

type Area struct {
	ID                int64              `json:"id"`
	Path              string             `json:"path"`
	Name              string             `json:"name,omitempty"`
	MapImage          Resource           `json:"map_image"`
	UIPath            string             `json:"ui_path,omitempty"`
	Dungeons          []DungeonEntry     `json:"dungeons,omitempty"`
	HellDungeon       OptionalInt        `json:"hell_dungeon"`
	HellQuestIDs      []int64            `json:"hell_quest_ids,omitempty"`
	HellFreePassItems []TicketItem       `json:"hell_free_pass_items,omitempty"`
	ItemConditions    []int64            `json:"item_conditions,omitempty"`
	SourceSections    []RawSection       `json:"source_sections,omitempty"`
	OpaqueSections    []RawSection       `json:"opaque_sections,omitempty"`
	UnknownSections   []RawSection       `json:"unknown_sections,omitempty"`
	Extensions        []ExtensionSection `json:"extensions,omitempty"`
	Diagnostics       []Diagnostic       `json:"diagnostics,omitempty"`
}

type Options struct {
	MapListPath      string
	WorldMapListPath string
	DungeonListPath  string
	SkipMaps         bool
	SkipAreas        bool
	SkipDungeons     bool
}

type Snapshot struct {
	Maps        int `json:"maps"`
	Areas       int `json:"areas"`
	Dungeons    int `json:"dungeons"`
	Mazes       int `json:"mazes"`
	DungeonRefs int `json:"dungeon_refs"`
}
