package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfprofession "longheng.io/server/internal/modules/dnf/profession"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestBuildCurrentFinishQuestSuccessBodyMatchesCurrentEXEExactItemRow(t *testing.T) {
	raw := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint16(raw[0:2], 65)
	binary.LittleEndian.PutUint32(raw[2:6], 10403)
	binary.LittleEndian.PutUint32(raw[6:10], 7)
	raw[0x0a] = 0x08
	binary.LittleEndian.PutUint16(raw[0x0b:0x0d], 0x1234)
	binary.LittleEndian.PutUint32(raw[0x0e:0x12], 0x55667788)
	raw[0x12] = 0xaa
	raw[0x13] = 0xbb

	body, err := buildCurrentFinishQuestSuccessBody(dnfquest.FinishCommitResult{
		AtomicCommitted: true,
		QuestID:         3145,
		CompletionKey:   "run-19/op117/426",
		Source:          "quest_pvf:n_quest/finish.qst",
		ExperienceDelta: 1780,
		Items: []dnfquest.FinishCommittedItem{{
			SlotKey: "0:65", SlotIndex: 65, ItemID: 10403,
			Delta: 1, PostCount: 7, CountOrSeed: 1, RawEntry: raw,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x49, 0x0c, // questID
		0x00,                   // completionType
		0xf4, 0x06, 0x00, 0x00, // EXP; current reader has no intervening gold field
		0x00, // consumedCount
		0x00, // chainType
		0x01, // insertedCount
		0x41, 0x00,
		0xa3, 0x28, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x08,
		0x34, 0x12,
		0x88, 0x77, 0x66, 0x55,
		0xaa, 0xbb,
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("finish ACK payload=%x want=%x", body, want)
	}
	if len(body) != 29 {
		t.Fatalf("finish ACK payload length=%d want=29", len(body))
	}
}

func TestBuildCurrentFinishQuestSuccessBodyCarriesConsumedMaterials(t *testing.T) {
	body, err := buildCurrentFinishQuestSuccessBody(dnfquest.FinishCommitResult{
		AtomicCommitted: true,
		QuestID:         3145,
		CompletionKey:   "quest-finish/19/3145/submit-material",
		Source:          "quest_pvf:n_quest/submit_material.qst",
		ExperienceDelta: 100,
		ConsumedItems: []dnfquest.FinishConsumedItem{{
			SlotKey: "0:8", SlotIndex: 8, ItemID: 9001, Delta: 30, PostCount: 0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x49, 0x0c, // questID
		0x00,                   // completionType
		0x64, 0x00, 0x00, 0x00, // EXP
		0x01,       // consumedCount
		0x00,       // consumed updateType
		0x08, 0x00, // slot
		0x1e, 0x00, 0x00, 0x00, // consumed stack count
		0x00, // chainType
		0x00, // insertedCount
	}
	if !bytes.Equal(body, want) || len(body) != 17 {
		t.Fatalf("finish consumed ACK payload=%x len=%d want=%x", body, len(body), want)
	}
}

func TestBuildCurrentFinishQuestSuccessBodyMatchesCurrentEXEProfessionChain(t *testing.T) {
	body, err := buildCurrentFinishQuestSuccessBody(dnfquest.FinishCommitResult{
		AtomicCommitted: true,
		QuestID:         100,
		CompletionKey:   "quest-finish/19/100/1",
		Source:          "quest_pvf:n_quest/change.qst",
		ExperienceDelta: 30,
		HasProfession:   true,
		Profession: dnfprofession.Transition{
			Kind: dnfprofession.KindClassChange, ChainType: 1, GrowNumber: 2,
			PreviousGrowType: 0, NewGrowType: 2, FirstGrowType: 2,
		},
		ProfessionGrants: []dnfprofession.Grant{
			{SkillID: 1, Level: 1},
			{SkillID: 2, Level: 3},
			{SkillID: 197, Level: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x64, 0x00, // questID
		0x00,                   // completionType
		0x1e, 0x00, 0x00, 0x00, // EXP
		0x00,                   // consumedCount
		0x01,                   // chainType=grow type
		0x02,                   // growNumber
		0x03,                   // tree0 inline grant count
		0x01, 0x01, 0x00, 0x01, // level=1, skill=1, learned
		0x03, 0x02, 0x00, 0x01, // level=3, skill=2, learned
		0x01, 0xc5, 0x00, 0x01, // level=1, skill=197 profession mastery
		0x03, // tree1 inline grant count
		0x01, 0x01, 0x00, 0x01,
		0x03, 0x02, 0x00, 0x01,
		0x01, 0xc5, 0x00, 0x01,
	}
	if !bytes.Equal(body, want) || len(body) != 36 {
		t.Fatalf("profession ACK payload=%x len=%d want=%x", body, len(body), want)
	}
}

func TestBuildCurrentFinishQuestSuccessBodyMatchesCurrentEXEExpertJobChain(t *testing.T) {
	body, err := buildCurrentFinishQuestSuccessBody(dnfquest.FinishCommitResult{
		AtomicCommitted: true,
		QuestID:         2710,
		CompletionKey:   "quest-finish/19/2710/1",
		Source:          "quest_pvf:n_quest/Expertjob/normal_60_Expert_job_disjointer_2.qst",
		ExperienceDelta: 300,
		HasExpertJob:    true,
		ExpertJobType:   3,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x96, 0x0a, // questID
		0x00,                   // completionType
		0x2c, 0x01, 0x00, 0x00, // EXP
		0x00, // consumedCount
		0x14, // chainType=expert job
		0x03, // expertJobType=disjointer
		0x00, // tree0 inline grant count
		0x00, // tree1 inline grant count
	}
	if !bytes.Equal(body, want) || len(body) != 12 {
		t.Fatalf("expert-job ACK payload=%x len=%d want=%x", body, len(body), want)
	}
}

func TestRealPVFCurrentExpertJobQuestFinishPublishesTypedStateAfterAck(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify expert-job quest finish projection")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	expertCatalog, err := dnfexpertjob.Load(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		questID uint16
		jobType byte
	}{
		{name: "enchanter", questID: 2702, jobType: dnfexpertjob.EnchanterType},
		{name: "alchemist", questID: 2708, jobType: dnfexpertjob.AlchemistType},
		{name: "disjointer", questID: 2710, jobType: dnfexpertjob.DisjointerType},
		{name: "doll-controller", questID: 2712, jobType: dnfexpertjob.DollControllerType},
	} {
		t.Run(test.name, func(t *testing.T) {
			repositories := finishBridgeSeed(t)
			acceptedAt := time.Date(2026, 8, 2, 13, 0, int(test.jobType), 0, time.UTC)
			if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
				CharacterID: "19",
				States: map[int64]dnfrepo.QuestState{
					int64(test.questID): {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt},
				},
				UpdatedAt: acceptedAt,
			}); err != nil {
				t.Fatal(err)
			}
			catalog := finishBridgeExpertJobQuestCatalog(t, test.questID, test.jobType)
			connection := &bufferConn{}
			service := &Service{
				options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
				questCatalog:       catalog,
				expertJobCatalog:   expertCatalog,
				repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
			}
			session := &gameSession{conn: connection, connID: "finish-expert-job-" + test.name, selectedCharacterID: 19}
			request := dnfquest.FinishQuestRequest{
				QuestID:           test.questID,
				RewardSelectIndex: ^uint16(0),
				Multiplier:        1,
				Reserved:          dnfquest.CurrentFinishQuestObservedTailMarker,
			}
			if err := service.handleCurrentFinishQuestWithResources(
				session,
				request,
				repositories,
				catalog,
				finishBridgeProgression(t),
				finishBridgeAllocator(nil),
			); err != nil {
				t.Fatal(err)
			}

			packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
			if len(packets) < 6 ||
				packets[0].Header.Classification != dnfproto.DefaultChannelClassification ||
				packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) ||
				packets[1].Header.Classification != 0 ||
				packets[1].Header.MsgID != currentExpertJobInfoNotification {
				t.Fatalf("expert-job finish order=%v", finishBridgePacketIDs(packets))
			}
			assertCurrentExpertJobQuestInfoBody(t, packets[1].Body, expertCatalog, test.jobType)
			for _, packet := range packets[2:] {
				if packet.Header.Classification == 0 &&
					(packet.Header.MsgID == 2 || packet.Header.MsgID == 754) {
					t.Fatalf("expert-job finish emitted incompatible actor carrier: %v", finishBridgePacketIDs(packets))
				}
			}
			assertFinishBridgePostCommitOrder(t, packets, test.questID)

			character, found, err := repositories.Character.Load(context.Background(), "19")
			if err != nil || !found || character.Stats["expert_job_type"] != int64(test.jobType) {
				t.Fatalf("committed expert job character=%+v found=%t err=%v", character, found, err)
			}
		})
	}
}

func assertCurrentExpertJobQuestInfoBody(t *testing.T, body []byte, catalog *dnfexpertjob.Catalog, jobType byte) {
	t.Helper()
	if len(body) < 2 || body[0] != 0 || body[1] != jobType {
		t.Fatalf("expert-job op205 body=%x type=%d", body, jobType)
	}
	if jobType == dnfexpertjob.DisjointerType {
		config, ok := catalog.Disjointer()
		if !ok || len(body) != 10 || binary.LittleEndian.Uint32(body[2:6]) != 1 ||
			int64(binary.LittleEndian.Uint32(body[6:10])) != config.InitialEndurance {
			t.Fatalf("disjointer op205 body=%x config=%+v found=%t", body, config, ok)
		}
		return
	}
	config, ok := catalog.Config(jobType)
	if !ok || len(body) < 3 {
		t.Fatalf("recipe expert-job op205 body=%x config=%+v found=%t", body, config, ok)
	}
	recipes := config.AutoRecipeIDs(0)
	offset := 3
	if int(body[2]) != len(recipes) || len(body) < offset+4*len(recipes) {
		t.Fatalf("expert-job op205 recipes body=%x want=%v", body, recipes)
	}
	for index, recipeID := range recipes {
		if int64(binary.LittleEndian.Uint32(body[offset:offset+4])) != recipeID {
			t.Fatalf("expert-job op205 recipe[%d]=%d want=%d body=%x", index, binary.LittleEndian.Uint32(body[offset:offset+4]), recipeID, body)
		}
		offset += 4
	}
	if jobType != dnfexpertjob.EnchanterType {
		if offset != len(body) {
			t.Fatalf("expert-job type=%d op205 trailing body=%x", jobType, body[offset:])
		}
		return
	}
	enchanter, ok := catalog.Enchanter()
	if !ok || len(body) <= offset {
		t.Fatalf("enchanter op205 body=%x config=%+v found=%t", body, enchanter, ok)
	}
	qualifications := enchanter.Qualifications(0)
	qualificationCount := int(body[offset])
	offset++
	if qualificationCount != len(qualifications) || len(body) != offset+qualificationCount+8 ||
		!bytes.Equal(body[offset:offset+qualificationCount], qualifications) {
		t.Fatalf("enchanter op205 qualifications body=%x want=%v", body, qualifications)
	}
	offset += qualificationCount
	if int(binary.LittleEndian.Uint32(body[offset:offset+4])) != config.Level(0) ||
		int64(binary.LittleEndian.Uint32(body[offset+4:offset+8])) != enchanter.InitialEndurance {
		t.Fatalf("enchanter op205 level/endurance body=%x", body)
	}
}

func TestBuildCurrentFinishQuestSuccessBodyUsesOrdinaryChainForSlotExpansion(t *testing.T) {
	body, err := buildCurrentFinishQuestSuccessBody(dnfquest.FinishCommitResult{
		AtomicCommitted:    true,
		QuestID:            2636,
		CompletionKey:      "quest-finish/19/2636/1",
		Source:             "quest_pvf:n_quest/new_quest/MetroCenter/system_90_earring_02.qst",
		ExperienceDelta:    300,
		HasSlotExpansion:   true,
		SlotExpansionIndex: 2,
		SlotExpansionBit:   dnfquest.ExEquipSlotEarring,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x4c, 0x0a, // questID
		0x00,                   // completionType
		0x2c, 0x01, 0x00, 0x00, // EXP
		0x00, // consumedCount
		0x00, // ordinary chainType; slot-expansion-only mode1 carries the committed bitset
		0x00, // item count
	}
	if !bytes.Equal(body, want) || len(body) != 10 {
		t.Fatalf("slot-expansion ACK payload=%x len=%d want=%x", body, len(body), want)
	}
}

func TestCurrentFinishQuestProfessionCommitPublishesCurrentEXEStateOrder(t *testing.T) {
	ctx := context.Background()
	repositories := finishBridgeSeed(t)
	character, _, _ := repositories.Character.Load(ctx, "19")
	character.Name = "profession"
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		100: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt},
	}}); err != nil {
		t.Fatal(err)
	}
	skill, _, _ := repositories.Skill.Load(ctx, "19")
	skill.Skills = map[int64]dnfrepo.SkillState{99: {Level: 2, Enabled: true}}
	if err := repositories.Skill.Save(ctx, skill); err != nil {
		t.Fatal(err)
	}
	catalog := finishBridgeProfessionCatalog(t)
	profiles, skillCatalog := finishBridgeProfessionResources(t)
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "finish-profession", selectedCharacterID: 19}
	request := dnfquest.FinishQuestRequest{QuestID: 100, Multiplier: 1, Reserved: dnfquest.CurrentFinishQuestObservedTailMarker}
	if err := service.handleCurrentFinishQuestWithResources(
		session, request, repositories, catalog, finishBridgeProgression(t), nil,
		currentQuestProfessionResources{Profiles: profiles, SkillCatalog: skillCatalog},
	); err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if len(packets) < 4 {
		t.Fatalf("profession post-commit packet count=%d ids=%v", len(packets), finishBridgePacketIDs(packets))
	}
	wantACK := []byte{
		1, 0x64, 0x00, 0x00, 0x64, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02,
		0x03, 0x01, 0x01, 0x00, 0x01, 0x01, 0x02, 0x00, 0x01, 0x01, 0xc5, 0x00, 0x01,
		0x03, 0x01, 0x01, 0x00, 0x01, 0x01, 0x02, 0x00, 0x01, 0x01, 0xc5, 0x00, 0x01,
	}
	if packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) || packets[0].Header.Classification != dnfproto.DefaultChannelClassification || !bytes.Equal(packets[0].Body, wantACK) {
		t.Fatalf("profession ACK header=%+v body=%x want=%x", packets[0].Header, packets[0].Body, wantACK)
	}
	indexBodyMode := func(mode byte) int {
		for index, packet := range packets {
			if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) && len(packet.Body) != 0 && packet.Body[0] == mode {
				return index
			}
		}
		return -1
	}
	indexMessage := func(msgID uint16) int {
		for index, packet := range packets {
			if packet.Header.Classification == 0 && packet.Header.MsgID == msgID {
				return index
			}
		}
		return -1
	}
	mode0, mode1, mode3 := indexBodyMode(0), indexBodyMode(1), indexBodyMode(3)
	characterIndex := indexMessage(currentDungeonCharacterStateMsgID)
	skillIndex := indexMessage(currentSkillInfoMsgID)
	clearQuestIndex := indexMessage(currentClearQuestListMsgID)
	activeIndex := indexMessage(currentActiveQuestSnapshotMsgID)
	acceptableIndex := indexMessage(currentAcceptableQuestListMsgID)
	if !(skillIndex == -1 && mode0 == -1 && mode1 == -1 && mode3 == -1 &&
		characterIndex > 0 && clearQuestIndex > characterIndex && activeIndex > clearQuestIndex && acceptableIndex > activeIndex) {
		t.Fatalf("profession order ack=0 skill=%d mode0=%d mode1=%d mode3=%d character=%d clear=%d active=%d acceptable=%d ids=%v", skillIndex, mode0, mode1, mode3, characterIndex, clearQuestIndex, activeIndex, acceptableIndex, finishBridgePacketIDs(packets))
	}
	assertFinishBridgeClearQuestProjection(t, packets[clearQuestIndex], 100)
	persistedCharacter, _, _ := repositories.Character.Load(ctx, "19")
	persistedSkill, _, _ := repositories.Skill.Load(ctx, "19")
	persistedQuest, _, _ := repositories.Quest.Load(ctx, "19")
	if persistedCharacter.Stats["grow_type"] != 2 || persistedSkill.Skills[1].Level != 1 || persistedSkill.Skills[2].Level != 1 || persistedQuest.States[100].Status != "completed" {
		t.Fatalf("profession commit character=%+v skill=%+v quest=%+v", persistedCharacter, persistedSkill, persistedQuest.States[100])
	}
}

