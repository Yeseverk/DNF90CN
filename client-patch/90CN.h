#pragma once

#include <windows.h>

// Starts the one-time current-EXE cipher proxy after 86JP.dll is loaded by
// the normal ijl15 plugin chain.
void Start90CNPatch();

// 接口名称：Queue90CNClientNotice
// 中文说明：把一条 UTF-16 游戏提示复制到 90CN 主线程队列，并唤醒游戏窗口线程。
// 参数：text 为不包含内嵌 NUL 的文本；length 为字符数，不包含结尾 NUL，最大 511。
// 返回值：TRUE 表示已经安全入队；FALSE 表示接口未就绪、参数无效或队列已满。
// 线程约束：调用者不会直接执行游戏 UI；实际提示只在 DNF 主窗口线程中显示。
EXTERN_C BOOL WINAPI Queue90CNClientNotice(
    const wchar_t* text, unsigned int length);

// 客户端事件类型：只表示已经由当前 EXE 的窗口或场景切换边界可靠确认的状态。
enum DNF90ClientEventType : unsigned int
{
    DNF90_CLIENT_EVENT_UI_READY = 1,
    DNF90_CLIENT_EVENT_UI_CLOSED = 2,
    DNF90_CLIENT_EVENT_ENTER_TOWN = 3,
    // 事件名称：enter_dungeon
    // 中文说明：当前 EXE 完成副本信息、首张地图和当前角色激活后产生一次。
    DNF90_CLIENT_EVENT_ENTER_DUNGEON = 4,
    // 事件名称：dungeon_room_changed
    // 中文说明：副本活动态接受一个与上次不同的完整 op29 房间后产生一次。
    DNF90_CLIENT_EVENT_DUNGEON_ROOM_CHANGED = 5,
    // 事件名称：leave_dungeon
    // 中文说明：副本活动态接受最终回城 op24 时，在 enter_town 前产生一次。
    DNF90_CLIENT_EVENT_LEAVE_DUNGEON = 6,
};

// 上下文标志名称：DNF90ClientEventContextFlag
// 中文说明：说明追加字段中哪些值已经由当前 EXE 的完整 typed 包可靠确认。
enum DNF90ClientEventContextFlag : unsigned int
{
    DNF90_CLIENT_EVENT_CONTEXT_DUNGEON_VALID = 1u << 0,
    DNF90_CLIENT_EVENT_CONTEXT_ROOM_VALID = 1u << 1,
    DNF90_CLIENT_EVENT_CONTEXT_PREVIOUS_ROOM_VALID = 1u << 2,
    DNF90_CLIENT_EVENT_CONTEXT_BOSS_ROOM = 1u << 3,
};

// 兼容常量名称：DNF90_CLIENT_EVENT_V1_SIZE
// 中文说明：旧版事件只有前五个字段；新旧 DLL 通过该固定前缀双向兼容。
constexpr unsigned int DNF90_CLIENT_EVENT_V1_SIZE =
    5u * sizeof(unsigned int);

// 结构名称：DNF90ClientEvent
// 中文说明：90CN.dll 复制到固定容量队列中的客户端生命周期事件。
// 字段：size 为结构大小；type 为 DNF90ClientEventType；sequence 为进程内递增编号；
//       processID 为客户端进程；uiThreadID 为确认该事件的 DNF UI 线程；
//       contextFlags 决定后续副本字段是否有效；previousRoom* 只用于换房事件。
struct DNF90ClientEvent
{
    unsigned int size;
    unsigned int type;
    unsigned int sequence;
    unsigned int processID;
    unsigned int uiThreadID;
    unsigned int contextFlags;
    unsigned int dungeonID;
    unsigned int roomX;
    unsigned int roomY;
    unsigned int roomLayerFlag;
    unsigned int mapID;
    unsigned int previousRoomX;
    unsigned int previousRoomY;
    unsigned int previousRoomLayerFlag;
    unsigned int previousMapID;
};

