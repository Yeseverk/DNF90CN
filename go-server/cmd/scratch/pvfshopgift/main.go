// pvfshopgift publishes the historical 2017 National Day, 2018 Spring
// Festival, and 2014 SAO Cera packages already present in a runtime PVF.
// It only appends real item definitions to etc/cerashop.etc and redirects
// expired target-script date references to a future date already present in
// the shared string pool. The input archive is never modified in place.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	ceraShopPath         = "etc/cerashop.etc"
	stackableListPath    = "stackable/stackable.lst"
	equipmentListPath    = "equipment/equipment.lst"
	futureExpiration     = "2028-08-16 06:00:00"
	expirationDateLayout = "2006-01-02 15:04:05"
	packageRowWidth      = 11
)

var cst = time.FixedZone("CST", 8*60*60)

type desiredItem struct {
	ItemID uint32
	Price  uint32
	Group  string
}

type itemReference struct {
	Path string
	Kind string
}

type packageRow struct {
	CommodityID uint32
	ItemID      uint32
	Price       uint32
	NameMagic   int32
	JobCode     int32
	Group       string
	Name        string
	Path        string
}

type datePatch struct {
	Old   string   `json:"old"`
	New   string   `json:"new"`
	Files []string `json:"files"`
}

type report struct {
	InputSHA256      string           `json:"input_sha256"`
	OutputSHA256     string           `json:"output_sha256"`
	Output           string           `json:"output"`
	Format           string           `json:"format"`
	OriginalRows     int              `json:"original_package_rows"`
	AddedRows        int              `json:"added_package_rows"`
	FirstCommodityID uint32           `json:"first_commodity_id"`
	LastCommodityID  uint32           `json:"last_commodity_id"`
	AddedByGroup     map[string]int   `json:"added_by_group"`
	DatePatches      []datePatch      `json:"date_patches,omitempty"`
	DatePatchFiles   int              `json:"date_patch_files"`
	DatePatchTokens  int              `json:"date_patch_tokens"`
	DatePatchChunks  int              `json:"date_patch_chunks"`
	VerifiedJobCodes map[string]int32 `json:"verified_job_codes"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pvfshopgift:", err)
		os.Exit(1)
	}
}

func run() error {
	input := flag.String("pvf", "", "input PVF path (never modified)")
	output := flag.String("out", "", "output PVF path (must differ from -pvf)")
	flag.Parse()
	if strings.TrimSpace(*input) == "" || strings.TrimSpace(*output) == "" {
		return fmt.Errorf("-pvf and -out are required")
	}
	inputAbs, err := filepath.Abs(*input)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if strings.EqualFold(inputAbs, outputAbs) {
		return fmt.Errorf("output must differ from input; in-place patching is not supported")
	}

	inputBytes, err := os.ReadFile(inputAbs)
	if err != nil {
		return err
	}
	archive, err := platformpvf.OpenBytes(inputBytes)
	if err != nil {
		return err
	}
	refs, stackableIDs, err := loadItemReferences(archive)
	if err != nil {
		return err
	}
	desired, err := desiredItems(stackableIDs)
	if err != nil {
		return err
	}
	if len(desired) != 228 {
		return fmt.Errorf("target item count=%d, want 228", len(desired))
	}

	dateGroups, err := collectExpiredTargetDates(archive, refs, desired)
	if err != nil {
		return err
	}
	result := report{
		InputSHA256:      checksum(inputBytes),
		Output:           outputAbs,
		Format:           string(archive.Format()),
		AddedByGroup:     make(map[string]int),
		VerifiedJobCodes: make(map[string]int32),
	}
	for _, oldValue := range sortedKeys(dateGroups) {
		paths := dateGroups[oldValue]
		sort.Strings(paths)
		patched, patchReport, patchErr := archive.PatchScriptString(paths, oldValue, futureExpiration)
		if patchErr != nil {
			return fmt.Errorf("patch expiration %q: %w", oldValue, patchErr)
		}
		archive, err = platformpvf.OpenBytes(patched)
		if err != nil {
			return fmt.Errorf("reopen date-patched PVF: %w", err)
		}
		result.DatePatches = append(result.DatePatches, datePatch{Old: oldValue, New: futureExpiration, Files: paths})
		result.DatePatchFiles += patchReport.FilesChanged
		result.DatePatchTokens += patchReport.TokensChanged
		result.DatePatchChunks += patchReport.ChunksChanged
	}

	ceraRaw, err := archive.ReadRaw(ceraShopPath)
	if err != nil {
		return err
	}
	packageStart, packageEnd, err := findRawSection(archive, ceraRaw, "[package]", "[/package]")
	if err != nil {
		return err
	}
	interior := ceraRaw[(packageStart+1)*5 : packageEnd*5]
	if len(interior)%(packageRowWidth*5) != 0 {
		return fmt.Errorf("existing package section bytes=%d are not %d-token rows", len(interior), packageRowWidth)
	}
	result.OriginalRows = len(interior) / (packageRowWidth * 5)
	existingItemIDs, existingCommodityIDs, err := parseExistingPackageRows(interior)
	if err != nil {
		return err
	}
	for _, item := range desired {
		if _, exists := existingItemIDs[item.ItemID]; exists {
			return fmt.Errorf("target item %d is already present in [package]", item.ItemID)
		}
	}

	jobCodes, err := deriveJobCodes(archive, refs, interior)
	if err != nil {
		return err
	}
	for key, value := range jobCodes {
		result.VerifiedJobCodes[key] = value
	}
	usedCeraValues := allRawIntegerValues(ceraRaw)
	var maxPackageCommodity uint32
	for commodityID := range existingCommodityIDs {
		if commodityID > maxPackageCommodity {
			maxPackageCommodity = commodityID
		}
	}
	if maxPackageCommodity == 0 || maxPackageCommodity == math.MaxUint32 {
		return fmt.Errorf("invalid maximum package commodity ID %d", maxPackageCommodity)
	}

	rows := make([]packageRow, 0, len(desired))
	nextCommodityID := maxPackageCommodity + 1
	for _, item := range desired {
		ref, ok := refs[item.ItemID]
		if !ok || ref.Kind != "stackable" {
			return fmt.Errorf("target item %d is not a real stackable.lst entry", item.ItemID)
		}
		document, _, err := readDocument(archive, ref.Path)
		if err != nil {
			return err
		}
		stackableType, found := document.Text("stackable type")
		if !found || !strings.EqualFold(strings.TrimSpace(stackableType), "[usable cera package]") {
			return fmt.Errorf("target item %d path=%s stackable_type=%q", item.ItemID, ref.Path, stackableType)
		}
		packageData := document.Ints("package data")
		if len(packageData) == 0 || len(packageData)%2 != 0 {
			return fmt.Errorf("target item %d path=%s invalid package data", item.ItemID, ref.Path)
		}
		name, found := document.Text("name")
		if !found || strings.TrimSpace(name) == "" {
			return fmt.Errorf("target item %d path=%s missing name", item.ItemID, ref.Path)
		}
		nameMagic, err := sectionStringMagic(archive, ref.Path, "[name]")
		if err != nil {
			return err
		}
		jobs := document.Texts("suitable job")
		if len(jobs) == 0 {
			return fmt.Errorf("target item %d path=%s missing suitable job", item.ItemID, ref.Path)
		}
		jobKey := normalizeJob(jobs[0])
		jobCode, found := jobCodes[jobKey]
		if !found {
			return fmt.Errorf("target item %d path=%s has unmapped job %q", item.ItemID, ref.Path, jobs[0])
		}
		for {
			if nextCommodityID == 0 {
				return fmt.Errorf("commodity ID space exhausted after %d", maxPackageCommodity)
			}
			if _, collision := usedCeraValues[nextCommodityID]; !collision {
				break
			}
			nextCommodityID++
		}
		commodityID := nextCommodityID
		usedCeraValues[commodityID] = struct{}{}
		nextCommodityID++
		rows = append(rows, packageRow{
			CommodityID: commodityID,
			ItemID:      item.ItemID,
			Price:       item.Price,
			NameMagic:   nameMagic,
			JobCode:     jobCode,
			Group:       item.Group,
			Name:        name,
			Path:        ref.Path,
		})
		result.AddedByGroup[item.Group]++
	}
	result.AddedRows = len(rows)
	result.FirstCommodityID = rows[0].CommodityID
	result.LastCommodityID = rows[len(rows)-1].CommodityID

	appendRaw := encodePackageRows(rows)
	newCeraRaw := make([]byte, 0, len(ceraRaw)+len(appendRaw))
	newCeraRaw = append(newCeraRaw, ceraRaw[:packageEnd*5]...)
	newCeraRaw = append(newCeraRaw, appendRaw...)
	newCeraRaw = append(newCeraRaw, ceraRaw[packageEnd*5:]...)
	outputBytes, err := archive.RepackRaw([]platformpvf.RawReplacement{{Path: ceraShopPath, Data: newCeraRaw}})
	if err != nil {
		return err
	}
	verified, err := platformpvf.OpenBytes(outputBytes)
	if err != nil {
		return fmt.Errorf("reopen output PVF: %w", err)
	}
	if err := verifyOutput(verified, refs, rows, result.OriginalRows); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputAbs), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputAbs, outputBytes, 0o644); err != nil {
		return err
	}
	result.OutputSHA256 = checksum(outputBytes)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func desiredItems(stackableIDs map[string]uint32) ([]desiredItem, error) {
	items := make([]desiredItem, 0, 228)
	appendDescending := func(first, last uint32, price uint32, group string) {
		for id := last; ; id-- {
			items = append(items, desiredItem{ItemID: id, Price: price, Group: group})
			if id == first {
				break
			}
		}
	}
	appendDescending(490701336, 490701387, 19900, "2017_national_day_dream")
	appendDescending(490701388, 490701439, 33800, "2017_national_day_romance")
	appendDescending(490701876, 490701927, 19900, "2018_spring_guide")
	appendDescending(490701928, 490701979, 39900, "2018_spring_guardian")

	for _, tier := range []struct {
		Pattern string
		Price   uint32
		Group   string
	}{
		{Pattern: "cash/chn_20141111_sao_package/chn_2014_sao_trade_package_", Price: 23900, Group: "2014_sao"},
		{Pattern: "cash/chn_20141111_sao_package/chn_2014_sao_1timefree_package_", Price: 25900, Group: "2014_sao_true"},
	} {
		matches := make([]uint32, 0, 10)
		for path, id := range stackableIDs {
			if strings.HasPrefix(strings.ToLower(path), tier.Pattern) {
				matches = append(matches, id)
			}
		}
		if len(matches) != 10 {
			return nil, fmt.Errorf("SAO group %s matched %d stackable entries, want 10", tier.Group, len(matches))
		}
		sort.Slice(matches, func(left, right int) bool { return matches[left] > matches[right] })
		for _, id := range matches {
			items = append(items, desiredItem{ItemID: id, Price: tier.Price, Group: tier.Group})
		}
	}
	return items, nil
}

func loadItemReferences(archive *platformpvf.Archive) (map[uint32]itemReference, map[string]uint32, error) {
	refs := make(map[uint32]itemReference)
	stackableIDs := make(map[string]uint32)
	for _, spec := range []struct {
		List string
		Root string
		Kind string
	}{{stackableListPath, "stackable", "stackable"}, {equipmentListPath, "equipment", "equipment"}} {
		text, err := archive.ReadText(spec.List)
		if err != nil {
			return nil, nil, err
		}
		document, err := dnfpvf.Parse(spec.List, text)
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range dnfpvf.ParseList(document) {
			if entry.ID <= 0 || entry.ID > math.MaxUint32 {
				continue
			}
			clean := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(entry.Path), "\\", "/"), "/")
			if !strings.HasPrefix(strings.ToLower(clean), spec.Root+"/") {
				clean = spec.Root + "/" + clean
			}
			id := uint32(entry.ID)
			if _, exists := refs[id]; !exists {
				refs[id] = itemReference{Path: clean, Kind: spec.Kind}
			}
			if spec.Kind == "stackable" {
				stackableIDs[strings.ToLower(strings.TrimPrefix(clean, "stackable/"))] = id
			}
		}
	}
	return refs, stackableIDs, nil
}

func collectExpiredTargetDates(archive *platformpvf.Archive, refs map[uint32]itemReference, desired []desiredItem) (map[string][]string, error) {
	now := time.Now().In(cst)
	paths := make(map[string]struct{})
	for _, item := range desired {
		ref, ok := refs[item.ItemID]
		if !ok {
			return nil, fmt.Errorf("target item %d is absent from supported item lists", item.ItemID)
		}
		paths[ref.Path] = struct{}{}
		// SAO sources have no static deadline. Audit their immediate rewards so
		// historical inner boxes are not granted already expired.
		if !strings.HasPrefix(item.Group, "2014_sao") {
			continue
		}
		document, _, err := readDocument(archive, ref.Path)
		if err != nil {
			return nil, err
		}
		values := document.Ints("package data")
		for offset := 0; offset+1 < len(values); offset += 2 {
			if values[offset] <= 0 || values[offset] > math.MaxUint32 {
				continue
			}
			if reward, exists := refs[uint32(values[offset])]; exists {
				paths[reward.Path] = struct{}{}
			}
		}
	}
	grouped := make(map[string][]string)
	for path := range paths {
		document, _, err := readDocument(archive, path)
		if err != nil {
			return nil, err
		}
		raw, found := document.Text("expiration date")
		if !found || strings.TrimSpace(raw) == "" {
			continue
		}
		expires, err := time.ParseInLocation(expirationDateLayout, strings.TrimSpace(raw), cst)
		if err != nil {
			return nil, fmt.Errorf("parse expiration path=%s value=%q: %w", path, raw, err)
		}
		if now.Before(expires) {
			continue
		}
		grouped[raw] = append(grouped[raw], path)
	}
	return grouped, nil
}

func deriveJobCodes(archive *platformpvf.Archive, refs map[uint32]itemReference, interior []byte) (map[string]int32, error) {
	jobCodes := make(map[string]int32)
	for offset := 0; offset < len(interior); offset += packageRowWidth * 5 {
		itemID, ok := intToken(interior[offset+5 : offset+10])
		if !ok || itemID < 490702380 || itemID > 490702491 {
			continue
		}
		jobCode, ok := intToken(interior[offset+10*5 : offset+11*5])
		if !ok {
			return nil, fmt.Errorf("summer row item %d has invalid job code", itemID)
		}
		ref, found := refs[uint32(itemID)]
		if !found {
			return nil, fmt.Errorf("summer row item %d is absent from stackable.lst", itemID)
		}
		document, _, err := readDocument(archive, ref.Path)
		if err != nil {
			return nil, err
		}
		jobs := document.Texts("suitable job")
		if len(jobs) == 0 {
			return nil, fmt.Errorf("summer row item %d missing suitable job", itemID)
		}
		key := normalizeJob(jobs[0])
		if previous, exists := jobCodes[key]; exists && previous != jobCode {
			return nil, fmt.Errorf("job %s maps to both %d and %d", key, previous, jobCode)
		}
		jobCodes[key] = jobCode
	}
	if len(jobCodes) < 13 {
		return nil, fmt.Errorf("derived only %d real job-code mappings from current summer rows", len(jobCodes))
	}
	return jobCodes, nil
}

func verifyOutput(archive *platformpvf.Archive, refs map[uint32]itemReference, rows []packageRow, originalRows int) error {
	raw, err := archive.ReadRaw(ceraShopPath)
	if err != nil {
		return err
	}
	start, end, err := findRawSection(archive, raw, "[package]", "[/package]")
	if err != nil {
		return err
	}
	interior := raw[(start+1)*5 : end*5]
	if len(interior)%(packageRowWidth*5) != 0 {
		return fmt.Errorf("verified package section is malformed")
	}
	if got := len(interior) / (packageRowWidth * 5); got != originalRows+len(rows) {
		return fmt.Errorf("verified package rows=%d, want %d", got, originalRows+len(rows))
	}
	byCommodity := make(map[uint32]packageRow)
	for offset := 0; offset < len(interior); offset += packageRowWidth * 5 {
		commodity, ok := intToken(interior[offset : offset+5])
		if !ok || commodity <= 0 {
			continue
		}
		item, itemOK := intToken(interior[offset+5 : offset+10])
		price, priceOK := intToken(interior[offset+4*5 : offset+5*5])
		job, jobOK := intToken(interior[offset+10*5 : offset+11*5])
		if itemOK && priceOK && jobOK {
			byCommodity[uint32(commodity)] = packageRow{ItemID: uint32(item), Price: uint32(price), JobCode: job}
		}
	}
	now := time.Now().In(cst)
	for _, want := range rows {
		got, found := byCommodity[want.CommodityID]
		if !found || got.ItemID != want.ItemID || got.Price != want.Price || got.JobCode != want.JobCode {
			return fmt.Errorf("verify commodity %d got=%+v want_item=%d want_price=%d want_job=%d", want.CommodityID, got, want.ItemID, want.Price, want.JobCode)
		}
		ref := refs[want.ItemID]
		document, _, err := readDocument(archive, ref.Path)
		if err != nil {
			return err
		}
		if values := document.Ints("package data"); len(values) == 0 || len(values)%2 != 0 {
			return fmt.Errorf("verify item %d has invalid package data", want.ItemID)
		}
		if rawDate, found := document.Text("expiration date"); found {
			expires, parseErr := time.ParseInLocation(expirationDateLayout, rawDate, cst)
			if parseErr != nil || !now.Before(expires) {
				return fmt.Errorf("verify item %d expiration=%q is not future", want.ItemID, rawDate)
			}
		}
	}
	return nil
}

func parseExistingPackageRows(interior []byte) (map[uint32]struct{}, map[uint32]struct{}, error) {
	items := make(map[uint32]struct{})
	commodities := make(map[uint32]struct{})
	for offset := 0; offset < len(interior); offset += packageRowWidth * 5 {
		commodityID, ok := intToken(interior[offset : offset+5])
		if !ok || commodityID <= 0 {
			return nil, nil, fmt.Errorf("existing package row %d has invalid commodity", offset/(packageRowWidth*5))
		}
		itemID, ok := intToken(interior[offset+5 : offset+10])
		if !ok || itemID <= 0 {
			return nil, nil, fmt.Errorf("existing package row %d has invalid item", offset/(packageRowWidth*5))
		}
		commodities[uint32(commodityID)] = struct{}{}
		items[uint32(itemID)] = struct{}{}
	}
	return items, commodities, nil
}

func findRawSection(archive *platformpvf.Archive, raw []byte, open, close string) (int, int, error) {
	start := -1
	for index := 0; index+5 <= len(raw); index++ {
		offset := index * 5
		if raw[offset] != 3 {
			continue
		}
		magic := int(int32(binary.LittleEndian.Uint32(raw[offset+1 : offset+5])))
		value := archive.ResolveString(magic)
		if value == open {
			if start >= 0 {
				return 0, 0, fmt.Errorf("duplicate section %s", open)
			}
			start = index
			continue
		}
		if start >= 0 && value == close {
			return start, index, nil
		}
	}
	return 0, 0, fmt.Errorf("section %s...%s not found", open, close)
}

func sectionStringMagic(archive *platformpvf.Archive, path, section string) (int32, error) {
	raw, err := archive.ReadRaw(path)
	if err != nil {
		return 0, err
	}
	found := false
	for offset := 0; offset+5 <= len(raw); offset += 5 {
		tokenType := raw[offset]
		magic := int32(binary.LittleEndian.Uint32(raw[offset+1 : offset+5]))
		if tokenType == 3 {
			value := archive.ResolveString(int(magic))
			if found {
				return 0, fmt.Errorf("path=%s section=%s has no string token", path, section)
			}
			found = value == section
			continue
		}
		if found && tokenType == 6 {
			return magic, nil
		}
	}
	return 0, fmt.Errorf("path=%s section=%s not found", path, section)
}

func encodePackageRows(rows []packageRow) []byte {
	out := make([]byte, 0, len(rows)*packageRowWidth*5)
	for _, row := range rows {
		out = appendInt(out, int32(row.CommodityID))
		out = appendInt(out, int32(row.ItemID))
		out = appendInt(out, 0)
		out = appendInt(out, 0)
		out = appendInt(out, int32(row.Price))
		out = appendToken(out, 6, row.NameMagic)
		out = appendInt(out, 1)
		out = appendInt(out, 0)
		out = appendInt(out, -1)
		out = appendInt(out, -1)
		out = appendInt(out, row.JobCode)
	}
	return out
}

func appendInt(out []byte, value int32) []byte { return appendToken(out, 0, value) }

func appendToken(out []byte, tokenType byte, value int32) []byte {
	var token [5]byte
	token[0] = tokenType
	binary.LittleEndian.PutUint32(token[1:], uint32(value))
	return append(out, token[:]...)
}

func intToken(token []byte) (int32, bool) {
	if len(token) != 5 || token[0] != 0 {
		return 0, false
	}
	return int32(binary.LittleEndian.Uint32(token[1:])), true
}

func allRawIntegerValues(raw []byte) map[uint32]struct{} {
	values := make(map[uint32]struct{})
	for offset := 0; offset+5 <= len(raw); offset += 5 {
		value, ok := intToken(raw[offset : offset+5])
		if !ok || value <= 0 {
			continue
		}
		values[uint32(value)] = struct{}{}
	}
	return values
}

func readDocument(archive *platformpvf.Archive, path string) (*dnfpvf.Document, string, error) {
	text, err := archive.ReadText(path)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	document, err := dnfpvf.Parse(path, text)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", path, err)
	}
	return document, text, nil
}

func normalizeJob(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func sortedKeys(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