func TestBuildCurrentFinishQuestSuccessBodyRejectsUncommittedOrMismatchedReceipt(t *testing.T) {
	raw := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint16(raw[0:2], 66)
	binary.LittleEndian.PutUint32(raw[2:6], 10403)
	base := dnfquest.FinishCommitResult{
		AtomicCommitted: true, QuestID: 3145, CompletionKey: "key", Source: "pvf",
		Items: []dnfquest.FinishCommittedItem{{SlotIndex: 65, ItemID: 10403, CountOrSeed: 1, RawEntry: raw}},
	}
	if _, err := buildCurrentFinishQuestSuccessBody(base); !errors.Is(err, errCurrentFinishQuestAckShape) {
		t.Fatalf("mismatched raw error=%v", err)
	}
	base.Items = nil
	base.AtomicCommitted = false
	if _, err := buildCurrentFinishQuestSuccessBody(base); !errors.Is(err, errCurrentFinishQuestAckShape) {
		t.Fatalf("uncommitted result error=%v", err)
	}
	base.AtomicCommitted = true
	base.Currency = []dnfquest.FinishCommittedCurrency{{Name: "gold", Delta: 1, PostValue: 1}}
	body, err := buildCurrentFinishQuestSuccessBody(base)
	if err != nil {
		t.Fatalf("currency must not alter current finish ACK shape: %v", err)
	}
	if want := []byte{0x49, 0x0c, 0x00, 0, 0, 0, 0, 0x00, 0x00, 0x00}; !bytes.Equal(body, want) {
		t.Fatalf("currency-only ACK payload=%x want=%x", body, want)
	}
}