static_assert(sizeof(DNF90ClientEvent) == 60,
    "DNF90ClientEvent append-only ABI size changed unexpectedly");

// 接口名称：Dequeue90CNClientEvent
// 中文说明：从 90CN.dll 的固定容量队列中取出一条客户端生命周期事件。
// 参数：output 指向调用者提供的可写结构；outputSize 至少为 DNF90_CLIENT_EVENT_V1_SIZE。
//       旧调用者只取得 20 字节稳定前缀；当前调用者取得完整追加字段。
// 返回值：TRUE 表示取出一条完整事件；FALSE 表示参数无效或当前队列为空。
// 线程约束：本接口只复制事件数据，不调用 Lua，也不执行游戏 UI。
EXTERN_C BOOL WINAPI Dequeue90CNClientEvent(
    DNF90ClientEvent* output, unsigned int outputSize);

// 有效标志名称：DNF90CharacterStatSnapshotFlag
// 中文说明：标记角色属性快照中已经由当前 EXE 的完整 class0/op2 包可靠取得的数据。
enum DNF90CharacterStatSnapshotFlag : unsigned int
{
    DNF90_CHARACTER_STATS_BASE_VALID = 1u << 0,
    DNF90_CHARACTER_STATS_SPEED_VALID = 1u << 1,
};

// 结构名称：DNF90CharacterStatSnapshot
// 中文说明：从当前 EXE 已成功处理的 92 字节角色状态块中复制出的只读快照。
// 字段：generation 每次收到有效 mode1/mode3 状态块时递增；四维已经还原为游戏数值，
//       不再保留线上协议使用的十倍缩放；速度字段保持当前协议中的原始整数单位。
struct DNF90CharacterStatSnapshot
{
    unsigned int size;
    unsigned int generation;
    unsigned int validFlags;
    unsigned int hpMax;
    unsigned int mpMax;
    int strength;
    int vitality;
    int intelligence;
    int spirit;
    unsigned int moveSpeed;
    unsigned int attackSpeed;
    unsigned int castSpeed;
};

static_assert(sizeof(DNF90CharacterStatSnapshot) == 48,
    "DNF90CharacterStatSnapshot append-only ABI size changed unexpectedly");

// 接口名称：Query90CNCharacterStatSnapshot
// 中文说明：读取最近一次可靠角色属性快照，供独立 Lua DLL 计算战斗力。
// 参数：output 指向调用者提供的可写结构；outputSize 必须能容纳当前完整结构。
// 返回值：TRUE 表示已复制有效快照；FALSE 表示尚未收到角色数据或参数无效。
// 线程约束：只在 SRW 锁内复制普通数据，不读取游戏对象，也不执行游戏 UI。
EXTERN_C BOOL WINAPI Query90CNCharacterStatSnapshot(
    DNF90CharacterStatSnapshot* output, unsigned int outputSize);

// 装备快照常量名称：DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS
// 中文说明：当前 EXE 的穿戴对象位图覆盖槽位 0～32；快照按已出现对象保存，
//           因而最多需要 33 条，不会读取或虚构这个范围以外的槽位。
enum : unsigned int
{
    DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS = 33,
};

// 有效标志名称：DNF90EquipmentSnapshotFlag
// 中文说明：标记装备快照已经由当前 EXE 完整接受的 class0/op2 mode1/mode3
//           穿戴对象创建列表可靠取得。
enum DNF90EquipmentSnapshotFlag : unsigned int
{
    DNF90_EQUIPMENT_SNAPSHOT_ROWS_VALID = 1u << 0,
};

