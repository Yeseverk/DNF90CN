# PVF 事件怪物位类遭遇勘察报告（一次性）

- 勘察对象：`D:/DNF/runtime/data/dnf/Script.pvf`（111,092,341 字节，protected_nkpi，1,057,625 文件，SHA256 前缀 `1f82563b9a84d1cb`，只读未修改）
- 勘察程序：`go-server/cmd/scratch/pvfsurvey/`（`main.go`+`q1..q5.go`+`objprofile.go`，构建产物 `out/pvfsurvey.exe`；`explore*.go` 为过程探查脚本，均带 `//go:build ignore`，不进生产路径）
- 加载方式：与生产一致 `platformpvf.Open` → `worldmap.LoadSource`（参照 `dungeon_suspicious_village_elevator_test.go`）；obj/lst/act 文本用同一归档的 `ReadText` + `worldmap.ParseDocument` 直接解析
- 世界表规模：maps=18,374 areas=80 dungeons=1,450 mazes=2,654
- 结果文件：`out/survey_report.json`（全量结构化）、`out/q1_event_monster_maps.tsv`、`out/q2_special_spawn_combos.tsv`、`out/q3_obj_section_census.tsv`、`out/q3_obj_signatures.tsv`、`out/q3_missing_obj_refs.tsv`、`out/q4_destroy_object_conditions.tsv`、`out/q5_elevator_candidates.tsv`
- 复现：`cd go-server && ./cmd/scratch/pvfsurvey/out/pvfsurvey.exe -phase all`（全程约 8 秒）

---

## Q1 含 EventMonsterPositions 的地图总量

- **17,207 张地图**含 `[event monster position]`（占全部 18,374 张的 93.6%）；其中 **17,182 张关联到 dungeon**（经 maze specification/boss/layered 引用或 map `[dungeon]` ownership），涉及 **1,108 个 dungeon**（全部 1,450 个的 76%）。
- 位置数分布直方图：

  | 位置数 | 地图数 |
  |---|---|
  | 3 | 9 |
  | **4** | **17,052** |
  | 5 | 85 |
  | 6 | 17 |
  | 7 | 5 |
  | 8 | 39 |

- 结论：**4 个事件位是全 PVF 的普适默认形态，完全不具备区分度**。疑惑之村电梯的"4 positions"不是特征，组合结构才是。
- 25 张无 dungeon 归属的地图均为 hell 图（`hell_*.map`）、quest 图（`q*.map`）与城镇/战场起始图，也全部是 4 位。
- 完整清单：`out/q1_event_monster_maps.tsv`（17,207 行，含 dungeon 归属）。

## Q2 special passive object 生成 monster 的组合

- 全 PVF 共 **15,515 个 special passive object 摆放**，14,115 个带 Spawns；spawn Kind 归一化分布：`item`=11,959、`trap`=2,144、**`monster`=1,332**、`hellparty`=79、`quest`=6。
- Kind=monster 的 **(ObjectID, monsterCode) 组合共 110 种**，来自 **41 个不同 ObjectID** × **58 个不同 monster code**。完整清单 `out/q2_special_spawn_combos.tsv`（110 行）。
- 出现次数 Top：木桶 `221→4`(158 次/67 图)、`221→3`(117 次)、mimicbox `10231→61479`(101 次/24 图/仅 dungeon 53)、浅栖棺材 `815→403`(66 次)、哥布林碉堡 `1015→4`(66 次)。
- **电梯组合 `1112→56716` 出现 3 次，地图 [16408, 76384, 400263]，全部 dungeon 53** —— 全 PVF 无第四处。

## Q3 被动对象定义文件的行为线索

`passiveobject/passiveobject.lst` 共 **25,975 条**（单行 ``id `path` `` 序列）；25,479 个 .obj 读取成功、**496 个引用在归档内无文件**（全部为 10900xxxxx 段 ActionObject 条目，见 `out/q3_missing_obj_refs.tsv`，属 PVF 自身数据缺口）、0 个解析失败。

1. **行为类型声明确实存在，但只覆盖少数对象**：
   - `[passive object type]`：294 个文件、**25 种值**（`[cinematic object]`×134、`[nenguard]`×37、`[missile fairy]`×17、`[warp object]`×16、各类 fairy 等）。
   - `[passive object sub type]`：144 个文件、**44 种值**——这是"房间事件导演"类：`[object pattern]`×67、`[frost cave qna room]`×15、以及大量 `... start room`/`... boss room`（`[sea chase start room]`、`[time crack boss room]`、`[vilmark second room]` 等）。
   - `[object destroy condition]`：5,362 个文件，种类收敛：`[destroy condition]` 5,146（主流值 `[on end of animation]` 2,441、`[time limite] N` 约 2,500、`[parent dead]` 37 等）、`[destroy action]` 109、`[on end of animation]` 25、`[on attack]` 3、另两种各 1。
   - 其余高频行为字段：`[hp destroy]` 4,555、`[hp max]` 4,204、`[team]` 5,999、`[attack info]` 10,138、`[pass type]` 17,574。