func TestCurrentQuestFinishItemAllocatorUsesDeltaInAckAndPostCountInInventory(t *testing.T) {
	catalog := &pvfDungeonDropCatalog{
		source: finishBridgePVFSource{"stackable/test.stk": "[stack limit]\n999\n"},
		itemRefs: map[uint32]dungeonDropItemReference{
			10403: {kind: dungeonDropItemStackable, path: "stackable/test.stk"},
		},
		itemCache: map[uint32]dungeonDropItemDefinition{
			10403: {ItemID: 10403, Kind: dungeonDropItemStackable, PVFPath: "stackable/test.stk", StackLimit: 999, SlotStart: 65, SlotEnd: 120},
		},
	}
	record := dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}}
	allocate := currentQuestFinishItemAllocator(catalog)
	first, err := allocate(&record, dnfquest.FinishItemGrantRequest{QuestID: 3145, CompletionKey: "key", Source: "pvf", ItemID: 10403, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := allocate(&record, dnfquest.FinishItemGrantRequest{QuestID: 3145, CompletionKey: "key", Source: "pvf", ItemID: 10403, Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	if first.CountOrSeed != 2 || first.PostCount != 2 || second.CountOrSeed != 3 || second.PostCount != 5 {
		t.Fatalf("allocator receipts first=%+v second=%+v", first, second)
	}
	stack := record.Slots["0:65"]
	if len(record.Slots) != 1 || stack.Count != 5 || binary.LittleEndian.Uint32(stack.RawEntry[6:10]) != 5 {
		t.Fatalf("post inventory=%+v raw=%x", record.Slots, stack.RawEntry)
	}
	if stack.Extra["last_grant_completion_key"] != "key" || stack.Extra["last_grant_quest_id"] != "3145" {
		t.Fatalf("allocator provenance=%+v", stack.Extra)
	}
}

func TestCurrentQuestFinishItemAllocatorInstantiatesEquipmentUsablePeriod(t *testing.T) {
	catalog := &pvfDungeonDropCatalog{
		source: finishBridgePVFSource{"equipment/trial.equ": "[usable period]\n2\n"},
		itemRefs: map[uint32]dungeonDropItemReference{
			100300883: {kind: dungeonDropItemEquipment, path: "equipment/trial.equ"},
		},
		itemCache: map[uint32]dungeonDropItemDefinition{
			100300883: {
				ItemID: 100300883, Kind: dungeonDropItemEquipment,
				PVFPath: "equipment/trial.equ", SlotStart: 9, SlotEnd: 64,
				UsablePeriodDays: 2,
			},
		},
	}
	record := dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}}
	before := time.Now().UTC().Add(2 * 24 * time.Hour)
	receipt, err := currentQuestFinishItemAllocator(catalog)(&record, dnfquest.FinishItemGrantRequest{
		QuestID: 3272, CompletionKey: "quest-finish/19/3272", Source: "pvf",
		ItemID: 100300883, Count: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC().Add(2 * 24 * time.Hour)
	stack := record.Slots[receipt.SlotKey]
	if stack.ExpireAt.Before(before.Add(-time.Second)) || stack.ExpireAt.After(after.Add(time.Second)) ||
		stack.Extra["usable_period_days"] != "2" || stack.Extra["expiration_source"] != "runtime_pvf_usable_period_grant" ||
		len(stack.RawEntry) != currentItemListEntryWireSize ||
		binary.LittleEndian.Uint32(stack.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != uint32(stack.ExpireAt.Unix()) {
		t.Fatalf("trial equipment receipt=%+v stack=%+v raw=%x", receipt, stack, stack.RawEntry)
	}
}

func TestCurrentFinishQuestLiveTenByteBodyCommitsAtomicOwner(t *testing.T) {
	repositories := finishBridgeSeed(t)
	connection := &bufferConn{}
	catalog := finishBridgeCatalog(t)
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		connID:                      "finish-quest-live-body",
		selectedCharacterID:         19,
		initialTownRouteCharacterID: 19,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	request, err := dnfquest.DecodeFinishQuestRequest([]byte{0x22, 0x00, 0x49, 0x0C, 0xFF, 0xFF, 0x01, 0x00, 0xFF, 0xFF})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleCurrentFinishQuestWithResources(
		session,
		request,
		repositories,
		catalog,
		finishBridgeProgression(t),
		finishBridgeAllocator(nil),
	); err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	assertFinishBridgePostCommitOrder(t, packets, 3145)
	assertFinishBridgeNoPersonalInfoSnapshots(t, packets)
	if packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) ||
		packets[0].Header.Classification != dnfproto.DefaultChannelClassification ||
		len(packets[0].Body) != 30 || packets[0].Body[0] != 1 ||
		binary.LittleEndian.Uint16(packets[0].Body[1:3]) != 3145 ||
		packets[0].Body[3] != 0 ||
		binary.LittleEndian.Uint32(packets[0].Body[4:8]) != 100 ||
		packets[0].Body[8] != 0 || packets[0].Body[9] != 0 || packets[0].Body[10] != 1 ||
		binary.LittleEndian.Uint32(packets[0].Body[13:17]) != 10403 ||
		binary.LittleEndian.Uint32(packets[0].Body[17:21]) != 2 {
		t.Fatalf("finish ACK=%+v body=%x", packets[0].Header, packets[0].Body)
	}
	assertFinishBridgeSuccessorSnapshots(t, packets, 3145, 3146)
	questRecord, found, err := repositories.Quest.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load quest found=%t err=%v", found, err)
	}
	if state := questRecord.States[3145]; state.Status != "completed" || state.Extra["reward_state"] != "granted" {
		t.Fatalf("live body did not commit quest atomically: %+v", state)
	}
	if state, exists := questRecord.States[3146]; exists && state.Status == "active" {
		t.Fatalf("successor quest became active before manual op31: %+v", state)
	}

	start := connection.write.Len()
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x4a, 0x0c}); err != nil {
		t.Fatal(err)
	}
	acceptPackets := splitAllFinishBridgePackets(t, connection.write.Bytes()[start:])
	if len(acceptPackets) < 2 || acceptPackets[0].Header.MsgID != uint16(dnfenum.CmdPacketAcceptQuest) ||
		acceptPackets[0].Body[0] != 1 {
		t.Fatalf("post-finish next quest accept packets=%v", finishBridgePacketIDs(acceptPackets))
	}
	questRecord, found, err = repositories.Quest.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("reload quest found=%t err=%v", found, err)
	}
	if state := questRecord.States[3146]; state.Status != "active" || state.ProgressValue != 2 {
		t.Fatalf("next main quest was not accepted after finish: %+v", state)
	}
}

