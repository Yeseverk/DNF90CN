package mysql

import (
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"testing"
)

func TestScanPetEntriesAcceptsLegacyCreatureMap(t *testing.T) {
	var record repository.PetRecord
	err := scanPetEntries(sql.NullString{
		Valid:  true,
		String: `{"37":{"pet_key":"37","item_id":63000,"level":4}}`,
	}, &record)
	if err != nil {
		t.Fatalf("scanPetEntries() error = %v", err)
	}
	if len(record.Artifacts) != 0 || record.Entries["37"].ItemID != 63000 || record.Entries["37"].Level != 4 {
		t.Fatalf("legacy record = %+v", record)
	}
}

func TestPetEntriesEnvelopeRoundTripsArtifactsWithoutSlotNumbers(t *testing.T) {
	want := repository.PetRecord{
		Entries: map[string]repository.PetEntry{
			"37": {PetKey: "37", CreatureKey: 37, ItemID: 63000, Level: 3},
		},
		Artifacts: map[string]repository.ItemStack{
			"red": {
				ItemID:   63500,
				Count:    1,
				RawEntry: []byte{1, 2, 3},
				Extra:    map[string]string{"serial": "9001"},
			},
		},
	}

	value, err := petEntriesJSONArg(want)
	if err != nil {
		t.Fatalf("petEntriesJSONArg() error = %v", err)
	}
	raw, ok := value.(string)
	if !ok {
		t.Fatalf("json argument type = %T", value)
	}
	if strings.Contains(raw, `"slot"`) || !strings.Contains(raw, `"version":1`) || !strings.Contains(raw, `"artifacts"`) {
		t.Fatalf("unexpected envelope: %s", raw)
	}

	var got repository.PetRecord
	if err := scanPetEntries(sql.NullString{Valid: true, String: raw}, &got); err != nil {
		t.Fatalf("scanPetEntries() error = %v", err)
	}
	if got.Entries["37"].ItemID != 63000 {
		t.Fatalf("creatures = %+v", got.Entries)
	}
	artifact := got.Artifacts["red"]
	if artifact.ItemID != 63500 || artifact.Count != 1 || string(artifact.RawEntry) != string([]byte{1, 2, 3}) || artifact.Extra["serial"] != "9001" {
		t.Fatalf("artifact = %+v", artifact)
	}
}

func TestScanPetEntriesRejectsUnknownEnvelopeVersion(t *testing.T) {
	var record repository.PetRecord
	err := scanPetEntries(sql.NullString{Valid: true, String: `{"version":2,"creatures":{}}`}, &record)
	if err == nil || !strings.Contains(err.Error(), "unsupported pet entries envelope version 2") {
		t.Fatalf("error = %v", err)
	}
}

func TestClonePetDetachesArtifacts(t *testing.T) {
	record := repository.PetRecord{Artifacts: map[string]repository.ItemStack{
		"blue": {ItemID: 64000, Count: 1, RawEntry: []byte{7}, Extra: map[string]string{"serial": "8"}},
	}}
	clone := repository.ClonePet(record)
	artifact := clone.Artifacts["blue"]
	artifact.RawEntry[0] = 9
	artifact.Extra["serial"] = "10"
	clone.Artifacts["blue"] = artifact
	if record.Artifacts["blue"].RawEntry[0] != 7 || record.Artifacts["blue"].Extra["serial"] != "8" {
		t.Fatalf("clone aliases original: original=%+v clone=%+v", record.Artifacts["blue"], clone.Artifacts["blue"])
	}
}
