// pvfsurvey 是一次性 PVF 数据勘察程序（不进入生产路径）。
// 加载真实 Script.pvf，回答关于"事件怪物位类遭遇"的五个问题：
//
//	Q1 含 EventMonsterPositions 的 dungeon 地图总量与分布
//	Q2 SpecialPassiveObject 生成 monster 的 (ObjectID, monsterCode) 组合
//	Q3 被动对象定义文件是否声明行为类型/事件脚本
//	Q4 [destroy object] clear 条件与特殊/普通被动对象的关联
//	Q5 电梯型结构（≥1 EventMonsterPositions + special 生成 monster）候选清单
//
// 用法：
//
//	go run ./cmd/scratch/pvfsurvey -phase worldmap   # Q1/Q2/Q4/Q5
//	go run ./cmd/scratch/pvfsurvey -phase objects    # Q3（读取全部被动对象定义）
//	go run ./cmd/scratch/pvfsurvey -phase all        # 全部
//
// 结果写入 cmd/scratch/pvfsurvey/out/。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	defaultPVFPath = `D:/DNF/runtime/data/dnf/Script.pvf`
	outDir         = `D:/DNF/go-server/cmd/scratch/pvfsurvey/out`
)

var lstEntryPattern = regexp.MustCompile("([0-9]+)\\s+`([^`]+)`")

func normSymbol(value string) string {
	return strings.ToLower(strings.Trim(value, "[] \t\r\n"))
}

func normPath(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
}

// ------------------------- 数据结构 -------------------------

type mapUsage struct {
	DungeonID int64  `json:"dungeon_id"`
	MazeIndex int    `json:"maze_index"`
	X         int64  `json:"x,omitempty"`
	Y         int64  `json:"y,omitempty"`
	Source    string `json:"source"`
}

func (u mapUsage) String() string {
	if u.Source == "ownership" {
		return fmt.Sprintf("dungeon=%d(ownership)", u.DungeonID)
	}
	return fmt.Sprintf("dungeon=%d maze=%d (%d,%d) %s", u.DungeonID, u.MazeIndex, u.X, u.Y, u.Source)
}

type q1Map struct {
	MapID     int64    `json:"map_id"`
	Path      string   `json:"path"`
	Name      string   `json:"name,omitempty"`
	Positions int      `json:"positions"`
	Dungeon   []string `json:"dungeons,omitempty"`
}

type comboKey struct {
	ObjectID int64
	Code     int64
}

type comboInfo struct {
	ObjectID int64           `json:"object_id"`
	Code     int64           `json:"monster_code"`
	Count    int             `json:"count"`
	Maps     map[int64]bool  `json:"-"`
	Dungeons map[int64]bool  `json:"-"`
	ObjRef   string          `json:"obj_ref,omitempty"`
	MapList  []int64         `json:"maps"`
	DgList   []int64         `json:"dungeons"`
	Levels   map[int64]bool  `json:"-"`
	LevelVal []int64         `json:"levels,omitempty"`
	RawKinds map[string]bool `json:"-"`
}

type q4Condition struct {
	DungeonID   int64    `json:"dungeon_id"`
	DungeonName string   `json:"dungeon_name,omitempty"`
	MazeIndex   int      `json:"maze_index"`
	TargetID    int64    `json:"target_id"`
	Count       int64    `json:"count"`
	ObjRef      string   `json:"obj_ref,omitempty"`
	Placements  []string `json:"placements"`
	Status      string   `json:"status"`
}

type q5Candidate struct {
	MapID          int64    `json:"map_id"`
	Path           string   `json:"path"`
	Name           string   `json:"name,omitempty"`
	Positions      int      `json:"positions"`
	PassiveIDs     []int64  `json:"passive_object_ids"`
	SpecialObjects []string `json:"special_objects"`
	ObjectClasses  string   `json:"object_classes"`
	Dungeons       []string `json:"dungeons,omitempty"`
	IsDungeon53    bool     `json:"is_known_dungeon_53"`
}

// ------------------------- 主流程 -------------------------