func TestCurrentFinishQuestSlotExpansionPublishesMode1WithoutPanelOpeningMode3(t *testing.T) {
	ctx := context.Background()
	repositories := finishBridgeSeed(t)
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			2636: {
				Status:        "active",
				ProgressValue: 0,
				UpdatedAt:     time.Date(2026, 7, 23, 3, 0, 0, 0, time.UTC),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	catalog := finishBridgeSlotExpansionCatalog(t)
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                            connection,
		connID:                          "finish-slot-expansion",
		selectedCharacterID:             19,
		connectionTownActorOwnerChannel: 253,
		townActorOwnerChannel:           253,
	}
	request := dnfquest.FinishQuestRequest{
		QuestID:    2636,
		Multiplier: 1,
		Reserved:   dnfquest.CurrentFinishQuestObservedTailMarker,
	}
	if err := service.handleCurrentFinishQuestWithResources(
		session,
		request,
		repositories,
		catalog,
		finishBridgeProgression(t),
		nil,
	); err != nil {
		t.Fatal(err)
	}

	character, found, err := repositories.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if got := character.Stats["ex_equip_slot_stat"]; got != int64(dnfquest.ExEquipSlotEarring) {
		t.Fatalf("persisted ex_equip_slot_stat=%d want=%d", got, dnfquest.ExEquipSlotEarring)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	assertFinishBridgePostCommitOrder(t, packets, 2636)
	var finishACK []byte
	for _, packet := range packets {
		if packet.Header.Classification == dnfproto.DefaultChannelClassification &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketFinishQuest) {
			finishACK = packet.Body
			break
		}
	}
	wantFinishACK := []byte{1, 0x4c, 0x0a, 0, 100, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(finishACK, wantFinishACK) {
		t.Fatalf("slot expansion finish ACK=%x want=%x", finishACK, wantFinishACK)
	}

	mode1Index, mode3Index := -1, -1
	for index, packet := range packets {
		if packet.Header.Classification != 0 ||
			packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
			len(packet.Body) == 0 {
			continue
		}
		switch packet.Body[0] {
		case 1:
			mode1Index = index
		case 3:
			mode3Index = index
		}
	}
	if mode1Index < 0 || mode3Index >= 0 {
		t.Fatalf("slot expansion mode1=%d mode3=%d ids=%v", mode1Index, mode3Index, finishBridgePacketIDs(packets))
	}
	if len(packets[mode1Index].Body) < 5 || packets[mode1Index].Body[4] != 253 {
		t.Fatalf("slot expansion mode1 owner=%x want=fd", packets[mode1Index].Body[:minInt(len(packets[mode1Index].Body), 5)])
	}
	adventureLevel := uint32(service.currentAccountAdventureGroupSummaryForPacket(ctx, session, character, true).ManageLevel)
	wantMode1 := service.buildCurrentSelectedUserInfoMode1BodyWithAdventureLevelInContext(
		ctx,
		session,
		nil,
		character,
		true,
		19,
		adventureLevel,
		253,
	)
	if !bytes.Equal(packets[mode1Index].Body, wantMode1) {
		t.Fatalf("slot expansion mode1 is not committed character snapshot")
	}
}

func TestCurrentFinishQuestWithoutLevelOrSPDeltaSkipsFullCharacterPackets(t *testing.T) {
	ctx := context.Background()
	repositories := finishBridgeSeed(t)
	character, found, err := repositories.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	skill, found, err := repositories.Skill.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load skill found=%t err=%v", found, err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       finishBridgeCatalog(t),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "finish-no-character-delta", selectedCharacterID: 19}
	if err := service.sendCurrentFinishQuestPostCommitSnapshots(session, dnfquest.FinishCommitResult{
		AtomicCommitted:     true,
		CharacterID:         "19",
		QuestID:             8581,
		CompletionKey:       "quest-finish/19/8581/no-character-delta",
		PreviousLevel:       character.Level,
		NewLevel:            character.Level,
		PostCommitCharacter: character,
		PostCommitSkill:     skill,
		PostCommitQuest: dnfrepo.QuestRecord{
			CharacterID: "19",
			States: map[int64]dnfrepo.QuestState{
				8581: {Status: "completed"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if len(packets) != 3 ||
		packets[0].Header.Classification != 0 || packets[0].Header.MsgID != currentClearQuestListMsgID ||
		packets[1].Header.Classification != 0 || packets[1].Header.MsgID != currentActiveQuestSnapshotMsgID ||
		packets[2].Header.Classification != 0 || packets[2].Header.MsgID != currentAcceptableQuestListMsgID {
		t.Fatalf("no-delta finish packets=%v", finishBridgePacketIDs(packets))
	}
	assertFinishBridgeClearQuestProjection(t, packets[0], 8581)
	assertFinishBridgeNoPersonalInfoSnapshots(t, packets)
	for _, packet := range packets {
		if packet.Header.MsgID == currentDungeonCharacterStateMsgID || packet.Header.MsgID == currentSkillInfoMsgID {
			t.Fatalf("no-delta finish sent full character/skill packet ids=%v", finishBridgePacketIDs(packets))
		}
	}
}

func TestCurrentFinishQuestPostCommitRefreshesConsumedMaterialDelete(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	session := &gameSession{conn: connection, connID: "finish-consumed-material-refresh", selectedCharacterID: 19}
	err := service.sendCurrentFinishQuestRewardItemUpdate(session, dnfquest.FinishCommitResult{
		QuestID:       600,
		CompletionKey: "quest-finish/19/600/submit-material",
		ConsumedItems: []dnfquest.FinishConsumedItem{{
			SlotKey: "0:8", SlotIndex: 8, ItemID: 9001, Delta: 30, PostCount: 0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if len(packets) != 1 || packets[0].Header.Classification != 0 ||
		packets[0].Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("consumed material refresh packets=%v", finishBridgePacketIDs(packets))
	}
	body := packets[0].Body
	if len(body) != 1+2+currentItemListEntryWireSize || body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(body[1:3]) != 1 {
		t.Fatalf("consumed material refresh body=%x", body)
	}
	row := body[3:]
	if binary.LittleEndian.Uint16(row[0:2]) != 8 ||
		binary.LittleEndian.Uint32(row[2:6]) != ^uint32(0) ||
		binary.LittleEndian.Uint32(row[6:10]) != 0 {
		t.Fatalf("consumed material delete row=%x", row[:10])
	}
}

func TestCurrentFinishQuestPostCommitRefreshesRemainingAccountSharedMaterial(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	session := &gameSession{conn: connection, connID: "finish-shared-material-refresh", selectedCharacterID: 19}
	err := service.sendCurrentFinishQuestRewardItemUpdate(session, dnfquest.FinishCommitResult{
		QuestID:       602,
		CompletionKey: "quest-finish/19/602/account-shared-material",
		ConsumedItems: []dnfquest.FinishConsumedItem{{
			SlotKey: "0:358", SlotIndex: 358, ItemID: 3037, Delta: 100, PostCount: 30,
		}},
		PostCommitAccountInventory: dnfrepo.AccountInventoryRecord{
			AccountID: "acc",
			Slots: map[string]dnfrepo.ItemStack{
				"0:358": {ItemID: 3037, Count: 30},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if len(packets) != 1 || packets[0].Header.Classification != 0 ||
		packets[0].Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("shared material refresh packets=%v", finishBridgePacketIDs(packets))
	}
	body := packets[0].Body
	if len(body) != 1+2+currentItemListEntryWireSize || body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(body[1:3]) != 1 {
		t.Fatalf("shared material refresh body=%x", body)
	}
	row := body[3:]
	if binary.LittleEndian.Uint16(row[0:2]) != 358 ||
		binary.LittleEndian.Uint32(row[2:6]) != 3037 ||
		binary.LittleEndian.Uint32(row[6:10]) != 30 {
		t.Fatalf("shared material refresh row=%x", row[:10])
	}
}

func TestCurrentFinishQuestRejectsNonObservedTailBeforeAtomicOwner(t *testing.T) {
	repositories := finishBridgeSeed(t)
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "finish-quest-invalid-tail", selectedCharacterID: 19}
	if err := service.handleCurrentFinishQuest(
		session,
		[]byte{0x22, 0x00, 0x49, 0x0C, 0xFF, 0xFF, 0x01, 0x00, 0xFF, 0x00},
	); err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if len(packets) != 1 || packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) ||
		packets[0].Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(packets[0].Body, []byte{0, 22}) {
		t.Fatalf("invalid-tail failure packets=%v", finishBridgePacketIDs(packets))
	}
	questRecord, found, err := repositories.Quest.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load quest found=%t err=%v", found, err)
	}
	if state := questRecord.States[3145]; state.Status != "active" || state.Extra["reward_state"] != "pending" {
		t.Fatalf("invalid tail reached atomic owner: %+v", state)
	}
}

func TestCurrentFinishQuestCommitReplayDoesNotDuplicateAndPublishesSnapshotsInOrder(t *testing.T) {
	repositories := finishBridgeSeed(t)
	connection := &bufferConn{}
	catalog := finishBridgeCatalog(t)
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "finish-quest-test", selectedCharacterID: 19}
	request := dnfquest.FinishQuestRequest{
		QuestID:           3145,
		RewardSelectIndex: ^uint16(0),
		Multiplier:        1,
		Reserved:          dnfquest.CurrentFinishQuestObservedTailMarker,
	}
	allocator := finishBridgeAllocator(nil)

	// First finish: full receipt and post-commit flow.
	start := connection.write.Len()
	if err := service.handleCurrentFinishQuestWithResources(session, request, repositories, catalog, finishBridgeProgression(t), allocator); err != nil {
		t.Fatalf("first finish: %v", err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes()[start:])
	assertFinishBridgePostCommitOrder(t, packets, 3145)
	assertFinishBridgeNoPersonalInfoSnapshots(t, packets)
	if packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) || packets[0].Header.Classification != dnfproto.DefaultChannelClassification || len(packets[0].Body) < 2 || packets[0].Body[0] != 1 {
		t.Fatalf("first ACK=%+v body=%x", packets[0].Header, packets[0].Body)
	}

	// Same-session duplicate (double click): suppressed so the client handler
	// never sees the receipt twice.
	start = connection.write.Len()
	if err := service.handleCurrentFinishQuestWithResources(session, request, repositories, catalog, finishBridgeProgression(t), allocator); err != nil {
		t.Fatalf("duplicate finish: %v", err)
	}
	if got := connection.write.Bytes()[start:]; len(got) != 0 {
		t.Fatalf("same-session duplicate sent %d bytes: %x", len(got), got)
	}

	// Reconnect (fresh session): the durable replay receipt is served again.
	reconnect := &gameSession{conn: connection, connID: "finish-quest-reconnect", selectedCharacterID: 19}
	start = connection.write.Len()
	if err := service.handleCurrentFinishQuestWithResources(reconnect, request, repositories, catalog, finishBridgeProgression(t), allocator); err != nil {
		t.Fatalf("reconnect finish: %v", err)
	}
	packets = splitAllFinishBridgePackets(t, connection.write.Bytes()[start:])
	if len(packets) == 0 || packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) || len(packets[0].Body) < 2 || packets[0].Body[0] != 1 {
		t.Fatalf("reconnect replay receipt missing: %+v", packets)
	}
	// The first durable replay is also a terminal receipt. A retry in that same
	// new session must be suppressed just like a live double click.
	start = connection.write.Len()
	if err := service.handleCurrentFinishQuestWithResources(reconnect, request, repositories, catalog, finishBridgeProgression(t), allocator); err != nil {
		t.Fatalf("reconnect duplicate finish: %v", err)
	}
	if got := connection.write.Bytes()[start:]; len(got) != 0 {
		t.Fatalf("same-session reconnect replay sent %d bytes: %x", len(got), got)
	}

	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if stack := inventory.Slots["0:65"]; len(inventory.Slots) != 1 || stack.Count != 2 {
		t.Fatalf("duplicate click duplicated reward inventory=%+v", inventory.Slots)
	}
	character, _, _ := repositories.Character.Load(context.Background(), "19")
	skill, _, _ := repositories.Skill.Load(context.Background(), "19")
	if character.Level != 2 || character.Stats["exp"] != 100 || character.Stats["gold"] != 10 || skill.Points.TotalSP != 30 || skill.Points.SyncedLevel != 2 {
		t.Fatalf("post commit character=%+v skill=%+v", character, skill.Points)
	}
}

func TestCurrentFinishQuestReplayKeyIsCharacterScoped(t *testing.T) {
	session := &gameSession{}
	characterA := newCurrentFinishQuestReplayKey(19, dnfquest.FinishQuestRequest{QuestID: 3145})
	characterB := newCurrentFinishQuestReplayKey(20, dnfquest.FinishQuestRequest{QuestID: 3145})
	session.markCurrentFinishQuestAnswered(characterA)
	if !session.currentFinishQuestAnswered(characterA) {
		t.Fatal("first character finish receipt was not recorded")
	}
	if session.currentFinishQuestAnswered(characterB) {
		t.Fatal("finish receipt leaked across selected characters with the same quest ID")
	}
}

func TestCurrentFinishQuestAckWriteFailureLeavesReplayRetryable(t *testing.T) {
	repositories := finishBridgeSeed(t)
	wantErr := errors.New("finish ACK write failed")
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       finishBridgeCatalog(t),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                &failNthDungeonWriteConn{failAt: 1, err: wantErr},
		connID:              "finish-quest-ack-write-retry",
		selectedCharacterID: 19,
	}
	request := dnfquest.FinishQuestRequest{
		QuestID:           3145,
		RewardSelectIndex: ^uint16(0),
		Multiplier:        1,
		Reserved:          dnfquest.CurrentFinishQuestObservedTailMarker,
	}
	catalog := finishBridgeCatalog(t)
	if err := service.handleCurrentFinishQuestWithResources(session, request, repositories, catalog, finishBridgeProgression(t), finishBridgeAllocator(nil)); !errors.Is(err, wantErr) {
		t.Fatalf("first finish error=%v want=%v", err, wantErr)
	}
	replayKey := newCurrentFinishQuestReplayKey(19, request)
	if session.currentFinishQuestAnswered(replayKey) {
		t.Fatal("failed finish ACK was incorrectly marked as delivered")
	}

	connection := &bufferConn{}
	session.conn = connection
	if err := service.handleCurrentFinishQuestWithResources(session, request, repositories, catalog, finishBridgeProgression(t), finishBridgeAllocator(nil)); err != nil {
		t.Fatalf("durable replay retry: %v", err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if len(packets) == 0 || packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) || len(packets[0].Body) < 2 || packets[0].Body[0] != 1 {
		t.Fatalf("durable replay ACK missing after write retry: %+v", packets)
	}
	if !session.currentFinishQuestAnswered(replayKey) {
		t.Fatal("successful durable replay ACK was not marked as delivered")
	}
}

func TestCurrentFinishQuestAppliesActiveGrowthContractPVFExperienceBonus(t *testing.T) {
	repositories := finishBridgeSeed(t)
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: defaultAccountPrefix + "1",
		Metadata: map[string]string{
			premium.MetadataKey(premium.TypeBonusExp): fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix()),
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	catalog := finishBridgeCatalog(t)
	service := &Service{
		options:            options{accountID: defaultAccountPrefix + "1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		premiumCatalog: &currentPremiumCatalog{
			effectsByType: map[int64]currentPremiumEffectInfo{
				premium.TypeBonusExp: {BonusExperiencePercent: 20},
			},
		},
	}
	session := &gameSession{conn: connection, connID: "finish-quest-growth-contract", selectedCharacterID: 19}
	request := dnfquest.FinishQuestRequest{
		QuestID:           3145,
		RewardSelectIndex: ^uint16(0),
		Multiplier:        1,
		Reserved:          dnfquest.CurrentFinishQuestObservedTailMarker,
	}
	if err := service.handleCurrentFinishQuestWithResources(
		session,
		request,
		repositories,
		catalog,
		finishBridgeProgression(t),
		finishBridgeAllocator(nil),
	); err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if len(packets) == 0 || packets[0].Header.MsgID != uint16(dnfenum.CmdPacketFinishQuest) ||
		len(packets[0].Body) < 8 || binary.LittleEndian.Uint32(packets[0].Body[4:8]) != 120 {
		t.Fatalf("growth-contract finish ACK=%+v", packets)
	}
	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found || character.Stats["exp"] != 120 {
		t.Fatalf("growth-contract committed character=%+v found=%t err=%v", character, found, err)
	}
}

func TestCurrentFinishQuestTransactionFailureEmitsZeroAck(t *testing.T) {
	repositories := finishBridgeSeed(t)
	wantErr := errors.New("settlement commit rejected")
	repositories.CharacterSettlement = finishBridgeFailingSettlement{err: wantErr}
	connection := &bufferConn{}
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	session := &gameSession{conn: connection, selectedCharacterID: 19}
	request := dnfquest.FinishQuestRequest{
		QuestID:           3145,
		RewardSelectIndex: ^uint16(0),
		Multiplier:        1,
		Reserved:          dnfquest.CurrentFinishQuestObservedTailMarker,
	}
	if err := service.handleCurrentFinishQuestWithResources(session, request, repositories, finishBridgeCatalog(t), finishBridgeProgression(t), finishBridgeAllocator(nil)); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("failed transaction emitted packet(s)=%x", connection.write.Bytes())
	}
	questRecord, _, _ := repositories.Quest.Load(context.Background(), "19")
	if state := questRecord.States[3145]; state.Status != "active" || state.Extra["reward_state"] != "pending" {
		t.Fatalf("failed transaction mutated quest=%+v", state)
	}
}

func assertFinishBridgePostCommitOrder(t *testing.T, packets []dnfproto.ChannelPacket, completedQuestID uint16) {
	t.Helper()
	if len(packets) < 5 {
		t.Fatalf("post-commit packet count=%d", len(packets))
	}
	index := func(class byte, msg uint16) int {
		for i, packet := range packets {
			if packet.Header.Classification == class && packet.Header.MsgID == msg {
				return i
			}
		}
		return -1
	}
	ack := index(dnfproto.DefaultChannelClassification, uint16(dnfenum.CmdPacketFinishQuest))
	item := index(0, uint16(dnfenum.CmdPacketWalkoutPartyMember))
	acceptable := index(0, currentAcceptableQuestListMsgID)
	active := index(0, currentActiveQuestSnapshotMsgID)
	clearQuest := index(0, currentClearQuestListMsgID)
	character := index(0, currentDungeonCharacterStateMsgID)
	skill := index(0, currentSkillInfoMsgID)
	mode := func(value byte) int {
		for i, packet := range packets {
			if packet.Header.Classification == 0 &&
				packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) &&
				len(packet.Body) != 0 && packet.Body[0] == value {
				return i
			}
		}
		return -1
	}
	mode1, mode3 := mode(1), mode(3)
	stateStart := ack
	if item >= 0 {
		if item <= ack {
			t.Fatalf("post-commit item order ack=%d item=%d packets=%v", ack, item, finishBridgePacketIDs(packets))
		}
		stateStart = item
	}
	if mode3 >= 0 {
		t.Fatalf("post-commit unexpectedly sent panel-opening mode3=%d packets=%v", mode3, finishBridgePacketIDs(packets))
	}
	if mode1 >= 0 {
		if mode1 <= stateStart {
			t.Fatalf("post-commit mode1 order ack=%d item=%d mode1=%d packets=%v", ack, item, mode1, finishBridgePacketIDs(packets))
		}
		stateStart = mode1
	}
	if !(ack == 0 && character > stateStart && skill > character && clearQuest > skill && active > clearQuest && acceptable > active) {
		t.Fatalf("post-commit order ack=%d item=%d mode1=%d mode3=%d clear=%d acceptable=%d active=%d character=%d skill=%d packets=%v",
			ack, item, mode1, mode3, clearQuest, acceptable, active, character, skill, finishBridgePacketIDs(packets))
	}
	assertFinishBridgeClearQuestProjection(t, packets[clearQuest], completedQuestID)
	if item >= 0 {
		if len(packets[item].Body) != 3+2*currentItemListEntryWireSize || packets[item].Body[0] != 0 ||
			binary.LittleEndian.Uint16(packets[item].Body[1:3]) != 2 ||
			binary.LittleEndian.Uint32(packets[item].Body[5:9]) != 0 ||
			binary.LittleEndian.Uint32(packets[item].Body[9:13]) != 10 ||
			binary.LittleEndian.Uint32(packets[item].Body[3+currentItemListEntryWireSize+2:3+currentItemListEntryWireSize+6]) != 10403 {
			t.Fatalf("quest reward op14 body=%x", packets[item].Body)
		}
	}
	if len(packets[character].Body) != currentFinishLoadingCharacterStateBodySize || packets[character].Body[0] != 2 || binary.LittleEndian.Uint32(packets[character].Body[1:5]) != 100 {
		t.Fatalf("character snapshot body=%x", packets[character].Body)
	}
	if len(packets[skill].Body) < 4 || int(binary.LittleEndian.Uint32(packets[skill].Body[:4])) != len(packets[skill].Body)-4 {
		t.Fatalf("skill snapshot body=%x", packets[skill].Body)
	}
}

func assertFinishBridgeClearQuestProjection(t *testing.T, packet dnfproto.ChannelPacket, completedQuestID uint16) {
	t.Helper()
	if packet.Header.Classification != 0 || packet.Header.MsgID != currentClearQuestListMsgID {
		t.Fatalf("clear-quest packet=%d/%d want=0/%d", packet.Header.Classification, packet.Header.MsgID, currentClearQuestListMsgID)
	}
	// splitAllFinishBridgePackets parses this mixed stream with the ordinary
	// 13-byte header. op356 uses the proved fixed16 header, so its two reserved
	// bytes and protected-body marker remain at the start of Body.
	if len(packet.Body) < 4 || packet.Body[0] != 0 || packet.Body[1] != 0 || packet.Body[2] != 1 {
		t.Fatalf("clear-quest fixed16 suffix/body=%x", packet.Body)
	}
	plain, err := zlibDecompress(packet.Body[3:])
	if err != nil {
		t.Fatalf("decompress committed clear-quest projection: %v", err)
	}
	if len(plain) != 4+currentClearQuestListPayloadSize ||
		binary.LittleEndian.Uint32(plain[:4]) != currentClearQuestListPayloadSize {
		t.Fatalf("committed clear-quest projection len=%d prefix=%x", len(plain), plain[:minInt(len(plain), 4)])
	}
	if int(completedQuestID) >= currentClearQuestListPayloadSize {
		t.Fatalf("committed quest %d exceeds clear-quest payload", completedQuestID)
	}
	if plain[4+int(completedQuestID)] != 1 {
		t.Fatalf("committed quest %d clear-quest flag=%d", completedQuestID, plain[4+int(completedQuestID)])
	}
}

func assertFinishBridgeNoPersonalInfoSnapshots(t *testing.T, packets []dnfproto.ChannelPacket) {
	t.Helper()
	for _, packet := range packets {
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) &&
			len(packet.Body) != 0 &&
			(packet.Body[0] == 1 || packet.Body[0] == 3) {
			t.Fatalf("ordinary finish pushed personal-info mode=%d ids=%v", packet.Body[0], finishBridgePacketIDs(packets))
		}
	}
}

func assertFinishBridgeSuccessorSnapshots(t *testing.T, packets []dnfproto.ChannelPacket, completedQuestID, successorQuestID uint16) {
	t.Helper()
	activeIndex := -1
	acceptableIndex := -1
	activeCount := 0
	acceptableCount := 0
	for index, packet := range packets {
		if packet.Header.Classification != 0 {
			continue
		}
		switch packet.Header.MsgID {
		case currentActiveQuestSnapshotMsgID:
			activeCount++
			activeIndex = index
		case currentAcceptableQuestListMsgID:
			acceptableCount++
			acceptableIndex = index
		}
	}
	if activeCount != 1 || acceptableCount != 1 {
		t.Fatalf("post-finish quest snapshot counts active=%d acceptable=%d packets=%v", activeCount, acceptableCount, finishBridgePacketIDs(packets))
	}

	activeBody := packets[activeIndex].Body
	if len(activeBody) < 4 {
		t.Fatalf("active snapshot body too short: %x", activeBody)
	}
	rowCount := int(binary.LittleEndian.Uint32(activeBody[:4]))
	if len(activeBody) != 4+rowCount*6 {
		t.Fatalf("active snapshot body len=%d count=%d body=%x", len(activeBody), rowCount, activeBody)
	}
	for offset := 4; offset < len(activeBody); offset += 6 {
		questID := binary.LittleEndian.Uint16(activeBody[offset : offset+2])
		if questID == completedQuestID {
			t.Fatalf("completed quest %d remained in active snapshot: %x", completedQuestID, activeBody)
		}
		if questID == successorQuestID {
			t.Fatalf("successor quest %d was auto-activated before manual op31: %x", successorQuestID, activeBody)
		}
	}

	acceptableBody := packets[acceptableIndex].Body
	if len(acceptableBody) < 4 || int(binary.LittleEndian.Uint32(acceptableBody[:4])) != len(acceptableBody)-4 {
		t.Fatalf("acceptable snapshot protobuf length mismatch: %x", acceptableBody)
	}
	_, messages := consumeCurrentSkillInfoFields(t, acceptableBody[4:])
	if len(messages[4]) != 1 {
		t.Fatalf("acceptable snapshot field4 rows=%d body=%x", len(messages[4]), acceptableBody)
	}
	questIDs := consumePackedQuestIDs(t, messages[4][0])
	foundSuccessor := false
	for _, questID := range questIDs {
		if questID == int32(completedQuestID) {
			t.Fatalf("completed quest %d remained in acceptable snapshot: %v", completedQuestID, questIDs)
		}
		if questID == int32(successorQuestID) {
			foundSuccessor = true
		}
	}
	if !foundSuccessor {
		t.Fatalf("successor quest %d missing from acceptable snapshot: %v", successorQuestID, questIDs)
	}
}

func splitAllFinishBridgePackets(t *testing.T, data []byte) []dnfproto.ChannelPacket {
	t.Helper()
	packets := make([]dnfproto.ChannelPacket, 0, 12)
	for len(data) > 0 {
		packet, rest := splitGameServerUpperPacket(t, data)
		packets = append(packets, packet)
		data = rest
	}
	return packets
}

func finishBridgePacketIDs(packets []dnfproto.ChannelPacket) []string {
	out := make([]string, 0, len(packets))
	for _, packet := range packets {
		out = append(out, fmt.Sprintf("%d/%d", packet.Header.Classification, packet.Header.MsgID))
	}
	return out
}

func finishBridgeSeed(t *testing.T) dnfrepo.Group {
	t.Helper()
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, save := range []func() error{
		func() error {
			return repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: defaultAccountPrefix + "1", Job: "2", Level: 1, Stats: map[string]int64{"exp": 0, "grow_type": 0}})
		},
		func() error {
			return repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{3145: {Status: "active", ProgressValue: 0, Extra: map[string]string{"completion_key": "run-19/op117/426", "completion_kind": "clear_map", "reward_state": "pending"}}}})
		},
		func() error {
			return repositories.Skill.Save(ctx, dnfrepo.SkillRecord{CharacterID: "19", Points: dnfrepo.SkillPointState{SyncedLevel: 1}, Layouts: map[int]dnfrepo.SkillLayout{currentSkillInfoTreeIndex: {}}})
		},
		func() error {
			return repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}})
		},
		func() error { return repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19"}) },
	} {
		if err := save(); err != nil {
			t.Fatal(err)
		}
	}
	return repositories
}

