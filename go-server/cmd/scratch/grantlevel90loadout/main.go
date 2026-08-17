package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	batchID          = "20260803-level90-strength-loadout"
	mainListType     = byte(0)
	medalListType    = byte(38)
	extraSlotMask    = byte(0x13)
	topQualitySeed   = uint32(999999998)
	amplifyStamina   = byte(1)
	amplifyStrength  = byte(3)
	amplifyBaseValue = uint16(7)
	itemRowSize      = 0x77
)

type instanceConfig struct {
	Database struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Name     string `json:"name"`
	} `json:"database"`
}

type itemSpec struct {
	ListType     byte
	Slot         int16
	ItemID       int64
	Name         string
	PVFPath      string
	Kind         string
	PVFType      string
	Rarity       int64
	Grade        int64
	Durability   uint16
	Count        int64
	UpgradeLevel byte
	Amplify      bool
}

type character struct {
	ID        string
	AccountID string
	Name      string
	Level     int
	SlotState byte
}

type amplifyRepairTarget struct {
	Equipped       bool
	CollectionName string
	EntryKey       string
	ItemID         int64
	RawEntry       []byte
}

func main() {
	configPath := flag.String("config", "../runtime/config/instance.json", "runtime instance config")
	pvfPath := flag.String("pvf", "../runtime/data/dnf/Script.pvf", "runtime Script.pvf")
	apply := flag.Bool("apply", false, "commit the validated grant")
	repairStrength := flag.Bool("repair-strength", false, "repair this batch from dimensional stamina to dimensional strength")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	specs := requestedItems()
	if err := validateManifest(*pvfPath, specs); err != nil {
		fatal(err)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	db, err := openDatabase(cfg)
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fatal(fmt.Errorf("ping database: %w", err))
	}

	if *repairStrength {
		result, repaired, repairErr := repairBatchStrength(ctx, db, specs, *apply)
		if repairErr != nil {
			fatal(repairErr)
		}
		mode := "DRY-RUN"
		if *apply {
			mode = "APPLIED"
		}
		fmt.Printf("%s strength-repair character=%s id=%s level=%d rows=%d old_type=%d new_type=%d batch=%s\n",
			mode, result.Name, result.ID, result.Level, repaired, amplifyStamina, amplifyStrength, batchID)
		return
	}

	result, err := grant(ctx, db, specs, *apply)
	if err != nil {
		fatal(err)
	}
	mode := "DRY-RUN"
	if *apply {
		mode = "APPLIED"
	}
	fmt.Printf("%s character=%s id=%s level=%d old_slot_mask=%#x new_slot_mask=%#x rows=%d main=%d medals=%d guardian_gem_stacks=%d guardian_gem_units=%d batch=%s\n",
		mode, result.Name, result.ID, result.Level, result.SlotState, result.SlotState|extraSlotMask,
		len(specs), countList(specs, mainListType), countMedals(specs), countGuardianGems(specs), guardianGemUnits(specs), batchID)
}

func loadConfig(path string) (instanceConfig, error) {
	var cfg instanceConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read instance config: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parse instance config: %w", err)
	}
	if strings.TrimSpace(cfg.Database.Host) == "" || cfg.Database.Port <= 0 || strings.TrimSpace(cfg.Database.User) == "" || strings.TrimSpace(cfg.Database.Name) == "" {
		return cfg, errors.New("instance config has incomplete database settings")
	}
	return cfg, nil
}

