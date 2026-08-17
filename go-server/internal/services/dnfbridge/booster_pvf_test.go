package dnfbridge

import (
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestCurrentBoosterSelectionCatalogAllowsUnselectedEmptyCategory(t *testing.T) {
	document, err := dnfpvf.Parse("stackable/test_select.stk", `
[booster select category]
1 0
[stackable]
10095199 1
[/booster select category]
[booster select category]
1 4
[/booster select category]
`)
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	candidates, categories, err := parseCurrentBoosterSelectionCatalog(document)
	if err != nil {
		t.Fatalf("parseCurrentBoosterSelectionCatalog error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ItemID != 10095199 {
		t.Fatalf("candidates = %+v", candidates)
	}
	if populated := categories[1]; len(populated) != 1 || populated[0].ItemID != 10095199 {
		t.Fatalf("populated category = %+v", populated)
	}
	emptyCategory := uint16(1 | 4<<8)
	if empty, exists := categories[emptyCategory]; !exists || len(empty) != 0 {
		t.Fatalf("empty placeholder exists=%t rows=%+v", exists, empty)
	}

	definition := currentBoosterDefinition{
		SelectionRequired: 1,
		SelectionCategory: categories,
	}
	selected, err := currentBoosterSelectedCandidates(definition, currentBoosterOpenRequest{
		Kind:             currentBoosterRequestSelection,
		SelectionContext: 1,
		Selections: []currentBoosterSelectionRequest{
			{ItemID: 10095199},
		},
	})
	if err != nil || len(selected) != 1 || selected[0].ItemID != 10095199 {
		t.Fatalf("populated selection = %+v error=%v", selected, err)
	}
	_, err = currentBoosterSelectedCandidates(definition, currentBoosterOpenRequest{
		Kind:             currentBoosterRequestSelection,
		SelectionContext: emptyCategory,
		Selections: []currentBoosterSelectionRequest{
			{ItemID: 10095199},
		},
	})
	if !errors.Is(err, errCurrentBoosterSelectionInvalid) {
		t.Fatalf("empty category error = %v, want %v", err, errCurrentBoosterSelectionInvalid)
	}
}
