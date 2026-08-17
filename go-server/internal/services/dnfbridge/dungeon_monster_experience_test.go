package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
	"time"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfhonor "longheng.io/server/internal/modules/dnf/honor"
	"longheng.io/server/internal/modules/dnf/premium"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestCurrentDungeonMonsterExperienceUsesRuntimePVFBaseAnd86JPRounding(t *testing.T) {
	service, runtime, _ := prepareCurrentDungeonMonsterExperienceTest(t)
	catalog, err := service.dungeonMonsterCatalog()
	if err != nil {
		t.Fatal(err)
	}
	resources, err := currentDungeonMonsterExperienceResourcesForCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	monster := runtime.Room.Snapshot().Monsters[0]

	// 86JP-domain arithmetic, with this test's runtime PVF base (100):
	// int(100 * 1.25) = 125; int(125 * 1.30) = 162;
	// uint(162 * 1.12) = 181.
	runtime.Character.Level = 19
	runtime.Dungeon.Metadata.ExperienceIncreasingPoint = worldmap.OptionalNumber{Value: 1.25, Set: true}
	runtime.Request.Difficulty = 0
	award, err := currentDungeonMonsterExperienceAwardFor(runtime, monster, resources.Sources)
	if err != nil {
		t.Fatal(err)
	}
	if award.MonsterTableEXP != 100 || award.MonsterLevel != 20 || award.MonsterType != 0 ||
		award.NamedMonster || award.PrePenaltyGain != 162 || award.Gain != 181 {
		t.Fatalf("award=%+v", award)
	}

	// Named is supplied by the parsed runtime dungeon metadata; it is not
	// inferred from an actor label or a C# packet.  The authorized domain rule
	// triples the type rate before the final killer-level penalty.
	runtime.Character.Level = 20
	runtime.Dungeon.Metadata.ExperienceIncreasingPoint = worldmap.OptionalNumber{Value: 1, Set: true}
	runtime.Dungeon.Metadata.NamedMonsters = []int64{monster.Spawn.MonsterID}
	award, err = currentDungeonMonsterExperienceAwardFor(runtime, monster, resources.Sources)
	if err != nil {
		t.Fatal(err)
	}
	if !award.NamedMonster || award.MonsterTypeRate != 3 || award.PrePenaltyGain != 390 || award.Gain != 390 {
		t.Fatalf("named award=%+v", award)
	}
}

func TestCurrentDungeonMonsterDeathPersistsExperienceAndSendsCurrentEXEOp37(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonMonsterExperienceTest(t)
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "dungeon-monster-experience-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	monster := runtime.Room.Snapshot().Monsters[0]
	request := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(request[0:4], monster.ObjectKey)
	binary.LittleEndian.PutUint16(request[4:6], currentSceneActorObjectKey(99))

	if err := service.handleDungeonMonsterDeath(session, request); err != nil {
		t.Fatal(err)
	}
	death, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if death.Header.Classification != 0 || death.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		len(death.Body) != currentDungeonZeroDropDeathBodySize {
		t.Fatalf("death=%+v body=%x", death.Header, death.Body)
	}
	state, rest := splitGameServerUpperPacket(t, rest)
	if state.Header.Classification != 0 || state.Header.MsgID != currentDungeonCharacterStateMsgID ||
		len(state.Body) != currentFinishLoadingCharacterStateBodySize {
		t.Fatalf("state=%+v body_len=%d rest=%x", state.Header, len(state.Body), rest)
	}
	assertCurrentDungeonFinalClearTail(t, rest)
	if state.Body[0] != 20 || binary.LittleEndian.Uint32(state.Body[1:5]) != 200 || state.Body[0x2e] != 0 {
		t.Fatalf("op37 body=%x", state.Body)
	}

	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("character found=%t err=%v", found, err)
	}
	if character.Level != 20 || character.Stats["exp"] != 200 {
		t.Fatalf("persisted character=%+v", character)
	}
	skill, found, err := repositories.Skill.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("skill found=%t err=%v", found, err)
	}
	if skill.Points != (dnfrepo.SkillPointState{TotalSP: 20, RemainingSP: 20, SyncedLevel: 20}) {
		t.Fatalf("persisted skill points=%+v", skill.Points)
	}
	if runtime.Character.Level != 20 || runtime.Character.Stats["exp"] != 200 {
		t.Fatalf("runtime character=%+v", runtime.Character)
	}

	// The room's accepted-death transition remains the duplicate guard.  A
	// byte-identical retry cannot emit a second op37 or mutate EXP again.
	connection.write.Reset()
	if err := service.handleDungeonMonsterDeath(session, request); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("duplicate death emitted=%x", connection.write.Bytes())
	}
	character, _, err = repositories.Character.Load(context.Background(), "99")
	if err != nil || character.Stats["exp"] != 200 {
		t.Fatalf("duplicate death changed character=%+v err=%v", character, err)
	}
}

