package worldmap

var dungeonCoreSections = stringSet(
	"name", "explain", "cutscene image", "minimap image", "entering title",
	"minimum required level", "basis level", "experience increasing point", "background pos",
	"dungeon type", "champion", "pathgate object", "worldmap pattern info", "worldmap info",
	"difficulty", "difficulty level", "designate dungeon difficulty", "tutorial dungeon", "no fatigue",
	"named monster", "recommended level", "limit party count", "special passive object item",
)

var dungeonIntegerSections = stringSet(
	"hell dungeon", "escape hell", "coin limit", "hell coin limit", "character coin limit",
	"party member coin limit", "limit inout count", "limit escape character", "gold card use",
	"join cost gold", "gold drop prob", "fatigue", "fatigue result", "prohibit practice",
	"shared difficult dungeon index", "impossible dungeon classification", "ai character appear rate",
	"dummy appear count", "party num check", "quest npc dungeon", "herosmode enable",
	"herosmode required quest", "event dungeon difficulty", "event dungeon cof",
	"adjust mob exp by level", "blood max round", "mob level charac level replace flag",
	"tower of despair", "tower fp cubepiece", "tower limit of stackable item",
	"tower max clear item num", "tower item drop", "tower random map indexes", "warroom map index",
	"max monster", "spawn step max", "battle spawn time", "player kc",
	"tournament round fatigue", "tournament clear reward gold rate", "monster random appear only",
	"remain monster count visible",
)

var dungeonNumberSections = stringSet("kill count const", "move speed")

var dungeonFlagSections = stringSet(
	"defense dungeon", "blood dungeon", "dimension dungeon", "tournament dungeon",
	"powerwar dungeon", "risk dungeon", "ancient dungeon", "event dungeon",
	"crack of dimension dungeon", "disable exit", "individual map movement",
	"open door even enemy", "move map even enemy", "enter without fatigue",
	"use fatigue only start dungeon", "special dungeon", "kronos dungeon", "sao dungeon",
	"no check enter boss room", "no giveup panalty", "boss mark disable", "enable apc stackable",
	"movable even boss die", "ignore clear effect and sound", "dont kill mob when boss die",
	"remove dungeon score and rank", "no revival timer limit", "multi start point",
	"ignore default dungeon clear",
)

var dungeonTextSections = stringSet(
	"revision table", "monsterapc diff table", "entering title next", "monster level rivision",
	"dungeon type for extra drop", "minimap icon",
)

var opaqueDungeonSections = stringSet(
	"special passive object item", "required item", "event required item", "added required item",
	"coin info", "schedule", "event monster", "event monster2", "event monster3",
	"event monster random map", "monster difficulty bonus", "tower dialog", "tower stage",
	"tower recovery", "tower high skill initial cool time", "tower high skill initial cool time rate",
	"death tower map indexes", "result card", "reward item rate", "tournament clear reward exp",
	"clear map", "clear reward item", "boss room entrance condition", "named monster map pos",
	"warp map condition", "dungeon minimap icon setting", "realdungeon checkup",
	"common passive object", "on clear add passive object", "appendage destory object",
	"linked dungeon", "clear action", "point by type", "dungeon exp bonus monster",
	"clear_party_buff_card", "advance altar type", "advance altar map",
	"advance altar clear reward", "advance altar survival map", "advance altar survival clear reward",
	"rank type", "fever time", "lost land parameters", "limit time", "nateram time attack info",
	"village attack revenge dungeon", "time spiral", "spicies dungeon", "summon monster",
	"monster type spawn prob", "monster type spawn cost", "monster type spawn interval rate",
	"monster spawn base interval", "monster spawn random interval", "spawn common monster index",
	"spawn common champion index", "spawn super champion index", "spawn boss index",
	"spawn step resource pool", "common monster item drop prob", "common champion item drop prob",
	"super champion item drop prob", "boss item drop prob", "common monster exp const",
	"common champion exp const", "super champion exp const", "boss exp const",
	"common monster item drop list", "common champion item drop list",
	"super champion item drop list", "boss item drop list", "monster exp bonus per user decrease",
	"result exp bonus per user decrease", "evil", "evil high level", "evil rate", "easy",
	"medium", "hard", "round", "list", "type", "object type", "hide grid",
	"on guide movie dungeon", "recommend party", "recommend equipment", "minimap boss icon",
	"sway effect", "free difficulty", "entry dungeon", "adjust hp gauge", "necessary party",
)

var knownMazeSections = stringSet(
	"maze info", "size", "greed", "map specification", "start map", "boss map", "hit count",
	"seal door appear rate", "seal door map index", "seal door pos", "quest connection",
	"randomized object creation", "clear condition", "boss map specification",
	"layered map specification", "select", "regenerate", "object", "map", "index", "pos",
	"team", "minimap icon", "monster", "neutral", "character", "destroy object", "seeking", "hunt monster",
	"hunt apc", "hunt boss",
)

var opaqueMazeSections = stringSet(
	"randomized object creation", "clear condition", "boss map specification",
	"layered map specification", "select", "regenerate", "object", "map", "index", "pos",
	"team", "minimap icon", "monster", "neutral", "character", "destroy object", "seeking", "hunt monster",
	"hunt apc", "hunt boss",
)

func isDungeonMetadataSection(name string) bool {
	key := sectionKey(name)
	for _, set := range []map[string]struct{}{
		dungeonCoreSections, dungeonIntegerSections, dungeonNumberSections,
		dungeonFlagSections, dungeonTextSections, opaqueDungeonSections,
	} {
		if _, ok := set[key]; ok {
			return true
		}
	}
	return false
}
