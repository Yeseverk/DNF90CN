package dnfenum

const (
	// CmdPacketNotifyUserState is the current NoPack server-to-client class-0
	// user-state notification. The generated CmdPacket table names opcode 3
	// from the client-to-server direction, where the same number means Exit.
	CmdPacketNotifyUserState CmdPacket = 3

	// CmdPacketNotifyDieMonster is the current NoPack server-to-client class-0
	// death notification. The generated CmdPacket table names opcode 38 from the
	// client-to-server direction, where the same number means UseSkill.
	CmdPacketNotifyDieMonster CmdPacket = 38

	// CmdPacketNotifyBossDieCheck is the current NoPack server-to-client class-0
	// tutorial/Boss completion notification. The generated CmdPacket table names
	// opcode 115 from the client-to-server direction as TraceError.
	CmdPacketNotifyBossDieCheck CmdPacket = 115
)
