package dnfbridge

import (
	"os"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func testAmplifyItemConfigText() string {
	return "[option mapping table] `[NONE]` 0 `[physical defense]` 1 `[magical defense]` 2 `[physical attack]` 3 `[magical attack]` 4 `[all]` 5\n" +
		"[option data] `[physical defense]` 2500 5 `[magical defense]` 2500 5 `[physical attack]` 2500 5 `[magical attack]` 2500 5\n" +
		"[rarity weight] `common` 1 `uncommon` 1 `rare` 1 `unique` 1.3 `epic` 1.5 `chronicle` 1.1 `legendary` 1.4\n" +
		"[equip level const] 55\n" +
		"[purify material] 1183 2\n" +
		"[purify material] 1184 1\n" +
		"[purify only material] 2001 1\n" +
		"[purify only cera material] 2002 3\n" +
		"[invest option] `[all]` 3001 1\n" +
		"[reinvest option] `[physical attack]` 3002 2\n" +
		"[random invest upgrade option] `[all]` 8238 1\n"
}

func TestParseCurrentAmplifyItemConfigRepeatedSections(t *testing.T) {
	document, err := dnfpvf.Parse(currentAmplifyItemPVFPath, testAmplifyItemConfigText())
	if err != nil {
		t.Fatal(err)
	}
	config, err := parseCurrentAmplifyItemConfig(document)
	if err != nil {
		t.Fatal(err)
	}
	if config.EquipLevelConst != 55 || config.Purify[1183] != 2 || config.Purify[1184] != 1 || config.Clear[2001] != 1 || config.Clear[2002] != 3 {
		t.Fatalf("material config = %+v", config)
	}
	if config.Invest[3001].Option != 5 || config.Reinvest[3002].Option != 3 || config.PureGold[8238].Count != 1 {
		t.Fatalf("option config = invest=%+v reinvest=%+v PureGold=%+v", config.Invest, config.Reinvest, config.PureGold)
	}
	values, err := currentAmplifyInitialValues(config, 4)
	if err != nil {
		t.Fatal(err)
	}
	for option := byte(1); option <= 4; option++ {
		if values[option] != 7 {
			t.Fatalf("epic option %d initial value = %d, want truncated 7", option, values[option])
		}
	}
}

func TestResolveCurrentAmplifyItemMetadataUsesMaterialRandomTable(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":           "",
		"stackable/stackable.lst":       "8238 `material/gold.stk`\n1183 `material/purify.stk`\n",
		"equipment/equipment.lst":       "700 `weapon/test.equ`\n",
		"stackable/material/gold.stk":   "[stackable type] `[material]`\n[amplification random value] 7 75 8 20 9 4 10 1\n",
		"stackable/material/purify.stk": "[stackable type] `[material]`\n",
		"equipment/weapon/test.equ":     "[equipment type] `[weapon]`\n[minimum level] 90\n[rarity] 4\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	document, _ := dnfpvf.Parse(currentAmplifyItemPVFPath, testAmplifyItemConfigText())
	config, err := parseCurrentAmplifyItemConfig(document)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolveCurrentAmplifyItemMetadata(catalog, source, config, 8238, 700)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.TargetKind != "equipment" || resolution.TargetMinimumLevel != 90 || resolution.TargetRarity != 4 || resolution.PureGoldOption != 5 || resolution.PureGoldMaterialCount != 1 {
		t.Fatalf("resolution = %+v", resolution)
	}
	if resolution.InitialValues[3] != 7 || len(resolution.PureGoldLevels) != 4 || resolution.PureGoldLevels[0].Level != 7 || resolution.PureGoldLevels[3].Weight != 1 {
		t.Fatalf("resolution values = %+v", resolution)
	}
	purify, err := resolveCurrentAmplifyItemMetadata(catalog, source, config, 1183, 700)
	if err != nil || purify.PurifyMaterialCount != 2 || len(purify.PureGoldLevels) != 0 {
		t.Fatalf("purify resolution = %+v err=%v", purify, err)
	}
}

func TestRealPVFAmplifyItemConfigAndPureGoldMaterial(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify runtime amplifyitem.etc")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := currentAmplifyItemConfigFromSource(archive)
	if err != nil {
		t.Fatal(err)
	}
	if config.EquipLevelConst != 55 || config.Purify[1183] != 1 || config.Invest[1286].Option != 5 || config.PureGold[8238].Count != 1 {
		t.Fatalf("runtime config mismatch: equip=%d purify=%d invest=%+v PureGold=%+v", config.EquipLevelConst, config.Purify[1183], config.Invest[1286], config.PureGold[8238])
	}
	listText, err := archive.ReadText("stackable/stackable.lst")
	if err != nil {
		t.Fatal(err)
	}
	listDocument, err := dnfpvf.Parse("stackable/stackable.lst", listText)
	if err != nil {
		t.Fatal(err)
	}
	var materialPath string
	for _, entry := range dnfpvf.ParseList(listDocument) {
		if entry.ID == 8238 {
			materialPath = "stackable/" + entry.Path
			break
		}
	}
	if materialPath == "" {
		t.Fatal("runtime stackable list missing Pure Gold material 8238")
	}
	materialText, err := archive.ReadText(materialPath)
	if err != nil {
		t.Fatal(err)
	}
	materialDocument, err := dnfpvf.Parse(materialPath, materialText)
	if err != nil {
		t.Fatal(err)
	}
	values := materialDocument.Ints("amplification random value")
	if len(values) != 8 || values[0] != 3 || values[1] != 50 || values[6] != 6 || values[7] != 5 {
		t.Fatalf("runtime Pure Gold random values = %v", values)
	}
}
