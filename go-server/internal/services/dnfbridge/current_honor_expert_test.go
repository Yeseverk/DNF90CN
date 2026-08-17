package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestCurrentHonorExpertExperienceGainSkipsPreCapAwardsWithoutCapTable(t *testing.T) {
	got, err := currentHonorExpertExperienceGain(nil, 20, 0, 200)
	if err != nil || got != 0 {
		t.Fatalf("pre-cap gain=%d err=%v, want zero without cap-table lookup", got, err)
	}

	got, err = currentHonorExpertExperienceGain(nil, currentAdventureCharacterLevelCap, 123, 200)
	if err != nil || got != 200 {
		t.Fatalf("at-cap gain=%d err=%v, want complete award", got, err)
	}
}

func TestCurrentHonorExpertStateProjectsAcrossCurrentEXECarriers(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		Level:       currentAdventureCharacterLevelCap,
		Stats: map[string]int64{
			"exp":                          currentAdventureCharacterLevelCap,
			currentHonorExpertLevelStatKey: 2,
			currentHonorExpertProgressExperienceStatKey: 0x0102030405060708,
		},
	}
	const wantLevel uint32 = 2
	const wantProgress uint64 = 0x0102030405060708

	service := &Service{}
	mode1 := service.buildCurrentSelectedUserInfoMode1BodyWithAdventureLevel(
		context.Background(), nil, nil, character, true, 19, 0,
	)
	if got := binary.LittleEndian.Uint32(mode1[7:11]); got != wantLevel {
		t.Fatalf("mode1 HonorExpert level=%d want=%d", got, wantLevel)
	}
	if got := binary.LittleEndian.Uint64(mode1[11:19]); got != wantProgress {
		t.Fatalf("mode1 HonorExpert progress=%x want=%x", got, wantProgress)
	}

	finishLoading := buildCurrentFinishLoadingCharacterStateBody(character, dnfrepo.SkillPointState{})
	if got := binary.LittleEndian.Uint32(finishLoading[55:59]); got != wantLevel {
		t.Fatalf("op37 HonorExpert level=%d want=%d", got, wantLevel)
	}
	if got := binary.LittleEndian.Uint64(finishLoading[59:67]); got != wantProgress {
		t.Fatalf("op37 HonorExpert progress=%x want=%x", got, wantProgress)
	}

	var tail packetWriter
	writeCurrentSceneObjectEntryTail(&tail, character, true)
	var want packetWriter
	want.writeUint32(wantLevel)
	want.writeUint64(wantProgress)
	if occurrences := bytes.Count(tail.bytes(), want.bytes()); occurrences != 1 {
		t.Fatalf("actor-tail HonorExpert occurrences=%d want=1 body=%x", occurrences, tail.bytes())
	}
}
