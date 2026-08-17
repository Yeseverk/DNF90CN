// 本文件验证已对齐 DNF 协议号的模块化注册表。
package dnfenum

import "testing"

func TestLookupAlignedCommandDirectDomains(t *testing.T) {
	tests := []struct {
		name    string
		opcode  CmdPacket
		domain  AlignedDomain
		support AlignedSupport
	}{
		{name: "inventory delete", opcode: CmdPacketDeleteItem, domain: AlignedDomainInventory, support: AlignedSupportDirect},
		{name: "pet rename", opcode: CmdPacketRenameCreature, domain: AlignedDomainPet, support: AlignedSupportDirect},
		{name: "pet hatch egg", opcode: CmdPacketHatchCreatureEgg, domain: AlignedDomainPet, support: AlignedSupportDirect},
		{name: "skill slot", opcode: CmdPacketChangeSkillslot, domain: AlignedDomainSkill, support: AlignedSupportDirect},
		{name: "skill init", opcode: CmdPacketSkillInit, domain: AlignedDomainSkill, support: AlignedSupportDirect},
		{name: "account cargo", opcode: CmdPacketCreateAccountCargo, domain: AlignedDomainCargo, support: AlignedSupportDirect},
		{name: "mailbox open", opcode: CmdPacketMailboxOpen, domain: AlignedDomainMail, support: AlignedSupportDirect},
		{name: "mailbox send", opcode: CmdPacketMailboxSend, domain: AlignedDomainMail, support: AlignedSupportDirect},
		{name: "mailbox extract", opcode: CmdPacketMailboxExtractItem, domain: AlignedDomainMail, support: AlignedSupportDirect},
		{name: "mailbox state", opcode: CmdPacketChangeLetterStat, domain: AlignedDomainMail, support: AlignedSupportDirect},
		{name: "mailbox account roles", opcode: CmdPacketRequestServerCharacterList, domain: AlignedDomainMail, support: AlignedSupportDirect},
		{name: "party set info", opcode: CmdPacketSetPartyInfo, domain: AlignedDomainParty, support: AlignedSupportDirect},
		{name: "dungeon pick item", opcode: CmdPacketGetItem, domain: AlignedDomainDungeon, support: AlignedSupportDirect},
		{name: "clone title animation", opcode: CmdPacketSetCloneTitle, domain: AlignedDomainAvatarTitle, support: AlignedSupportDirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LookupAlignedCommand(uint16(tt.opcode))
			if !ok {
				t.Fatalf("LookupAlignedCommand(%d) not found", tt.opcode)
			}
			if got.Domain != tt.domain || got.Support != tt.support {
				t.Fatalf("LookupAlignedCommand(%d) = domain %q support %q, want domain %q support %q",
					tt.opcode, got.Domain, got.Support, tt.domain, tt.support)
			}
		})
	}
}

func TestConclusionACommandsAreDirect(t *testing.T) {
	directA := []CmdPacket{
		CmdPacketSelectCharacter,
		CmdPacketCreateCharacter,
		CmdPacketDeleteCharacter,
		CmdPacketReturnSelectCharacter,
		CmdPacketGetUserinfo,
		CmdPacketCheckDoubleCharacterName,
		CmdPacketSetPartyInfo,
		CmdPacketLeaveParty,
		CmdPacketCallPartyMemberRealtimeInfo,
		CmdPacketDeleteItem,
		CmdPacketMoveItemspace,
		CmdPacketSortItem,
		CmdPacketBuyItem,
		CmdPacketSellItem,
		CmdPacketRepairEquipment,
		CmdPacketDisjointItem,
		CmdPacketUseStackable,
		CmdPacketUpgradeItem,
		CmdPacketEnchantByBead,
		CmdPacketRenameCreature,
		CmdPacketHatchCreature,
		CmdPacketHatchCreatureEgg,
		CmdPacketRequestHatchedCreature,
		CmdPacketUseBoosterItem,
		CmdPacketUseRandomboxItem,
		CmdPacketCompoundAvatar,
		CmdPacketUseEmblem,
		CmdPacketAddAvatarSocket,
		CmdPacketTitleBookPut,
		CmdPacketTitleBookGet,
		CmdPacketSetCloneTitle,
		CmdPacketChangeSkillslot,
		CmdPacketBuySkill,
		CmdPacketSkillInit,
		CmdPacketChangeAnotherSkillTree,
		CmdPacketSkillCommandCustomizing,
		CmdPacketSkillCommandAllDefault,
		CmdPacketCreateAccountCargo,
		CmdPacketUpgradeAccountCargo,
		CmdPacketDepositMoney,
		CmdPacketWithdrawMoney,
		CmdPacketRequestItemLock,
		CmdPacketRequestItemUnlock,
		CmdPacketRequestItemUnlockCancel,
		CmdPacketEnterSelectDungeon,
		CmdPacketSelectDungeon,
		CmdPacketGetItem,
		CmdPacketMoveMap,
		CmdPacketAcceptQuest,
		CmdPacketGiveupQuest,
		CmdPacketSetQuestTrigger,
		CmdPacketFinishQuest,
		CmdPacketMailboxSend,
		CmdPacketMailboxExtractItem,
		CmdPacketMailboxOpen,
		CmdPacketChangeLetterStat,
		CmdPacketMultiMailboxSend,
		CmdPacketQueryCharacInfoMailbox,
		CmdPacketRequestServerCharacterList,
	}
	for _, opcode := range directA {
		got, ok := LookupAlignedCommand(uint16(opcode))
		if !ok {
			t.Fatalf("A command %d missing from aligned commands", opcode)
		}
		if got.Support != AlignedSupportDirect {
			t.Fatalf("A command %d support = %q, want %q", opcode, got.Support, AlignedSupportDirect)
		}
	}
}

