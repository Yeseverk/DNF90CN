package skillcmd

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeBuySkillRequest(t *testing.T) {
	got, err := DecodeBuySkillRequest([]byte{1, 1, 0x34, 0x12, 0, 3, 3})
	if err != nil {
		t.Fatalf("DecodeBuySkillRequest error = %v", err)
	}
	if got.RawSkillTree != 1 || got.Count != 1 || len(got.Entries) != 1 {
		t.Fatalf("got = %+v", got)
	}
	if got.Entries[0].SkillID != 0x1234 || got.Entries[0].LevelDelta != 3 || got.FinalMode != 3 {
		t.Fatalf("entry = %+v", got.Entries[0])
	}
}

func TestDecodeBuySkillRequestRejectsDeclaredCountMismatch(t *testing.T) {
	if _, err := DecodeBuySkillRequest([]byte{1, 2, 7, 0, 0, 1, 0}); err == nil {
		t.Fatal("DecodeBuySkillRequest accepted a truncated second entry")
	}
}

func TestDecodeSkillInitRequestMatchesLiveCurrentEXEBody(t *testing.T) {
	got, err := DecodeSkillInitRequest([]byte{0, 0})
	if err != nil {
		t.Fatalf("DecodeSkillInitRequest() error = %v", err)
	}
	if got.SkillTree != 0 || got.Mode != 0 {
		t.Fatalf("request = %+v", got)
	}
	if _, err := DecodeSkillInitRequest([]byte{0}); err == nil {
		t.Fatal("DecodeSkillInitRequest() accepted a truncated live body")
	}
}

func TestDecodeChangeAnotherSkillTreeRequestMatchesLiveCurrentEXEBody(t *testing.T) {
	got, err := DecodeChangeAnotherSkillTreeRequest([]byte{0, 0x3e, 0x74, 0x5e, 0x7a})
	if err != nil {
		t.Fatalf("DecodeChangeAnotherSkillTreeRequest() error = %v", err)
	}
	if got.SkillTree != 0 || got.TransportTail != [4]byte{0x3e, 0x74, 0x5e, 0x7a} {
		t.Fatalf("request = %+v", got)
	}
	for _, body := range [][]byte{{0}, {2, 1, 2, 3, 4}, {0, 1, 2, 3, 4, 5}} {
		if _, err := DecodeChangeAnotherSkillTreeRequest(body); err == nil {
			t.Fatalf("DecodeChangeAnotherSkillTreeRequest(%x) accepted malformed body", body)
		}
	}
}

func TestHandlerSkillInitCommitsBeforeCurrentEXESuccess(t *testing.T) {
	ctx := context.Background()
	repos := seededSkillOwnerRepositories(t, ctx)
	record, _, _ := repos.Skill.Load(ctx, "31")
	record.Skills[47] = dnfrepo.SkillState{Level: 1, Enabled: true}
	record.Points.RemainingSP = 50
	if err := repos.Skill.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSkillInit),
		Body:                []byte{0, 0},
		AccountID:           "acc-skill",
		SelectedCharacterID: 31,
		Repositories:        repos,
		SkillCatalog:        skillRuleCatalog(t),
		InitialSkillLevels:  map[uint16]int{46: 1},
		SkillPointBaseline:  &dnfrepo.SkillPointState{TotalSP: 100, RemainingSP: 100, TotalTP: 10, RemainingTP: 10, SyncedLevel: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if len(got.PostActions) != 1 || got.PostActions[0] != alignedcmd.PostActionRefreshSelectedActorSkills {
		t.Fatalf("post actions = %+v, want selected actor skill refresh", got.PostActions)
	}
	response := got.UpperResponses[0]
	if response.MsgID != uint16(dnfenum.CmdPacketSkillInit) || string(response.Body) != string([]byte{1, 0, 1}) {
		t.Fatalf("response = %+v", response)
	}
	persisted, _, _ := repos.Skill.Load(ctx, "31")
	if len(persisted.Skills) != 1 || persisted.Skills[46].Level != 1 || persisted.Points.RemainingSP != 100 {
		t.Fatalf("persisted reset = %+v", persisted)
	}
}