func finishBridgeCatalog(t *testing.T) *dnfquest.Catalog {
	t.Helper()
	source := finishBridgePVFSource{
		dnfquest.DefaultList: "3145 `finish.qst`\n3146 `next.qst`\n",
		"n_quest/finish.qst": "[grade]\n`[epic]`\n[type]\n`[clear map]`\n[difficulty]\n`N`\n[reward type]\n`[item]`\n[reward int data]\n0 1 10403 2\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[int data]\n76126 0 0 1\n",
		"n_quest/next.qst":   "[grade]\n`[epic]`\n[type]\n`[quest clear]`\n[difficulty]\n`N`\n[reward type]\n`[item]`\n[reward int data]\n10404 1\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[pre required quest]\n3145\n[int data]\n3157 3054\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func finishBridgeExpertJobQuestCatalog(t *testing.T, questID uint16, jobType byte) *dnfquest.Catalog {
	t.Helper()
	source := finishBridgePVFSource{
		dnfquest.DefaultList: fmt.Sprintf("%d `expert.qst`\n", questID),
		"n_quest/expert.qst": fmt.Sprintf("[grade]\n`[side]`\n[type]\n`[seeking]`\n[difficulty]\n`N`\n[reward type]\n`[expert job]`\n[reward int data]\n%d\n[level]\n1 99\n[job]\n`[all]`\n[exposed by npc]\n1\n", jobType),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func finishBridgeProfessionCatalog(t *testing.T) *dnfquest.Catalog {
	t.Helper()
	source := finishBridgePVFSource{
		dnfquest.DefaultList: "100 `change.qst`\n",
		"n_quest/change.qst": "[grade]\n`[epic]`\n[type]\n`[meet npc]`\n[difficulty]\n`N`\n[reward type]\n`[grow type]`\n[reward int data]\n2\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[job change quest]\n1\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func finishBridgeSlotExpansionCatalog(t *testing.T) *dnfquest.Catalog {
	t.Helper()
	source := finishBridgePVFSource{
		dnfquest.DefaultList:  "2636 `earring.qst`\n",
		"n_quest/earring.qst": "[grade]\n`[side]`\n[type]\n`[meet npc]`\n[difficulty]\n`N`\n[reward type]\n`[slot expansion]`\n[reward int data]\n2\n[level]\n1 99\n[job]\n`[all]`\n[exposed by npc]\n1\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func finishBridgeProfessionResources(t *testing.T) (*dnfprofession.Profiles, *dnfskill.Table) {
	t.Helper()
	source := finishBridgePVFSource{
		dnfprofession.DefaultCharacterList: "2 `Gunner/Gunner.chr`\n",
		"character/Gunner/Gunner.chr":      "[initial value]\n[skill]\n1 1\n[/skill]\n[growtype 3]\n[skill]\n2 1\n197 1\n[/skill]\n",
		dnfskill.DefaultList:               "2 `GunnerSkill.lst`\n",
		"skill/GunnerSkill.lst":            "1 `Gunner/a.skl` 2 `Gunner/b.skl` 197 `Gunner/mastery.skl`\n",
		"skill/Gunner/a.skl":               "[name]\n`a`\n[type]\n`[active]`\n",
		"skill/Gunner/b.skl":               "[name]\n`b`\n[type]\n`[passive]`\n",
		"skill/Gunner/mastery.skl":         "[name]\n`mastery`\n[type]\n`[passive]`\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfskill.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfskill.Load(context.Background(), index, dnfskill.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := dnfprofession.LoadProfiles(context.Background(), source, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return profiles, catalog
}

func finishBridgeProgression(t *testing.T) *progression.Tables {
	t.Helper()
	tables, err := progression.Load(context.Background(), finishBridgePVFSource{
		progression.ExperienceTablePath: "100 250 500\n",
		progression.SkillPointTablePath: "[sp table]\n1 0\n2 30\n3 30\n4 40\n[/sp table]\n[tp table]\n50 1\n[/tp table]\n",
		progression.QuestParameterPath:  "[difficulty]\n`N` 100\n[/difficulty]\n[exp reward table]\n100 -1\n200 -1\n300 -1\n[gold reward table]\n10 -1\n[green level penalty]\n80\n[grey level penalty]\n30\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	return tables
}

func finishBridgeAllocator(fail error) dnfquest.FinishItemAllocator {
	return func(record *dnfrepo.InventoryRecord, request dnfquest.FinishItemGrantRequest) (dnfquest.FinishCommittedItem, error) {
		if record.Slots == nil {
			record.Slots = make(map[string]dnfrepo.ItemStack)
		}
		const slot = uint16(65)
		const key = "0:65"
		postCount := request.Count
		if current, ok := record.Slots[key]; ok {
			postCount += current.Count
		}
		raw := make([]byte, currentItemListEntryWireSize)
		binary.LittleEndian.PutUint16(raw[0:2], slot)
		binary.LittleEndian.PutUint32(raw[2:6], uint32(request.ItemID))
		binary.LittleEndian.PutUint32(raw[6:10], uint32(postCount))
		record.Slots[key] = dnfrepo.ItemStack{ItemID: request.ItemID, Count: postCount, RawEntry: append([]byte(nil), raw...)}
		if fail != nil {
			return dnfquest.FinishCommittedItem{}, fail
		}
		return dnfquest.FinishCommittedItem{SlotKey: key, SlotIndex: slot, ItemID: request.ItemID, Delta: request.Count, PostCount: postCount, CountOrSeed: uint32(request.Count), RawEntry: raw}, nil
	}
}

type finishBridgePVFSource map[string]string

func (s finishBridgePVFSource) ReadText(path string) (string, error) {
	text, ok := s[path]
	if !ok {
		return "", fmt.Errorf("missing %s", path)
	}
	return text, nil
}

type finishBridgeFailingSettlement struct{ err error }

func (f finishBridgeFailingSettlement) WithinCharacterSettlement(context.Context, string, func(dnfrepo.Group) error) error {
	return f.err
}

var _ dnfrepo.CharacterSettlementUnitOfWork = finishBridgeFailingSettlement{}
