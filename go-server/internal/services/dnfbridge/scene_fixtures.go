// 本文件负责承载旧 DOVE 进场景阶段的明文 fixture，并映射到当前 Go 枚举常量。
package dnfbridge

import (
	"bytes"
	"embed"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"sort"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

//go:embed fixtures/dove_scene/*.bin
var longHengSceneFixtureFS embed.FS

const csharpLongHengSceneBootstrapKind = "select_scene_longheng_bootstrap"
const csharpLongHengSceneTailAfterHudKind = "select_scene_longheng_tail_after_hud"
const csharpLongHengSceneRuntimeAfterBlacklistKind = "select_scene_longheng_runtime_after_blacklist"
const csharpCurrentActionTableStateKind = "select_scene_current_action_table_state"
const csharpCurrentSceneObjectListKind = "select_scene_current_object_list"
const csharpCurrentSceneStageStateKind = "select_scene_current_stage_state"
const csharpCurrentRuntimeObjectStateKind = "select_scene_current_runtime_object_state"
const csharpCurrentSceneOp9ActorDisplayKind = "select_scene_current_op9_actor_display"
const csharpNotiFinishLoadingMsgID uint16 = 0x001e
const longHengCurrentSceneMainHudInfoMsgID = uint16(dnfenum.CmdPacketRequestComboScore)
const longHengCurrentSceneStageMsgID = uint16(dnfenum.CmdPacketSelectCollectbox)

// Historical audit rows still use this alias while proving that old DOVE op2
// bodies cannot fit the current reader. Production op356 ownership is defined
// by currentClearQuestListMsgID in scene_clear_quest_list.go.
const longHengCurrentRuntimeObjectStateMsgID = currentClearQuestListMsgID
const currentCancelCargoPadTransportCodec = "current_op391_cargo_pad_reset_fixed_deflate_protected_tail"
const currentCancelCargoPadHeaderSize = 8
const currentCancelCargoPadSlotCount = 127
const currentCancelCargoPadProtectedTailSize = 7

// The cargo-pad reset is always the same logical state: type 2, no flags, and
// 127 unused u32 slots. Its current protected transport uses one final fixed-
// Huffman DEFLATE block. Keeping the DEFLATE member explicit avoids changing
// its wire shape merely because Go's general zlib encoder chooses a different
// legal (but client-incompatible) block representation.
var currentCancelCargoPadFixedDeflate = [...]byte{0x63, 0x60, 0x62, 0x00, 0x83, 0xff, 0xa3, 0x60, 0xc4, 0x02, 0x00}

// The generated enum still carries an old-version name for opcode 491. The
// current EXE registers it to sub_1D55D60, which owns local transition state.
const currentSceneLocalStateMsgID uint16 = 491
const longHengSceneStageFixtureObjectKey uint16 = 0x01a1

// 851 是 DOVE 进场景 HUD-ready 计数门闩；只恢复 24 个 HUD 包，继续禁止 851 后面的社交/活动 tail。
const longHengSceneMainHudInfoWireCount = 24

// The post-HUD DOVE tail is selected by longHengSceneTailAfterHudLedger:
// only entries with implemented_current_body are proactively sent.
const longHengSceneBeforeHudStartIndex = 25
const longHengSceneTailStartIndex = 92

// 120 后继续补到下一段大 op2 场景块；发送时仍经过主动 UI 黑名单，避免恢复礼物/远征/栏位解锁弹窗。
const longHengRuntimeAfterBlacklistSafeRawCount = 329

type longHengSceneFixtureSpec struct {
	class            byte
	msgID            uint16
	kind             string
	file             string
	deferred         bool
	body             []byte
	marker           uint32
	bodyEncoded      bool
	bodyCodec        string
	successEnvelope  bool
	trimDstrZeroTail bool
}

type longHengScenePacketStatus string

const (
	longHengPacketImplementedCurrentBody longHengScenePacketStatus = "implemented_current_body"
	longHengPacketPendingCurrentStruct   longHengScenePacketStatus = "pending_current_struct"
	longHengPacketRequestDriven          longHengScenePacketStatus = "request_driven"
	longHengPacketNotUsedCurrentClient   longHengScenePacketStatus = "not_used_by_current_client"
	longHengPacketUnsafeOldBody          longHengScenePacketStatus = "unsafe_old_body_do_not_send"
)

type longHengScenePacketLedgerEntry struct {
	idx         int
	phase       string
	class       byte
	msgID       uint16
	status      longHengScenePacketStatus
	reason      string
	implemented bool
}

// DOVE 完整日志的 scene_bootstrap_before_hud 阶段，按 idx 25-67 的 dispatcher 明文顺序回放。
// 当前 build 缺少 select 后半段抓包，先用已确认能被 NoPack 当前枚举接受的 Go 常量承载字节形态。
var longHengSceneBootstrapSpec = []longHengSceneFixtureSpec{
	{msgID: uint16(dnfenum.CmdPacketCreateCharacter), file: "000022_05_scene_bootstrap_before_hud_cls0_op0005_ENUM_CMDPACKET_CREATE_CHARACTER_transport.bin"},
	// MCP scene_bootstrap_handlers_mcp_20260702: op13/op14/op108 are protected transport
	// bodies in DOVE. The client expands them before the scene-object readers consume the records.
	{msgID: uint16(dnfenum.CmdPacketLeaveParty), file: "000023_05_scene_bootstrap_before_hud_cls0_op0013_ENUM_CMDPACKET_LEAVE_PARTY_transport.bin", marker: 1, bodyEncoded: true, bodyCodec: "dove_scene_op13_transport_zlib"},
	{msgID: uint16(dnfenum.CmdPacketLeaveParty), file: "000024_05_scene_bootstrap_before_hud_cls0_op0013_ENUM_CMDPACKET_LEAVE_PARTY_transport.bin"},
	{msgID: uint16(dnfenum.CmdPacketLeaveParty), file: "000025_05_scene_bootstrap_before_hud_cls0_op0013_ENUM_CMDPACKET_LEAVE_PARTY_transport.bin"},
	{msgID: uint16(dnfenum.CmdPacketGatheringPartyStatus), file: "000026_05_scene_bootstrap_before_hud_cls0_op0105_ENUM_CMDPACKET_GATHERING_PARTY_STATUS_transport.bin"},
	{msgID: uint16(dnfenum.CmdPacketWalkoutPartyMember), file: "000027_05_scene_bootstrap_before_hud_cls0_op0014_ENUM_CMDPACKET_WALKOUT_PARTY_MEMBER_transport.bin", marker: 1, bodyEncoded: true, bodyCodec: "dove_scene_op14_transport_zlib"},
	{msgID: uint16(dnfenum.CmdPacketWalkoutPartyMember), file: "000028_05_scene_bootstrap_before_hud_cls0_op0014_ENUM_CMDPACKET_WALKOUT_PARTY_MEMBER_transport.bin", marker: 1, bodyEncoded: true, bodyCodec: "dove_scene_op14_transport_zlib"},
	{msgID: uint16(dnfenum.CmdPacketWalkoutPartyMember), file: "000029_05_scene_bootstrap_before_hud_cls0_op0014_ENUM_CMDPACKET_WALKOUT_PARTY_MEMBER_transport.bin", marker: 1, bodyEncoded: true, bodyCodec: "dove_scene_op14_transport_zlib"},
	{msgID: uint16(dnfenum.CmdPacketGetAvatarSpecEvent), file: "000030_05_scene_bootstrap_before_hud_cls0_op0633_ENUM_CMDPACKET_GET_AVATAR_SPEC_EVENT_transport.bin"},
	{msgID: uint16(dnfenum.CmdPacketLeaveParty), file: "000031_05_scene_bootstrap_before_hud_cls0_op0013_ENUM_CMDPACKET_LEAVE_PARTY_transport.bin"},
	{msgID: uint16(dnfenum.CmdPacketReport4Hack), file: "000032_05_scene_bootstrap_before_hud_cls0_op0108_ENUM_CMDPACKET_REPORT_4_HACK_transport.bin", marker: 1, bodyEncoded: true, bodyCodec: "dove_scene_op108_transport_zlib"},
	{msgID: uint16(dnfenum.CmdPacketPeerConnectResult), file: "000036_05_scene_bootstrap_before_hud_cls0_op0097_ENUM_CMDPACKET_PEER_CONNECT_RESULT_body8.bin"},
	// Current op1021's handler sub_1845B30 consumes a one-byte scene-state
	// selector after transport expansion.  The old DOVE transport contained an
	// entire foreign object graph; use the current neutral selector instead.
	{msgID: longHengCurrentSceneStageMsgID, kind: csharpCurrentSceneStageStateKind, body: buildCurrentSceneStageTransportBody(), marker: 1, bodyEncoded: true, bodyCodec: "current_op1021_scene_state_transport_zlib"},
	{msgID: uint16(dnfenum.CmdPacketMoveItemspace), file: "000038_05_scene_bootstrap_before_hud_cls0_op0019_ENUM_CMDPACKET_MOVE_ITEMSPACE_body80.bin"},
	{msgID: longHengCurrentSceneStageMsgID, kind: csharpCurrentSceneStageStateKind, body: buildCurrentSceneStageTransportBody(), marker: 1, bodyEncoded: true, bodyCodec: "current_op1021_scene_state_transport_zlib"},
	// op996 is a local switch in the current client. Its old DOVE account/event
	// payload has no current state owner, so do not proactively emit it here.
	{msgID: uint16(dnfenum.CmdPacketSeriaRidableInHiddenTruthDungeon), deferred: true},
	{msgID: uint16(dnfenum.CmdPacketChangeDeckInfo), file: "000041_05_scene_bootstrap_before_hud_cls0_op0657_ENUM_CMDPACKET_CHANGE_DECK_INFO_body24.bin"},
	{msgID: uint16(dnfenum.CmdPacketDungeonNPCBuffInfo), file: "000042_05_scene_bootstrap_before_hud_cls0_op0273_ENUM_CMDPACKET_DUNGEON_NPC_BUFF_INFO_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketLeaveParty), file: "000040_05_scene_bootstrap_before_hud_cls0_op0013_ENUM_CMDPACKET_LEAVE_PARTY_transport.bin"},
	{msgID: uint16(dnfenum.CmdPacketLeaveParty), file: "000041_05_scene_bootstrap_before_hud_cls0_op0013_ENUM_CMDPACKET_LEAVE_PARTY_transport.bin"},
	// 当前 pcap 证据：op350 是配置模板 transport zlib 包，客户端随后会用 class1/op129 回传展开体前 160 字节。
	{msgID: uint16(dnfenum.CmdPacketApproveJoinGuild), file: "000042_current_pcap_cls0_op0350_config_template_transport.bin", marker: 1, bodyEncoded: true, bodyCodec: "dove_current_op350_config_template_transport_zlib"},
	{msgID: uint16(dnfenum.CmdPacketCancelJoinGuild), file: "000046_05_scene_bootstrap_before_hud_cls0_op0349_ENUM_CMDPACKET_CANCEL_JOIN_GUILD_body8.bin"},
	// MCP/IDA 确认当前 NoPack 注册链 sub_2262A00 将 op356 映射到 sub_1D58470。
	// sub_1D58470 调 sub_3457C50 读取 u32 长度和 raw 字节到 0x7530 位图。
	// 旧 DOVE transport 首四字节会被当成巨大长度，导致客户端栈被写坏；真实对象状态重建前先发当前格式的最小玩家对象状态。
	{msgID: currentClearQuestListMsgID, body: buildCurrentPassGateObjectTransportBody(), marker: 1, bodyEncoded: true, bodyCodec: currentClearQuestListTransportCodec},
	// MCP 跳过包追踪：old op856/current msg521 读取 u8 state，state=10 时才追加 u32。
	// 旧 16 字节样本不是当前明文字段，按当前 reader 回写空状态，仍保留 DOVE 顺序发送。
	{msgID: uint16(dnfenum.CmdPacketCancelIntegratedMatching), body: buildCurrentInfiniteDifficultyCharacInfoBody()},
	// MCP 跳过包追踪：old op177/current msg204 只读取 u8 enabled_or_join_state。
	{msgID: uint16(dnfenum.CmdPacketPurifyItem), body: buildCurrentJoinPowerBody()},
	// MCP 当前 class0/op104 handler 0x015E7F50 读取：u8 count，再循环 wstr[31]+u8+u32。
	// 旧 DOVE op68 body 在当前映射下首字节是 0x21，会被客户端当成 33 条记录读取并连续打印公会创建公告。
	// 这里按当前结构回写空列表，而不是继续发送旧样本。
	{msgID: uint16(dnfenum.CmdPacketRequestAvagachaCoupon), body: []byte{0}},
	// Current EXE registration/disassembly: hexadecimal op0x15 (decimal 21)
	// parses u32 protobuf length and PB_ENUM_NOTIPACKET_ACCEPTABLE_QUEST_LIST.
	// Decimal op138 is a separate fixed seven-byte object/monster state reader.
	{msgID: currentAcceptableQuestListMsgID, kind: currentAcceptableQuestListKind},
	// MCP scene_bootstrap_handlers：old op48/current msg281 读取 56 字节耐久基础结构。
	// 旧 fixture 内是未还原的随机层级；这里按字段写 10000 耐久和空状态，避免 359 finalizer 刷新坏对象。
	{msgID: uint16(dnfenum.CmdPacketHtIs), body: buildCurrentDecreaseDurabilityBody()},
	// MCP 当前注册链 sub_217DD60：class0/op376 -> sub_217D850，仅读 u8/u8/u8。
	// 第 1 字节经 sub_3851100 选择 unk_51AE338 资源表；第 3 字节保持 0，避免主动触发 op432 请求链。
	// 旧 DOVE 16 字节样本不能挂到 msg71；这里按当前枚举 358 回写 5 个空列表页。
	{msgID: uint16(dnfenum.CmdPacketRequestOverseer), body: buildCurrentRequestOverseerBody(0)},
	{msgID: uint16(dnfenum.CmdPacketRequestOverseer), body: buildCurrentRequestOverseerBody(1)},
	{msgID: uint16(dnfenum.CmdPacketRequestOverseer), body: buildCurrentRequestOverseerBody(2)},
	{msgID: uint16(dnfenum.CmdPacketRequestOverseer), body: buildCurrentRequestOverseerBody(3)},
	{msgID: uint16(dnfenum.CmdPacketRequestOverseer), body: buildCurrentRequestOverseerBody(4)},
	// C# 86JP 行为参考：op359 是 achievement complete 列表，u32 count 后跟 12 字节条目。
	{msgID: uint16(dnfenum.CmdPacketInsertOverseer), body: buildCurrentInsertOverseerBody()},
	// 当前链路此前把模板里的 0x007c 延迟掉了，导致 24 个 HUD 包后仍停在 Loading。
	{msgID: uint16(dnfenum.CmdPacketReportClientSpec), body: []byte{}},
	// MCP 复核 current op9 是 sub_1D64CA0 大 actor/display 解析器；
	// 注册点为 sub_2262A00 -> sub_2262980(9, sub_1D64CA0, 0)。该 handler
	// 有 40 次 upper_pkt_read_*，包含 u16 count、kind 分支和 raw_len/read_raw 外观链。
	// 旧 DOVE op9 transport 在当前客户端会落到 0x01D653C9 附近坏指针；这里保留 DOVE 顺序，
	// 但发送时按 current op9 最小 actor/display record 动态构造 body。
	{msgID: uint16(dnfenum.CmdPacketRecoverStamina), bodyCodec: csharpCurrentSceneOp9ActorDisplayKind},
	{msgID: uint16(dnfenum.CmdPacketGuildCargoPushItem), file: "000062_05_scene_bootstrap_before_hud_cls0_op0174_ENUM_CMDPACKET_REQUEST_HATCHED_CREATURE_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketReqRepresentCharacter), file: "000063_05_scene_bootstrap_before_hud_cls0_op0172_ENUM_CMDPACKET_SECURITY_CARD_AUTH_CANCEL_body16.bin"},
	// 共享特效必须来自真实穿戴/使用链，不能在进场景时主动回放 DOVE 样本。
}

var longHengSceneBootstrapBeforeHudPackets = buildLongHengSceneBootstrapPackets()
var longHengSceneMainHudInfoWireBodies = buildLongHengSceneMainHudInfoWireBodies()

// DOVE scene_ready_main_hud 后紧跟的 social/guild tail。这里先只保留 class0 包；
// class1/op442 远征、class1/op140 公会大表和 class1 活动包暂不主动推，避免再次弹远征/社交 UI。
var longHengSceneTailAfterHudSpec = []longHengSceneFixtureSpec{
	{msgID: uint16(dnfenum.CmdPacketAdventurerMakerCreate), file: "game_54226_s2c_0089_class0_op858_seq0_body192.bin"},
	{msgID: uint16(dnfenum.CmdPacketAdventurerMakerCreate), file: "game_54226_s2c_0090_class0_op858_seq0_body192.bin"},
	{msgID: uint16(dnfenum.CmdPacketItemHyperlinkMessage), file: "game_54226_s2c_0091_class0_op424_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketApcTnBetting), file: "game_54226_s2c_0092_class0_op981_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketTitleBookPut), file: "game_54226_s2c_0093_class0_op412_seq0_body72.bin"},
	{msgID: currentSceneLocalStateMsgID, body: buildCurrentSceneLocalStateBody()},
	// Preserve the observed ordering slots, but do not emit static DOVE account
	// or activity samples as live scene state. Each waits for its real owner.
	{msgID: uint16(dnfenum.CmdPacketSeriaRidableInHiddenTruthDungeon), deferred: true},
	{msgID: uint16(dnfenum.CmdPacketLevelupSupport3rdEventGetItem), deferred: true},
	{class: dnfproto.DefaultChannelClassification, msgID: uint16(dnfenum.CmdPacketEventDnftrendGetReward), deferred: true},
	{msgID: uint16(dnfenum.CmdPacketToBeZombie), deferred: true},
	{msgID: uint16(dnfenum.CmdPacketModuleExist), deferred: true},
	{msgID: uint16(dnfenum.CmdPacketRemoveCollectboxItem), file: "game_54226_s2c_0100_class0_op1023_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketRemoveCollectboxItem), file: "game_54226_s2c_0101_class0_op1023_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketRemoveCollectboxItem), file: "game_54226_s2c_0102_class0_op1023_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketRemoveCollectboxItem), file: "game_54226_s2c_0103_class0_op1023_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0104_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0105_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0106_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0107_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0108_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0109_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0110_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0111_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0112_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0113_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0114_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0115_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0116_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), file: "game_54226_s2c_0117_class0_op1199_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketUpdateBestDamageRank), file: "game_54226_s2c_0118_class0_op1089_seq0_body24.bin"},
	{msgID: uint16(dnfenum.CmdPacketWelcombackAttendance), file: "game_54226_s2c_0119_class0_op1145_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketReqTerritoryCombatAllianceList), file: "game_54226_s2c_0120_class0_op825_seq0_body32.bin"},
	{msgID: uint16(dnfenum.CmdPacketHardcoreCharacList), body: buildCurrentHardcoreCharacListBody()},
	{msgID: uint16(dnfenum.CmdPacketFatigueAccelerationStateChange), file: "game_54226_s2c_0122_class0_op562_seq0_body32.bin"},
	{msgID: uint16(dnfenum.CmdPacketBuyGuildContents), file: "game_54226_s2c_0123_class0_op761_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketRequestWarroomReward), file: "game_54226_s2c_0124_class0_op808_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketGetAvatarSpecEvent), file: "game_54226_s2c_0125_class0_op633_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketAttendanceCheck), file: "game_54226_s2c_0126_class0_op428_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketEquipmentSwapInfo), file: "game_54226_s2c_0127_class0_op707_seq0_body8.bin"},
	// 当前 build 在旧 DOVE 公会联盟退出 tail 的 msg760 handler 内崩溃，先等待客户端请求或场景状态成熟后再补。
	{msgID: uint16(dnfenum.CmdPacketLuckyBalloon), file: "game_54226_s2c_0129_class0_op1264_seq0_body16.bin"},
	// msg1301 是旧 DOVE 公会红包 tail，当前 build 会主动打开右侧系统/聊天面板，不能在选角进场景链里主动推。
	{msgID: uint16(dnfenum.CmdPacketDoubleupMinigame), file: "game_54226_s2c_0131_class0_op1316_seq0_body32.bin"},
	{msgID: uint16(dnfenum.CmdPacketCardGameCompound), file: "game_54226_s2c_0132_class0_op1237_seq0_body24.bin"},
	{msgID: uint16(dnfenum.CmdPacketTitleBookGet), file: "game_54226_s2c_0133_class0_op413_seq0_body8.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "game_54226_s2c_0134_class0_op12_seq0_body32.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "game_54226_s2c_0135_class0_op12_seq0_body48.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "game_54226_s2c_0136_class0_op12_seq0_body32.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "game_54226_s2c_0137_class0_op12_seq0_body32.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "game_54226_s2c_0138_class0_op12_seq0_body32.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "game_54226_s2c_0139_class0_op12_seq0_body48.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "game_54226_s2c_0140_class0_op12_seq0_body64.bin"},
	{msgID: uint16(dnfenum.CmdPacketSetPVPReadyState), file: "game_54226_s2c_0141_class0_op53_seq0_body16.bin"},
	{msgID: uint16(dnfenum.CmdPacketJoustBetting), file: "game_54226_s2c_0143_class0_op1292_seq0_body16.bin"},
}

var longHengSceneSafeTailAfterHudPackets = buildLongHengSceneImplementedTailAfterHudPackets()
var currentRequestBlacklistResponseBody = buildCurrentUpperBlacklistResponseBody()

// Historical runtime packets are never materialized in production. The
// fixture audit tests populate this slice from their test-only init hook.
var longHengSceneRuntimeAfterBlacklistPackets []csharpSelectInitPacket

func buildLongHengSceneBootstrapPackets() []csharpSelectInitPacket {
	out := make([]csharpSelectInitPacket, 0, len(longHengSceneBootstrapSpec)+4)
	selectedUserInfoInserted := false
	currentObjectInserted := false
	for _, spec := range longHengSceneBootstrapSpec {
		spec = normalizeLongHengSceneTransportSpec(spec)
		if spec.bodyCodec == csharpCurrentSceneOp9ActorDisplayKind {
			out = append(out, csharpSelectInitPacket{
				class: 0,
				msgID: spec.msgID,
				kind:  csharpCurrentSceneOp9ActorDisplayKind,
			})
			continue
		}
		// A fixture is ordering evidence only.  Do not turn an old DOVE body into
		// a current scene-state packet: the current EXE has different readers for
		// the remaining item, party, event, guild, and cargo packets.  Each of
		// those is either emitted by its repository/request owner or remains
		// explicitly pending in the before-HUD ledger.
		if spec.file != "" || spec.deferred {
			continue
		}
		if !selectedUserInfoInserted && spec.msgID == longHengCurrentSceneStageMsgID {
			out = append(out,
				csharpSelectInitPacket{
					class:      0,
					msgID:      uint16(dnfenum.UpperMsgCharacterRoster),
					occurrence: 0,
					kind:       "select_scene_userinfo",
				},
				csharpSelectInitPacket{
					class:      0,
					msgID:      uint16(dnfenum.UpperMsgCharacterRoster),
					occurrence: 1,
					kind:       "select_scene_userinfo",
				},
			)
			selectedUserInfoInserted = true
		}
		if !currentObjectInserted && spec.msgID == uint16(dnfenum.CmdPacketInsertOverseer) {
			out = append(out, csharpSelectInitPacket{
				class: 0,
				msgID: uint16(dnfenum.CmdPacketPVPMissionHpPercent),
				body:  buildCurrentActionTableStateBody(),
				kind:  csharpCurrentActionTableStateKind,
			})
			// Current object mode0 must run after the op358 list clears and
			// before op359 finalizes the scene object containers.
			out = append(out, csharpSelectInitPacket{
				class: 0,
				msgID: uint16(dnfenum.CmdPacketSetUDPIPPort),
				kind:  csharpCurrentSceneObjectListKind,
			})
			currentObjectInserted = true
		}
		out = append(out, csharpSelectInitPacket{
			class:       spec.class,
			msgID:       spec.msgID,
			marker:      spec.marker,
			body:        mustLongHengSceneBody(spec),
			kind:        longHengScenePacketKind(spec),
			bodyEncoded: spec.bodyEncoded,
			bodyCodec:   spec.bodyCodec,
		})
	}
	return out
}

func longHengScenePacketKind(spec longHengSceneFixtureSpec) string {
	if spec.kind != "" {
		return spec.kind
	}
	return csharpLongHengSceneBootstrapKind
}

func buildCurrentActionTableStateBody() []byte {
	// Current EXE class0/op376 -> sub_217D850 reads three state bytes, then
	// decodes dword_51B0F14/F18 and calls sub_3851100(selector).
	return []byte{0, 0, 0}
}

func buildCurrentSceneStageTransportBody() []byte {
	// Current EXE sub_1845B30 consumes the expanded u8 selector.  Selector 0
	// clears the transient scene-stage flag and reads no DOVE object records.
	// The marker=1 envelope expects the zlib transport layer, hence compression
	// is part of this builder rather than a replayed fixture.
	body, err := zlibCompress([]byte{0})
	if err != nil {
		panic(fmt.Sprintf("compress current op1021 scene state: %v", err))
	}
	return body
}

func buildCurrentCancelCargoPadPlainBody() []byte {
	// The current protected cargo-pad reset state is an 8-byte header followed
	// by 127 signed slot values.  The second header byte selects the reset
	// record type and every slot is -1 (unused).  Construct it from the current
	// state model rather than replaying the old compressed scene capture.
	plain := make([]byte, currentCancelCargoPadHeaderSize+currentCancelCargoPadSlotCount*4)
	plain[1] = 2
	for offset := currentCancelCargoPadHeaderSize; offset < len(plain); offset += 4 {
		binary.LittleEndian.PutUint32(plain[offset:offset+4], ^uint32(0))
	}
	return plain
}

func buildCurrentCancelCargoPadTransportBody() []byte {
	// marker=1 uses the current client's protected zlib transport layer. The
	// fixed DEFLATE member is followed by the seven-byte all-FF unset-record
	// tail owned by that transport layer; omitting it leaves the cargo-pad
	// object incomplete for the later warehouse UI.
	plain := buildCurrentCancelCargoPadPlainBody()
	body := make([]byte, 0, 2+len(currentCancelCargoPadFixedDeflate)+4+currentCancelCargoPadProtectedTailSize)
	body = append(body, 0x78, 0x9c)
	body = append(body, currentCancelCargoPadFixedDeflate[:]...)
	var checksum [4]byte
	binary.BigEndian.PutUint32(checksum[:], adler32.Checksum(plain))
	body = append(body, checksum[:]...)
	body = append(body, bytes.Repeat([]byte{0xff}, currentCancelCargoPadProtectedTailSize)...)
	return body
}

func longHengSceneBeforeHudLedger() []longHengScenePacketLedgerEntry {
	const phase = "05_scene_bootstrap_before_hud"
	implemented := func(idx int, msgID uint16, reason string) longHengScenePacketLedgerEntry {
		return longHengScenePacketLedgerEntry{idx: idx, phase: phase, class: 0, msgID: msgID, status: longHengPacketImplementedCurrentBody, reason: reason, implemented: true}
	}
	pending := func(idx int, msgID uint16, reason string) longHengScenePacketLedgerEntry {
		return longHengScenePacketLedgerEntry{idx: idx, phase: phase, class: 0, msgID: msgID, status: longHengPacketPendingCurrentStruct, reason: reason}
	}
	requestDriven := func(idx int, msgID uint16, reason string) longHengScenePacketLedgerEntry {
		return longHengScenePacketLedgerEntry{idx: idx, phase: phase, class: 0, msgID: msgID, status: longHengPacketRequestDriven, reason: reason}
	}
	return []longHengScenePacketLedgerEntry{
		pending(25, uint16(dnfenum.CmdPacketCreateCharacter), "old_dove_body_excluded_no_current_scene_owner"),
		pending(26, uint16(dnfenum.CmdPacketLeaveParty), "current_op13_container_list_is_repository_driven_old_dove_body_excluded"),
		pending(27, uint16(dnfenum.CmdPacketLeaveParty), "current_op13_container_list_is_repository_driven_old_dove_body_excluded"),
		pending(28, uint16(dnfenum.CmdPacketLeaveParty), "current_op13_container_list_is_repository_driven_old_dove_body_excluded"),
		pending(29, uint16(dnfenum.CmdPacketGatheringPartyStatus), "no_current_exact_handler_old_dove_body_excluded"),
		pending(30, uint16(dnfenum.CmdPacketWalkoutPartyMember), "current_op14_update_requires_real_existing_item_rows_old_dove_body_excluded"),
		pending(31, uint16(dnfenum.CmdPacketWalkoutPartyMember), "current_op14_update_requires_real_existing_item_rows_old_dove_body_excluded"),
		pending(32, uint16(dnfenum.CmdPacketWalkoutPartyMember), "current_op14_update_requires_real_existing_item_rows_old_dove_body_excluded"),
		requestDriven(33, uint16(dnfenum.CmdPacketGetAvatarSpecEvent), "avatar_spec_event_requires_matching_current_request"),
		pending(34, uint16(dnfenum.CmdPacketLeaveParty), "current_op13_container_list_is_repository_driven_old_dove_body_excluded"),
		pending(35, uint16(dnfenum.CmdPacketReport4Hack), "old_dove_transport_excluded_no_current_scene_owner"),
		pending(36, uint16(dnfenum.CmdPacketPeerConnectResult), "old_dove_body_excluded_no_current_scene_owner"),
		implemented(37, longHengCurrentSceneStageMsgID, "current_op1021_scene_stage_transport_sent_object_graph_still_trace_target"),
		requestDriven(38, uint16(dnfenum.CmdPacketMoveItemspace), "itemspace_move_requires_matching_current_item_transaction"),
		implemented(39, longHengCurrentSceneStageMsgID, "current_op1021_scene_stage_transport_sent_object_graph_still_trace_target"),
		requestDriven(40, uint16(dnfenum.CmdPacketSeriaRidableInHiddenTruthDungeon), "current_op996_local_switch_has_no_reader_or_state_owner_proactive_dove_derived_body_excluded"),
		requestDriven(41, uint16(dnfenum.CmdPacketChangeDeckInfo), "deck_state_requires_matching_current_request"),
		requestDriven(42, uint16(dnfenum.CmdPacketDungeonNPCBuffInfo), "dungeon_npc_buff_state_requires_matching_current_scene_owner"),
		pending(43, uint16(dnfenum.CmdPacketLeaveParty), "current_op13_container_list_is_repository_driven_old_dove_body_excluded"),
		pending(44, uint16(dnfenum.CmdPacketLeaveParty), "current_op13_container_list_is_repository_driven_old_dove_body_excluded"),
		requestDriven(45, uint16(dnfenum.CmdPacketApproveJoinGuild), "guild_config_requires_matching_current_request"),
		requestDriven(46, uint16(dnfenum.CmdPacketCancelJoinGuild), "guild_join_state_requires_matching_current_request"),
		implemented(47, currentClearQuestListMsgID, "current_op356_fixed_30000_byte_completed_quest_id_table"),
		implemented(48, uint16(dnfenum.CmdPacketCancelCargoPad), "current_protected_cargo_pad_reset_sent_before_real_item_lists"),
		implemented(49, uint16(dnfenum.CmdPacketCancelIntegratedMatching), "current_empty_infinite_difficulty_state_sent"),
		implemented(50, uint16(dnfenum.CmdPacketPurifyItem), "current_join_power_state_sent"),
		implemented(51, uint16(dnfenum.CmdPacketRequestAvagachaCoupon), "current_empty_guild_notice_list_sent"),
		implemented(52, currentAcceptableQuestListMsgID, "current_pvf_db_acceptable_quest_list_sent"),
		implemented(53, uint16(dnfenum.CmdPacketHtIs), "current_durability_state_sent"),
		implemented(54, uint16(dnfenum.CmdPacketRequestOverseer), "current_request_overseer_empty_row_sent"),
		implemented(55, uint16(dnfenum.CmdPacketRequestOverseer), "current_request_overseer_empty_row_sent"),
		implemented(56, uint16(dnfenum.CmdPacketRequestOverseer), "current_request_overseer_empty_row_sent"),
		implemented(57, uint16(dnfenum.CmdPacketRequestOverseer), "current_request_overseer_empty_row_sent"),
		implemented(58, uint16(dnfenum.CmdPacketRequestOverseer), "current_request_overseer_empty_row_sent"),
		implemented(59, uint16(dnfenum.CmdPacketInsertOverseer), "current_insert_overseer_empty_finalizer_sent"),
		implemented(60, uint16(dnfenum.CmdPacketReportClientSpec), "current_scene_ready_no_body_sent"),
		implemented(61, uint16(dnfenum.CmdPacketRecoverStamina), "current_op9_minimal_actor_display_body_sent"),
		requestDriven(62, uint16(dnfenum.CmdPacketGuildCargoPushItem), "guild_cargo_requires_matching_current_request"),
		requestDriven(63, uint16(dnfenum.CmdPacketReqRepresentCharacter), "represent_character_requires_matching_current_request"),
		requestDriven(64, uint16(dnfenum.CmdPacketUseSharedEffectItem), "current_op251_shared_effect_equipment_state_requires_real_use_or_equipment_chain"),
		pending(65, uint16(dnfenum.CmdPacketFrameLagStatistics), "current_op194_reads_u16_count_rows_old_body174_incompatible"),
		requestDriven(66, uint16(dnfenum.CmdPacketAuctionRegistItem), "current_op183_auction_result_ui_state_requires_matching_request"),
		requestDriven(67, uint16(dnfenum.CmdPacketAuctionRegistItem), "current_op183_auction_result_ui_state_requires_matching_request"),
	}
}

func buildCurrentSceneObjectRawStateFromLongHengTemplate() ([]byte, bool) {
	raw, _, ok := buildCurrentSceneObjectLongHengTemplateParts()
	return raw, ok
}

func buildCurrentSceneObjectTailFromLongHengTemplate() ([]byte, bool) {
	_, tail, ok := buildCurrentSceneObjectLongHengTemplateParts()
	return tail, ok
}

func buildCurrentSceneObjectLongHengTemplateParts() ([]byte, []byte, bool) {
	body, err := longHengSceneFixtureFS.ReadFile("fixtures/dove_scene/000037_05_scene_bootstrap_before_hud_cls0_op0002_ENUM_CMDPACKET_SET_UDP_IP_PORT_transport.bin")
	if err != nil {
		return nil, nil, false
	}
	plain, err := zlibDecompress(body)
	if err != nil {
		return nil, nil, false
	}
	oldKey := make([]byte, 2)
	binary.LittleEndian.PutUint16(oldKey, longHengSceneStageFixtureObjectKey)
	keyOffset := bytes.LastIndex(plain, oldKey)
	if keyOffset < 0x47 || keyOffset+6 > len(plain) {
		return nil, nil, false
	}
	nameLen := int(binary.LittleEndian.Uint32(plain[keyOffset+2 : keyOffset+6]))
	if nameLen < 0 || nameLen > rosterNameMaxBytes {
		return nil, nil, false
	}
	nameEnd := keyOffset + 6 + nameLen
	if nameEnd > len(plain) {
		return nil, nil, false
	}
	raw := append([]byte(nil), plain[keyOffset-0x47:keyOffset]...)
	if len(raw) != 0x47 {
		return nil, nil, false
	}
	tail := append([]byte(nil), plain[nameEnd:]...)
	if len(tail) == 0 {
		return nil, nil, false
	}
	return raw, tail, true
}

func patchLongHengSceneStageTransportObjectKey(body []byte, sceneObjectKey uint16) ([]byte, bool) {
	return patchLongHengSceneStageTransportObject(body, sceneObjectKey, dnfrepo.CharacterRecord{}, false, "")
}

func patchLongHengSceneStageTransportObject(body []byte, sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string) ([]byte, bool) {
	if sceneObjectKey == 0 || sceneObjectKey == longHengSceneStageFixtureObjectKey {
		return body, false
	}
	plain, err := zlibDecompress(body)
	if err != nil {
		return body, false
	}
	oldKey := make([]byte, 2)
	newKey := make([]byte, 2)
	binary.LittleEndian.PutUint16(oldKey, longHengSceneStageFixtureObjectKey)
	binary.LittleEndian.PutUint16(newKey, sceneObjectKey)
	if bytes.Count(plain, oldKey) == 0 {
		return body, false
	}
	patchedPlain := append([]byte(nil), plain...)
	// MCP/live hook：1021 是 DOVE 场景大块 transport，内部包含多对象和后续嵌套状态。
	// 2026-07-06 实测替换整条 entry 会把后续 sub_3457C80 字符串链切歪并跳到 0x30303030；
	// 因此这里只改对象 key，真实玩家对象字段交给后面的当前格式 msg2/359 刷新。
	patchedPlain = bytes.ReplaceAll(patchedPlain, oldKey, newKey)
	patched, err := zlibCompress(patchedPlain)
	if err != nil {
		return body, false
	}
	return patched, true
}

func locateLongHengSceneStageObjectEntry(plain []byte, oldKey []byte) (int, int, bool) {
	keyOffset := bytes.LastIndex(plain, oldKey)
	if keyOffset < 0x47 || keyOffset+6 > len(plain) {
		return 0, 0, false
	}
	nameLen := int(binary.LittleEndian.Uint32(plain[keyOffset+2 : keyOffset+6]))
	if nameLen < 0 || nameLen > rosterNameMaxBytes {
		return 0, 0, false
	}
	nameEnd := keyOffset + 6 + nameLen
	if nameEnd > len(plain) {
		return 0, 0, false
	}
	entryEnd, ok := locateNextLongHengSceneObjectEntryStart(plain, nameEnd)
	if !ok {
		entryEnd = nameEnd + currentSceneObjectPostNameTailLength()
	}
	if entryEnd > len(plain) {
		return 0, 0, false
	}
	return keyOffset - 0x47, entryEnd, true
}

func currentSceneObjectPostNameTailLength() int {
	return len(buildCurrentSceneObjectEntryTail(dnfrepo.CharacterRecord{}, false))
}

func locateNextLongHengSceneObjectEntryStart(plain []byte, currentNameEnd int) (int, bool) {
	minKeyOffset := currentNameEnd + 0x47
	maxKeyOffset := currentNameEnd + 0x500
	if maxKeyOffset > len(plain)-6 {
		maxKeyOffset = len(plain) - 6
	}
	for keyOffset := minKeyOffset; keyOffset <= maxKeyOffset; keyOffset++ {
		key := binary.LittleEndian.Uint16(plain[keyOffset : keyOffset+2])
		if key < 0x0100 || key > 0x1000 || key == longHengSceneStageFixtureObjectKey {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint32(plain[keyOffset+2 : keyOffset+6]))
		if nameLen <= 0 || nameLen > rosterNameMaxBytes || keyOffset+6+nameLen > len(plain) {
			continue
		}
		name := plain[keyOffset+6 : keyOffset+6+nameLen]
		if !looksLikeSceneObjectName(name) {
			continue
		}
		entryStart := keyOffset - 0x47
		if entryStart > currentNameEnd {
			return entryStart, true
		}
	}
	return 0, false
}

func looksLikeSceneObjectName(name []byte) bool {
	for _, b := range name {
		if b == 0 {
			continue
		}
		if b < 0x20 {
			return false
		}
		return true
	}
	return false
}

func normalizeLongHengSceneTransportSpec(spec longHengSceneFixtureSpec) longHengSceneFixtureSpec {
	switch spec.msgID {
	case uint16(dnfenum.CmdPacketFrameLagStatistics):
		if strings.Contains(spec.file, "_body174.bin") {
			// MCP/static decode: old DOVE op194 is also checksum_flag=1; keep the transport layer.
			spec.file = "000062_05_scene_bootstrap_before_hud_cls0_op0194_ENUM_CMDPACKET_FRAME_LAG_STATISTICS_transport.bin"
			spec.marker = 1
			spec.bodyEncoded = true
			spec.bodyCodec = "dove_scene_op194_transport_zlib"
		}
	}
	return spec
}

func buildLongHengSceneMainHudInfoWireBodies() [][]byte {
	// The historical 24-record HUD phase has no current-EXE body mapping.
	// Current op583 reads a count plus u32/u32 rows and current op851 reads one
	// scalar; neither accepts the DOVE 16-byte records. Do not relabel/replay.
	return nil
}

func buildLongHengSceneTailAfterHudPacketsFrom(specs []longHengSceneFixtureSpec) []csharpSelectInitPacket {
	out := make([]csharpSelectInitPacket, 0, len(specs))
	for _, spec := range specs {
		if spec.deferred {
			continue
		}
		out = append(out, csharpSelectInitPacket{
			class:       spec.class,
			msgID:       spec.msgID,
			marker:      spec.marker,
			body:        mustLongHengSceneBody(spec),
			kind:        csharpLongHengSceneTailAfterHudKind,
			bodyEncoded: spec.bodyEncoded,
			bodyCodec:   spec.bodyCodec,
		})
	}
	return out
}

func buildLongHengSceneTailAfterHudPackets() []csharpSelectInitPacket {
	return buildLongHengSceneTailAfterHudPacketsFrom(longHengSceneTailAfterHudSpec)
}

func buildLongHengSceneImplementedTailAfterHudPackets() []csharpSelectInitPacket {
	ledger := longHengSceneTailAfterHudLedger()
	specs := make([]longHengSceneFixtureSpec, 0, len(ledger))
	for _, entry := range ledger {
		if entry.status != longHengPacketImplementedCurrentBody {
			continue
		}
		specIndex := entry.idx - longHengSceneTailStartIndex
		if specIndex < 0 || specIndex >= len(longHengSceneTailAfterHudSpec) {
			panic(fmt.Sprintf("tail implemented idx %d has no DOVE spec", entry.idx))
		}
		spec := longHengSceneTailAfterHudSpec[specIndex]
		if spec.class != entry.class || spec.msgID != entry.msgID {
			panic(fmt.Sprintf("tail implemented idx %d spec class/msg %d/%d != ledger %d/%d",
				entry.idx, spec.class, spec.msgID, entry.class, entry.msgID))
		}
		if spec.file != "" || spec.deferred {
			// A ledger entry may not promote a historical fixture into an active
			// packet.  The test suite keeps this branch visible until a current
			// builder is supplied for that DOVE ordering slot.
			continue
		}
		specs = append(specs, spec)
	}
	return buildLongHengSceneTailAfterHudPacketsFrom(specs)
}

func longHengSceneTailAfterHudLedger() []longHengScenePacketLedgerEntry {
	const phase = "07_scene_bootstrap_tail_social_guild"
	implemented := func(idx int, class byte, msgID uint16, reason string) longHengScenePacketLedgerEntry {
		return longHengScenePacketLedgerEntry{idx: idx, phase: phase, class: class, msgID: msgID, status: longHengPacketImplementedCurrentBody, reason: reason, implemented: true}
	}
	requestDriven := func(idx int, class byte, msgID uint16, reason string) longHengScenePacketLedgerEntry {
		return longHengScenePacketLedgerEntry{idx: idx, phase: phase, class: class, msgID: msgID, status: longHengPacketRequestDriven, reason: reason}
	}
	notUsed := func(idx int, class byte, msgID uint16, reason string) longHengScenePacketLedgerEntry {
		return longHengScenePacketLedgerEntry{idx: idx, phase: phase, class: class, msgID: msgID, status: longHengPacketNotUsedCurrentClient, reason: reason}
	}
	class1 := byte(dnfproto.DefaultChannelClassification)
	return []longHengScenePacketLedgerEntry{
		requestDriven(92, 0, uint16(dnfenum.CmdPacketAdventurerMakerCreate), "activity_maker_state_requires_matching_current_request_old_dove_body_excluded"),
		requestDriven(93, 0, uint16(dnfenum.CmdPacketAdventurerMakerCreate), "activity_maker_state_requires_matching_current_request_old_dove_body_excluded"),
		requestDriven(94, 0, uint16(dnfenum.CmdPacketItemHyperlinkMessage), "item_hyperlink_state_requires_matching_current_request_old_dove_body_excluded"),
		requestDriven(95, 0, uint16(dnfenum.CmdPacketApcTnBetting), "apc_betting_state_requires_matching_current_request_old_dove_body_excluded"),
		requestDriven(96, 0, uint16(dnfenum.CmdPacketTitleBookPut), "title_book_state_requires_matching_current_request_old_dove_body_excluded"),
		implemented(97, 0, currentSceneLocalStateMsgID, "current_handler_default_local_state_body_sent"),
		requestDriven(98, 0, uint16(dnfenum.CmdPacketSeriaRidableInHiddenTruthDungeon), "current_op996_local_switch_has_no_reader_or_state_owner_proactive_dove_derived_body_excluded"),
		requestDriven(99, 0, uint16(dnfenum.CmdPacketLevelupSupport3rdEventGetItem), "current_op1004_activity_reward_requires_matching_event_request_and_real_account_state"),
		requestDriven(100, class1, uint16(dnfenum.CmdPacketEventDnftrendGetReward), "current_class1_op901_event_state_requires_matching_current_event_request_no_body_reader"),
		requestDriven(101, 0, uint16(dnfenum.CmdPacketToBeZombie), "current_op542_zombie_list_requires_live_zombie_state_source_no_proactive_empty_notification"),
		notUsed(102, 0, uint16(dnfenum.CmdPacketModuleExist), "current_op581_no_network_reader_or_current_module_owner"),
		requestDriven(103, 0, uint16(dnfenum.CmdPacketRemoveCollectboxItem), "current_op1023_collectbox_result_no_body_reader_old_body8_incompatible"),
		requestDriven(104, 0, uint16(dnfenum.CmdPacketRemoveCollectboxItem), "current_op1023_collectbox_result_no_body_reader_old_body8_incompatible"),
		requestDriven(105, 0, uint16(dnfenum.CmdPacketRemoveCollectboxItem), "current_op1023_collectbox_result_no_body_reader_old_body8_incompatible"),
		requestDriven(106, 0, uint16(dnfenum.CmdPacketRemoveCollectboxItem), "current_op1023_collectbox_result_no_body_reader_old_body8_incompatible"),
		notUsed(107, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(108, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(109, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(110, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(111, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(112, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(113, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(114, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(115, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(116, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(117, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(118, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(119, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(120, 0, uint16(dnfenum.CmdPacketCrackOfDimmensionRewardJpn), "current_op1199_has_no_registered_handler_old_body16_incompatible"),
		notUsed(121, 0, uint16(dnfenum.CmdPacketUpdateBestDamageRank), "current_op1089_registered_to_DoNothing_old_body24_incompatible"),
		notUsed(122, 0, uint16(dnfenum.CmdPacketWelcombackAttendance), "current_op1145_has_no_registered_handler_old_body8_incompatible"),
		requestDriven(123, 0, uint16(dnfenum.CmdPacketReqTerritoryCombatAllianceList), "current_op825_territory_alliance_request_driven_old_body32_incompatible"),
		implemented(124, 0, uint16(dnfenum.CmdPacketHardcoreCharacList), "current_op647_empty_hardcore_list_body_sent"),
		requestDriven(125, 0, uint16(dnfenum.CmdPacketFatigueAccelerationStateChange), "current_op562_fatigue_acceleration_state_request_driven_old_body32_incompatible"),
		requestDriven(126, 0, uint16(dnfenum.CmdPacketBuyGuildContents), "current_op761_buy_guild_contents_result_no_body_reader_request_driven_old_body8_incompatible"),
		requestDriven(127, 0, uint16(dnfenum.CmdPacketRequestWarroomReward), "current_op808_warroom_reward_result_request_driven_old_body8_incompatible"),
		requestDriven(128, 0, uint16(dnfenum.CmdPacketGetAvatarSpecEvent), "current_op633_avatar_spec_event_request_driven_old_body16_incompatible"),
		notUsed(129, 0, uint16(dnfenum.CmdPacketAttendanceCheck), "current_op428_has_no_registered_handler_old_body16_incompatible"),
		notUsed(130, 0, uint16(dnfenum.CmdPacketEquipmentSwapInfo), "current_op707_has_no_registered_handler_old_body8_incompatible"),
		requestDriven(131, 0, uint16(dnfenum.CmdPacketSecedeGuildAlliance), "current_op760_guild_alliance_state_request_driven_old_body16_incompatible"),
		requestDriven(132, 0, uint16(dnfenum.CmdPacketLuckyBalloon), "current_op1264_lucky_balloon_ui_result_request_driven_old_body16_incompatible"),
		requestDriven(133, 0, uint16(dnfenum.CmdPacketGetGuildHongbaoPointList), "current_op1301_guild_hongbao_point_list_request_driven_old_body8_incompatible"),
		notUsed(134, 0, uint16(dnfenum.CmdPacketDoubleupMinigame), "current_op1316_has_no_registered_handler_old_body32_incompatible"),
		requestDriven(135, 0, uint16(dnfenum.CmdPacketCardGameCompound), "current_op1237_card_game_result_request_driven_old_body24_incompatible"),
		requestDriven(136, 0, uint16(dnfenum.CmdPacketTitleBookGet), "current_op413_title_book_get_request_driven_old_body8_incompatible"),
		requestDriven(137, 0, uint16(dnfenum.CmdPacketSetPartyInfo), "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"),
		requestDriven(138, 0, uint16(dnfenum.CmdPacketSetPartyInfo), "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"),
		requestDriven(139, 0, uint16(dnfenum.CmdPacketSetPartyInfo), "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"),
		requestDriven(140, 0, uint16(dnfenum.CmdPacketSetPartyInfo), "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"),
		requestDriven(141, 0, uint16(dnfenum.CmdPacketSetPartyInfo), "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"),
		requestDriven(142, 0, uint16(dnfenum.CmdPacketSetPartyInfo), "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"),
		requestDriven(143, 0, uint16(dnfenum.CmdPacketSetPartyInfo), "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"),
		requestDriven(144, 0, uint16(dnfenum.CmdPacketSetPVPReadyState), "current_op53_pvp_ready_state_request_driven_old_body16_incompatible"),
		requestDriven(145, class1, uint16(dnfenum.CmdPacketMercenaryCompetition), "current_class1_op442_mercenary_competition_no_body_reader_request_driven_old_body48_incompatible"),
		requestDriven(146, 0, uint16(dnfenum.CmdPacketJoustBetting), "current_op1292_joust_betting_request_driven_old_body16_incompatible"),
		requestDriven(147, class1, uint16(dnfenum.CmdPacketGuildAllMemberList), "current_class1_op140_large_guild_member_list_request_driven_old_body15962_incompatible"),
	}
}

func buildCurrentHardcoreCharacListBody() []byte {
	return []byte{0}
}

func buildCurrentSceneLocalStateBody() []byte {
	// Current class0/op491 -> sub_1D55D60 reads transition mode and local
	// state first. Modes 1..3 have feature-specific side effects, and mode 3
	// reads two more bytes. Zero/zero preserves the current EXE's initialized
	// no-transition state without replaying the old DOVE handler's body.
	return []byte{0, 0}
}

func buildCurrentUpperBlacklistResponseBody() []byte {
	// Upper/status op120 consumes the result byte followed by the date-list count.
	return []byte{0, 0}
}

func buildCurrentSceneActorPlacementBody() []byte {
	// Main/class0 op120 reads actor_slot and placement_seed without a result byte.
	return []byte{0, 0}
}

func buildCurrentInfiniteDifficultyCharacInfoBody() []byte {
	// MCP：current msg521 只读取 state；0 表示没有需要弹出的无限难度/角色状态提示。
	return []byte{0}
}

func buildCurrentJoinPowerBody() []byte {
	// MCP：current msg204 只读取 enabled_or_join_state；0 表示当前场景不启用势力/阵营状态。
	return []byte{0}
}

func buildCurrentDecreaseDurabilityBody() []byte {
	// MCP：current msg281 读取 56 字节耐久基础结构；DOVE 明文首字段为 0x2710，其余状态为空。
	body := make([]byte, 56)
	binary.LittleEndian.PutUint32(body[:4], 10000)
	return body
}

func buildCurrentRequestOverseerBody(listIndex uint32) []byte {
	// MCP 0x01D76D00：u8 mode, u16 owner, u32 list_index, u32 count。
	// 非空行还会读取固定 22 字节和 sub_3457C50 的 u32 raw_len+raw；
	// count=0 时不会读取行，适合当前进场景空 overseer 列表。
	return buildCurrentRequestOverseerBodyWithRows(0, 0, listIndex, nil)
}

func buildCurrentInsertOverseerBody() []byte {
	// Current sub_1D625C0 always consumes u32 count, count*10-byte rows, then
	// a fixed raw[16] tail. The empty finalizer is therefore 20 bytes, not 4.
	return buildCurrentInsertOverseerBodyWithRows(nil)
}

func buildLongHengSceneRuntimeAfterBlacklistPackets() []csharpSelectInitPacket {
	specs := []struct {
		class byte
		msgID uint16
		file  string
	}{
		{class: 0, msgID: uint16(dnfenum.CmdPacketSellItem), file: "runtime_after_blacklist_000149_class0_op22_body16.bin"},
		{class: dnfproto.DefaultChannelClassification, msgID: uint16(dnfenum.CmdPacketWeddingResponse), file: "runtime_after_blacklist_000150_class1_op1033_body104.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000151_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000152_class0_op9_body16.bin"},
		{class: dnfproto.DefaultChannelClassification, msgID: uint16(dnfenum.CmdPacketWeddingResponse), file: "runtime_after_blacklist_000153_class1_op1033_body104.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000154_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000155_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000156_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000157_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000158_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000159_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000160_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000161_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000162_class0_op6_body8.bin"},
		// 这里的 op2 来自 DOVE 08_in_scene_runtime_updates；当前 NoPack 静态对照显示它是 op356
		// 对象/状态块族，不是前面 zlib 场景 bootstrap 的 op1021。
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000163_class0_op2_body415.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000164_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000165_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000166_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000167_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000168_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000169_class0_op9_body16.bin"},
		// 000170..000177 是 DOVE REQUEST_BLACKLIST 之后到下一段大 op2 的最小连续运行态片段。
		{class: 0, msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "runtime_after_blacklist_000170_class0_op12_body48.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000171_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000172_class0_op6_body8.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000173_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000174_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000175_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000176_class0_op9_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000177_class0_op2_body417.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "runtime_after_blacklist_000178_class0_op12_body48.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000179_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000180_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000181_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000182_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000183_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000184_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000185_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000186_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000187_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000188_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000189_class0_op6_body8.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000190_class0_op9_body64.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000191_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000192_class0_op9_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000193_class0_op2_body360.bin"},
		// 000194..000209 继续沿用 DOVE REQUEST_BLACKLIST 后运行态顺序，只补到下一段大 op2，避免一次恢复过多社交/活动 tail。
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000194_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000195_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000196_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000197_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketSetLabyrinthSeatState), file: "runtime_after_blacklist_000198_class0_op380_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000199_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000200_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000201_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000202_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000203_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000204_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000205_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000206_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000207_class0_op6_body8.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000208_class0_op3_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000209_class0_op2_body419.bin"},
		// 000210..000220 继续补到下一段 DOVE 小运行态和大 op2，观察客户端是否进入地图/加载完成分支。
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000210_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000211_class0_op2_body254.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000212_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000213_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000214_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000215_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000216_class0_op6_body8.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000217_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000218_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000219_class0_op3_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000220_class0_op2_body402.bin"},
		// 000221..000231 仍然是 DOVE 运行态小包到下一段大 op2，不恢复会主动打开社交/活动 UI 的 HUD 后 tail。
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000221_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000222_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000223_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000224_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000225_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000226_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000227_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000228_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000229_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000230_class0_op9_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000231_class0_op2_body421.bin"},
		// 000232..000247 对齐 DOVE idx 998..1013；继续跳过后续 op12 队伍 UI，避免主动打开侧边面板。
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000232_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000233_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000234_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000235_class0_op3_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000236_class0_op2_body425.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000237_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000238_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000239_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000240_class0_op2_body431.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketSellItem), file: "runtime_after_blacklist_000241_class0_op22_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRepairEquipment), file: "runtime_after_blacklist_000242_class0_op23_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000243_class0_op2_body425.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000244_class0_op2_body425.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000245_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000246_class0_op2_body202.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000247_class0_op3_body16.bin"},
		// 000248..000303 对齐 DOVE idx 1014..1069；op12 继续由黑名单过滤，其余为 runtime 小包/对象块。
		{class: 0, msgID: uint16(dnfenum.CmdPacketSetPartyInfo), file: "runtime_after_blacklist_000248_class0_op12_body48.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000249_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000250_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000251_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000252_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000253_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000254_class0_op2_body202.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000255_class0_op2_body204.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000256_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000257_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000258_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000259_class0_op2_body200.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000260_class0_op6_body8.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000261_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000262_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000263_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000264_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000265_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000266_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000267_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000268_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000269_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000270_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000271_class0_op2_body200.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000272_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000273_class0_op2_body202.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000274_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000275_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000276_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000277_class0_op2_body204.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000278_class0_op3_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000279_class0_op2_body403.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000280_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000281_class0_op2_body229.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000282_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000283_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000284_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000285_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000286_class0_op6_body8.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000287_class0_op3_body16.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000288_class0_op2_body377.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000289_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000290_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000291_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000292_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000293_class0_op2_body383.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000294_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000295_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000296_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000297_class0_op2_body403.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000298_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketRecoverStamina), file: "runtime_after_blacklist_000299_class0_op9_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000300_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketExit), file: "runtime_after_blacklist_000301_class0_op3_body16.bin"},
		{class: 0, msgID: uint16(dnfenum.CmdPacketDeleteCharacter), file: "runtime_after_blacklist_000302_class0_op6_body8.bin"},
		{class: 0, msgID: longHengCurrentRuntimeObjectStateMsgID, file: "runtime_after_blacklist_000303_class0_op2_body396.bin"},
	}
	out := make([]csharpSelectInitPacket, 0, longHengRuntimeAfterBlacklistSafeRawCount)
	seen := make(map[string]struct{}, longHengRuntimeAfterBlacklistSafeRawCount)
	for _, spec := range specs {
		seen[spec.file] = struct{}{}
		out = append(out, buildRuntimeAfterBlacklistPacket(spec.class, spec.msgID, spec.file))
	}
	return appendAdditionalLongHengSceneRuntimeAfterBlacklistPackets(out, seen)
}

func appendAdditionalLongHengSceneRuntimeAfterBlacklistPackets(out []csharpSelectInitPacket, seen map[string]struct{}) []csharpSelectInitPacket {
	entries, err := longHengSceneFixtureFS.ReadDir("fixtures/dove_scene")
	if err != nil {
		panic(fmt.Sprintf("read DOVE scene fixture dir: %v", err))
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "runtime_after_blacklist_") || !strings.HasSuffix(name, ".bin") {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		class, msgID, err := parseLongHengRuntimeAfterBlacklistFixtureName(name)
		if err != nil {
			panic(fmt.Sprintf("parse DOVE runtime fixture %s: %v", name, err))
		}
		out = append(out, buildRuntimeAfterBlacklistPacket(class, msgID, name))
	}
	return out
}

func buildRuntimeAfterBlacklistPacket(class byte, msgID uint16, file string) csharpSelectInitPacket {
	isRuntimeSceneObject := class == 0 && strings.Contains(file, "_class0_op2_")
	if isRuntimeSceneObject {
		// Keep old DOVE op2 visible while the current object-stream structure is pending.
		// S2C op356 is the unrelated completed-quest table, so it cannot replace
		// any old object-stream row.
		msgID = uint16(dnfenum.CmdPacketSetUDPIPPort)
	}
	packet := csharpSelectInitPacket{
		class: class,
		msgID: msgID,
		file:  file,
		body:  currentRuntimeAfterBlacklistBody(class, msgID, file),
		kind:  csharpLongHengSceneRuntimeAfterBlacklistKind,
	}
	return packet
}

// buildCurrentPassGateObjectBody is retained only for historical fixture-audit
// tests. The generated enum's PASS_GATE_OBJECT name was an early direction
// collision; the S2C body is the completed-quest clear table.
func buildCurrentPassGateObjectBody() []byte {
	return buildCurrentClearQuestListBody(dnfrepo.QuestRecord{}, false)
}

func buildCurrentPassGateObjectTransportBody() []byte {
	transport, err := buildCurrentClearQuestListTransportBody(dnfrepo.QuestRecord{}, false)
	if err != nil {
		panic(fmt.Sprintf("compress current op356 clear-quest body: %v", err))
	}
	return transport
}

func currentRuntimeAfterBlacklistBody(class byte, msgID uint16, file string) []byte {
	if class == 0 {
		switch msgID {
		case uint16(dnfenum.CmdPacketRecoverStamina):
			return buildCurrentSceneOp9NoopBody()
		case uint16(dnfenum.CmdPacketExit):
			return buildCurrentRuntimeExitSceneBody()
		case uint16(dnfenum.CmdPacketDeleteCharacter):
			return buildCurrentRuntimeDeleteCharacterBody()
		case longHengCurrentRuntimeObjectStateMsgID:
			// MCP 0x03457C50/0x01D58470 确认当前 356 只读取 u32 长度 + 30000 字节对象位图。
			// DOVE 旧 op2 body 首 DWORD 是对象内容，直接映射到 356 会被当成超大长度读取并触发 0x30303030 崩溃。
			return buildCurrentPassGateObjectBody()
		}
	}
	return mustLongHengSceneBody(longHengSceneFixtureSpec{file: file})
}

func buildCurrentRuntimeExitSceneBody() []byte {
	// MCP/runtime dump 0x0160C7D0：当前 class0/op3 读取 u16 后读取 u8 group_count。
	// 旧 DOVE op3 的 16 字节 dispatcher 体第三字节会被当成 count，必须按当前 reader 回写空组。
	var writer packetWriter
	writer.writeUint16(0)
	writer.writeByte(0)
	return writer.bytes()
}

func buildCurrentRuntimeDeleteCharacterBody() []byte {
	// MCP/runtime dump 0x015DB030：当前 class0/op6 入口为 no-op ret，不读取包体。
	return nil
}

func parseLongHengRuntimeAfterBlacklistFixtureName(name string) (byte, uint16, error) {
	stem := strings.TrimSuffix(name, ".bin")
	parts := strings.Split(stem, "_")
	if len(parts) < 7 {
		return 0, 0, fmt.Errorf("fixture name has %d parts", len(parts))
	}
	classPart := parts[4]
	opPart := parts[5]
	if !strings.HasPrefix(classPart, "class") || !strings.HasPrefix(opPart, "op") {
		return 0, 0, fmt.Errorf("missing class/op part")
	}
	classValue, err := strconv.ParseUint(strings.TrimPrefix(classPart, "class"), 10, 8)
	if err != nil {
		return 0, 0, fmt.Errorf("parse class: %w", err)
	}
	opValue, err := strconv.ParseUint(strings.TrimPrefix(opPart, "op"), 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("parse op: %w", err)
	}
	return byte(classValue), uint16(opValue), nil
}

func longHengSceneRuntimeAfterBlacklistPrefixLedger() []longHengScenePacketLedgerEntry {
	count := longHengRuntimeAfterBlacklistSafeRawCount
	if count > len(longHengSceneRuntimeAfterBlacklistPackets) {
		count = len(longHengSceneRuntimeAfterBlacklistPackets)
	}
	out := make([]longHengScenePacketLedgerEntry, 0, count)
	for idx, packet := range longHengSceneRuntimeAfterBlacklistPackets[:count] {
		status, reason := longHengSceneRuntimePacketLedgerStatus(packet.class, packet.msgID)
		out = append(out, longHengScenePacketLedgerEntry{
			idx:         149 + idx,
			phase:       "08_in_scene_runtime_updates",
			class:       packet.class,
			msgID:       packet.msgID,
			status:      status,
			reason:      reason,
			implemented: status == longHengPacketImplementedCurrentBody,
		})
	}
	return out
}

func longHengSceneRuntimePacketLedgerStatus(class byte, msgID uint16) (longHengScenePacketStatus, string) {
	if class == 0 && msgID == uint16(dnfenum.CmdPacketRecoverStamina) {
		return longHengPacketNotUsedCurrentClient, "current_op9_zero_record_noop_has_no_runtime_state_owner_proactive_dove_order_excluded"
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketExit) {
		return longHengPacketNotUsedCurrentClient, "current_op3_empty_group_body_has_no_runtime_state_owner_proactive_dove_order_excluded"
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketDeleteCharacter) {
		return longHengPacketNotUsedCurrentClient, "current_op6_no_read_body_has_no_runtime_state_owner_proactive_dove_order_excluded"
	}
	if longHengSceneRuntimePacketAllowedAfterBlacklist(class, msgID) {
		return longHengPacketImplementedCurrentBody, "currently_sent_in_runtime_safe_prefix"
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSetUDPIPPort) {
		return longHengPacketPendingCurrentStruct, "old_dove_op2_runtime_object_stream_not_current_msg2_and_unrelated_to_op356_clear_quest_list"
	}
	if class == 0 && msgID == longHengCurrentRuntimeObjectStateMsgID {
		return longHengPacketPendingCurrentStruct, "old_dove_op2_object_body_incompatible_with_current_op356_completed_quest_table"
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSellItem) {
		return longHengPacketPendingCurrentStruct, "current_op22_sub_1D83990_reads_u16_actor_u16_x_u16_y_u8_state_u16_value_old_dove_body16_incompatible"
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketRepairEquipment) {
		return longHengPacketPendingCurrentStruct, "current_class0_op23_sub_1D89590_reads_10_byte_actor_scene_state_class1_op23_is_separate_repair_result_old_dove_body16_incompatible"
	}
	if class == dnfproto.DefaultChannelClassification && msgID == uint16(dnfenum.CmdPacketWeddingResponse) {
		return longHengPacketRequestDriven, "current_op1033_wedding_response_is_request_driven_ui_state_not_runtime_scene_state"
	}
	if class == dnfproto.DefaultChannelClassification && msgID == uint16(dnfenum.CmdPacketExit) {
		return longHengPacketNotUsedCurrentClient, "current_upper_msg3_has_no_network_reader_and_triggers_ui_transition_not_runtime_scene_state"
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSetLabyrinthSeatState) {
		return longHengPacketRequestDriven, "current_op380_reads_u8_u16_count_then_labyrinth_entries_and_triggers_activity_ui"
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSetPartyInfo) {
		return longHengPacketRequestDriven, "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible"
	}
	if longHengSceneProactiveReplayPacketBlocked(class, msgID) {
		return longHengPacketRequestDriven, "active_ui_or_social_packet_must_wait_for_current_request_or_real_state"
	}
	return longHengPacketPendingCurrentStruct, "filtered_runtime_packet_needs_current_reader"
}

func longHengSceneRuntimeAfterBlacklistSafePrefixPackets() []csharpSelectInitPacket {
	// The old DOVE runtime suffix is only ordering evidence. Its repeated
	// op3/op6/op9 packets have no current scene-state owner; even rewritten as
	// valid empty readers they are still a replay-driven sequence. The selected
	// actor's real mode1/mode3 and container state are sent by their repository
	// owners before this callback, so no proactive packet belongs here.
	return nil
}

func longHengSceneRuntimePacketAllowedAfterBlacklist(class byte, msgID uint16) bool {
	if class == 0 && msgID == uint16(dnfenum.CmdPacketRecoverStamina) {
		// Before-HUD current op9 actor/display is validated to the runtime seed.
		// Runtime old op9 rows are rewritten to the current zero-record no-op body,
		// avoiding all per-record and raw_len/read_raw branches.
		return true
	}
	if class == 0 && msgID == longHengCurrentRuntimeObjectStateMsgID {
		// Runtime old op2 rows were remapped to current op356 during an earlier
		// merge. op356 is a completed-quest table and has no object-stream
		// ownership, so historical rows remain excluded.
		return false
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSetUDPIPPort) {
		// These are old DOVE runtime object-stream bodies, not current msg2
		// cleartext. Keep them accounted for but out of the send prefix.
		return false
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketRepairEquipment) {
		// Current class0/op23 -> sub_1D89590 consumes a 10-byte actor/scene
		// state update. The old DOVE body16 is a different layout. Class1/op23
		// remains the separate request-driven equipment-repair result.
		return false
	}
	if class == dnfproto.DefaultChannelClassification && msgID == uint16(dnfenum.CmdPacketWeddingResponse) {
		// Wedding response updates UI/request state and is not a runtime scene-state packet.
		return false
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSellItem) {
		// Current class0/op22 -> sub_1D83990 consumes u16 actor key, u16 X,
		// u16 Y, u8 state/direction, and u16 value. Login placement belongs to
		// op24; old DOVE body16 is not a current typed actor update.
		return false
	}
	if class == dnfproto.DefaultChannelClassification && msgID == uint16(dnfenum.CmdPacketExit) {
		return false
	}
	return !longHengSceneProactiveReplayPacketBlocked(class, msgID)
}

// longHengSceneProactiveReplayPacketBlocked protects only the old DOVE-derived
// unsolicited S2C replay path. It is not an opcode ban: typed repository,
// request, dungeon, party, equipment, and scene owners do not consult it.
func longHengSceneProactiveReplayPacketBlocked(class byte, msgID uint16) bool {
	switch class {
	case 0:
		switch msgID {
		case uint16(dnfenum.CmdPacketSetPartyInfo),
			uint16(dnfenum.CmdPacketLeaveParty),
			uint16(dnfenum.CmdPacketWalkoutPartyMember),
			uint16(dnfenum.CmdPacketReport4Hack),
			uint16(dnfenum.CmdPacketGetAvatarSpecEvent),
			uint16(dnfenum.CmdPacketAttendanceCheck),
			uint16(dnfenum.CmdPacketSetLabyrinthSeatState),
			uint16(dnfenum.CmdPacketWelcombackAttendance),
			uint16(dnfenum.CmdPacketGiftOfSeria),
			uint16(dnfenum.CmdPacketLuckyBalloon),
			uint16(dnfenum.CmdPacketDiePVPCharacter),
			uint16(dnfenum.CmdPacketUseSharedEffectItem),
			uint16(dnfenum.CmdPacketAboutHope),
			uint16(dnfenum.CmdPacketGetGuildHongbaoPointList):
			return true
		}
	case dnfproto.DefaultChannelClassification:
		switch msgID {
		case uint16(dnfenum.CmdPacketContentsPlayInfo),
			uint16(dnfenum.CmdPacketLetsPickPresent),
			uint16(dnfenum.CmdPacketMercenaryInfo),
			uint16(dnfenum.CmdPacketMercenaryCompetition),
			uint16(dnfenum.UpperMsgLoadExtendCharacs),
			uint16(dnfenum.UpperMsgCharacSlotExtendEffect),
			uint16(dnfenum.CmdPacketGuildAllMemberList):
			return true
		}
	}
	return false
}

func mustLongHengSceneBody(spec longHengSceneFixtureSpec) []byte {
	wrap := func(body []byte) []byte {
		if spec.trimDstrZeroTail {
			body = trimLongHengDstrZeroTail(body)
		}
		if spec.successEnvelope {
			return upperSuccessBody(body)
		}
		return body
	}
	if spec.body != nil {
		return wrap(spec.body)
	}
	if spec.file == "" {
		return nil
	}
	body, err := longHengSceneFixtureFS.ReadFile("fixtures/dove_scene/" + spec.file)
	if err != nil {
		panic(fmt.Sprintf("read DOVE scene fixture %s: %v", spec.file, err))
	}
	return wrap(body)
}

func trimLongHengDstrZeroTail(body []byte) []byte {
	if len(body) < 4 {
		return body
	}
	rawLen := int(binary.LittleEndian.Uint32(body[:4]))
	if rawLen < 0 || rawLen > len(body)-4 {
		return body
	}
	raw := body[4 : 4+rawLen]
	lastNonZero := -1
	for i := len(raw) - 1; i >= 0; i-- {
		if raw[i] != 0 {
			lastNonZero = i
			break
		}
	}
	trimmedLen := 0
	if lastNonZero >= 0 {
		trimmedLen = lastNonZero + 1
	}
	out := make([]byte, 4+trimmedLen)
	binary.LittleEndian.PutUint32(out[:4], uint32(trimmedLen))
	copy(out[4:], raw[:trimmedLen])
	return out
}