func TestHandlerChangeAnotherSkillTreeCommitsBeforeCurrentEXESuccess(t *testing.T) {
	ctx := context.Background()
	repos := seededSkillOwnerRepositories(t, ctx)
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketChangeAnotherSkillTree),
		Body:                []byte{0, 0x3e, 0x74, 0x5e, 0x7a},
		AccountID:           "acc-skill",
		SelectedCharacterID: 31,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 || len(got.PostActions) != 0 {
		t.Fatalf("result = %+v", got)
	}
	response := got.UpperResponses[0]
	if response.MsgID != uint16(dnfenum.CmdPacketChangeAnotherSkillTree) ||
		response.Classification != dnfproto.DefaultChannelClassification || !response.AllowCodec ||
		string(response.Body) != string([]byte{1, 1}) {
		t.Fatalf("response = %+v, want success/target", response)
	}
	character, ok, loadErr := repos.Character.Load(ctx, "31")
	if loadErr != nil || !ok || character.Stats[currentEXESkillTreeIndexStat] != 1 {
		t.Fatalf("persisted character ok=%t err=%v record=%+v", ok, loadErr, character)
	}
}

func TestHandlerChangeAnotherSkillTreeReturnsFailureForStaleOrMalformedRequest(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		body []byte
	}{
		{name: "stale current", body: []byte{1, 0x3e, 0x74, 0x5e, 0x7a}},
		{name: "malformed", body: []byte{0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repos := seededSkillOwnerRepositories(t, ctx)
			got, err := NewHandler().Handle(ctx, alignedcmd.Request{
				Opcode:              uint16(dnfenum.CmdPacketChangeAnotherSkillTree),
				Body:                test.body,
				AccountID:           "acc-skill",
				SelectedCharacterID: 31,
				Repositories:        repos,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 ||
				string(got.UpperResponses[0].Body) != string([]byte{0, 19}) {
				t.Fatalf("result = %+v, want failure/code19", got)
			}
			character, _, _ := repos.Character.Load(ctx, "31")
			if character.Stats[currentEXESkillTreeIndexStat] != 0 {
				t.Fatalf("failed switch mutated persisted tree = %d", character.Stats[currentEXESkillTreeIndexStat])
			}
		})
	}
}

func TestDecodeSkillCommandRequest(t *testing.T) {
	got, err := DecodeSkillCommandRequest([]byte{
		1, 9, 0, 2, 'A', 'B',
		10, 0, 1, 'C',
	})
	if err != nil {
		t.Fatalf("DecodeSkillCommandRequest error = %v", err)
	}
	if got.SkillTree != 1 || len(got.Records) != 2 {
		t.Fatalf("got = %+v", got)
	}
	if got.Records[0].SkillID != 9 || string(got.Records[0].CommandBytes) != "AB" {
		t.Fatalf("first = %+v", got.Records[0])
	}
	if got.Records[1].SkillID != 10 || string(got.Records[1].CommandBytes) != "C" {
		t.Fatalf("second = %+v", got.Records[1])
	}
}

func TestHandlerCommitsSkillSlotAndBuildsCurrentEXEResponse(t *testing.T) {
	ctx := context.Background()
	repos := seededSkillOwnerRepositories(t, ctx)
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketChangeSkillslot),
		Body:                []byte{0, 0, 1, 0xff, 0xff, 0xff, 0xff, 2},
		AccountID:           " acc-skill ",
		SelectedCharacterID: 31,
		Repositories:        repos,
		SkillCatalog:        skillRuleCatalog(t),
		InitialSkillLevels:  map[uint16]int{46: 1},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v, want one current op28 response", got)
	}
	if got.Operation != "change_skill_slot" {
		t.Fatalf("operation = %q", got.Operation)
	}
	response := got.UpperResponses[0]
	if response.MsgID != uint16(dnfenum.CmdPacketChangeSkillslot) || response.Classification != dnfproto.DefaultChannelClassification || !response.AllowCodec || string(response.Body) != string([]byte{1, 0, 0, 1}) {
		t.Fatalf("response = %+v, want success/tree/from/to", response)
	}
	record, ok, loadErr := repos.Skill.Load(ctx, "31")
	if loadErr != nil || !ok || record.Layouts[0][1] != 46 {
		t.Fatalf("persisted moved layout ok=%t err=%v record=%+v", ok, loadErr, record)
	}
}

