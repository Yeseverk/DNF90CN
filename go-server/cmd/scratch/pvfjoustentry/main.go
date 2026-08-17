// Command pvfjoustentry prepares a candidate Script.pvf that places the
// existing 2017 Chinese joust NPC at a map-owned legacy event-NPC slot. The
// server event catalog owns all-day activation; the NPC's native event gates
// must stay present so the current client enables its joust role action. It
// never edits the active PVF in place.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	sourceNPCPath    = "npc/chn_masang_2017.npc"
	targetNPCPath    = "npc/chn_event_lions10th_2.npc"
	sourceShopPath   = "itemshop/chn_masang_shop_2017.shp"
	targetShopPath   = "itemshop/chn_10th_lions_2.shp"
	itemShopListPath = "itemshop/itemshop.lst"
	mapPath          = "map/cataclysm/town/hendonmyre/new_hendon_main.map"
)

type report struct {
	InputPath         string   `json:"input_path"`
	OutputPath        string   `json:"output_path"`
	SourceNPC         string   `json:"source_npc"`
	TargetNPC         string   `json:"target_npc"`
	TargetShop        string   `json:"target_shop"`
	TownMap           string   `json:"town_map"`
	TownNPCID         int      `json:"town_npc_id"`
	TownX             int      `json:"town_x"`
	TownY             int      `json:"town_y"`
	PreservedSections []string `json:"preserved_sections"`
	InputSize         int64    `json:"input_size"`
	OutputSize        int      `json:"output_size"`
	OutputSHA256      string   `json:"output_sha256"`
}

