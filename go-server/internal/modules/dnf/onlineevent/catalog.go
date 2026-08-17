package onlineevent

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	// AttendanceEventPVFPath is the only attendance table proved in the
	// current production Script.pvf. Loading it does not imply that the event
	// is enabled, dated, or attached to a client command.
	AttendanceEventPVFPath      = "event/chn_event/chn_attendanceevent.evt"
	attendanceStackableListPath = "stackable/stackable.lst"

	attendanceRewardCount = 4
)

var (
	ErrAttendancePVFMalformed     = errors.New("online-event attendance PVF is malformed")
	ErrAttendanceRewardUnresolved = errors.New("online-event attendance reward item is unresolved")
)

const (
	processTimeSecondsSection        = "process time seconds"
	rewardItemSection                = "reward item"
	processSecondsForMaxCountSection = "process seconds for max count"
	rewardItemForSumSection          = "reward item for sum"
	rewardActivityForSumSection      = "reward activity for sum"
)

// AttendancePVFItemReward is one item/count pair copied directly from an
// attendance PVF section. DefinitionPath is resolved through stackable.lst
// and verified readable while the catalog is loaded.
type AttendancePVFItemReward struct {
	ItemID         int64
	Count          int64
	DefinitionPath string
}

// AttendancePVFSnapshot is a detached view of the five proved PVF sections.
// ProcessDurationsSeconds deliberately preserves each raw duration; it is not
// converted to cumulative time. SumThresholds likewise retains the source
// values without supplying reset, date, activation, or protocol semantics.
type AttendancePVFSnapshot struct {
	ProcessDurationsSeconds []int64
	RewardItems             []AttendancePVFItemReward
	SumThresholds           []int64
	RewardItemsForSum       []AttendancePVFItemReward
	RewardActivityForSum    []int64
}

// AttendancePVFCatalog is an immutable, protocol-independent projection of
// chn_attendanceevent.evt. It is intentionally not converted into Definition:
// the current PVF does not prove an event ID, dates, enabled state, daily reset
// rule, or whether the raw process durations are cumulative.
type AttendancePVFCatalog struct {
	snapshot AttendancePVFSnapshot
}

