package charstat

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

var (
	tagLineRE       = regexp.MustCompile(`(?is)\[\s*%s\s*\]\s*([-\d.]+)`)
	growtypeTagRE   = regexp.MustCompile(`(?i)\[\s*growtype\s+([1-6])\s*\]`)
	awakeningTagRE  = regexp.MustCompile(`(?i)\[\s*awakening\s+([1-2])\s*\]`)
	initialValueTag = regexp.MustCompile(`(?i)\[\s*initial\s+value\s*\]`)
)

// Load 从 PVF 文本来源加载 character.lst 和每个职业 chr 文件。
func Load(ctx context.Context, source Source, options Options) (*Table, error) {
	if source == nil {
		return nil, ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	listPath := cleanPath(options.ListPath)
	if listPath == "" {
		listPath = DefaultList
	}
	listText, err := source.ReadText(listPath)
	if err != nil {
		return nil, err
	}
	doc, err := dnfpvf.Parse(listPath, listText)
	if err != nil {
		return nil, err
	}
	entries := dnfpvf.ParseList(doc)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
	}
	table := &Table{jobs: make(map[byte]jobTables)}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.ID < 0 || entry.ID > 0xff || strings.TrimSpace(entry.Path) == "" {
			continue
		}
		charPath := resolvePath(path.Dir(listPath), entry.Path)
		text, actualPath, err := readTextAny(source, charPath, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("read character stat job %d: %w", entry.ID, err)
		}
		tables, err := parseJobTables(actualPath, text)
		if err != nil {
			return nil, fmt.Errorf("parse character stat job %d %s: %w", entry.ID, actualPath, err)
		}
		table.jobs[byte(entry.ID)] = tables
	}
	if len(table.jobs) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
	}
	return table, nil
}

// Compute 计算 job/growType/level 对应的职业基础属性。
func (t *Table) Compute(job byte, growType byte, level int) (Vector, error) {
	if t == nil {
		return Vector{}, ErrSourceRequired
	}
	tables, ok := t.jobs[job]
	if !ok {
		return Vector{}, fmt.Errorf("%w: %d", ErrJobMissing, job)
	}
	first, second := DecodeGrowType(growType)
	if first < 0 || first > 5 || second < 0 || second > 2 {
		return Vector{}, fmt.Errorf("%w: job=%d grow=%d", ErrGrowthMissing, job, growType)
	}
	if level < 1 {
		level = 1
	}
	out := tables.base
	for current := 1; current < level; current++ {
		switch {
		case current <= 14:
			growth, ok := tables.growth(job, 0, 0)
			if !ok {
				return Vector{}, fmt.Errorf("%w: job=%d growtype=1", ErrGrowthMissing, job)
			}
			out.add(growth)
		case current <= 49:
			growth, ok := tables.growth(job, first, 0)
			if !ok {
				return Vector{}, fmt.Errorf("%w: job=%d growtype=%d", ErrGrowthMissing, job, first+1)
			}
			out.add(growth)
		default:
			growth, ok := tables.growth(job, first, second)
			if !ok {
				return Vector{}, fmt.Errorf("%w: job=%d growtype=%d awakening=%d", ErrGrowthMissing, job, first+1, second)
			}
			out.add(growth)
		}
	}
	return out, nil
}

func (t jobTables) growth(_ byte, first int, second int) (Vector, bool) {
	growIndex := first + 1
	if growIndex < 1 || growIndex >= len(t.growtype) {
		return Vector{}, false
	}
	if second == 0 {
		return t.growtype[growIndex], t.hasGrow[growIndex]
	}
	if second < 0 || second >= len(t.awakening[growIndex]) {
		return Vector{}, false
	}
	return t.awakening[growIndex][second], t.hasAwake[growIndex][second]
}

func parseJobTables(charPath string, text string) (jobTables, error) {
	initial := initialValueTag.FindStringIndex(text)
	if initial == nil {
		return jobTables{}, fmt.Errorf("missing [initial value]")
	}
	growMatches := growtypeTagRE.FindAllStringSubmatchIndex(text, -1)
	firstGrow := len(text)
	for _, match := range growMatches {
		if match[0] > initial[0] && match[0] < firstGrow {
			firstGrow = match[0]
		}
	}
	tables := jobTables{
		path: charPath,
		base: parseVector(text[initial[0]:firstGrow]),
	}
	growStarts := make([]struct {
		no    int
		start int
		end   int
	}, 0, len(growMatches))
	for _, match := range growMatches {
		no, err := strconv.Atoi(text[match[2]:match[3]])
		if err != nil || no < 1 || no > 6 {
			continue
		}
		growStarts = append(growStarts, struct {
			no    int
			start int
			end   int
		}{no: no, start: match[0]})
	}
	sort.Slice(growStarts, func(i, j int) bool { return growStarts[i].start < growStarts[j].start })
	for idx := range growStarts {
		end := len(text)
		if idx+1 < len(growStarts) {
			end = growStarts[idx+1].start
		}
		growStarts[idx].end = end
		block := text[growStarts[idx].start:end]
		parseGrowthBlock(block, growStarts[idx].no, &tables)
	}
	if !tables.hasGrow[1] {
		return jobTables{}, fmt.Errorf("missing [growtype 1]")
	}
	return tables, nil
}