func openDatabase(cfg instanceConfig) (*sql.DB, error) {
	mysqlConfig := mysql.Config{
		User:      cfg.Database.User,
		Passwd:    cfg.Database.Password,
		Net:       "tcp",
		Addr:      net.JoinHostPort(cfg.Database.Host, strconv.Itoa(cfg.Database.Port)),
		DBName:    cfg.Database.Name,
		ParseTime: true,
		Loc:       time.UTC,
		Params:    map[string]string{"charset": "utf8mb4"},
	}
	dsn := mysqlConfig.FormatDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

func grant(ctx context.Context, db *sql.DB, specs []itemSpec, apply bool) (character, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return character{}, fmt.Errorf("begin grant transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT character_id, account_id, name, level, ex_equip_slot_stat FROM dnf_characters WHERE level = 90 AND delete_flag = 0 FOR UPDATE`)
	if err != nil {
		return character{}, fmt.Errorf("lock level-90 character: %w", err)
	}
	var characters []character
	for rows.Next() {
		var row character
		if err := rows.Scan(&row.ID, &row.AccountID, &row.Name, &row.Level, &row.SlotState); err != nil {
			rows.Close()
			return character{}, fmt.Errorf("scan level-90 character: %w", err)
		}
		characters = append(characters, row)
	}
	if err := rows.Close(); err != nil {
		return character{}, fmt.Errorf("close character rows: %w", err)
	}
	if len(characters) != 1 {
		return character{}, fmt.Errorf("expected exactly one active level-90 character, found %d", len(characters))
	}
	selected := characters[0]

	occupiedRows, err := tx.QueryContext(ctx, `SELECT entry_key FROM dnf_inventory_items WHERE character_id = ? AND collection_name = 'slots' FOR UPDATE`, selected.ID)
	if err != nil {
		return character{}, fmt.Errorf("lock inventory: %w", err)
	}
	occupied := make(map[string]struct{})
	for occupiedRows.Next() {
		var key string
		if err := occupiedRows.Scan(&key); err != nil {
			occupiedRows.Close()
			return character{}, fmt.Errorf("scan occupied inventory slot: %w", err)
		}
		occupied[key] = struct{}{}
	}
	if err := occupiedRows.Close(); err != nil {
		return character{}, fmt.Errorf("close inventory rows: %w", err)
	}
	for _, spec := range specs {
		key := itemKey(spec)
		if _, exists := occupied[key]; exists {
			return character{}, fmt.Errorf("required destination slot %s is occupied", key)
		}
	}

	if !apply {
		return selected, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dnf_characters SET ex_equip_slot_stat = ex_equip_slot_stat | ?, updated_at = UTC_TIMESTAMP(6) WHERE character_id = ?`, extraSlotMask, selected.ID); err != nil {
		return character{}, fmt.Errorf("unlock extra equipment slots: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dnf_inventories SET updated_at = UTC_TIMESTAMP(6) WHERE character_id = ?`, selected.ID); err != nil {
		return character{}, fmt.Errorf("touch inventory: %w", err)
	}
	for _, spec := range specs {
		if err := insertItem(ctx, tx, selected.ID, spec); err != nil {
			return character{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return character{}, fmt.Errorf("commit loadout grant: %w", err)
	}
	return selected, nil
}

func repairBatchStrength(ctx context.Context, db *sql.DB, specs []itemSpec, apply bool) (character, int, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return character{}, 0, fmt.Errorf("begin amplification repair transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT character_id, account_id, name, level, ex_equip_slot_stat FROM dnf_characters WHERE level = 90 AND delete_flag = 0 FOR UPDATE`)
	if err != nil {
		return character{}, 0, fmt.Errorf("lock level-90 character: %w", err)
	}
	var characters []character
	for rows.Next() {
		var row character
		if err := rows.Scan(&row.ID, &row.AccountID, &row.Name, &row.Level, &row.SlotState); err != nil {
			rows.Close()
			return character{}, 0, fmt.Errorf("scan level-90 character: %w", err)
		}
		characters = append(characters, row)
	}
	if err := rows.Close(); err != nil {
		return character{}, 0, fmt.Errorf("close character rows: %w", err)
	}
	if len(characters) != 1 {
		return character{}, 0, fmt.Errorf("expected exactly one active level-90 character, found %d", len(characters))
	}
	selected := characters[0]

	targets, err := loadAmplifyRepairTargets(ctx, tx, selected.ID)
	if err != nil {
		return character{}, 0, err
	}
	expected := make(map[int64]struct{}, 16)
	for _, spec := range specs {
		if spec.Amplify {
			expected[spec.ItemID] = struct{}{}
		}
	}
	if len(targets) != len(expected) {
		return character{}, 0, fmt.Errorf("expected %d stamina-amplified batch rows, found %d", len(expected), len(targets))
	}
	seen := make(map[int64]struct{}, len(targets))
	for _, target := range targets {
		if _, ok := expected[target.ItemID]; !ok {
			return character{}, 0, fmt.Errorf("unexpected batch item %d at %s", target.ItemID, target.EntryKey)
		}
		if _, duplicate := seen[target.ItemID]; duplicate {
			return character{}, 0, fmt.Errorf("duplicate batch item %d", target.ItemID)
		}
		seen[target.ItemID] = struct{}{}
		if len(target.RawEntry) != itemRowSize || target.RawEntry[0x0A]&0x1F != 13 || target.RawEntry[0x13] != amplifyStamina || binary.LittleEndian.Uint16(target.RawEntry[0x14:0x16]) != amplifyBaseValue {
			return character{}, 0, fmt.Errorf("item %d at %s has unexpected raw amplification state", target.ItemID, target.EntryKey)
		}
	}
	if !apply {
		return selected, len(targets), nil
	}

	for _, target := range targets {
		raw := append([]byte(nil), target.RawEntry...)
		raw[0x13] = amplifyStrength
		if target.Equipped {
			result, err := tx.ExecContext(ctx, `UPDATE dnf_equipment_entries SET raw_entry = ? WHERE character_id = ? AND entry_key = ? AND item_id = ?`, raw, selected.ID, target.EntryKey, target.ItemID)
			if err != nil {
				return character{}, 0, fmt.Errorf("update equipped raw item %d: %w", target.ItemID, err)
			}
			if err := requireAffected(result, 1, "equipped raw", target.ItemID); err != nil {
				return character{}, 0, err
			}
			result, err = tx.ExecContext(ctx, `UPDATE dnf_equipment_entry_extra SET extra_value = ? WHERE character_id = ? AND entry_key = ? AND extra_key IN ('amplify_type','amplification_type','byte_13','value_13','value_c') AND extra_value = ?`,
				strconv.Itoa(int(amplifyStrength)), selected.ID, target.EntryKey, strconv.Itoa(int(amplifyStamina)))
			if err != nil {
				return character{}, 0, fmt.Errorf("update equipped amplification metadata item %d: %w", target.ItemID, err)
			}
			if err := requireAffected(result, 5, "equipped amplification metadata", target.ItemID); err != nil {
				return character{}, 0, err
			}
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE dnf_inventory_items SET raw_entry = ? WHERE character_id = ? AND collection_name = ? AND entry_key = ? AND item_id = ?`,
			raw, selected.ID, target.CollectionName, target.EntryKey, target.ItemID)
		if err != nil {
			return character{}, 0, fmt.Errorf("update inventory raw item %d: %w", target.ItemID, err)
		}
		if err := requireAffected(result, 1, "inventory raw", target.ItemID); err != nil {
			return character{}, 0, err
		}
		result, err = tx.ExecContext(ctx, `UPDATE dnf_inventory_item_extra SET extra_value = ? WHERE character_id = ? AND collection_name = ? AND entry_key = ? AND extra_key IN ('amplify_type','amplification_type','byte_13','value_13','value_c') AND extra_value = ?`,
			strconv.Itoa(int(amplifyStrength)), selected.ID, target.CollectionName, target.EntryKey, strconv.Itoa(int(amplifyStamina)))
		if err != nil {
			return character{}, 0, fmt.Errorf("update inventory amplification metadata item %d: %w", target.ItemID, err)
		}
		if err := requireAffected(result, 5, "inventory amplification metadata", target.ItemID); err != nil {
			return character{}, 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dnf_inventories SET updated_at = UTC_TIMESTAMP(6) WHERE character_id = ?`, selected.ID); err != nil {
		return character{}, 0, fmt.Errorf("touch inventory after amplification repair: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return character{}, 0, fmt.Errorf("commit amplification repair: %w", err)
	}
	return selected, len(targets), nil
}

func loadAmplifyRepairTargets(ctx context.Context, tx *sql.Tx, characterID string) ([]amplifyRepairTarget, error) {
	var targets []amplifyRepairTarget
	rows, err := tx.QueryContext(ctx, `SELECT i.collection_name, i.entry_key, i.item_id, i.raw_entry FROM dnf_inventory_items i JOIN dnf_inventory_item_extra b ON b.character_id=i.character_id AND b.collection_name=i.collection_name AND b.entry_key=i.entry_key AND b.extra_key='grant_batch' AND b.extra_value=? JOIN dnf_inventory_item_extra a ON a.character_id=i.character_id AND a.collection_name=i.collection_name AND a.entry_key=i.entry_key AND a.extra_key='amplify_type' AND a.extra_value=? WHERE i.character_id=? FOR UPDATE`,
		batchID, strconv.Itoa(int(amplifyStamina)), characterID)
	if err != nil {
		return nil, fmt.Errorf("lock inventory amplification rows: %w", err)
	}
	for rows.Next() {
		var target amplifyRepairTarget
		if err := rows.Scan(&target.CollectionName, &target.EntryKey, &target.ItemID, &target.RawEntry); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan inventory amplification row: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close inventory amplification rows: %w", err)
	}

	rows, err = tx.QueryContext(ctx, `SELECT e.entry_key, e.item_id, e.raw_entry FROM dnf_equipment_entries e JOIN dnf_equipment_entry_extra b ON b.character_id=e.character_id AND b.entry_key=e.entry_key AND b.extra_key='grant_batch' AND b.extra_value=? JOIN dnf_equipment_entry_extra a ON a.character_id=e.character_id AND a.entry_key=e.entry_key AND a.extra_key='amplify_type' AND a.extra_value=? WHERE e.character_id=? FOR UPDATE`,
		batchID, strconv.Itoa(int(amplifyStamina)), characterID)
	if err != nil {
		return nil, fmt.Errorf("lock equipped amplification rows: %w", err)
	}
	for rows.Next() {
		target := amplifyRepairTarget{Equipped: true}
		if err := rows.Scan(&target.EntryKey, &target.ItemID, &target.RawEntry); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan equipped amplification row: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close equipped amplification rows: %w", err)
	}
	return targets, nil
}

func requireAffected(result sql.Result, want int64, field string, itemID int64) error {
	got, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows for %s item %d: %w", field, itemID, err)
	}
	if got != want {
		return fmt.Errorf("%s item %d affected %d rows, want %d", field, itemID, got, want)
	}
	return nil
}

func insertItem(ctx context.Context, tx *sql.Tx, characterID string, spec itemSpec) error {
	key := itemKey(spec)
	raw := buildRaw(spec)
	if _, err := tx.ExecContext(ctx, `INSERT INTO dnf_inventory_items (character_id, collection_name, entry_key, item_id, item_count, bind_flag, expire_at, raw_entry) VALUES (?, 'slots', ?, ?, ?, 0, NULL, ?)`,
		characterID, key, spec.ItemID, spec.Count, raw); err != nil {
		return fmt.Errorf("insert %s (%d) at %s: %w", spec.Name, spec.ItemID, key, err)
	}
	extra := itemExtra(spec)
	keys := make([]string, 0, len(extra))
	for extraKey := range extra {
		keys = append(keys, extraKey)
	}
	sort.Strings(keys)
	for _, extraKey := range keys {
		if _, err := tx.ExecContext(ctx, `INSERT INTO dnf_inventory_item_extra (character_id, collection_name, entry_key, extra_key, extra_value) VALUES (?, 'slots', ?, ?, ?)`,
			characterID, key, extraKey, extra[extraKey]); err != nil {
			return fmt.Errorf("insert metadata %s for %s at %s: %w", extraKey, spec.Name, key, err)
		}
	}
	return nil
}

func buildRaw(spec itemSpec) []byte {
	raw := make([]byte, itemRowSize)
	binary.LittleEndian.PutUint16(raw[0x00:0x02], uint16(spec.Slot))
	binary.LittleEndian.PutUint32(raw[0x02:0x06], uint32(spec.ItemID))
	amount := uint32(spec.Count)
	if spec.ListType == mainListType && spec.Kind == "equipment" {
		amount = topQualitySeed
	}
	binary.LittleEndian.PutUint32(raw[0x06:0x0A], amount)
	raw[0x0A] = spec.UpgradeLevel & 0x1F
	binary.LittleEndian.PutUint16(raw[0x0B:0x0D], spec.Durability)
	if spec.Kind == "stackable" {
		binary.LittleEndian.PutUint32(raw[0x0E:0x12], uint32(spec.ItemID))
	}
	if spec.Amplify {
		raw[0x13] = amplifyStrength
		binary.LittleEndian.PutUint16(raw[0x14:0x16], amplifyBaseValue)
	}
	return raw
}

func itemExtra(spec itemSpec) map[string]string {
	extra := map[string]string{
		"source":      "operator_direct_database_grant",
		"grant_batch": batchID,
		"item_name":   spec.Name,
		"item_kind":   spec.Kind,
		"pvf_path":    spec.PVFPath,
	}
	if spec.PVFType != "" {
		if spec.Kind == "equipment" {
			extra["equipment_type"] = spec.PVFType
		} else {
			extra["stackable_type"] = spec.PVFType
		}
	}
	if spec.Kind == "equipment" {
		extra["durability"] = strconv.Itoa(int(spec.Durability))
		extra["max_durability"] = strconv.Itoa(int(spec.Durability))
		if spec.ListType == mainListType {
			extra["quality_seed"] = strconv.FormatUint(uint64(topQualitySeed), 10)
		}
		if spec.UpgradeLevel != 0 {
			level := strconv.Itoa(int(spec.UpgradeLevel))
			extra["ext_data0"] = level
			extra["packed_flag_byte"] = level
			extra["reinforce"] = level
			extra["upgrade_level"] = level
		}
	} else {
		count := strconv.FormatInt(spec.Count, 10)
		extra["amount"] = count
		extra["amount_or_count"] = count
		extra["count"] = count
		extra["value_a"] = strconv.FormatInt(spec.ItemID, 10)
	}
	if spec.Amplify {
		typeValue := strconv.Itoa(int(amplifyStrength))
		baseValue := strconv.Itoa(int(amplifyBaseValue))
		for _, key := range []string{"amplify_type", "amplification_type", "byte_13", "value_13", "value_c"} {
			extra[key] = typeValue
		}
		for _, key := range []string{"amplify_value", "amplification_value", "marker_16", "marker16", "value_d"} {
			extra[key] = baseValue
		}
	}
	return extra
}

func itemKey(spec itemSpec) string {
	return fmt.Sprintf("%d:%d", spec.ListType, spec.Slot)
}

func validateManifest(pvfPath string, specs []itemSpec) error {
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		return fmt.Errorf("open runtime PVF: %w", err)
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		key := itemKey(spec)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate destination slot %s", key)
		}
		seen[key] = struct{}{}
		text, err := archive.ReadText(spec.PVFPath)
		if err != nil {
			return fmt.Errorf("read PVF item %d at %s: %w", spec.ItemID, spec.PVFPath, err)
		}
		doc, err := dnfpvf.Parse(spec.PVFPath, text)
		if err != nil {
			return fmt.Errorf("parse PVF item %d at %s: %w", spec.ItemID, spec.PVFPath, err)
		}
		name, _ := doc.Text("name")
		if name != spec.Name {
			return fmt.Errorf("PVF name mismatch item %d: got %q want %q", spec.ItemID, name, spec.Name)
		}
		section := "equipment type"
		if spec.Kind == "stackable" {
			section = "stackable type"
		}
		pvfType, _ := doc.Text(section)
		if !strings.EqualFold(strings.TrimSpace(pvfType), spec.PVFType) {
			return fmt.Errorf("PVF type mismatch item %d: got %q want %q", spec.ItemID, pvfType, spec.PVFType)
		}
		rarity, _ := doc.Int("rarity")
		if rarity != spec.Rarity {
			return fmt.Errorf("PVF rarity mismatch item %d: got %d want %d", spec.ItemID, rarity, spec.Rarity)
		}
		if spec.Grade != 0 {
			grade, _ := doc.Int("grade")
			if grade != spec.Grade {
				return fmt.Errorf("PVF grade mismatch item %d: got %d want %d", spec.ItemID, grade, spec.Grade)
			}
		}
	}
	return nil
}

func requestedItems() []itemSpec {
	main := []itemSpec{
		{ItemID: 100070551, Name: "超大陆 - 瓦巴拉的大地", PVFPath: "equipment/character/common/jacket/larmor/100070551.equ", PVFType: "[coat]", Rarity: 4, Durability: 38},
		{ItemID: 100120544, Name: "超大陆 - 盘古大陆的地震", PVFPath: "equipment/character/common/pants/larmor/100120544.equ", PVFType: "[pants]", Rarity: 4, Durability: 32},
		{ItemID: 100170519, Name: "超大陆 - 潘诺西亚的火山", PVFPath: "equipment/character/common/shoulder/larmor/100170519.equ", PVFType: "[shoulder]", Rarity: 4, Durability: 30},
		{ItemID: 100220519, Name: "超大陆 - 罗迪尼亚的熔岩", PVFPath: "equipment/character/common/belt/larmor/100220519.equ", PVFType: "[waist]", Rarity: 4, Durability: 25},
		{ItemID: 100270515, Name: "超大陆 - 凯诺兰的地壳", PVFPath: "equipment/character/common/shoes/larmor/100270515.equ", PVFType: "[shoes]", Rarity: 4, Durability: 25},
		{ItemID: 100300733, Name: "氤氲之息", PVFPath: "equipment/character/common/amulet/100300733.equ", PVFType: "[amulet]", Rarity: 4},
		{ItemID: 100312425, Name: "启明星的指引", PVFPath: "equipment/character/common/wrist/100312425.equ", PVFType: "[wrist]", Rarity: 4},
		{ItemID: 100322294, Name: "清泉流响", PVFPath: "equipment/character/common/ring/100322294.equ", PVFType: "[ring]", Rarity: 4},
		{ItemID: 100390011, Name: "黑暗祭礼", PVFPath: "equipment/character/common/earring/100390011.equ", PVFType: "[earring]", Rarity: 4},
		{ItemID: 100344511, Name: "波利斯的黄金杯", PVFPath: "equipment/character/common/support/100344511.equ", PVFType: "[support]", Rarity: 4},
		{ItemID: 100352822, Name: "罗塞塔石碑", PVFPath: "equipment/character/common/magicstone/100352822.equ", PVFType: "[magic stone]", Rarity: 4},
		{ItemID: 100390003, Name: "英雄王的象征", PVFPath: "equipment/character/common/earring/100390003.equ", PVFType: "[earring]", Rarity: 4},
		{ItemID: 100344527, Name: "王冠非冠", PVFPath: "equipment/character/common/support/100344527.equ", PVFType: "[support]", Rarity: 4},
		{ItemID: 100352839, Name: "王座本源", PVFPath: "equipment/character/common/magicstone/100352839.equ", PVFType: "[magic stone]", Rarity: 4},
		{ItemID: 101010864, Name: "圣耀救赎太刀", PVFPath: "equipment/character/swordman/weapon/katana/101010864.equ", PVFType: "[weapon]", Rarity: 4, Durability: 45},
		{ItemID: 101030741, Name: "圣耀救赎巨剑", PVFPath: "equipment/character/swordman/weapon/hsword/101030741.equ", PVFType: "[weapon]", Rarity: 4, Durability: 45},
		{ItemID: 400330106, Name: "天选之人", PVFPath: "equipment/character/common/title/chn_400330106.equ", PVFType: "[title name]", Rarity: 6},
	}
	for index := range main {
		main[index].ListType = mainListType
		main[index].Slot = int16(9 + index)
		main[index].Kind = "equipment"
		main[index].Count = 1
		if index < len(main)-1 {
			main[index].UpgradeLevel = 13
			main[index].Amplify = true
		}
	}

	medalNames := []string{
		"完美的野兽图腾勋章", "完美的神速图腾勋章", "完美的斗志图腾勋章", "完美的集中图腾勋章", "完美的减速图腾勋章", "完美的协作图腾勋章", "完美的再生图腾勋章",
		"完美的守护图腾勋章", "完美的祝福图腾勋章", "完美的惩罚图腾勋章", "完美的元气图腾勋章", "完美的觉醒图腾勋章", "完美的坚韧图腾勋章",
	}
	medals := make([]itemSpec, 0, len(medalNames))
	for index, name := range medalNames {
		itemID := int64(100380051 + index)
		medals = append(medals, itemSpec{
			ListType: medalListType, Slot: int16(index), ItemID: itemID, Name: name,
			PVFPath: fmt.Sprintf("equipment/character/common/flag/%d.equ", itemID), Kind: "equipment", PVFType: "[flag]",
			Rarity: 6, Grade: 55, Count: 1, UpgradeLevel: 16,
		})
	}

	gemIDs := []int64{90003, 90013, 90023, 90033, 90043, 90053, 90063, 90073, 90083, 90093, 90103, 90113, 90123}
	gemNames := []string{
		"玲珑之光守护珠 (物理防御力)", "玲珑之光守护珠 (魔法防御力)", "玲珑之光守护珠 (攻击速度)", "玲珑之光守护珠 (移动速度)", "玲珑之光守护珠 (施放速度)",
		"玲珑之光守护珠 (物理暴击)", "玲珑之光守护珠 (魔法暴击)", "玲珑之光守护珠 (火属性强化)", "玲珑之光守护珠 (冰属性强化)", "玲珑之光守护珠 (暗属性强化)",
		"玲珑之光守护珠 (光属性强化)", "玲珑之光守护珠 (命中率)", "玲珑之光守护珠 (负重上限)",
	}
	gems := make([]itemSpec, 0, len(gemIDs))
	for index, itemID := range gemIDs {
		gems = append(gems, itemSpec{
			ListType: medalListType, Slot: int16(49 + index), ItemID: itemID, Name: gemNames[index],
			PVFPath: fmt.Sprintf("stackable/flaggem/%d.stk", itemID), Kind: "stackable", PVFType: "[flag gem]",
			Rarity: 3, Grade: 3, Count: 10,
		})
	}
	return append(append(main, medals...), gems...)
}

func countList(specs []itemSpec, listType byte) int {
	count := 0
	for _, spec := range specs {
		if spec.ListType == listType {
			count++
		}
	}
	return count
}

func countMedals(specs []itemSpec) int {
	count := 0
	for _, spec := range specs {
		if spec.PVFType == "[flag]" {
			count++
		}
	}
	return count
}

func countGuardianGems(specs []itemSpec) int {
	count := 0
	for _, spec := range specs {
		if spec.PVFType == "[flag gem]" {
			count++
		}
	}
	return count
}

func guardianGemUnits(specs []itemSpec) int64 {
	var count int64
	for _, spec := range specs {
		if spec.PVFType == "[flag gem]" {
			count += spec.Count
		}
	}
	return count
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "grantlevel90loadout:", err)
	os.Exit(1)
}
