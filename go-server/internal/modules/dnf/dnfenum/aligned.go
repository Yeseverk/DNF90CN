// 本文件记录当前 EXE/MCP 证据允许进入 Go 模块化迁移的 DNF command 边界。
package dnfenum

import "sort"

// AlignedDomain 表示当前已确认协议号一致的业务模块边界。
// 这里只放协议合同，不在枚举层直接实现业务回包。
type AlignedDomain string

const (
	AlignedDomainCharacter   AlignedDomain = "character"
	AlignedDomainInventory   AlignedDomain = "inventory"
	AlignedDomainPet         AlignedDomain = "pet"
	AlignedDomainPackage     AlignedDomain = "package"
	AlignedDomainAvatarTitle AlignedDomain = "avatar_title"
	AlignedDomainSkill       AlignedDomain = "skill"
	AlignedDomainCargo       AlignedDomain = "cargo"
	AlignedDomainItemLock    AlignedDomain = "item_lock"
	AlignedDomainDungeon     AlignedDomain = "dungeon"
	AlignedDomainQuest       AlignedDomain = "quest"
	AlignedDomainMail        AlignedDomain = "mail"
	AlignedDomainParty       AlignedDomain = "party"
	AlignedDomainRaid        AlignedDomain = "raid"
)

// AlignedSupport 表示迁移把握度。
// direct：协议号和用途可先建 Go 模块入口；partial：协议号能对齐，但包体/顺序还要继续用 EXE/日志补证据。
type AlignedSupport string

const (
	AlignedSupportDirect  AlignedSupport = "direct"
	AlignedSupportPartial AlignedSupport = "partial"
)

// AlignedCommand 记录 NoPack 当前 EXE 与旧 C# 对比后可进入模块化迁移队列的命令。
type AlignedCommand struct {
	Opcode   CmdPacket
	Domain   AlignedDomain
	Support  AlignedSupport
	Evidence string
	Note     string
}

// BlockedMigration 记录已经被当前 EXE/MCP 证据排除的旧映射，避免误接旧 C# 回包。
type BlockedMigration struct {
	Opcode   CmdPacket
	Domain   AlignedDomain
	Evidence string
	Reason   string
}

const alignedEvidenceMatrix20260705 = "outputs/csharp_go_protocol_reuse_matrix_20260705.md"

