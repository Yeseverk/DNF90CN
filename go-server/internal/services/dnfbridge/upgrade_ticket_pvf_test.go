package dnfbridge

import (
	"testing"
)

func newUpgradeTicketTestCatalog(t *testing.T) (*pvfDungeonDropCatalog, dungeonDropCatalogTestSource) {
	t.Helper()
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":             "",
		"stackable/stackable.lst":         "9100 `ticket/plus10.stk`\n9101 `ticket/amplify12.stk`\n9102 `ticket/random.stk`\n9103 `ticket/plain.stk`\n",
		"equipment/equipment.lst":         "700 `weapon/test_sword.equ`\n701 `weapon/sealed.equ`\n",
		"stackable/ticket/plus10.stk":     "[name] `+10 Ticket`\n[stackable type] `[material]`\n[equipment reinforcement ticket] 10 100\n",
		"stackable/ticket/amplify12.stk":  "[name] `+12 Amplify Ticket`\n[stackable type] `[material]`\n[equipment amplify reinforcement ticket] 12 50\n",
		"stackable/ticket/random.stk":     "[name] `Random Ticket`\n[stackable type] `[material]`\n[enchant random]\n10 1000\n",
		"stackable/ticket/plain.stk":      "[name] `Plain Material`\n[stackable type] `[material]`\n",
		"equipment/weapon/test_sword.equ": "[name] `Test Sword`\n[equipment type] `[weapon]`\n[durability] 57\n",
		"equipment/weapon/sealed.equ":     "[name] `Sealed`\n[equipment type] `[weapon]`\n[impossible contents] `[upgrade]`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	return catalog, source
}

func TestResolveCurrentUpgradeTicketMetadata(t *testing.T) {
	catalog, source := newUpgradeTicketTestCatalog(t)

	resolution, err := resolveCurrentUpgradeTicketMetadata(catalog, source, 9100, 700)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if resolution.TicketMode != "reinforce" || resolution.TargetLevel != 10 || resolution.SuccessWeight != 100000 {
		t.Fatalf("reinforce ticket = %+v", resolution)
	}
	if resolution.TicketRandom {
		t.Fatalf("regular ticket marked random")
	}
	if resolution.TargetKind != "equipment" || resolution.TargetEquipmentType != "[weapon]" || resolution.TargetUpgradeForbidden {
		t.Fatalf("target = %+v", resolution)
	}

	amplify, err := resolveCurrentUpgradeTicketMetadata(catalog, source, 9101, 700)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if amplify.TicketMode != "amplify" || amplify.TargetLevel != 12 || amplify.SuccessWeight != 50000 {
		t.Fatalf("amplify ticket = %+v", amplify)
	}

	random, err := resolveCurrentUpgradeTicketMetadata(catalog, source, 9102, 700)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if !random.TicketRandom || random.TicketMode == "" {
		t.Fatalf("random ticket = %+v", random)
	}

	forbidden, err := resolveCurrentUpgradeTicketMetadata(catalog, source, 9100, 701)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if !forbidden.TargetUpgradeForbidden {
		t.Fatalf("forbidden target not flagged: %+v", forbidden)
	}
}

func TestResolveCurrentUpgradeTicketMetadataNonTickets(t *testing.T) {
	catalog, source := newUpgradeTicketTestCatalog(t)
	tests := []struct {
		name     string
		material int64
	}{
		{name: "plain stackable", material: 9103},
		{name: "unknown material", material: 9999},
		{name: "equipment as material", material: 700},
		{name: "zero material", material: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolution, err := resolveCurrentUpgradeTicketMetadata(catalog, source, tc.material, 700)
			if err != nil {
				t.Fatalf("resolve error = %v", err)
			}
			if resolution.TicketMode != "" {
				t.Fatalf("ticket mode = %q, want empty for non-ticket", resolution.TicketMode)
			}
		})
	}
}
