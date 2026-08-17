package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const currentDungeonRankSystemPVFPath = "Etc/RankSystemInfo.etc"

var (
	errCurrentDungeonClearRankCatalogInvalid   = errors.New("current dungeon clear-rank PVF catalog is invalid")
	errCurrentDungeonSettlementPresentationBad = errors.New("current dungeon settlement presentation state is invalid")
)

// currentDungeonClearRankCatalog is the small, read-only domain portion of
// Etc/RankSystemInfo.etc that maps the rank point sent by the current client
// in C2S op46 to the result grade.  It deliberately has no hardcoded fallback:
// a server running a different Script.pvf must either load that PVF table or
// block result emission instead of silently inventing grades.
type currentDungeonClearRankCatalog struct {
	gradeThresholds []byte
}

func loadCurrentDungeonClearRankCatalog(source dnfpvf.Source) (currentDungeonClearRankCatalog, error) {
	if source == nil {
		return currentDungeonClearRankCatalog{}, errCurrentDungeonClearRankCatalogInvalid
	}
	text, err := source.ReadText(currentDungeonRankSystemPVFPath)
	if err != nil {
		return currentDungeonClearRankCatalog{}, fmt.Errorf("read %s: %w", currentDungeonRankSystemPVFPath, err)
	}
	document, err := dnfpvf.Parse(currentDungeonRankSystemPVFPath, text)
	if err != nil {
		return currentDungeonClearRankCatalog{}, fmt.Errorf("parse %s: %w", currentDungeonRankSystemPVFPath, err)
	}
	values := document.Ints("rank grade")
	if len(values) == 0 {
		return currentDungeonClearRankCatalog{}, fmt.Errorf("%w: path=%s section=rank grade", errCurrentDungeonClearRankCatalogInvalid, currentDungeonRankSystemPVFPath)
	}
	thresholds := make([]byte, 0, len(values))
	previous := int64(math.MaxUint8) + 1
	for index, value := range values {
		if value < 0 || value > math.MaxUint8 || value >= previous {
			return currentDungeonClearRankCatalog{}, fmt.Errorf(
				"%w: path=%s section=rank grade index=%d value=%d previous=%d",
				errCurrentDungeonClearRankCatalogInvalid,
				currentDungeonRankSystemPVFPath,
				index,
				value,
				previous,
			)
		}
		thresholds = append(thresholds, byte(value))
		previous = value
	}
	return currentDungeonClearRankCatalog{gradeThresholds: thresholds}, nil
}

func (catalog currentDungeonClearRankCatalog) GradeForPoint(rankPoint byte) byte {
	for _, threshold := range catalog.gradeThresholds {
		if rankPoint >= threshold {
			return threshold
		}
	}
	return 0
}

type currentDungeonSettlementPresentation struct {
	RankGrade       byte
	ClearTimeMS     uint32
	TimeBonusPoint  byte
	AllVisitedClear bool
}

// currentDungeonSettlementPresentationForRuntime freezes only op34 fields
// whose source is known.  The completion timestamp is the Phase-A clear-map
// timestamp when it exists; tutorial paths do not own that Phase-A row, so
// they use the already-frozen op31 settlement-entry timestamp instead.
//
// The current EXE reader establishes the field grammar but not a server-side
// time-bonus formula.  Accordingly TimeBonusPoint remains the neutral zero
// value; rank grade is derived only from the real PVF thresholds and the
// client-provided rank point already accepted by the current op46 owner.
func currentDungeonSettlementPresentationForRuntime(
	runtime *runtimeDungeonState,
	catalog currentDungeonClearRankCatalog,
	rankPoint byte,
) (currentDungeonSettlementPresentation, error) {
	if runtime == nil || runtime.Session == nil {
		return currentDungeonSettlementPresentation{}, errCurrentDungeonSettlementPresentationBad
	}
	completedAt := runtime.clearMapCompletionAt
	if completedAt.IsZero() {
		completedAt = runtime.settlementEntryAt
	}
	return currentDungeonSettlementPresentationForRun(
		runtime.startedAt,
		completedAt,
		runtime.Session.Snapshot().Run,
		catalog,
		rankPoint,
	)
}

func currentDungeonSettlementPresentationForRun(
	startedAt time.Time,
	completedAt time.Time,
	run worldmap.DungeonRunSnapshot,
	catalog currentDungeonClearRankCatalog,
	rankPoint byte,
) (currentDungeonSettlementPresentation, error) {
	if run.Status != worldmap.DungeonRunCompleted || len(catalog.gradeThresholds) == 0 {
		return currentDungeonSettlementPresentation{}, errCurrentDungeonSettlementPresentationBad
	}
	elapsed, err := currentDungeonSettlementElapsedMilliseconds(startedAt, completedAt)
	if err != nil {
		return currentDungeonSettlementPresentation{}, err
	}
	return currentDungeonSettlementPresentation{
		RankGrade:       catalog.GradeForPoint(rankPoint),
		ClearTimeMS:     elapsed,
		TimeBonusPoint:  0,
		AllVisitedClear: currentDungeonAllVisitedRoomsCleared(run),
	}, nil
}

func currentDungeonSettlementElapsedMilliseconds(startedAt, completedAt time.Time) (uint32, error) {
	if startedAt.IsZero() || completedAt.IsZero() || completedAt.Before(startedAt) {
		return 0, errCurrentDungeonSettlementPresentationBad
	}
	milliseconds := uint64(completedAt.Sub(startedAt) / time.Millisecond)
	if milliseconds > math.MaxUint32 {
		return 0, fmt.Errorf("%w: elapsed_ms=%d", errCurrentDungeonSettlementPresentationBad, milliseconds)
	}
	return uint32(milliseconds), nil
}

func currentDungeonAllVisitedRoomsCleared(run worldmap.DungeonRunSnapshot) bool {
	if run.Status != worldmap.DungeonRunCompleted || len(run.Visited) == 0 || len(run.Cleared) != len(run.Visited) {
		return false
	}
	cleared := make(map[worldmap.RoomCoordinate]struct{}, len(run.Cleared))
	for _, coordinate := range run.Cleared {
		cleared[coordinate] = struct{}{}
	}
	if len(cleared) != len(run.Cleared) {
		return false
	}
	for _, coordinate := range run.Visited {
		if _, found := cleared[coordinate]; !found {
			return false
		}
	}
	return true
}