func main() {
	pvfPath := flag.String("pvf", defaultPVFPath, "Script.pvf 路径")
	phase := flag.String("phase", "all", "worldmap|objects|all")
	objLimit := flag.Int("obj-limit", 0, "Q3 最多读取多少个 obj 定义（0=不限）")
	flag.Parse()

	started := time.Now()
	archive, err := platformpvf.Open(*pvfPath)
	if err != nil {
		panic(err)
	}
	snap := archive.Snapshot()
	fmt.Printf("[survey] pvf opened: format=%s files=%d size=%d checksum=%s (%.1fs)\n",
		snap.Format, snap.FileCount, snap.Size, snap.Checksum[:16], time.Since(started).Seconds())

	fmt.Printf("[survey] loading worldmap table ...\n")
	loadStart := time.Now()
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		panic(err)
	}
	tableSnap := table.Snapshot()
	fmt.Printf("[survey] worldmap loaded: %+v (%.1fs)\n", tableSnap, time.Since(loadStart).Seconds())

	// 基础索引
	dungeonNames := map[int64]string{}
	for _, d := range table.Dungeons() {
		dungeonNames[d.ID] = d.Metadata.Name
	}
	mapUsages := buildMapUsages(table)

	// passiveobject.lst 索引（Q2/Q3/Q4/Q5 共用）
	objRefs := loadPassiveObjectList(archive)
	fmt.Printf("[survey] passiveobject.lst entries=%d\n", len(objRefs))

	// 地图实际摆放对象集合与行为画像（Q3/Q5 共用）
	usedOnMaps, usedOnDungeonMaps := collectUsedObjects(table, mapUsages)
	profileStart := time.Now()
	profiles := buildObjProfiles(archive, objRefs, usedOnMaps)
	fmt.Printf("[survey] obj profiles built: used_on_maps=%d used_on_dungeon_maps=%d (%.1fs)\n",
		len(usedOnMaps), len(usedOnDungeonMaps), time.Since(profileStart).Seconds())

	report := map[string]any{
		"pvf": map[string]any{
			"path": *pvfPath, "format": string(snap.Format), "file_count": snap.FileCount,
			"size": snap.Size, "checksum": snap.Checksum,
		},
		"worldmap_snapshot": tableSnap,
		"generated_at":      time.Now().Format(time.RFC3339),
	}

	if *phase == "worldmap" || *phase == "all" {
		report["q1"] = runQ1(table, mapUsages, dungeonNames)
		report["q2"] = runQ2(table, mapUsages, dungeonNames, objRefs)
		report["q4"] = runQ4(table, mapUsages, dungeonNames, objRefs)
		report["q5"] = runQ5(table, mapUsages, dungeonNames, objRefs, profiles)
	}
	if *phase == "objects" || *phase == "all" {
		report["q3"] = runQ3(archive, table, mapUsages, objRefs, profiles, usedOnMaps, usedOnDungeonMaps, *objLimit)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		panic(err)
	}
	reportPath := path.Join(outDir, "survey_report.json")
	if *phase != "all" {
		reportPath = path.Join(outDir, fmt.Sprintf("survey_report_%s.json", *phase))
	}
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("[survey] report written: %s (%.1fs total)\n", reportPath, time.Since(started).Seconds())
}

func collectUsedObjects(table *worldmap.Table, usages map[int64][]mapUsage) (map[int64]bool, map[int64]bool) {
	usedAll := map[int64]bool{}
	usedDungeon := map[int64]bool{}
	for _, m := range table.Maps() {
		placed := make([]int64, 0, len(m.PassiveObjects)+len(m.SpecialPassiveObjects))
		for _, o := range m.PassiveObjects {
			placed = append(placed, o.ObjectID)
		}
		for _, o := range m.SpecialPassiveObjects {
			placed = append(placed, o.ObjectID)
		}
		for _, id := range placed {
			usedAll[id] = true
			if len(usages[m.ID]) > 0 {
				usedDungeon[id] = true
			}
		}
	}
	return usedAll, usedDungeon
}

func buildMapUsages(table *worldmap.Table) map[int64][]mapUsage {
	usages := map[int64][]mapUsage{}
	add := func(mapID, dungeonID int64, mazeIndex int, x, y int64, source string) {
		key := mapUsage{DungeonID: dungeonID, MazeIndex: mazeIndex, X: x, Y: y, Source: source}
		for _, existing := range usages[mapID] {
			if existing == key {
				return
			}
		}
		usages[mapID] = append(usages[mapID], key)
	}
	for _, d := range table.Dungeons() {
		for _, maze := range d.Mazes {
			for _, spec := range maze.MapSpecifications {
				for _, id := range spec.MapIDs {
					add(id, d.ID, maze.Index, spec.Coordinate.X, spec.Coordinate.Y, "spec:"+spec.Type)
				}
			}
			for _, spec := range maze.BossSpecifications {
				for _, id := range spec.MapIDs {
					add(id, d.ID, maze.Index, spec.Coordinate.X, spec.Coordinate.Y, "boss-spec:"+spec.Type)
				}
			}
			for _, spec := range maze.LayeredSpecifications {
				for _, id := range spec.MapIDs {
					add(id, d.ID, maze.Index, spec.Coordinate.X, spec.Coordinate.Y, "layered-spec:"+spec.Type)
				}
			}
		}
	}
	for _, m := range table.Maps() {
		for _, id := range m.DungeonIDs {
			add(m.ID, id, -1, 0, 0, "ownership")
		}
	}
	return usages
}

func loadPassiveObjectList(archive *platformpvf.Archive) map[int64]string {
	text, err := archive.ReadText("passiveobject/passiveobject.lst")
	if err != nil {
		fmt.Printf("[survey] WARN: passiveobject.lst unreadable: %v\n", err)
		return map[int64]string{}
	}
	out := map[int64]string{}
	for _, m := range lstEntryPattern.FindAllStringSubmatch(text, -1) {
		var id int64
		fmt.Sscan(m[1], &id)
		if _, exists := out[id]; !exists {
			out[id] = strings.ReplaceAll(m[2], "\\", "/")
		}
	}
	return out
}

func usageStrings(usages []mapUsage) []string {
	if len(usages) == 0 {
		return nil
	}
	out := make([]string, 0, len(usages))
	for _, u := range usages {
		out = append(out, u.String())
	}
	sort.Strings(out)
	return out
}

func sortedIDs(set map[int64]bool) []int64 {
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
