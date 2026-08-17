package dnfbridge

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type dungeonAICatalogSource map[string]string

func (source dungeonAICatalogSource) ReadText(relativePath string) (string, error) {
	want := strings.ToLower(strings.ReplaceAll(relativePath, "\\", "/"))
	for candidate, text := range source {
		if strings.ToLower(strings.ReplaceAll(candidate, "\\", "/")) == want {
			return text, nil
		}
	}
	return "", fmt.Errorf("missing %s", relativePath)
}

func TestPVFDungeonAICharacterCatalogParsesTypedMinimumInfoAndPreservesSections(t *testing.T) {
	source := dungeonAICatalogSource{
		defaultDungeonAICharacterList: "4001 `Test/Actor.aic`\n4001 `Ignored.aic`\n",
		"AICharacter/Test/Actor.aic":  "[minimum info]\n`PVF APC` 1 2 3 4 25 99\n[custom current pvf]\n7 `opaque`\n",
	}
	catalog, err := newPVFDungeonAICharacterCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Count() != 1 {
		t.Fatalf("count=%d want=1", catalog.Count())
	}
	definition, found, err := catalog.Find(4001)
	if err != nil || !found {
		t.Fatalf("find found=%t error=%v", found, err)
	}
	if definition.Name != "PVF APC" || definition.Level != 25 || definition.Path != "AICharacter/Test/Actor.aic" {
		t.Fatalf("definition=%+v", definition)
	}
	if len(definition.MinimumInfo) != 7 || len(definition.Sections) != 2 || definition.Sections[1].Name != "custom current pvf" || len(definition.Sections[1].Tokens) != 2 {
		t.Fatalf("preserved sections=%+v minimum=%+v", definition.Sections, definition.MinimumInfo)
	}
	definition.Sections[1].Tokens[0].Int = 999
	again, _, err := catalog.Find(4001)
	if err != nil || again.Sections[1].Tokens[0].Int != 7 {
		t.Fatalf("catalog cache leaked mutation: definition=%+v error=%v", again, err)
	}
	if _, found, err := catalog.Find(9999); err != nil || found {
		t.Fatalf("missing find found=%t error=%v", found, err)
	}
}

func TestPVFDungeonAICharacterCatalogRejectsMalformedMinimumInfo(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing", body: "[name]\n`No minimum`\n"},
		{name: "short", body: "[minimum info]\n`Short` 1 2 3 4\n"},
		{name: "zero level", body: "[minimum info]\n`Zero` 1 2 3 4 0\n"},
		{name: "overflow level", body: "[minimum info]\n`Overflow` 1 2 3 4 256\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := newPVFDungeonAICharacterCatalog(dungeonAICatalogSource{
				defaultDungeonAICharacterList: "4001 `Actor.aic`\n",
				"AICharacter/Actor.aic":       test.body,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, found, err := catalog.Find(4001)
			if found || !errors.Is(err, errDungeonAICharacterMinimumInfo) {
				t.Fatalf("found=%t error=%v", found, err)
			}
		})
	}
}
