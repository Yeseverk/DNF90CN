package joust

import (
	"context"
	"fmt"
	"strings"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

type joustCatalogTestSource map[string]string

func (s joustCatalogTestSource) ReadText(path string) (string, error) {
	text, ok := s[path]
	if !ok {
		return "", dnfpvf.ErrDocNotFound
	}
	return text, nil
}

func TestLoadCatalogReadsRidersAndBattleTables(t *testing.T) {
	catalog, err := LoadCatalog(context.Background(), joustCatalogTestSource{EventPVFPath: testJoustPVF(12)})
	if err != nil {
		t.Fatal(err)
	}
	riders := catalog.Riders()
	if len(riders) != 12 || riders[0].ID != 0 || riders[0].AttackType != 0 || riders[0].Name != "骑士0" ||
		len(riders[0].Win) != 28 || len(riders[0].Loss) != 28 || riders[11].ID != 11 {
		t.Fatalf("riders=%+v", riders)
	}
	riders[0].Win[0] = 999
	if catalog.Riders()[0].Win[0] == 999 {
		t.Fatal("catalog rider slices were not detached")
	}
}

func TestLoadCatalogRejectsTooFewRiders(t *testing.T) {
	_, err := LoadCatalog(context.Background(), joustCatalogTestSource{EventPVFPath: testJoustPVF(7)})
	if err == nil || !strings.Contains(err.Error(), "want-at-least=8") {
		t.Fatalf("error=%v", err)
	}
}

func testJoustPVF(riderCount int) string {
	var text strings.Builder
	text.WriteString("[min level]\n50\n[max betting]\n10000\n[reward]\n490005585\n")
	text.WriteString("[betting reward]\n490005593\n[material]\n490005585 490005586\n[/material]\n[knight info]\n")
	for index := 0; index < riderCount; index++ {
		attackType := index % 2
		if index >= 10 {
			attackType = 28
		}
		fmt.Fprintf(&text, "[knight]\n[index]\n%d\n[attack type]\n%d\n[knight name]\n`骑士%d`\n", index, attackType, index)
		text.WriteString("[win]\n")
		for value := 0; value < 28; value++ {
			fmt.Fprintf(&text, "%d ", []int{10, 25, 40, 55, 70, 85, 100}[value%7])
		}
		text.WriteString("\n[/win]\n[loss]\n")
		for value := 0; value < 28; value++ {
			fmt.Fprintf(&text, "%d ", []int{10, 25, 40, 55, 70, 85, 100}[value%7])
		}
		text.WriteString("\n[/loss]\n[/knight]\n")
	}
	text.WriteString("[/knight info]\n")
	return text.String()
}