func parseGrowthBlock(block string, growNo int, tables *jobTables) {
	awakeMatches := awakeningTagRE.FindAllStringSubmatchIndex(block, -1)
	growEnd := len(block)
	if len(awakeMatches) > 0 {
		growEnd = awakeMatches[0][0]
	}
	tables.growtype[growNo] = parseVector(block[:growEnd])
	tables.hasGrow[growNo] = true
	for idx, match := range awakeMatches {
		no, err := strconv.Atoi(block[match[2]:match[3]])
		if err != nil || no < 1 || no > 2 {
			continue
		}
		end := len(block)
		if idx+1 < len(awakeMatches) {
			end = awakeMatches[idx+1][0]
		}
		tables.awakening[growNo][no] = parseVector(block[match[0]:end])
		tables.hasAwake[growNo][no] = true
	}
}

func parseVector(section string) Vector {
	physicalAttack := statRaw(section, "physical attack")
	physicalDefense := statRaw(section, "physical defense")
	magicalAttack := statRaw(section, "magical attack")
	magicalDefense := statRaw(section, "magical defense")
	strength := statRaw(section, "strength")
	intelligence := statRaw(section, "intelligence")
	vitality := statRaw(section, "vitality")
	spirit := statRaw(section, "spirit")
	// 当前 23.4.15.0 PVF 的 etc/avatarabilitystringtable.etc 把
	// PHYSICAL_ATTACK/PHYSICAL_DEFENSE/MAGICAL_ATTACK/MAGICAL_DEFENSE 显示为
	// 力量/体力/智力/精神；部分 .chr 只写旧标签，所以这里按客户端能力表补齐四维。
	if strength == 0 {
		strength = physicalAttack
	}
	if vitality == 0 {
		vitality = physicalDefense
	}
	if intelligence == 0 {
		intelligence = magicalAttack
	}
	if spirit == 0 {
		spirit = magicalDefense
	}
	return Vector{
		HPMax:             statScaled10(section, "HP MAX"),
		MPMax:             statScaled10(section, "MP MAX"),
		Strength:          strength,
		Intelligence:      intelligence,
		Vitality:          vitality,
		Spirit:            spirit,
		PhysicalAttack:    physicalAttack,
		PhysicalDefense:   physicalDefense,
		MagicalAttack:     magicalAttack,
		MagicalDefense:    magicalDefense,
		IndependentAttack: statRaw(section, "independent attack"),
		FireResistance:    statRaw(section, "fire resistance"),
		WaterResistance:   statRaw(section, "water resistance"),
		DarkResistance:    statRaw(section, "dark resistance"),
		LightResistance:   statRaw(section, "light resistance"),
		InventoryLimit:    statScaled10(section, "inventory limit"),
		HPRegenSpeed:      statScaled10(section, "HP regen speed"),
		MPRegenSpeed:      statScaled10(section, "MP regen speed"),
		MoveSpeed:         statScaled10(section, "move speed"),
		AttackSpeed:       statScaled10(section, "attack speed"),
		CastSpeed:         statScaled10(section, "cast speed"),
		HitRecovery:       statScaled10(section, "hit recovery"),
		JumpPower:         statScaled10(section, "jump power"),
		Weight:            statScaled10(section, "weight"),
	}
}

func statRaw(section string, key string) int64 {
	return statNumber(section, key, 1)
}

func statScaled10(section string, key string) int64 {
	return statNumber(section, key, 10)
}

func statNumber(section string, key string, scale float32) int64 {
	pattern := fmt.Sprintf(tagLineRE.String(), regexp.QuoteMeta(key))
	matches := regexp.MustCompile(pattern).FindStringSubmatch(section)
	if len(matches) < 2 {
		return 0
	}
	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}
	return int64(float32(value) * scale)
}

func readTextAny(source Source, candidates ...string) (string, string, error) {
	seen := make(map[string]struct{}, len(candidates))
	var lastErr error
	for _, candidate := range candidates {
		clean := cleanPath(candidate)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		text, err := source.ReadText(clean)
		if err == nil {
			return text, clean, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("pvf path candidates are empty")
}

func resolvePath(base string, ref string) string {
	base = cleanPath(base)
	ref = cleanPath(ref)
	if base == "" || ref == "" {
		return ref
	}
	lowerRef := strings.ToLower(ref)
	lowerBase := strings.ToLower(base)
	if lowerRef == lowerBase || strings.HasPrefix(lowerRef, lowerBase+"/") {
		return ref
	}
	return cleanPath(path.Join(base, ref))
}

func cleanPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.HasPrefix(value, "./") || strings.HasPrefix(value, "/") {
		value = strings.TrimPrefix(value, "./")
		value = strings.TrimPrefix(value, "/")
	}
	if value == "" {
		return ""
	}
	clean := strings.TrimSuffix(path.Clean(value), "/")
	if clean == "." {
		return ""
	}
	return clean
}
