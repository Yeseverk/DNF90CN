package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestFinishLoadingStatusThenMainCharacterStateUsesRealRecord(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "account-1",
		Job:         "15",
		Level:       7,
		Stats: map[string]int64{
			"exp":                                 1234,
			"stat_hp_max":                         1700,
			"stat_mp_max":                         1500,
			"stat_inventory_limit":                450000,
			"stat_weight":                         500000,
			"sp_style_0":                          11,
			"sp_style_1":                          12,
			"tp_style_0":                          13,
			"tp_style_1":                          14,
			"finish_loading_currency_slot2_total": 99,
			"finish_loading_exp_category_1":       21,
			"finish_loading_independent_scalar":   22,
			// These obsolete generic keys deliberately conflict with the
			// valid neutral HonorExpert state. They must not reach op37.
			"finish_loading_auxiliary_state_id":      23,
			"finish_loading_auxiliary_state_value_0": 24,
			"finish_loading_auxiliary_state_value_1": 25,
		},
	}
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    116000011,
				RawEntry:  buildInitialEquipmentRawEntry(11, 116000011, 45),
				Extra:     map[string]string{"source": "pvf_create_equipment_list"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Skill.Save(context.Background(), dnfrepo.SkillRecord{
		CharacterID: "77",
		Points: dnfrepo.SkillPointState{
			TotalSP: 11, RemainingSP: 11,
			TotalTP: 14, RemainingTP: 14,
			SyncedLevel: 7,
		},
		Skills: map[int64]dnfrepo.SkillState{
			1: {Level: 1, Enabled: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Settings.Save(context.Background(), dnfrepo.SettingsRecord{
		Scope: dnfrepo.CharacterContainerStateScope("77"),
		Values: map[string]string{
			"source":                      "current_exe_86jp_op13_container_state",
			"main_list_param16":           "24",
			"avatar_list_param16":         "0",
			"personal_cargo_list_param16": "8",
		},
	}); err != nil {
		t.Fatal(err)
	}
	skillCatalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":     "15 `job15.lst`\n",
		"skill/job15.lst":         "1 `job15/one.skl`\n",
		"skill/job15/one.skl":     "[skill type]\n`active`\n[skill class]\n1\n[required level]\n1\n",
		"character/character.lst": "15 `job15.chr`\n",
		"character/job15.chr":     "[job]\n`[gunblader]`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		initialSkillsByJob: map[byte][]initialSkillEntry{15: {{SkillID: 1, Level: 1}}},
		initialSPTable:     map[int]int{1: 11},
		initialTPTable:     map[int]int{1: 14},
		skillCatalog:       skillCatalog,
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:                              connection,
		selectedCharacterID:               77,
		postStartMapPlayerStateSent:       true,
		townActorOwnerChannel:             16,
		deferredDungeonUserStateObjectKey: 77,
	}
	if err := service.sendFinishLoadingStatus(session, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	status, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if status.Header.Classification != dnfproto.DefaultChannelClassification || status.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) || !bytes.Equal(status.Body, []byte{1}) {
		t.Fatalf("finish-loading status=%+v body=%x", status.Header, status.Body)
	}
	state, rest := splitGameServerUpperPacket(t, rest)
	if state.Header.Classification != 0 || state.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) || len(state.Body) != currentFinishLoadingCharacterStateBodySize {
		t.Fatalf("finish-loading state=%+v body_len=%d", state.Header, len(state.Body))
	}
	if state.Body[0] != 7 || binary.LittleEndian.Uint32(state.Body[1:5]) != 1234 || binary.LittleEndian.Uint16(state.Body[9:11]) != 11 || binary.LittleEndian.Uint16(state.Body[15:17]) != 14 {
		t.Fatalf("finish-loading core fields=%x", state.Body[:17])
	}
	if binary.LittleEndian.Uint32(state.Body[17:21]) != 99 || binary.LittleEndian.Uint32(state.Body[21:25]) != 21 || binary.LittleEndian.Uint32(state.Body[34:38]) != 22 || state.Body[0x2e] != 0 {
		t.Fatalf("finish-loading scalar fields=%x", state.Body)
	}
	if !bytes.Equal(state.Body[55:67], make([]byte, 12)) {
		t.Fatalf("finish-loading HonorExpert state=%x, want neutral 0/0", state.Body[55:67])
	}
	commit, rest := splitGameServerUpperPacket(t, rest)
	if commit.Header.Classification != 0 || commit.Header.MsgID != currentIncreaseStatusResultMsgID || !bytes.Equal(commit.Body, make([]byte, currentIncreaseStatusResultBodySize)) {
		t.Fatalf("increase-status result=%+v body=%x", commit.Header, commit.Body)
	}
	skills, rest := splitGameServerUpperPacket(t, rest)
	if skills.Header.Classification != 0 || skills.Header.MsgID != currentSkillInfoMsgID || len(skills.Body) < 4 || int(binary.LittleEndian.Uint32(skills.Body[:4])) != len(skills.Body)-4 {
		t.Fatalf("post-finish-loading skills=%+v body=%x", skills.Header, skills.Body)
	}
	outerVarints, outerMessages := consumeCurrentSkillInfoFields(t, skills.Body[4:])
	if got := outerVarints[1]; len(got) != 1 || got[0] != currentSkillInfoMessageType || len(outerMessages[2]) != currentSkillInfoTreeCount {
		t.Fatalf("post-finish-loading skill fields=%v trees=%d", outerVarints, len(outerMessages[2]))
	}
	placement, rest := splitGameServerUpperPacket(t, rest)
	if placement.Header.Classification != 0 || placement.Header.MsgID != uint16(dnfenum.CmdPacketRequestBlacklist) || !bytes.Equal(placement.Body, []byte{0, 0}) {
		t.Fatalf("post-finish-loading placement (without a second mode3)=%+v body=%x", placement.Header, placement.Body)
	}
	userState, rest := splitGameServerUpperPacket(t, rest)
	wantUserState, err := buildCurrentDungeonUserStateBody(77)
	if err != nil {
		t.Fatal(err)
	}
	if userState.Header.Classification != 0 ||
		userState.Header.MsgID != uint16(dnfenum.CmdPacketNotifyUserState) ||
		!bytes.Equal(userState.Body, wantUserState) {
		t.Fatalf("post-finish-loading deferred user state=%+v body=%x want=%x", userState.Header, userState.Body, wantUserState)
	}
	if len(rest) != 0 || !session.currentFinishLoadingStateSent || !session.currentFinishLoadingCompletionSent {
		t.Fatalf("finish-loading trailing=%x state=%v completion=%v",
			rest, session.currentFinishLoadingStateSent, session.currentFinishLoadingCompletionSent)
	}
	if !session.postFinishLoadingPlayerStateSent {
		t.Fatal("postFinishLoadingPlayerStateSent = false")
	}
	if session.deferredDungeonUserStateObjectKey != 0 {
		t.Fatalf("deferredDungeonUserStateObjectKey = %d, want cleared", session.deferredDungeonUserStateObjectKey)
	}

	townConnection := &bufferConn{}
	townSession := &gameSession{
		conn:                        townConnection,
		selectedCharacterID:         77,
		initialTownRouteCharacterID: 77,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
		initialTownSkillInfoSent:    true,
	}
	if err := service.sendFinishLoadingStatus(townSession, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	townStatus, townRest := splitGameServerUpperPacket(t, townConnection.write.Bytes())
	if townStatus.Header.Classification != dnfproto.DefaultChannelClassification ||
		townStatus.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		!bytes.Equal(townStatus.Body, []byte{1}) {
		t.Fatalf("town finish-loading status=%+v body=%x", townStatus.Header, townStatus.Body)
	}
	townState, townRest := splitGameServerUpperPacket(t, townRest)
	if townState.Header.Classification != 0 || townState.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		len(townState.Body) != currentFinishLoadingCharacterStateBodySize {
		t.Fatalf("town finish-loading state=%+v body_len=%d", townState.Header, len(townState.Body))
	}
	townCompletion, townRest := splitGameServerUpperPacket(t, townRest)
	if townCompletion.Header.Classification != 0 || townCompletion.Header.MsgID != currentIncreaseStatusResultMsgID ||
		!bytes.Equal(townCompletion.Body, make([]byte, currentIncreaseStatusResultBodySize)) {
		t.Fatalf("town finish-loading completion=%+v body=%x", townCompletion.Header, townCompletion.Body)
	}
	townPlacement, townRest := splitGameServerUpperPacket(t, townRest)
	if townPlacement.Header.Classification != 0 || townPlacement.Header.MsgID != uint16(dnfenum.CmdPacketRequestBlacklist) ||
		!bytes.Equal(townPlacement.Body, buildCurrentSceneActorPlacementBody()) || len(townRest) != 0 {
		t.Fatalf("town finish-loading placement=%+v body=%x rest=%x", townPlacement.Header, townPlacement.Body, townRest)
	}
	if !townSession.currentFinishLoadingStateSent || !townSession.currentFinishLoadingCompletionSent ||
		!townSession.initialTownSkillInfoSent || !townSession.postFinishLoadingPlayerStateSent {
		t.Fatalf("town finish-loading gates current=%t completion=%t skill=%t post=%t",
			townSession.currentFinishLoadingStateSent,
			townSession.currentFinishLoadingCompletionSent,
			townSession.initialTownSkillInfoSent,
			townSession.postFinishLoadingPlayerStateSent)
	}
	if townSession.initialTownSkillInfoPrepared || len(townSession.initialTownSkillInfo.body) != 0 {
		t.Fatal("town finish-loading created a duplicate initial skill projection")
	}

}

func TestFinishLoadingStatusAfterCompletedDungeonReturnAcksWithoutMainState(t *testing.T) {
	connection := &bufferConn{}
	session := &gameSession{
		conn:                           connection,
		selectedCharacterID:            77,
		returnTownFinishLoadingAckOnly: true,
	}
	service := &Service{}
	if err := service.sendFinishLoadingStatus(session, make([]byte, currentFinishLoadingLegacyRequestBodySize)); err != nil {
		t.Fatalf("return-town finish-loading status: %v", err)
	}
	status, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if status.Header.Classification != dnfproto.DefaultChannelClassification ||
		status.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		!bytes.Equal(status.Body, []byte{1}) || len(rest) != 0 {
		t.Fatalf("return-town status=%+v body=%x trailing=%x", status.Header, status.Body, rest)
	}
	if session.currentFinishLoadingStateSent || session.currentFinishLoadingCompletionSent || session.postFinishLoadingPlayerStateSent {
		t.Fatalf("return-town op37 emitted a state path: state=%t completion=%t post=%t",
			session.currentFinishLoadingStateSent,
			session.currentFinishLoadingCompletionSent,
			session.postFinishLoadingPlayerStateSent)
	}
}

func TestBuildCurrentFinishLoadingCharacterStateUsesSkillLedgerForBothTrees(t *testing.T) {
	body := buildCurrentFinishLoadingCharacterStateBody(
		dnfrepo.CharacterRecord{Level: 90, Stats: map[string]int64{
			"sp_style_0": 1,
			"sp_style_1": 2,
			"tp_style_0": 3,
			"tp_style_1": 4,
		}},
		dnfrepo.SkillPointState{RemainingSP: 11970, RemainingTP: 41},
	)
	if len(body) != currentFinishLoadingCharacterStateBodySize {
		t.Fatalf("body length=%d, want %d", len(body), currentFinishLoadingCharacterStateBodySize)
	}
	if got := binary.LittleEndian.Uint16(body[9:11]); got != 11970 {
		t.Fatalf("tree0 SP=%d, want 11970", got)
	}
	if got := binary.LittleEndian.Uint16(body[11:13]); got != 11970 {
		t.Fatalf("tree1 SP=%d, want 11970", got)
	}
	if got := binary.LittleEndian.Uint16(body[13:15]); got != 41 {
		t.Fatalf("tree0 TP=%d, want 41", got)
	}
	if got := binary.LittleEndian.Uint16(body[15:17]); got != 41 {
		t.Fatalf("tree1 TP=%d, want 41", got)
	}
}

func TestBuildCurrentFinishLoadingCharacterStatePresentsGrowthContractWithoutFatigueBurn(t *testing.T) {
	body := buildCurrentFinishLoadingCharacterStateBodyWithPresentation(
		dnfrepo.CharacterRecord{Level: 20, Stats: map[string]int64{
			"exp":                           240,
			"finish_loading_exp_category_3": 999,
			"finish_loading_exp_category_4": 888,
		}},
		dnfrepo.SkillPointState{},
		&currentFinishLoadingExperiencePresentation{GrowthContractBonus: 40},
	)
	if len(body) != currentFinishLoadingCharacterStateBodySize {
		t.Fatalf("body length=%d, want %d", len(body), currentFinishLoadingCharacterStateBodySize)
	}
	if got := binary.LittleEndian.Uint32(body[30:34]); got != 40 {
		t.Fatalf("growth-contract bonus at +0x1e=%d, want 40", got)
	}
	if got := binary.LittleEndian.Uint32(body[38:42]); got != 888 {
		t.Fatalf("fatigue-burn bonus at +0x26=%d, want persisted 888", got)
	}

	zero := buildCurrentFinishLoadingCharacterStateBodyWithPresentation(
		dnfrepo.CharacterRecord{Level: 20, Stats: map[string]int64{
			"finish_loading_exp_category_3": 999,
		}},
		dnfrepo.SkillPointState{},
		&currentFinishLoadingExperiencePresentation{},
	)
	if got := binary.LittleEndian.Uint32(zero[30:34]); got != 0 {
		t.Fatalf("zero presentation retained persisted growth bonus=%d", got)
	}
}

func TestBuildCurrentFinishLoadingCharacterStateMatchesCurrentEXEFixedLayout(t *testing.T) {
	body := buildCurrentFinishLoadingCharacterStateBody(
		dnfrepo.CharacterRecord{Level: 0x5a, Stats: map[string]int64{
			"exp":                                    0x01020304,
			"finish_loading_exp_category_0":          0x11121314,
			"finish_loading_currency_slot2_total":    0x21222324,
			"finish_loading_exp_category_1":          0x31323334,
			"finish_loading_result_flag":             0x45,
			"finish_loading_exp_category_2":          0x51525354,
			"finish_loading_exp_category_3":          0x61626364,
			"finish_loading_independent_scalar":      0x71727374,
			"finish_loading_exp_category_4":          0x11121315,
			"finish_loading_exp_category_5":          0x21222325,
			"finish_loading_exp_category_6":          0x31323335,
			"finish_loading_exp_category_7":          0x41424345,
			"finish_loading_auxiliary_state_id":      0x51525355,
			"finish_loading_auxiliary_state_value_0": 0x61626365,
			"finish_loading_auxiliary_state_value_1": 0x71727375,
			"finish_loading_exp_category_8":          0x12131415,
			"finish_loading_exp_category_9":          0x22232425,
			"finish_loading_exp_category_10":         0x32333435,
			"finish_loading_exp_category_11":         0x42434445,
		}},
		dnfrepo.SkillPointState{RemainingSP: 0x1234, RemainingTP: 0x2345},
	)
	if len(body) != 87 {
		t.Fatalf("body length=%d, want 87", len(body))
	}
	if body[0] != 0x5a || binary.LittleEndian.Uint32(body[1:5]) != 0x01020304 ||
		binary.LittleEndian.Uint32(body[5:9]) != 0x11121314 {
		t.Fatalf("level/EXP prefix=%x", body[:9])
	}
	for _, check := range []struct {
		offset int
		want   uint16
	}{
		{9, 0x1234}, {11, 0x1234}, {13, 0x2345}, {15, 0x2345},
	} {
		if got := binary.LittleEndian.Uint16(body[check.offset : check.offset+2]); got != check.want {
			t.Fatalf("u16 at +0x%02x=%04x, want %04x", check.offset, got, check.want)
		}
	}
	if body[25] != 0x45 || body[46] != 0 {
		t.Fatalf("flags result=%02x dynamic_count=%02x", body[25], body[46])
	}
	for _, check := range []struct {
		offset int
		want   uint32
	}{
		{17, 0x21222324}, {21, 0x31323334}, {26, 0x51525354},
		{30, 0x61626364}, {34, 0x71727374}, {38, 0x11121315},
		{42, 0x21222325}, {47, 0x31323335}, {51, 0x41424345},
		{67, 0x12131415}, {71, 0}, {75, 0x22232425},
		{79, 0x32333435}, {83, 0x42434445},
	} {
		if got := binary.LittleEndian.Uint32(body[check.offset : check.offset+4]); got != check.want {
			t.Fatalf("u32 at +0x%02x=%08x, want %08x", check.offset, got, check.want)
		}
	}
	if !bytes.Equal(body[55:67], make([]byte, 12)) {
		t.Fatalf("HonorExpert at +0x37=%x, want neutral 0/0", body[55:67])
	}
}

func TestFinishLoadingMainStateIsDeferredWithoutPostMapPlayer(t *testing.T) {
	service := &Service{}
	connection := &bufferConn{}
	session := &gameSession{conn: connection, selectedCharacterID: 77}
	if err := service.sendFinishLoadingStatus(session, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	status, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if status.Header.Classification != dnfproto.DefaultChannelClassification || !bytes.Equal(status.Body, []byte{1}) || len(rest) != 0 {
		t.Fatalf("deferred finish-loading status=%+v body=%x rest=%x", status.Header, status.Body, rest)
	}
}