var alignedCommands = map[CmdPacket]AlignedCommand{
	CmdPacketSetPartyInfo:                aligned(CmdPacketSetPartyInfo, AlignedDomainParty, AlignedSupportDirect, "创建/修改单人队伍信息"),
	CmdPacketLeaveParty:                  aligned(CmdPacketLeaveParty, AlignedDomainParty, AlignedSupportDirect, "退出当前队伍"),
	CmdPacketCallPartyMemberRealtimeInfo: aligned(CmdPacketCallPartyMemberRealtimeInfo, AlignedDomainParty, AlignedSupportDirect, "请求队员实时状态"),
	CmdPacketRegisterQuickParty: aligned(
		CmdPacketRegisterQuickParty,
		AlignedDomainParty,
		AlignedSupportPartial,
		"MCP 20260705: C2S 0x1BB 写 u32 count 后循环写 u32 target + u8 available；S2C 同号包不是 ACK，先只接入解析记录",
	),
	CmdPacketCancelQuickParty: aligned(
		CmdPacketCancelQuickParty,
		AlignedDomainParty,
		AlignedSupportDirect,
		"MCP 20260705: S2C 0x1BC 成功分支不读正文；可先按取消快速组队 ACK-only 接入",
	),
	CmdPacketDirectEntranceDungeonQuickParty: aligned(
		CmdPacketDirectEntranceDungeonQuickParty,
		AlignedDomainParty,
		AlignedSupportPartial,
		"MCP 20260705: S2C 0x1BD 成功分支不读正文；这里只开放入口 ACK，后续进场景链另行补齐",
	),
	CmdPacketReserveLeaveParty: aligned(
		CmdPacketReserveLeaveParty,
		AlignedDomainParty,
		AlignedSupportPartial,
		"MCP 20260705: C2S 0x2B3 写 u8 flag；class1 S2C 成功分支读 u8 flag + u16 targetChar，列表通知另有 u32 count + 三个 u32",
	),
	CmdPacketChangePartyMemberPosition: aligned(
		CmdPacketChangePartyMemberPosition,
		AlignedDomainParty,
		AlignedSupportPartial,
		"MCP 20260705: C2S sub_30D0320 写 u8 slot,u8 pos(1/3)；S2C 注册点 0x01D371A3 -> sub_1D195E0，成功分支读 u8 fromSlot + u8 toSlot",
	),

	CmdPacketSelectCharacter:       aligned(CmdPacketSelectCharacter, AlignedDomainCharacter, AlignedSupportDirect, "角色选择请求"),
	CmdPacketCreateCharacter:       aligned(CmdPacketCreateCharacter, AlignedDomainCharacter, AlignedSupportDirect, "角色创建请求"),
	CmdPacketDeleteCharacter:       aligned(CmdPacketDeleteCharacter, AlignedDomainCharacter, AlignedSupportDirect, "角色删除请求"),
	CmdPacketReturnSelectCharacter: aligned(CmdPacketReturnSelectCharacter, AlignedDomainCharacter, AlignedSupportDirect, "返回角色选择"),
	CmdPacketGetUserinfo:           aligned(CmdPacketGetUserinfo, AlignedDomainCharacter, AlignedSupportDirect, "USERINFO 初始化请求"),
	CmdPacketCheckDoubleCharacterName: aligned(
		CmdPacketCheckDoubleCharacterName,
		AlignedDomainCharacter,
		AlignedSupportDirect,
		"角色名重复检查；以当前 NoPack CmdPacket 表为准",
	),

	CmdPacketDeleteItem:              aligned(CmdPacketDeleteItem, AlignedDomainInventory, AlignedSupportDirect, "删除物品"),
	CmdPacketMoveItemspace:           aligned(CmdPacketMoveItemspace, AlignedDomainInventory, AlignedSupportDirect, "移动物品栏位置"),
	CmdPacketSortItem:                aligned(CmdPacketSortItem, AlignedDomainInventory, AlignedSupportDirect, "整理物品栏"),
	CmdPacketBuyItem:                 aligned(CmdPacketBuyItem, AlignedDomainInventory, AlignedSupportDirect, "NPC 购买"),
	CmdPacketSellItem:                aligned(CmdPacketSellItem, AlignedDomainInventory, AlignedSupportDirect, "NPC 出售"),
	CmdPacketRepairEquipment:         aligned(CmdPacketRepairEquipment, AlignedDomainInventory, AlignedSupportDirect, "修理装备"),
	CmdPacketDisjointItem:            aligned(CmdPacketDisjointItem, AlignedDomainInventory, AlignedSupportDirect, "分解物品"),
	CmdPacketUseStackable:            aligned(CmdPacketUseStackable, AlignedDomainInventory, AlignedSupportDirect, "使用消耗品"),
	CmdPacketUseStackableAction:      aligned(CmdPacketUseStackableAction, AlignedDomainInventory, AlignedSupportDirect, "current EXE op515: damage-font action 162 request and response verified"),
	CmdPacketSelectDamageFontSkin:    aligned(CmdPacketSelectDamageFontSkin, AlignedDomainInventory, AlignedSupportDirect, "current EXE op1288: u16 font index request; success and error-17 responses verified"),
	CmdPacketUpgradeItem:             aligned(CmdPacketUpgradeItem, AlignedDomainInventory, AlignedSupportDirect, "装备强化"),
	CmdPacketEnchantByBead:           aligned(CmdPacketEnchantByBead, AlignedDomainInventory, AlignedSupportDirect, "宝珠附魔"),
	CmdPacketPurifyItem:              aligned(CmdPacketPurifyItem, AlignedDomainInventory, AlignedSupportDirect, "装备净化/清除"),
	CmdPacketInvestItemAmplifyOption: aligned(CmdPacketInvestItemAmplifyOption, AlignedDomainInventory, AlignedSupportDirect, "装备增幅属性赋予/扭转/黄金书"),
	CmdPacketUnsealRandomOption:      aligned(CmdPacketUnsealRandomOption, AlignedDomainInventory, AlignedSupportDirect, "魔法封印装备解除"),
	CmdPacketChangeRandomOption:      aligned(CmdPacketChangeRandomOption, AlignedDomainInventory, AlignedSupportDirect, "魔法封印装备单词条变更"),

	CmdPacketRenameCreature: aligned(
		CmdPacketRenameCreature,
		AlignedDomainPet,
		AlignedSupportDirect,
		"Current NoPack: C2S sub_31F5220 writes rename-card u16 slot,u8 list7,DSTR name; S2C sub_1D1D5C0 consumes the echoed card row",
	),
	CmdPacketHatchCreature:          aligned(CmdPacketHatchCreature, AlignedDomainPet, AlignedSupportDirect, "宠物孵化"),
	CmdPacketHatchCreatureEgg:       aligned(CmdPacketHatchCreatureEgg, AlignedDomainPet, AlignedSupportDirect, "宠物蛋孵化"),
	CmdPacketRequestHatchedCreature: aligned(CmdPacketRequestHatchedCreature, AlignedDomainPet, AlignedSupportDirect, "请求已孵化宠物"),

	CmdPacketUseBoosterItem:   aligned(CmdPacketUseBoosterItem, AlignedDomainPackage, AlignedSupportDirect, "选择类礼包；协议号与最新 C#/EXE 对齐"),
	CmdPacketUseRandomboxItem: aligned(CmdPacketUseRandomboxItem, AlignedDomainPackage, AlignedSupportDirect, "随机盒子开启"),
	CmdPacketUseRandomboxItemExpand: aligned(
		CmdPacketUseRandomboxItemExpand,
		AlignedDomainPackage,
		AlignedSupportDirect,
		"随机盒子连开（全部开启）；C2S 15 字节布局来自 2026-07-25 当前 NoPack 客户端实测抓包",
	),

	CmdPacketCompoundAvatar: aligned(CmdPacketCompoundAvatar, AlignedDomainAvatarTitle, AlignedSupportDirect, "时装合成"),
	CmdPacketUseEmblem:      aligned(CmdPacketUseEmblem, AlignedDomainAvatarTitle, AlignedSupportDirect, "徽章使用"),
	CmdPacketDisjointAvatar: aligned(CmdPacketDisjointAvatar, AlignedDomainAvatarTitle, AlignedSupportDirect, "时装分解"),
	CmdPacketCompoundEmblem: aligned(CmdPacketCompoundEmblem, AlignedDomainAvatarTitle, AlignedSupportDirect, "徽章合成"),
	CmdPacketAddAvatarSocket: aligned(
		CmdPacketAddAvatarSocket,
		AlignedDomainAvatarTitle,
		AlignedSupportDirect,
		"时装开孔",
	),
	CmdPacketTitleBookPut: aligned(CmdPacketTitleBookPut, AlignedDomainAvatarTitle, AlignedSupportDirect, "称号簿放入"),
	CmdPacketTitleBookGet: aligned(CmdPacketTitleBookGet, AlignedDomainAvatarTitle, AlignedSupportDirect, "称号簿取出"),
	CmdPacketSetCloneTitle: aligned(
		CmdPacketSetCloneTitle,
		AlignedDomainAvatarTitle,
		AlignedSupportDirect,
		"Current NoPack live C2S op568/0x0238 writes one u32 title item ID; S2C op568 is registered as DoNothing and the visible result is projected through class0/op2 mode0 title slot 13",
	),

	CmdPacketChangeSkillslot: aligned(
		CmdPacketChangeSkillslot,
		AlignedDomainSkill,
		AlignedSupportDirect,
		"Current EXE: C2S sub_268DA30 writes u8 tree,u8 from,u8 to,i32 context,u8 mode; S2C sub_1D0A4B0 reads tree/from/to",
	),
	CmdPacketBuySkill: aligned(
		CmdPacketBuySkill,
		AlignedDomainSkill,
		AlignedSupportDirect,
		"Current EXE: C2S sub_20F46F0/sub_22147B0 write tree,count,(u16 skill,u8 refund,u8 delta)*,mode; S2C sub_1D1E080",
	),
	CmdPacketSkillInit: aligned(
		CmdPacketSkillInit,
		AlignedDomainSkill,
		AlignedSupportDirect,
		"Current EXE: live C2S op491 body is u8 tree,u8 mode; S2C sub_1FEC390 reads tree,u8 success-refresh flag",
	),
	CmdPacketChangeAnotherSkillTree: aligned(
		CmdPacketChangeAnotherSkillTree,
		AlignedDomainSkill,
		AlignedSupportDirect,
		"Current EXE live200: C2S body is current tree byte plus four-byte transport tail; S2C sub_1D0C8F0 reads success then one target-tree byte",
	),
	CmdPacketSkillCommandCustomizing: aligned(
		CmdPacketSkillCommandCustomizing,
		AlignedDomainSkill,
		AlignedSupportDirect,
		"Current EXE: C2S sub_2689980 writes tree then repeated u16 skill,u8 command_len,raw command; S2C sub_1D0CEE0 is ACK-only",
	),
	CmdPacketSkillCommandAllDefault: aligned(
		CmdPacketSkillCommandAllDefault,
		AlignedDomainSkill,
		AlignedSupportDirect,
		"Current EXE: C2S sub_320A960 sends an empty body; S2C sub_1D19560 is ACK-only",
	),
	CmdPacketCreateAccountCargo:      aligned(CmdPacketCreateAccountCargo, AlignedDomainCargo, AlignedSupportDirect, "创建账号仓库"),
	CmdPacketUpgradeAccountCargo:     aligned(CmdPacketUpgradeAccountCargo, AlignedDomainCargo, AlignedSupportDirect, "升级账号仓库"),
	CmdPacketDepositMoney:            aligned(CmdPacketDepositMoney, AlignedDomainCargo, AlignedSupportDirect, "存入金币"),
	CmdPacketWithdrawMoney:           aligned(CmdPacketWithdrawMoney, AlignedDomainCargo, AlignedSupportDirect, "取出金币"),
	CmdPacketRequestItemLock:         aligned(CmdPacketRequestItemLock, AlignedDomainItemLock, AlignedSupportDirect, "物品锁定"),
	CmdPacketRequestItemUnlock:       aligned(CmdPacketRequestItemUnlock, AlignedDomainItemLock, AlignedSupportDirect, "物品解锁"),
	CmdPacketRequestItemUnlockCancel: aligned(CmdPacketRequestItemUnlockCancel, AlignedDomainItemLock, AlignedSupportDirect, "取消物品解锁"),
	CmdPacketEnterSelectDungeon:      aligned(CmdPacketEnterSelectDungeon, AlignedDomainDungeon, AlignedSupportDirect, "进入选择地下城"),
	CmdPacketSelectDungeon:           aligned(CmdPacketSelectDungeon, AlignedDomainDungeon, AlignedSupportDirect, "选择地下城"),
	CmdPacketDieMonster:              aligned(CmdPacketDieMonster, AlignedDomainDungeon, AlignedSupportPartial, "怪物死亡；场景内顺序需继续验证"),
	CmdPacketDieCharacter:            aligned(CmdPacketDieCharacter, AlignedDomainDungeon, AlignedSupportPartial, "角色死亡；场景内顺序需继续验证"),
	CmdPacketUseCoin:                 aligned(CmdPacketUseCoin, AlignedDomainDungeon, AlignedSupportPartial, "复活币；场景内顺序需继续验证"),
	CmdPacketGetItem:                 aligned(CmdPacketGetItem, AlignedDomainDungeon, AlignedSupportDirect, "地下城拾取物品；协议号与最新 C#/EXE 对齐"),
	CmdPacketMoveMap:                 aligned(CmdPacketMoveMap, AlignedDomainDungeon, AlignedSupportDirect, "切换房间；协议号与最新 C#/EXE 对齐"),
	CmdPacketSetPlayResult:           aligned(CmdPacketSetPlayResult, AlignedDomainDungeon, AlignedSupportPartial, "副本结果；结算链需继续验证"),
	CmdPacketChangeTutorialFlag:      aligned(CmdPacketChangeTutorialFlag, AlignedDomainDungeon, AlignedSupportPartial, "教学标记；旧链可对照但需当前 EXE 证据"),
	CmdPacketDungeonEventStoryPause:  aligned(CmdPacketDungeonEventStoryPause, AlignedDomainDungeon, AlignedSupportPartial, "地下城剧情暂停"),
	CmdPacketRequestDisjointItem:     aligned(CmdPacketRequestDisjointItem, AlignedDomainDungeon, AlignedSupportPartial, "地下城分解请求；需继续核上下文"),
	CmdPacketAcceptQuest:             aligned(CmdPacketAcceptQuest, AlignedDomainQuest, AlignedSupportDirect, "接受任务"),
	CmdPacketGiveupQuest:             aligned(CmdPacketGiveupQuest, AlignedDomainQuest, AlignedSupportDirect, "放弃任务"),
	CmdPacketSetQuestTrigger:         aligned(CmdPacketSetQuestTrigger, AlignedDomainQuest, AlignedSupportDirect, "任务触发状态"),
	CmdPacketFinishQuest:             aligned(CmdPacketFinishQuest, AlignedDomainQuest, AlignedSupportDirect, "完成任务"),
	CmdPacketMailboxSend:             aligned(CmdPacketMailboxSend, AlignedDomainMail, AlignedSupportDirect, "current NoPack op94 exact decode + atomic asset/mail transfer"),
	CmdPacketMailboxExtractItem:      aligned(CmdPacketMailboxExtractItem, AlignedDomainMail, AlignedSupportDirect, "current NoPack op95 batch decode + atomic claim"),
	CmdPacketMailboxOpen:             aligned(CmdPacketMailboxOpen, AlignedDomainMail, AlignedSupportDirect, "current NoPack op96 empty request + count ACK + class0/0x61 mailbox snapshot"),
	CmdPacketChangeLetterStat:        aligned(CmdPacketChangeLetterStat, AlignedDomainMail, AlignedSupportDirect, "current NoPack op134 exact state request/result"),
	CmdPacketMultiMailboxSend:        aligned(CmdPacketMultiMailboxSend, AlignedDomainMail, AlignedSupportDirect, "current NoPack op315 multi-attachment send"),
	CmdPacketQueryCharacInfoMailbox:  aligned(CmdPacketQueryCharacInfoMailbox, AlignedDomainMail, AlignedSupportDirect, "current NoPack op324 exact character query"),
	CmdPacketRequestServerCharacterList: aligned(
		CmdPacketRequestServerCharacterList,
		AlignedDomainMail,
		AlignedSupportDirect,
		"current NoPack mailbox account-role request op789: u8 server id; class0/op718 server-character rows",
	),
	CmdPacketEntryIntoParty: aligned(
		CmdPacketEntryIntoParty,
		AlignedDomainParty,
		AlignedSupportPartial,
		"MCP 20260705: C2S 0x2C1 写 u32 target；S2C 成功分支读 u32,u32，失败码 3/19/20；成功需要在线 session 协调",
	),
	CmdPacketEntryIntoPartyFinish: aligned(
		CmdPacketEntryIntoPartyFinish,
		AlignedDomainParty,
		AlignedSupportPartial,
		"Current EXE: C2S 0x2C2 body is empty; S2C is class0 and reads u8 state,u8 count,count*(u32 key,u32 value); no class1 success/failure envelope is registered",
	),
	CmdPacketRaidEntryCostInfo: aligned(
		CmdPacketRaidEntryCostInfo,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B19C0 发送 658 RaidEntryCostInfo，body=u8 bool；未确认 S2C 成功结构前只登记解析",
	),
	CmdPacketRaidSetSymbol: aligned(
		CmdPacketRaidSetSymbol,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B1600 发送 662 RaidSetSymbol，body=u32,u32,u8(symbol<3)；raid 标记同步仍需 owner 证据",
	),
	CmdPacketCreateRaid: aligned(
		CmdPacketCreateRaid,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_1B49240 发送 664 创建攻坚队，body=u8 route,dstr name,u8 tail；成功后刷新 raw 0x24F mode=3",
	),
	CmdPacketLeaveRaid: aligned(
		CmdPacketLeaveRaid,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B1E30/sub_324F850 发送 665 离开攻坚队，body=u16 key；成功后刷新 raw 0x24F mode=3",
	),
	CmdPacketStartRaid: aligned(
		CmdPacketStartRaid,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B2820 发送 666 开始攻坚为空包；服务端开始后按攻坚小组自动生成普通 4 人队伍",
	),
	CmdPacketSetRaidWaiting: aligned(
		CmdPacketSetRaidWaiting,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_1B4C2A0 发送 667 等待状态，body=u8 flag,u8 route；对象级刷新还需继续确认",
	),
	CmdPacketRejoinRaid: aligned(
		CmdPacketRejoinRaid,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_1D638D0 触发 668 重新加入，body=u32 raid_key；服务端应重推 raw 0x24F mode=3",
	),
	CmdPacketRaidManagerWork: aligned(
		CmdPacketRaidManagerWork,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: C2S 669 为攻坚队长小组编辑请求，body=u32,u32 member,u32 target_group；S2C 669 是 DoNothing，真实刷新走 raw 0x24F mode=3",
	),
	CmdPacketModifyRaidInfo: aligned(
		CmdPacketModifyRaidInfo,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_1B49240 发送 670 修改攻坚信息，body=u8 route,dstr name,u8 tail；成功后刷新 raw 0x24F mode=3",
	),
	CmdPacketRaidOtherChannelRequestJoin: aligned(
		CmdPacketRaidOtherChannelRequestJoin,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B1530 发送 820 跨频道攻坚申请，body=u8,u16,u32,u16；需要公共频道 owner 回包",
	),
	CmdPacketRaidMemberChangeState: aligned(
		CmdPacketRaidMemberChangeState,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B1C40 发送 823 成员状态变化，body=u8 state；在线 roster 可更新后推 0x24F mode=3",
	),
	CmdPacketRaidUserMoveChannelFail: aligned(
		CmdPacketRaidUserMoveChannelFail,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B17A0 发送 824 跨频道移动失败上报，body=u8,u16；只记录不伪造成功",
	),
	CmdPacketRaidOtherChannelList: aligned(
		CmdPacketRaidOtherChannelList,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B3140 发送 831 公共频道列表，body=u8 或 u8,raw8,dstr；列表回包待 owner",
	),
	CmdPacketRaidCheckRaidUser: aligned(
		CmdPacketRaidCheckRaidUser,
		AlignedDomainRaid,
		AlignedSupportPartial,
		"MCP 20260705: sub_17B1D50 发送 889 攻坚用户检查，body=u8,u16；S2C 889 为复杂列表",
	),
}