// 结构名称：DNF90EquippedItemSnapshot
// 中文说明：一件已穿戴对象的只读线协议摘要。
// 字段：actorSlot 是当前 EXE 的运行时穿戴槽；upgradeLevel 来自 extData 低五位；
//       amplifyType 与 amplifyValue 来自同一条已验证 0x77 状态投影。
// 边界：itemID/qualitySeed 只作为身份与换装变化证据；PVF 词条由服务端权威投影提供，
//       Lua 不凭线协议 ID 猜稀有度或词条。
struct DNF90EquippedItemSnapshot
{
    unsigned int itemID;
    unsigned int qualitySeed;
    unsigned short durability;
    unsigned short amplifyValue;
    unsigned char actorSlot;
    unsigned char upgradeLevel;
    unsigned char amplifyType;
    unsigned char reserved;
};

static_assert(sizeof(DNF90EquippedItemSnapshot) == 16,
    "DNF90EquippedItemSnapshot append-only ABI size changed unexpectedly");

// 结构名称：DNF90EquipmentSnapshot
// 中文说明：最近一次本地角色完整穿戴对象创建列表的固定容量只读副本。
// 字段：generation 每次接受完整列表时递增；sourceStatGeneration 对应同包属性版本；
//       itemCount 仅说明 items 前多少条有效，允许为零以表示确实没有穿戴对象。
struct DNF90EquipmentSnapshot
{
    unsigned int size;
    unsigned int generation;
    unsigned int sourceStatGeneration;
    unsigned int validFlags;
    unsigned int itemCount;
    DNF90EquippedItemSnapshot items[DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS];
};

static_assert(sizeof(DNF90EquipmentSnapshot) == 548,
    "DNF90EquipmentSnapshot append-only ABI size changed unexpectedly");

// 接口名称：Query90CNEquipmentSnapshot
// 中文说明：读取最近一次可靠装备快照，供独立 Lua DLL 计算装备加成。
// 参数：output 指向调用者提供的可写结构；outputSize 必须能容纳当前完整结构。
// 返回值：TRUE 表示已复制有效快照；FALSE 表示尚未收到完整列表或参数无效。
// 线程约束：只在 SRW 锁内复制普通数据，不读取游戏对象，也不执行游戏 UI。
EXTERN_C BOOL WINAPI Query90CNEquipmentSnapshot(
    DNF90EquipmentSnapshot* output, unsigned int outputSize);

// 有效标志名称：DNF90DamageAffixSnapshotFlag
// 中文说明：标记伤害词条快照已由服务端按当前 Script.pvf 与完整穿戴位图计算完成。
enum DNF90DamageAffixSnapshotFlag : unsigned int
{
    DNF90_DAMAGE_AFFIX_SNAPSHOT_VALUES_VALID = 1u << 0,
    DNF90_DAMAGE_AFFIX_SNAPSHOT_IDENTITY_VALID = 1u << 1,
    DNF90_DAMAGE_AFFIX_SNAPSHOT_THREE_ATTACKS_VALID = 1u << 2,
    DNF90_DAMAGE_AFFIX_SNAPSHOT_EQUIPMENT_SCORE_VALID = 1u << 3,
};

// 结构名称：DNF90DamageAffixSnapshot
// 中文说明：服务端从当前 Script.pvf 汇总的穿戴词条只读快照；所有百分比以 0.1% 为单位。
// 字段说明：whiteDamageTenths=白字（附加伤害），yellowDamageTenths=黄字（对敌人伤害增加），
//           criticalDamageTenths=爆伤（暴击伤害）；黄追和爆追始终单独保存，不能冒充前三项；
//           allAttackTenths=所有攻击力/最终伤害，只并入三攻加成；
//           pvfEquipmentScore=服务端按 PVF 等级、品级和稀有度计算的装备基础分。
struct DNF90DamageAffixSnapshot
{
    unsigned int size;
    unsigned int generation;
    unsigned int validFlags;
    unsigned int version;
    unsigned int whiteDamageTenths;
    unsigned int yellowDamageTenths;
    unsigned int criticalDamageTenths;
    unsigned int yellowAdditionalTenths;
    unsigned int criticalAdditionalTenths;
    unsigned int allAttackTenths;
    unsigned int equippedItemCount;
    unsigned int activeSetCount;
    unsigned int job;
    unsigned int growType;
    unsigned int level;
    unsigned int physicalAttack;
    unsigned int magicalAttack;
    unsigned int independentAttack;
    char professionUtf8[32];
    unsigned int pvfEquipmentScore;
};