func TestHandlerBuySkillBlocksAckWithoutPVFRules(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "31",
		AccountID:   "acc-skill",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Skill.Save(ctx, dnfrepo.SkillRecord{
		CharacterID: "31",
		Skills: map[int64]dnfrepo.SkillState{
			7: {Level: 3, Enabled: true},
		},
	}); err != nil {
		t.Fatalf("save skill: %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketBuySkill),
		Body:                []byte{0, 1, 7, 0, 0, 3, 0},
		AccountID:           "acc-skill",
		SelectedCharacterID: 31,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if !strings.Contains(got.Reason, "skill owner preflight failed") || !strings.Contains(got.Reason, "skill PVF rules are unavailable") {
		t.Fatalf("reason should name missing PVF rules, got %q", got.Reason)
	}
}

func TestHandlerBuySkillCommitsAndBuildsCurrentEXEResponse(t *testing.T) {
	ctx := context.Background()
	repos := seededSkillOwnerRepositories(t, ctx)
	body := []byte{
		0, 2,
		47, 0, 0, 1,
		46, 0, 0, 1,
		3,
	}
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketBuySkill),
		Body:                body,
		AccountID:           "acc-skill",
		SelectedCharacterID: 31,
		Repositories:        repos,
		SkillCatalog:        skillRuleCatalog(t),
		InitialSkillLevels:  map[uint16]int{46: 1},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v, want one real response", got)
	}
	response := got.UpperResponses[0]
	if response.MsgID != uint16(dnfenum.CmdPacketBuySkill) || response.Classification != dnfproto.DefaultChannelClassification || !response.AllowCodec {
		t.Fatalf("response metadata = %+v", response)
	}
	want := []byte{
		1, 0,
		50, 0, 10, 0,
		2,
		1, 47, 0, 1, 0,
		0, 46, 0, 2, 0,
		3,
	}
	if string(response.Body) != string(want) {
		t.Fatalf("response body = % X, want % X", response.Body, want)
	}
	record, _, _ := repos.Skill.Load(ctx, "31")
	if record.Skills[46].Level != 2 || record.Skills[47].Level != 1 || record.Points.RemainingSP != 50 {
		t.Fatalf("persisted state = %+v", record)
	}
}

func TestHandlerBuySkillRejectsUnprovenTreeWithoutSuccessACK(t *testing.T) {
	ctx := context.Background()
	repos := seededSkillOwnerRepositories(t, ctx)
	unitOfWork := &countingCharacterSkillUnitOfWork{delegate: repos.CharacterSkills}
	repos.CharacterSkills = unitOfWork

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketBuySkill),
		Body:                []byte{1, 1, 46, 0, 0, 1, 0},
		AccountID:           "acc-skill",
		SelectedCharacterID: 31,
		Repositories:        repos,
		SkillCatalog:        skillRuleCatalog(t),
		InitialSkillLevels:  map[uint16]int{46: 1},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("result = %+v, want handled rejection without response", got)
	}
	if unitOfWork.calls != 0 {
		t.Fatalf("CharacterSkillUnitOfWork calls = %d, want 0", unitOfWork.calls)
	}
	if !strings.Contains(got.Reason, ErrSkillTree.Error()) {
		t.Fatalf("reason should identify unproven tree, got %q", got.Reason)
	}
}

func TestBuyCommandRecordsSkillOwnerGap(t *testing.T) {
	cmd := NewBuyCommand(alignedcmd.Request{
		AccountID:           " acc-skill-2 ",
		SelectedCharacterID: 44,
	}, BuySkillRequest{
		RawSkillTree: 2,
		Count:        2,
		FinalMode:    3,
		Entries: []BuySkillEntry{
			{SkillID: 7, LevelDelta: 3},
			{SkillID: 8, RefundFlag: 1, LevelDelta: 1},
		},
	})
	if cmd.AccountID != "acc-skill-2" || cmd.SelectedCharacterID != 44 || cmd.SkillTree != 2 || cmd.FinalMode != 3 || cmd.DeclaredCount != 2 || cmd.EntryCount != 2 || cmd.RefundCount != 1 {
		t.Fatalf("cmd = %+v", cmd)
	}
	if !strings.Contains(cmd.String(), "SP/TP validation") || !strings.Contains(cmd.String(), "NOTI 0x13 order") {
		t.Fatalf("command plan must name skill write gap: %s", cmd.String())
	}
}