var blockedMigrations = map[CmdPacket]BlockedMigration{
	CmdPacketPCRoomPlayTimeReward:     blocked(CmdPacketPCRoomPlayTimeReward, AlignedDomainSkill, "旧 SkillInit 邻近包不能直接复用，需按当前 EXE 重新追包体"),
	CmdPacketRequestCharacSkillInfo:   blocked(CmdPacketRequestCharacSkillInfo, AlignedDomainSkill, "Current EXE has no type-484 registration through sub_2262980 and no upper_pkt_start_msg13(484) sender; the old C# mapping is not reusable"),
	CmdPacketGetItembox:               blocked(CmdPacketGetItembox, AlignedDomainPackage, "当前 EXE 证据显示 519 属于强化直播/捐赠房间链，不是旧开礼包映射"),
	CmdPacketRequestFreelyGiveItemBox: blocked(CmdPacketRequestFreelyGiveItemBox, AlignedDomainPackage, "旧礼包链不能直接复用，需重新确认请求/回包结构"),
	CmdPacketSurveyContents:           blocked(CmdPacketSurveyContents, AlignedDomainAvatarTitle, "旧 CloneTitle 邻近映射不能直接复用"),
	CmdPacketCheckTerritoryCombatChannelEnter: blocked(
		CmdPacketCheckTerritoryCombatChannelEnter,
		AlignedDomainDungeon,
		"旧地域/活动链不能直接复用，需当前 EXE handler 证据",
	),
	CmdPacketCheckTerritoryCombatExerciseModeTime: blocked(
		CmdPacketCheckTerritoryCombatExerciseModeTime,
		AlignedDomainDungeon,
		"旧地域/活动链不能直接复用，需当前 EXE handler 证据",
	),
	CmdPacketEventAccountFatigueStat: blocked(CmdPacketEventAccountFatigueStat, AlignedDomainInventory, "旧活动疲劳统计包不能直接复用"),
}