// LoadAttendancePVFCatalog parses the five proved attendance sections and
// verifies all eight reward item definitions through stackable.lst. It does
// not register a handler or publish any client packet.
func LoadAttendancePVFCatalog(ctx context.Context, source dnfpvf.Source) (*AttendancePVFCatalog, error) {
	if source == nil {
		return nil, dnfpvf.ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	text, err := source.ReadText(AttendanceEventPVFPath)
	if err != nil {
		return nil, fmt.Errorf("read online-event attendance PVF %q: %w", AttendanceEventPVFPath, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, err := dnfpvf.Parse(AttendanceEventPVFPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse online-event attendance PVF %q: %w", AttendanceEventPVFPath, err)
	}
	if err := validateAttendanceSectionLayout(document); err != nil {
		return nil, err
	}

	processDurations, err := attendanceIntegerSection(document, processTimeSecondsSection, attendanceRewardCount)
	if err != nil {
		return nil, err
	}
	if err := requirePositiveAttendanceValues(processTimeSecondsSection, processDurations); err != nil {
		return nil, err
	}
	rewardValues, err := attendanceIntegerSection(document, rewardItemSection, attendanceRewardCount*2)
	if err != nil {
		return nil, err
	}
	rewards, err := parseAttendanceRewards(rewardItemSection, rewardValues)
	if err != nil {
		return nil, err
	}
	sumThresholds, err := attendanceIntegerSection(document, processSecondsForMaxCountSection, attendanceRewardCount)
	if err != nil {
		return nil, err
	}
	if err := requirePositiveAttendanceValues(processSecondsForMaxCountSection, sumThresholds); err != nil {
		return nil, err
	}
	sumRewardValues, err := attendanceIntegerSection(document, rewardItemForSumSection, attendanceRewardCount*2)
	if err != nil {
		return nil, err
	}
	sumRewards, err := parseAttendanceRewards(rewardItemForSumSection, sumRewardValues)
	if err != nil {
		return nil, err
	}
	rewardActivityForSum, err := attendanceIntegerSection(document, rewardActivityForSumSection, 1)
	if err != nil {
		return nil, err
	}

	allRewards := make([]AttendancePVFItemReward, 0, len(rewards)+len(sumRewards))
	allRewards = append(allRewards, rewards...)
	allRewards = append(allRewards, sumRewards...)
	definitionPaths, err := resolveAttendanceRewardDefinitions(ctx, source, allRewards)
	if err != nil {
		return nil, err
	}
	attachAttendanceDefinitionPaths(rewards, definitionPaths)
	attachAttendanceDefinitionPaths(sumRewards, definitionPaths)

	return &AttendancePVFCatalog{snapshot: AttendancePVFSnapshot{
		ProcessDurationsSeconds: append([]int64(nil), processDurations...),
		RewardItems:             append([]AttendancePVFItemReward(nil), rewards...),
		SumThresholds:           append([]int64(nil), sumThresholds...),
		RewardItemsForSum:       append([]AttendancePVFItemReward(nil), sumRewards...),
		RewardActivityForSum:    append([]int64(nil), rewardActivityForSum...),
	}}, nil
}

// Snapshot returns copies of every slice so callers cannot mutate the loaded
// catalog.
func (c *AttendancePVFCatalog) Snapshot() AttendancePVFSnapshot {
	if c == nil {
		return AttendancePVFSnapshot{}
	}
	snapshot := c.snapshot
	snapshot.ProcessDurationsSeconds = append([]int64(nil), snapshot.ProcessDurationsSeconds...)
	snapshot.RewardItems = append([]AttendancePVFItemReward(nil), snapshot.RewardItems...)
	snapshot.SumThresholds = append([]int64(nil), snapshot.SumThresholds...)
	snapshot.RewardItemsForSum = append([]AttendancePVFItemReward(nil), snapshot.RewardItemsForSum...)
	snapshot.RewardActivityForSum = append([]int64(nil), snapshot.RewardActivityForSum...)
	return snapshot
}

func attendanceIntegerSection(document *dnfpvf.Document, name string, exactCount int) ([]int64, error) {
	if document == nil {
		return nil, fmt.Errorf("%w: path=%s section=[%s] document is nil", ErrAttendancePVFMalformed, AttendanceEventPVFPath, name)
	}
	sectionCount := 0
	for _, section := range document.Sections {
		if strings.EqualFold(strings.TrimSpace(section.Name), name) {
			sectionCount++
		}
	}
	if sectionCount != 1 {
		return nil, fmt.Errorf("%w: path=%s section=[%s] count=%d want=1", ErrAttendancePVFMalformed, AttendanceEventPVFPath, name, sectionCount)
	}
	tokens, ok := document.Section(name)
	if !ok {
		return nil, fmt.Errorf("%w: path=%s section=[%s] is unreadable", ErrAttendancePVFMalformed, AttendanceEventPVFPath, name)
	}
	if len(tokens) != exactCount {
		return nil, fmt.Errorf("%w: path=%s section=[%s] values=%d want=%d", ErrAttendancePVFMalformed, AttendanceEventPVFPath, name, len(tokens), exactCount)
	}
	values := make([]int64, len(tokens))
	for index, token := range tokens {
		if token.Kind != dnfpvf.TokenInt {
			return nil, fmt.Errorf(
				"%w: path=%s section=[%s] index=%d token=%q at %d:%d is not an integer",
				ErrAttendancePVFMalformed,
				AttendanceEventPVFPath,
				name,
				index,
				token.Raw,
				token.Line,
				token.Column,
			)
		}
		values[index] = token.Int
	}
	return values, nil
}

func validateAttendanceSectionLayout(document *dnfpvf.Document) error {
	if document == nil {
		return fmt.Errorf("%w: path=%s document is nil", ErrAttendancePVFMalformed, AttendanceEventPVFPath)
	}
	names := []string{
		processTimeSecondsSection,
		rewardItemSection,
		processSecondsForMaxCountSection,
		rewardItemForSumSection,
		rewardActivityForSumSection,
	}
	expected := make([]string, 0, len(names)*2)
	for _, name := range names {
		expected = append(expected, strings.ToLower(name), "/"+strings.ToLower(name))
	}
	if len(document.Sections) != len(expected) {
		return fmt.Errorf(
			"%w: path=%s section_markers=%d want=%d",
			ErrAttendancePVFMalformed,
			AttendanceEventPVFPath,
			len(document.Sections),
			len(expected),
		)
	}
	for index, section := range document.Sections {
		name := strings.ToLower(strings.TrimSpace(section.Name))
		if name != expected[index] {
			return fmt.Errorf(
				"%w: path=%s section_marker[%d]=[%s] want=[%s]",
				ErrAttendancePVFMalformed,
				AttendanceEventPVFPath,
				index,
				section.Name,
				expected[index],
			)
		}
		markerIndex := section.Start - 1
		if markerIndex < 0 || markerIndex >= len(document.Tokens) ||
			document.Tokens[markerIndex].Kind != dnfpvf.TokenSection ||
			!strings.EqualFold(strings.TrimSpace(document.Tokens[markerIndex].Value), strings.TrimSpace(section.Name)) {
			return fmt.Errorf(
				"%w: path=%s section_marker[%d] token range is invalid",
				ErrAttendancePVFMalformed,
				AttendanceEventPVFPath,
				index,
			)
		}
		if index == 0 && markerIndex != 0 {
			return fmt.Errorf(
				"%w: path=%s has %d root tokens before the first section",
				ErrAttendancePVFMalformed,
				AttendanceEventPVFPath,
				markerIndex,
			)
		}
		if strings.HasPrefix(name, "/") && section.Start != section.End {
			return fmt.Errorf(
				"%w: path=%s closing section=[%s] contains %d values",
				ErrAttendancePVFMalformed,
				AttendanceEventPVFPath,
				section.Name,
				section.End-section.Start,
			)
		}
	}
	return nil
}

func requirePositiveAttendanceValues(section string, values []int64) error {
	for index, value := range values {
		if value <= 0 {
			return fmt.Errorf("%w: path=%s section=[%s] index=%d value=%d is not positive", ErrAttendancePVFMalformed, AttendanceEventPVFPath, section, index, value)
		}
	}
	return nil
}

func parseAttendanceRewards(section string, values []int64) ([]AttendancePVFItemReward, error) {
	if len(values) != attendanceRewardCount*2 {
		return nil, fmt.Errorf("%w: path=%s section=[%s] values=%d want=%d", ErrAttendancePVFMalformed, AttendanceEventPVFPath, section, len(values), attendanceRewardCount*2)
	}
	rewards := make([]AttendancePVFItemReward, 0, attendanceRewardCount)
	for index := 0; index < len(values); index += 2 {
		itemID, count := values[index], values[index+1]
		if itemID <= 0 || count <= 0 {
			return nil, fmt.Errorf(
				"%w: path=%s section=[%s] pair=%d item_id=%d count=%d",
				ErrAttendancePVFMalformed,
				AttendanceEventPVFPath,
				section,
				index/2,
				itemID,
				count,
			)
		}
		rewards = append(rewards, AttendancePVFItemReward{ItemID: itemID, Count: count})
	}
	return rewards, nil
}

func resolveAttendanceRewardDefinitions(
	ctx context.Context,
	source dnfpvf.Source,
	rewards []AttendancePVFItemReward,
) (map[int64]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listText, err := source.ReadText(attendanceStackableListPath)
	if err != nil {
		return nil, fmt.Errorf("read online-event reward list %q: %w", attendanceStackableListPath, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listDocument, err := dnfpvf.Parse(attendanceStackableListPath, listText)
	if err != nil {
		return nil, fmt.Errorf("parse online-event reward list %q: %w", attendanceStackableListPath, err)
	}

	wanted := make(map[int64]struct{}, len(rewards))
	wantedOrder := make([]int64, 0, len(rewards))
	for _, reward := range rewards {
		if _, duplicate := wanted[reward.ItemID]; !duplicate {
			wantedOrder = append(wantedOrder, reward.ItemID)
		}
		wanted[reward.ItemID] = struct{}{}
	}
	listed := make(map[int64]string, len(wanted))
	for _, entry := range dnfpvf.ParseList(listDocument) {
		if _, needed := wanted[entry.ID]; !needed {
			continue
		}
		if previous, duplicate := listed[entry.ID]; duplicate {
			return nil, fmt.Errorf(
				"%w: list=%s item_id=%d duplicate paths=%q,%q",
				ErrAttendancePVFMalformed,
				attendanceStackableListPath,
				entry.ID,
				previous,
				entry.Path,
			)
		}
		listed[entry.ID] = entry.Path
	}

	resolved := make(map[int64]string, len(wanted))
	for _, itemID := range wantedOrder {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		listedPath, ok := listed[itemID]
		if !ok {
			return nil, fmt.Errorf("%w: list=%s item_id=%d", ErrAttendanceRewardUnresolved, attendanceStackableListPath, itemID)
		}
		definitionPath, err := attendanceStackableDefinitionPath(listedPath)
		if err != nil {
			return nil, fmt.Errorf("%w: list=%s item_id=%d path=%q: %v", ErrAttendanceRewardUnresolved, attendanceStackableListPath, itemID, listedPath, err)
		}
		if _, err := source.ReadText(definitionPath); err != nil {
			return nil, fmt.Errorf("%w: item_id=%d definition=%s: %v", ErrAttendanceRewardUnresolved, itemID, definitionPath, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved[itemID] = definitionPath
	}
	return resolved, nil
}

func attendanceStackableDefinitionPath(listedPath string) (string, error) {
	clean := strings.TrimSpace(strings.ReplaceAll(listedPath, "\\", "/"))
	if strings.HasPrefix(clean, "/") || (len(clean) >= 2 && clean[1] == ':') {
		return "", errors.New("definition path is absolute")
	}
	clean = strings.TrimPrefix(clean, "./")
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".." {
			return "", errors.New("definition path escapes stackable root")
		}
	}
	clean = path.Clean(clean)
	if clean == "" || clean == "." {
		return "", errors.New("empty definition path")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("definition path escapes stackable root")
	}
	root := path.Dir(attendanceStackableListPath)
	if !strings.HasPrefix(strings.ToLower(clean), strings.ToLower(root)+"/") {
		clean = path.Join(root, clean)
	}
	if !strings.HasPrefix(strings.ToLower(clean), strings.ToLower(root)+"/") {
		return "", errors.New("definition path escapes stackable root")
	}
	if !strings.EqualFold(path.Ext(clean), ".stk") {
		return "", errors.New("definition path is not a stackable .stk file")
	}
	return clean, nil
}

func attachAttendanceDefinitionPaths(rewards []AttendancePVFItemReward, paths map[int64]string) {
	for index := range rewards {
		rewards[index].DefinitionPath = paths[rewards[index].ItemID]
	}
}
