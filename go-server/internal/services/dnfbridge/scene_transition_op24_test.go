package dnfbridge

import (
	"bytes"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildCurrentSceneTransitionBody(t *testing.T) {
	body, err := buildCurrentSceneTransitionBody(1, 0, nil)
	if err != nil {
		t.Fatalf("build zero-row op24 body: %v", err)
	}
	if want := []byte{1, 0, 0, 0}; !bytes.Equal(body, want) {
		t.Fatalf("zero-row op24 body = %x, want %x", body, want)
	}

	body, err = buildCurrentSceneTransitionBody(2, 3, []currentSceneTransitionRow{{
		ObjectOrResourceKey: 0x1122,
		Value1:              0x3344,
		Value2:              0x5566,
		Value3:              0x77,
		Value4:              0x88,
	}})
	if err != nil {
		t.Fatalf("build one-row op24 body: %v", err)
	}
	want := []byte{2, 3, 1, 0, 0x22, 0x11, 0x44, 0x33, 0x66, 0x55, 0x77, 0x88}
	if !bytes.Equal(body, want) {
		t.Fatalf("one-row op24 body = %x, want %x", body, want)
	}
}

func TestCurrentSceneTransitionLocationRequiresRealFields(t *testing.T) {
	tests := []struct {
		name         string
		character    dnfrepo.CharacterRecord
		hasCharacter bool
		wantTown     byte
		wantArea     byte
		wantErr      bool
	}{
		{name: "real fields", character: dnfrepo.CharacterRecord{Stats: map[string]int64{"town_id": 1, "area_id": 0}}, hasCharacter: true, wantTown: 1, wantArea: 0},
		{name: "record missing", wantErr: true},
		{name: "stats missing", character: dnfrepo.CharacterRecord{}, hasCharacter: true, wantErr: true},
		{name: "town missing", character: dnfrepo.CharacterRecord{Stats: map[string]int64{"area_id": 0}}, hasCharacter: true, wantErr: true},
		{name: "area missing", character: dnfrepo.CharacterRecord{Stats: map[string]int64{"town_id": 1}}, hasCharacter: true, wantErr: true},
		{name: "town negative", character: dnfrepo.CharacterRecord{Stats: map[string]int64{"town_id": -1, "area_id": 0}}, hasCharacter: true, wantErr: true},
		{name: "area over u8", character: dnfrepo.CharacterRecord{Stats: map[string]int64{"town_id": 1, "area_id": 256}}, hasCharacter: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			townID, areaID, err := currentSceneTransitionLocation(test.character, test.hasCharacter)
			if (err != nil) != test.wantErr {
				t.Fatalf("location error = %v, wantErr=%v", err, test.wantErr)
			}
			if err == nil && (townID != test.wantTown || areaID != test.wantArea) {
				t.Fatalf("location = (%d,%d), want (%d,%d)", townID, areaID, test.wantTown, test.wantArea)
			}
		})
	}
}