func main() {
	pvfPath := flag.String("pvf", `D:/DNF/runtime/data/dnf/Script.pvf`, "source Script.pvf")
	outPath := flag.String("out", `D:/DNF/runtime/tmp/Script-joust-entry-candidate.pvf`, "candidate output path")
	flag.Parse()

	archive, err := platformpvf.Open(*pvfPath)
	must(err)
	sourceText, err := archive.ReadText(sourceNPCPath)
	must(err)
	sourceShopText, err := archive.ReadText(sourceShopPath)
	must(err)
	itemShopListText, err := archive.ReadText(itemShopListPath)
	must(err)
	mapText, err := archive.ReadText(mapPath)
	must(err)

	requireContains(sourceNPCPath, sourceText,
		"`[joust event]` 0",
		"`[item shop]` 101262",
		"[open visible event]\n2365",
		"[event open]\n2365 -1 -1",
	)
	requireContains(sourceShopPath, sourceShopText,
		"490005585", "490005586", "490005588", "490005589",
		"490005590", "490005591", "490005592",
	)
	requireContains(itemShopListPath, itemShopListText, "400000022", "chn_10th_lions_2.shp")
	if !strings.Contains(mapText, "20449 `[left]` 1478 354 0") &&
		!strings.Contains(mapText, "20449 `[left]` 2183 156 0") {
		panic(fmt.Errorf("%s has neither the original nor midpoint joust NPC tuple", mapPath))
	}

	sourceRaw, err := archive.ReadRaw(sourceNPCPath)
	must(err)
	placedNPCRaw, err := replaceSectionIntValue(archive, sourceRaw, "item shop", 101262, 400000022)
	must(err)
	sourceShopRaw, err := archive.ReadRaw(sourceShopPath)
	must(err)
	placedShopRaw, err := replaceSectionIntValue(archive, sourceShopRaw, "npc", 20325, 20449)
	must(err)
	mapRaw, err := archive.ReadRaw(mapPath)
	must(err)
	placedMapRaw, err := ensureTownNPCPlacement(archive, mapRaw, 20449, 1478, 354, 2183, 156)
	must(err)

	patched, err := archive.RepackRaw([]platformpvf.RawReplacement{
		{Path: targetNPCPath, Data: placedNPCRaw},
		{Path: targetShopPath, Data: placedShopRaw},
		{Path: mapPath, Data: placedMapRaw},
	})
	must(err)
	verified, err := platformpvf.OpenBytes(patched)
	must(err)
	verifiedText, err := verified.ReadText(targetNPCPath)
	must(err)
	requireContains(targetNPCPath, verifiedText,
		"`[joust event]` 0",
		"`[item shop]` 400000022",
		"[open visible event]\n2365",
		"[event open]\n2365 -1 -1",
	)
	for _, forbidden := range []string{"101262"} {
		if strings.Contains(verifiedText, forbidden) {
			panic(fmt.Errorf("verified target %s still contains %q", targetNPCPath, forbidden))
		}
	}
	verifiedSource, err := verified.ReadText(sourceNPCPath)
	must(err)
	if verifiedSource != sourceText {
		panic(fmt.Errorf("source NPC %s changed", sourceNPCPath))
	}
	verifiedShop, err := verified.ReadText(targetShopPath)
	must(err)
	requireContains(targetShopPath, verifiedShop,
		"490005585", "490005586", "490005588", "490005589",
		"490005590", "490005591", "490005592",
	)
	must(requireSectionIntValue(verified, targetShopPath, "npc", 20449))
	verifiedItemShopList, err := verified.ReadText(itemShopListPath)
	must(err)
	if verifiedItemShopList != itemShopListText {
		panic(fmt.Errorf("indexed item-shop list %s changed", itemShopListPath))
	}
	verifiedMap, err := verified.ReadText(mapPath)
	must(err)
	requireContains(mapPath, verifiedMap,
		"159 `[left]` 1896 156 0",
		"20449 `[left]` 2183 156 0",
		"45 `[right]` 2470 156 0",
	)
	if strings.Contains(verifiedMap, "20449 `[left]` 1478 354 0") {
		panic(fmt.Errorf("verified town map retained the old joust NPC placement"))
	}

	must(os.MkdirAll(filepath.Dir(*outPath), 0o755))
	must(os.WriteFile(*outPath, patched, 0o644))
	inputInfo, err := os.Stat(*pvfPath)
	must(err)
	sum := sha256.Sum256(patched)
	result := report{
		InputPath:         *pvfPath,
		OutputPath:        *outPath,
		SourceNPC:         sourceNPCPath,
		TargetNPC:         targetNPCPath,
		TargetShop:        targetShopPath,
		TownMap:           mapPath,
		TownNPCID:         20449,
		TownX:             2183,
		TownY:             156,
		PreservedSections: []string{"open visible event", "event open"},
		InputSize:         inputInfo.Size(),
		OutputSize:        len(patched),
		OutputSHA256:      strings.ToUpper(hex.EncodeToString(sum[:])),
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	must(err)
	fmt.Println(string(encoded))
}

func ensureTownNPCPlacement(archive *platformpvf.Archive, raw []byte, npcID, oldX, oldY, newX, newY int) ([]byte, error) {
	if archive == nil || len(raw)%5 != 0 {
		return nil, fmt.Errorf("map token stream is invalid: bytes=%d", len(raw))
	}
	out := append([]byte(nil), raw...)
	inNPCSection := false
	matches := 0
	for offset := 0; offset < len(out); offset += 5 {
		token := out[offset : offset+5]
		if token[0] == 3 {
			label := normalizedLabel(archive.ResolveString(scriptTokenInt(token)))
			inNPCSection = label == "npc"
			continue
		}
		if !inNPCSection || token[0] != 0 || scriptTokenInt(token) != npcID || offset+25 > len(out) {
			continue
		}
		direction := out[offset+5 : offset+10]
		xToken := out[offset+10 : offset+15]
		yToken := out[offset+15 : offset+20]
		zToken := out[offset+20 : offset+25]
		if direction[0] != 6 || normalizedLabel(archive.ResolveString(scriptTokenInt(direction))) != "left" ||
			xToken[0] != 0 || yToken[0] != 0 || zToken[0] != 0 || scriptTokenInt(zToken) != 0 {
			return nil, fmt.Errorf("town NPC %d placement does not match the proved tuple", npcID)
		}
		x, y := scriptTokenInt(xToken), scriptTokenInt(yToken)
		switch {
		case x == oldX && y == oldY:
			binary.LittleEndian.PutUint32(xToken[1:5], uint32(int32(newX)))
			binary.LittleEndian.PutUint32(yToken[1:5], uint32(int32(newY)))
		case x == newX && y == newY:
		default:
			return nil, fmt.Errorf("town NPC %d placement=(%d,%d), want original=(%d,%d) or midpoint=(%d,%d)", npcID, x, y, oldX, oldY, newX, newY)
		}
		matches++
	}
	if matches != 1 {
		return nil, fmt.Errorf("town NPC %d placement matches=%d, want=1", npcID, matches)
	}
	return out, nil
}

func replaceSectionIntValue(archive *platformpvf.Archive, raw []byte, section string, oldValue, newValue int) ([]byte, error) {
	if archive == nil || len(raw)%5 != 0 {
		return nil, fmt.Errorf("script token stream is invalid: bytes=%d", len(raw))
	}
	out := append([]byte(nil), raw...)
	matches := 0
	for offset := 0; offset+10 <= len(out); offset += 5 {
		token := out[offset : offset+5]
		if (token[0] != 3 && token[0] != 6) || normalizedLabel(archive.ResolveString(scriptTokenInt(token))) != normalizedLabel(section) {
			continue
		}
		valueToken := out[offset+5 : offset+10]
		if valueToken[0] != 0 || scriptTokenInt(valueToken) != oldValue {
			return nil, fmt.Errorf("section [%s] value does not match proved integer %d", section, oldValue)
		}
		binary.LittleEndian.PutUint32(valueToken[1:5], uint32(int32(newValue)))
		matches++
	}
	if matches != 1 {
		return nil, fmt.Errorf("section [%s] matches=%d, want=1", section, matches)
	}
	return out, nil
}

func requireSectionIntValue(archive *platformpvf.Archive, path, section string, want int) error {
	raw, err := archive.ReadRaw(path)
	if err != nil {
		return err
	}
	matches := 0
	for offset := 0; offset+10 <= len(raw); offset += 5 {
		token := raw[offset : offset+5]
		if (token[0] != 3 && token[0] != 6) || normalizedLabel(archive.ResolveString(scriptTokenInt(token))) != normalizedLabel(section) {
			continue
		}
		valueToken := raw[offset+5 : offset+10]
		if valueToken[0] != 0 || scriptTokenInt(valueToken) != want {
			return fmt.Errorf("section [%s] value does not match required integer %d", section, want)
		}
		matches++
	}
	if matches != 1 {
		return fmt.Errorf("section [%s] matches=%d, want=1", section, matches)
	}
	return nil
}

func normalizedLabel(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "[]"))
}

func scriptTokenInt(token []byte) int {
	return int(int32(binary.LittleEndian.Uint32(token[1:5])))
}

func requireContains(path, text string, values ...string) {
	for _, value := range values {
		if !strings.Contains(text, value) {
			panic(fmt.Errorf("%s is missing required evidence %q", path, value))
		}
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