static_assert(sizeof(DNF90DamageAffixSnapshot) == 108,
    "DNF90DamageAffixSnapshot append-only ABI size changed unexpectedly");

// 接口名称：Query90CNDamageAffixSnapshot
// 中文说明：读取服务端按 PVF 汇总的完整穿戴词条，供独立 Lua DLL 计算并显示战斗力。
// 参数：output 为调用方可写结构；outputSize 必须容纳完整结构。
// 返回值：TRUE 表示已有有效快照；FALSE 表示尚未收到投影或参数无效。
EXTERN_C BOOL WINAPI Query90CNDamageAffixSnapshot(
    DNF90DamageAffixSnapshot* output, unsigned int outputSize);

// 有效标志名称：DNF90CombatPanelStateFlag
// 中文说明：说明 Lua 提交的战斗力面板中哪些分项已经有真实数据来源。
enum DNF90CombatPanelStateFlag : unsigned int
{
    DNF90_COMBAT_PANEL_ENABLED = 1u << 0,
    DNF90_COMBAT_PANEL_BASE_SCORE_VALID = 1u << 1,
    DNF90_COMBAT_PANEL_EQUIPMENT_SCORE_VALID = 1u << 2,
    DNF90_COMBAT_PANEL_DAMAGE_AFFIXES_VALID = 1u << 3,
    DNF90_COMBAT_PANEL_IDENTITY_VALID = 1u << 4,
    DNF90_COMBAT_PANEL_THREE_ATTACKS_VALID = 1u << 5,
};

// 结构名称：DNF90CombatPanelState
// 中文说明：Lua 完成公式计算后交给主 DLL 绘制的纯数据状态。
// 字段：sourceGeneration 对应属性快照版本；formulaVersion 用于区分将来的公式升级；
//       equipmentScore 只有在对应有效标志存在时才允许显示为数值。
struct DNF90CombatPanelState
{
    unsigned int size;
    unsigned int revision;
    unsigned int sourceGeneration;
    unsigned int validFlags;
    unsigned int formulaVersion;
    unsigned int totalScore;
    unsigned int baseAttributeScore;
    unsigned int equipmentScore;
    unsigned int hpMax;
    unsigned int mpMax;
    int strength;
    int vitality;
    int intelligence;
    int spirit;
    unsigned int affixGeneration;
    unsigned int whiteDamageTenths;
    unsigned int yellowDamageTenths;
    unsigned int criticalDamageTenths;
    unsigned int yellowAdditionalTenths;
    unsigned int criticalAdditionalTenths;
    unsigned int allAttackTenths;
    unsigned int equippedItemCount;
    unsigned int activeSetCount;
    unsigned int job;
    unsigned int growType;
    unsigned int level;
    unsigned int physicalAttack;
    unsigned int magicalAttack;
    unsigned int independentAttack;
    char professionUtf8[32];
};

static_assert(sizeof(DNF90CombatPanelState) == 148,
    "DNF90CombatPanelState append-only ABI size changed unexpectedly");

// 接口名称：Update90CNCombatPanel
// 中文说明：复制 Lua 已计算好的面板状态，并唤醒 DNF UI 线程刷新侧栏。
// 参数：state 指向只读状态；stateSize 必须等于当前完整结构大小。
// 返回值：TRUE 表示已安全接收；FALSE 表示接口未就绪、参数无效或数值越界。
// 线程约束：调用者不直接绘制；所有窗口显示、隐藏和重绘均在 DNF UI 线程执行。
EXTERN_C BOOL WINAPI Update90CNCombatPanel(
    const DNF90CombatPanelState* state, unsigned int stateSize);