func assertCurrentDungeonFinalClearTail(t *testing.T, data []byte) {
	t.Helper()
	bossCheck, rest := splitGameServerUpperPacket(t, data)
	if bossCheck.Header.Classification != 0 || bossCheck.Header.MsgID != uint16(dnfenum.CmdPacketNotifyBossDieCheck) ||
		len(bossCheck.Body) != currentDungeonBossDieCheckResponseSize {
		t.Fatalf("boss check=%+v body=%x rest=%x", bossCheck.Header, bossCheck.Body, rest)
	}
	settlement, rest := splitGameServerUpperPacket(t, rest)
	if settlement.Header.Classification != 0 || settlement.Header.MsgID != currentDungeonSettlementEntryMsgID ||
		!bytes.Equal(settlement.Body, []byte{0}) || len(rest) != 0 {
		t.Fatalf("settlement=%+v body=%x rest=%x", settlement.Header, settlement.Body, rest)
	}
}

func TestCurrentDungeonMonsterExperienceLevelUpCommitsSPWithExperience(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonMonsterExperienceTest(t)
	catalog, err := service.dungeonMonsterCatalog()
	if err != nil {
		t.Fatal(err)
	}
	source := catalog.source.(bridgePVFSource)
	// The level-20 threshold is crossed by this ordinary kill; level 21 has
	// a later threshold so the result is exactly one level and one PVF SP row.
	source[progression.ExperienceTablePath] = strings.TrimSpace(strings.Repeat("0 ", 19) + "100 1000")
	source[progression.SkillPointTablePath] = "[sp table]\n20 20\n21 10\n[/sp table]\n[tp table]\n50 1\n[/tp table]\n"

	session := &gameSession{selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	monster := runtime.Room.Snapshot().Monsters[0]
	result, err := service.awardCurrentDungeonMonsterExperience(context.Background(), session, runtime, monster)
	if err != nil {
		t.Fatal(err)
	}
	if result.Award.Gain != 200 || result.Character.Level != 21 || result.Character.Stats["exp"] != 200 || result.SPGain != 10 ||
		result.Skill.Points != (dnfrepo.SkillPointState{TotalSP: 30, RemainingSP: 30, SyncedLevel: 21}) {
		t.Fatalf("result=%+v", result)
	}
	character, _, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || character.Level != 21 || character.Stats["exp"] != 200 {
		t.Fatalf("persisted character=%+v err=%v", character, err)
	}
	skill, _, err := repositories.Skill.Load(context.Background(), "99")
	if err != nil || skill.Points != result.Skill.Points {
		t.Fatalf("persisted skill=%+v err=%v", skill, err)
	}
}

func TestCurrentDungeonMonsterExperienceAtCapCommitsHonorExpertWithProgression(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonMonsterExperienceTest(t)
	honorTables, err := dnfhonor.LoadTables(honorServiceTestSource{
		dnfhonor.TablePath: strings.Replace(honorServiceTestDocument(), "1 `100`", "1 `5`", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	service.honorTable = honorTables

	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	character.Level = currentAdventureCharacterLevelCap
	character.Stats = map[string]int64{"fatigue": 100, "exp": 123, "grow_type": 0}
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	skill, found, err := repositories.Skill.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load skill found=%t err=%v", found, err)
	}
	skill.Points = dnfrepo.SkillPointState{TotalSP: 20, RemainingSP: 20, SyncedLevel: currentAdventureCharacterLevelCap}
	if err := repositories.Skill.Save(context.Background(), skill); err != nil {
		t.Fatal(err)
	}
	runtime.Character = dnfrepo.CloneCharacter(character)

	session := &gameSession{selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	result, err := service.awardCurrentDungeonMonsterExperience(
		context.Background(), session, runtime, runtime.Room.Snapshot().Monsters[0],
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.HonorExpertGain != 10 || result.Character.Level != currentAdventureCharacterLevelCap ||
		result.Character.Stats["exp"] != 133 ||
		result.Character.Stats[currentHonorExpertLevelStatKey] != 1 ||
		result.Character.Stats[currentHonorExpertProgressExperienceStatKey] != 5 {
		t.Fatalf("result=%+v", result)
	}
	persisted, _, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || persisted.Stats[currentHonorExpertLevelStatKey] != 1 ||
		persisted.Stats[currentHonorExpertProgressExperienceStatKey] != 5 {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
	body := buildCurrentFinishLoadingCharacterStateBody(result.Character, result.Skill.Points)
	if got := binary.LittleEndian.Uint32(body[55:59]); got != 1 {
		t.Fatalf("op37 HonorExpert level=%d", got)
	}
	if got := binary.LittleEndian.Uint64(body[59:67]); got != 5 {
		t.Fatalf("op37 HonorExpert progress=%d", got)
	}
}

func prepareCurrentDungeonMonsterExperienceTest(t *testing.T) (*Service, *runtimeDungeonState, dnfrepo.Group) {
	t.Helper()
	service, runtime, repositories := prepareCurrentDungeonDropTest(t, 0, 3227)
	catalog, err := service.dungeonMonsterCatalog()
	if err != nil {
		t.Fatal(err)
	}
	source, ok := catalog.source.(bridgePVFSource)
	if !ok {
		t.Fatalf("monster catalog source type=%T", catalog.source)
	}
	installCurrentDungeonMonsterExperienceTestPVF(source)
	statTable, err := dnfcharstat.Load(context.Background(), source, dnfcharstat.Options{})
	if err != nil {
		t.Fatalf("load character stat test PVF: %v", err)
	}
	service.characterStats = statTable

	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	character.Level = 20
	character.Job = "0"
	character.Stats = map[string]int64{"fatigue": 100, "exp": 0, "grow_type": 0}
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Skill.Save(context.Background(), dnfrepo.SkillRecord{
		CharacterID: "99",
		Points: dnfrepo.SkillPointState{
			TotalSP: 20, RemainingSP: 20, SyncedLevel: 20,
		},
	}); err != nil {
		t.Fatal(err)
	}
	runtime.Character = dnfrepo.CloneCharacter(character)
	return service, runtime, repositories
}

func installCurrentDungeonMonsterExperienceTestPVF(source bridgePVFSource) {
	source["character/character.lst"] = "0 `test.chr`\n"
	source["character/test.chr"] = "[initial value]\n[HP MAX] 100\n[MP MAX] 100\n[physical attack] 10\n" +
		"[growtype 1]\n[HP MAX] 1\n[MP MAX] 1\n[physical attack] 1\n"
	source[progression.ExperienceTablePath] = strings.TrimSpace(strings.Repeat("1000 ", 20))
	source[progression.SkillPointTablePath] = "[sp table]\n20 20\n[/sp table]\n[tp table]\n50 1\n[/tp table]\n"
	source[progression.QuestParameterPath] = "[difficulty]\n`N` 100\n[/difficulty]\n" +
		"[exp reward table]\n100 -1\n200 -1\n300 -1\n400 -1\n500 -1\n" +
		"[green level penalty]\n80\n[grey level penalty]\n30\n"

	var monsterTable strings.Builder
	for level := 1; level <= 20; level++ {
		value := 30 + (level-1)*3
		if level == 20 {
			value = 100
		}
		monsterTable.WriteString(strconv.Itoa(level))
		monsterTable.WriteByte(' ')
		monsterTable.WriteString(strconv.Itoa(value))
		monsterTable.WriteByte(' ')
	}
	source[progression.MonsterExperienceTablePath] = strings.TrimSpace(monsterTable.String())
	source[progression.ServerParameterPath] = "[monster exp bonusrate]\n1.00\n" +
		"[party user number exp bonusrate]\n1 2 3 4\n" +
		"[party user number exp bonusrate starter server]\n1 2 3 4\n" +
		"[dungeon difficulty exp bonusrate]\n1.30 2.00 2.50 3.00 4.00\n" +
		"[clear exp bonusrate]\n1.00\n" +
		"[clear rank exp bonusrate]\n0.05 0.10 0.12 0.15 0.20\n" +
		"[experience increasing point]\n700 1.00\n[/experience increasing point]\n"
	source[progression.WorldMapExperiencePenaltyPath] = "[penalty table info]\n" +
		"-16 0.30 -15 0.50 -14 0.50 -13 0.50 -12 0.70 -11 0.70 -10 0.70 -9 0.80 -8 0.80 -7 0.80 -6 0.90 -5 0.90 -4 0.90 -3 1.00 -2 1.00 -1 1.00 0 1.00 1 1.00 2 1.00 3 1.00 4 1.00 5 1.00 6 0.90 7 0.90 8 0.90 9 0.80 10 0.80 11 0.80 12 0.70 13 0.70 14 0.70 15 0.50 16 0.50 17 0.50 18 0.30 19 0.30 20 0.30\n" +
		"[/penalty table info]\n"
}

func TestCurrentDungeonMonsterExperienceGrowthContractAddsTwentyPercent(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonMonsterExperienceTest(t)
	service.premiumCatalog = &currentPremiumCatalog{
		effectsByType: map[int64]currentPremiumEffectInfo{
			premium.TypeBonusExp: {BonusExperiencePercent: 20},
		},
	}
	future := time.Now().Add(24 * time.Hour).Unix()
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{"premium_expire_84": strconv.FormatInt(future, 10)},
	}); err != nil {
		t.Fatal(err)
	}
	session := &gameSession{selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	monster := runtime.Room.Snapshot().Monsters[0]
	result, err := service.awardCurrentDungeonMonsterExperience(context.Background(), session, runtime, monster)
	if err != nil {
		t.Fatal(err)
	}
	if result.Award.Gain != 200 || result.GrowthContractBonus != 40 {
		t.Fatalf("result award=%+v bonus=%d, want gain=200 bonus=40", result.Award, result.GrowthContractBonus)
	}
	if result.Character.Stats["exp"] != 240 {
		t.Fatalf("result character exp=%d, want 240", result.Character.Stats["exp"])
	}
	stateBody := buildCurrentFinishLoadingCharacterStateBodyWithPresentation(
		result.Character,
		result.Skill.Points,
		&currentFinishLoadingExperiencePresentation{GrowthContractBonus: result.GrowthContractBonus},
	)
	if binary.LittleEndian.Uint32(stateBody[1:5]) != 240 ||
		binary.LittleEndian.Uint32(stateBody[30:34]) != 40 ||
		binary.LittleEndian.Uint32(stateBody[38:42]) != 0 {
		t.Fatalf("growth-contract op37 body=%x", stateBody)
	}
	character, _, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || character.Stats["exp"] != 240 || character.Level != 20 {
		t.Fatalf("persisted character=%+v err=%v", character, err)
	}

	// Expired contract: no bonus.
	account, _, _ := repositories.Account.Load(context.Background(), "account-1")
	account.Metadata["premium_expire_84"] = strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	if err := repositories.Account.Save(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if bonus := service.currentGrowthContractBonusExp(context.Background(), "account-1", 200); bonus != 0 {
		t.Fatalf("expired contract bonus=%d, want 0", bonus)
	}
}

func TestRecordCurrentDungeonMonsterExperienceTracksResultWindowCategories(t *testing.T) {
	runtime := &runtimeDungeonState{}
	recordCurrentDungeonMonsterExperience(runtime, runtimeDungeonMonster{
		Spawn: worldmap.MonsterSpawn{MonsterID: 1, Rank: "[champion]"},
	}, currentDungeonMonsterExperienceCommitResult{
		Award:               currentDungeonMonsterExperienceAward{MonsterID: 1, MonsterType: 1, PrePenaltyGain: 100},
		GrowthContractBonus: 20,
	})
	recordCurrentDungeonMonsterExperience(runtime, runtimeDungeonMonster{
		Spawn: worldmap.MonsterSpawn{MonsterID: 2, Rank: "[normal]", SuffixMarker: "[boss]"},
	}, currentDungeonMonsterExperienceCommitResult{
		Award:               currentDungeonMonsterExperienceAward{MonsterID: 2, MonsterType: 0, PrePenaltyGain: 300},
		GrowthContractBonus: 60,
	})
	recordCurrentDungeonMonsterExperience(runtime, runtimeDungeonMonster{
		Spawn: worldmap.MonsterSpawn{MonsterID: 3, Rank: "[super champion]"},
	}, currentDungeonMonsterExperienceCommitResult{
		Award: currentDungeonMonsterExperienceAward{
			MonsterID: 3, MonsterType: 2, PrePenaltyGain: 200, NamedMonster: true,
		},
		GrowthContractBonus: 40,
	})

	if runtime.settlementMonsterExperienceTotal != 600 ||
		runtime.settlementMonsterGrowthContractBonus != 120 ||
		runtime.settlementChampionExperience != 100 ||
		runtime.settlementSuperChampionExperience != 0 ||
		runtime.settlementBossExperience != 300 {
		t.Fatalf("monster result statistics=%+v", runtime)
	}
}