2. **真正的脚本在 .act**：19,173 个 .obj 带 `[basic action]`；.act 内含 `[TRIGGER]`/`[DO BEHAVIOR]`/`[BEHAVIOR]`/`[DESTROY]` 块（样本 `questdummy/Action/dummy2.act`：按 cinematic id 触发 destroy 行为）。即动作对象是**数据驱动**的。
3. 地图实际摆放的 5,492 个对象（5,483 个在 dungeon 图）按行为分类：**scripted_action（含 .act）4,064**、declared_behavior（有行为字段无 .act）1,078、**presentation_only 仅 125**、not_in_lst 214、missing 11。行为签名共 386 种（`out/q3_obj_signatures.tsv`）。
4. **电梯三件套（1111 ElevatorScroll / 1112 ElevatorSummon / 1113 ElevatorControl）全部是 presentation_only**：只有 `[layer]`+`[basic motion]`+`[int data]`+`[name]`（1113 多 `[etc motion]`），无 type/sub type/destroy condition/basic action。**电梯行为不是数据驱动，是 EXE 硬编码**，PVF 侧只能靠"对象身份 + 地图结构"识别。

## Q4 [destroy object] clear 条件关联

- 全 PVF 共 **37 个 `[destroy object]` clear 条件**，分布在 **26 个 dungeon / 37 个 maze**，**count 全部 = 1**。
- 关联结果：**36/37 count_match**（该 dungeon 至少一张地图恰好摆放 1 个目标对象，与现行空房结算修复形状一致）；唯一例外 **dungeon 1002（永恒梦境）maze 0 目标 16072（`ActionObject/Monster/3_Time.obj`）在任何关联地图上都找不到摆放**。
- 目标 ID 与 op38 u16 分界：**11 个条件 target > 65535**（69291/69292/80454/80456/80458/80459/85194/64060/64062/53764…，即现行修复覆盖的形状），**26 个 ≤ 65535**（可进普通 actor 表）。目标 ID 全集 13 个值。
- 完整清单：`out/q4_destroy_object_conditions.tsv`。

## Q5 电梯同款候选

- 规则"≥1 EventMonsterPositions 且 special 生成 monster"共命中 **413 张地图**、35 个 dungeon。但按"special 对象 ID 集合"聚簇只有 **28 个类簇**：

  | 类簇（special 对象） | 地图数 | dungeon | 性质 |
  |---|---|---|---|
  | [221] 木桶 | 182 | 2-9 | 破坏出怪（scripted） |
  | [815] 浅栖棺材 | 49 | 31/32/35/3011/3012 | 破坏出怪 |
  | [1015] 哥布林碉堡 | 34 | 3/4/7/9 | 陷阱出怪 |
  | [10231] mimicbox | 24 | **53** | 破坏出怪（宝箱怪） |
  | [109006930] SC_Coffin | 23 | 32 | 破坏出怪 |
  | [221,1015] | 17 | 3/4/7/9 | 混合 |
  | 其余 21 簇（773/775/8938+8939/779/…） | 81 | 各 1 簇 1 副本 | 副本专属机关 |
  | **[1112] ElevatorSummon** | **3** | **53** | **电梯（presentation-only，EXE 硬编码）** |

- **电梯型结构全 PVF 恰好 3 张，无第四处**：
  1. map **16408** `map/SuspiciousVillage/(2,5)start.map` — dungeon 53 maze 0 (2,5)，4 位 + 1111/1113 + 1112→56716（纯 presentation_only）
  2. map **400263** `map/SuspiciousVillage/M3786_(2,5)start.map` — dungeon 53 maze 1 (2,5)，同上 + 多一个 scripted 对象 53858
  3. map **76384** `map/Cataclysm/Northmyre/05_Town_of_doubt/3352_76384.map` — dungeon 53 maze 2 **(1,2)（layered spec）**，同上 + 多一个 scripted 对象 15336
-  dungeon 53（疑惑之村）共 27 个候选：24 张是 mimicbox 宝箱怪图，3 张才是电梯。**maze 2 的 76384 不在已合并修复包的真实 PVF 测试覆盖（只测 16408/400263）内**，但现行识别纯按 PVF 语义（4 位 + 1111/1113 + 1112→56716 + 房内 special-monster actor），理论上自动覆盖；建议补一条 maze 2 的断言。
- 完整清单：`out/q5_elevator_candidates.tsv`（413 行，含每图对象行为分类）。

---

## 数据侧结论：能否一次性通用绑定？

1. **可以一次性枚举完，不存在"发现一个修一个"的长尾**：事件怪物位是普适结构（17,207 图/1,108 副本），但"位 + special 生成 monster"只收敛到 **413 图 / 28 个对象类簇**；真正的电梯机制收敛到 **3 图 / 1 个对象组合（1112→56716）**，全部位于 dungeon 53 三个 maze。全 PVF 没有第二部电梯机制。
2. **行为类簇极少且边界清晰**：
   - **电梯类**（presentation-only 三件套 + 4 位 + summon→monster）：3 图，EXE 硬编码行为，只能靠 PVF 语义识别 —— 现行方案方向正确，且已实质覆盖全部实例（含未测的 maze 2）。
   - **破坏/陷阱出怪类**（木桶/棺材/碉堡/mimicbox 等 27 簇）：对象带 `.act` 脚本或行为字段，是数据驱动的普通机制，**绝不能与电梯共用绑定**——若按"4 位 + special 生成 monster"裸绑定电梯逻辑会把 410 张普通图误判。
   - **[destroy object] clear 类**：37 条件全部 count=1、36/37 精确匹配，形状高度统一，可以一次性通用绑定；注意 26/37 目标 ≤65535 不触发 u16 越界分支，1002/16072 是唯一无摆放例外。
3. **建议的绑定键**：按 `(特殊对象集合, presentation_only 判定, positions)` 三元组绑定，而不是按遭遇名单或 dungeon/map ID。对电梯而言该键在全 PVF 唯一（{1111,1112,1113} + 4 位），天然不会误伤其余 27 簇。