func aligned(op CmdPacket, domain AlignedDomain, support AlignedSupport, note string) AlignedCommand {
	return AlignedCommand{
		Opcode:   op,
		Domain:   domain,
		Support:  support,
		Evidence: alignedEvidenceMatrix20260705,
		Note:     note,
	}
}

func blocked(op CmdPacket, domain AlignedDomain, reason string) BlockedMigration {
	return BlockedMigration{
		Opcode:   op,
		Domain:   domain,
		Evidence: alignedEvidenceMatrix20260705,
		Reason:   reason,
	}
}

// LookupAlignedCommand 返回已确认可进入 Go 模块化迁移队列的命令。
func LookupAlignedCommand(opcode uint16) (AlignedCommand, bool) {
	cmd, ok := alignedCommands[CmdPacket(opcode)]
	return cmd, ok
}

// LookupBlockedMigration 返回不能按旧 C# 映射直接迁移的命令。
func LookupBlockedMigration(opcode uint16) (BlockedMigration, bool) {
	blocked, ok := blockedMigrations[CmdPacket(opcode)]
	return blocked, ok
}

// AlignedCommands 返回当前已确认协议号一致的命令清单副本，供路由覆盖测试和调试面板使用。
func AlignedCommands() []AlignedCommand {
	out := make([]AlignedCommand, 0, len(alignedCommands))
	for _, command := range alignedCommands {
		out = append(out, command)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Opcode < out[j].Opcode
	})
	return out
}

// BlockedMigrations 返回当前被 EXE 证据阻断的旧迁移清单副本。
func BlockedMigrations() []BlockedMigration {
	out := make([]BlockedMigration, 0, len(blockedMigrations))
	for _, blocked := range blockedMigrations {
		out = append(out, blocked)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Opcode < out[j].Opcode
	})
	return out
}
