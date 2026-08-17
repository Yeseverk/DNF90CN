// Package dnfenum 集中维护 DNF 协议和频道目录枚举。
package dnfenum

// ChannelMsg 是频道服协议的消息号。
type ChannelMsg uint8

const (
	MsgCSAskChannelInfo    ChannelMsg = 1
	MsgCSUpdateChannelInfo ChannelMsg = 2
	MsgSCAskChannelInfo    ChannelMsg = 3
	MsgCSNoticeChannel     ChannelMsg = 4
	MsgCSCheckScript       ChannelMsg = 5
	MsgSCCheckScript       ChannelMsg = 6
	MsgCSAskChannelScript  ChannelMsg = 7
	MsgSCAskChannelScript  ChannelMsg = 8
	MsgCSGetScript         ChannelMsg = 9
	MsgSCGetScript         ChannelMsg = 10
	MsgCSConnect           ChannelMsg = 11
	MsgSCConnect           ChannelMsg = 12
	MsgCSGetGCInfo         ChannelMsg = 13
	MsgSCGetGCInfo         ChannelMsg = 14
	MsgCBGetChannelInfo    ChannelMsg = 15
	MsgBCGetChannelInfo    ChannelMsg = 16
	MsgCSAskChannelInfoNew ChannelMsg = 17
	MsgSCAskChannelInfoNew ChannelMsg = 18
)

// LegacyChannelMsg 是 NoPack/DOF 登录阶段仍在使用的旧式频道消息号。
type LegacyChannelMsg uint16

const (
	LegacyMsgDofLoginPreface LegacyChannelMsg = 11019
	LegacyMsgDofGetScript    LegacyChannelMsg = 2825
	LegacyMsgDofChannelProbe LegacyChannelMsg = LegacyMsgDofGetScript
	LegacyMsgDofScript       LegacyChannelMsg = 2826
	LegacyMsgDofLoginAck     LegacyChannelMsg = 12044
	LegacyMsgAskChannelInfo  LegacyChannelMsg = 8977
	LegacyMsgChannelInfo     LegacyChannelMsg = 28434
)

// GameCmd 是最新 game 业务包的 cmd 分类。
type GameCmd byte

const (
	GameCmdNotice  GameCmd = 0
	GameCmdCommand GameCmd = 1
)

// GameType 是当前 dnfbridge 已确认的最新 game 业务包 type。
// 它和 NoPack.exe 的 CmdPacket 运行时名表分开维护，避免把未验证的名字表
// 直接改成服务端行为分发。
type GameType uint16

const (
	GameTypeLogin                GameType = 1
	GameTypeCharacterList        GameType = 2
	GameTypeSelectCharacter      GameType = GameType(CmdPacketSelectCharacter)
	GameTypeCreateCharacter      GameType = 5
	GameTypeGetUserInfo          GameType = 8
	GameTypeEnterSelectDungeon   GameType = GameType(CmdPacketEnterSelectDungeon)
	GameTypeSelectDungeon        GameType = GameType(CmdPacketSelectDungeon)
	GameTypeFinishLoading        GameType = 37
	GameTypeMercenaryCompetition GameType = GameType(CmdPacketMercenaryCompetition)
	GameTypeDailyChallenge       GameType = 646
	GameTypeStaticsRuntimeTing   GameType = GameType(CmdPacketStaticsRuntimeTing)
	GameTypeClientCheck          GameType = GameTypeStaticsRuntimeTing
	GameTypeContentsPlayInfo     GameType = GameType(CmdPacketContentsPlayInfo)
	GameTypeCheckName            GameType = GameType(CmdPacketCheckDoubleCharacterName)
)

// UpperMsg 是 game 连接上 raw upper 包的消息号。
type UpperMsg uint16

const (
	UpperMsgGameEndpoint           UpperMsg = 1
	UpperMsgCharacterRoster        UpperMsg = 2
	UpperMsgSetUDPIPPort           UpperMsg = UpperMsg(CmdPacketSetUDPIPPort)
	UpperMsgSelectCharacter        UpperMsg = UpperMsg(CmdPacketSelectCharacter)
	UpperMsgInit                   UpperMsg = UpperMsgSelectCharacter
	UpperMsgCreateCharacter        UpperMsg = 5
	UpperMsgSelectAck              UpperMsg = 7
	UpperMsgGetUserInfo            UpperMsg = UpperMsg(CmdPacketGetUserinfo)
	UpperMsgSelectStart            UpperMsg = 15
	UpperMsgSelectEnter            UpperMsg = 16
	UpperMsgFollowUpStatus         UpperMsg = 63
	UpperMsgFollowUpReady          UpperMsg = 120
	UpperMsgLoadExtendCharacs      UpperMsg = UpperMsg(CmdPacketLoadExtendCharacs)
	UpperMsgCharacterPage          UpperMsg = UpperMsgLoadExtendCharacs
	UpperMsgCreatePostState        UpperMsg = 689
	UpperMsgStaticsRuntimeTing     UpperMsg = UpperMsg(CmdPacketStaticsRuntimeTing)
	UpperMsgClientCheck            UpperMsg = UpperMsgStaticsRuntimeTing
	UpperMsgLetsPickPresent        UpperMsg = UpperMsg(CmdPacketLetsPickPresent)
	UpperMsgInformNotice           UpperMsg = UpperMsg(CmdPacketInformNotice)
	UpperMsgRebirthHardcoreCharac  UpperMsg = UpperMsg(CmdPacketRebirthHardcoreCharac)
	UpperMsgCheckDoubleCharName    UpperMsg = UpperMsg(CmdPacketCheckDoubleCharacterName)
	UpperMsgCheckCharacterGate     UpperMsg = 693
	UpperMsgMercenaryInfo          UpperMsg = UpperMsg(CmdPacketMercenaryInfo)
	UpperMsgMercenaryCompetition   UpperMsg = UpperMsg(CmdPacketMercenaryCompetition)
	UpperMsgCharacViewHiddenInfo   UpperMsg = UpperMsg(CmdPacketCharacViewHiddenCharacInfo)
	UpperMsgCharacSlotExtendEffect UpperMsg = UpperMsg(CmdPacketCharacSlotExtendEffect)
	UpperMsgCheckUserConnection    UpperMsg = UpperMsg(CmdPacketCheckUserConnection)
	UpperMsgAntibot                UpperMsg = UpperMsg(CmdPacketAntibot)
	UpperMsgDprotoCallback         UpperMsg = UpperMsg(CmdPacketDprotoCallback)
	UpperMsgGuardControl           UpperMsg = 889 // 0x0379
)

const (
	ChannelPacketClass         = 124
	LoginChannelServerIndex    = 1
	DefaultGameChannelID       = 11
	BootstrapChannelID         = 19
	DefaultAutoChannelID       = 38
	DefaultAutoChannelType     = 3
	GamePortBase               = 10000
	ChannelNamePrefix          = "ch."
	DefaultChannelMaxUsers     = 500
	DefaultChannelCurrentUsers = 0
)

const (
	GroupCain       = "cain"
	GroupTrade      = "trade"
	GroupDeathTower = "deathtower"
	GroupCrack      = "crack"
	GroupRaid       = "raid"
	GroupAttackRaid = "attackraid"
)

const (
	TradeChannelType      = 3
	DeathTowerChannelType = 11
	RaidChannelType       = 23
	HiddenRaidType        = 32
)