func TestPartyMCPPartialCommands(t *testing.T) {
	partial := []CmdPacket{
		CmdPacketRegisterQuickParty,
		CmdPacketDirectEntranceDungeonQuickParty,
		CmdPacketReserveLeaveParty,
		CmdPacketChangePartyMemberPosition,
		CmdPacketEntryIntoParty,
		CmdPacketEntryIntoPartyFinish,
	}
	for _, opcode := range partial {
		got, ok := LookupAlignedCommand(uint16(opcode))
		if !ok {
			t.Fatalf("party partial command %d missing from aligned commands", opcode)
		}
		if got.Domain != AlignedDomainParty || got.Support != AlignedSupportPartial {
			t.Fatalf("party partial command %d = domain %q support %q", opcode, got.Domain, got.Support)
		}
	}
}

func TestRaidMCPPartialCommands(t *testing.T) {
	partial := []CmdPacket{
		CmdPacketCreateRaid,
		CmdPacketLeaveRaid,
		CmdPacketStartRaid,
		CmdPacketSetRaidWaiting,
		CmdPacketRejoinRaid,
		CmdPacketRaidManagerWork,
		CmdPacketModifyRaidInfo,
		CmdPacketRaidOtherChannelRequestJoin,
		CmdPacketRaidMemberChangeState,
		CmdPacketRaidUserMoveChannelFail,
		CmdPacketRaidOtherChannelList,
		CmdPacketRaidCheckRaidUser,
	}
	for _, opcode := range partial {
		got, ok := LookupAlignedCommand(uint16(opcode))
		if !ok {
			t.Fatalf("raid partial command %d missing from aligned commands", opcode)
		}
		if got.Domain != AlignedDomainRaid || got.Support != AlignedSupportPartial {
			t.Fatalf("raid partial command %d = domain %q support %q", opcode, got.Domain, got.Support)
		}
	}
}

func TestLookupBlockedMigration(t *testing.T) {
	if _, ok := LookupAlignedCommand(uint16(CmdPacketGetItembox)); ok {
		t.Fatalf("519 must not be treated as a reusable package command")
	}
	blocked, ok := LookupBlockedMigration(uint16(CmdPacketGetItembox))
	if !ok {
		t.Fatalf("LookupBlockedMigration(519) not found")
	}
	if blocked.Domain != AlignedDomainPackage {
		t.Fatalf("blocked 519 domain = %q, want %q", blocked.Domain, AlignedDomainPackage)
	}
}

func TestCurrentEXEBlocksOldCharacterSkillInfoMapping(t *testing.T) {
	if _, ok := LookupAlignedCommand(uint16(CmdPacketRequestCharacSkillInfo)); ok {
		t.Fatalf("484 must not be treated as a current EXE skill command")
	}
	blocked, ok := LookupBlockedMigration(uint16(CmdPacketRequestCharacSkillInfo))
	if !ok || blocked.Domain != AlignedDomainSkill {
		t.Fatalf("blocked 484 = %+v, found=%t", blocked, ok)
	}
}

func TestAlignedCommandsSortedCopy(t *testing.T) {
	commands := AlignedCommands()
	if len(commands) == 0 {
		t.Fatalf("AlignedCommands is empty")
	}
	for i := 1; i < len(commands); i++ {
		if commands[i-1].Opcode > commands[i].Opcode {
			t.Fatalf("AlignedCommands not sorted at %d: %d > %d", i, commands[i-1].Opcode, commands[i].Opcode)
		}
	}
	commands[0].Note = "changed by test"
	again := AlignedCommands()
	if again[0].Note == "changed by test" {
		t.Fatalf("AlignedCommands returned mutable backing data")
	}
}
