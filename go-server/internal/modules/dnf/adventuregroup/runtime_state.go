package adventuregroup

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const RuntimeStateMetadataKey = "adventure_group_runtime_v1"

var ErrRuntimeStateInvalid = errors.New("adventure-group runtime state is invalid")

type RuntimeState struct {
	Version          int                        `json:"version"`
	ShopPoints       [ShopPointTypeCount]uint32 `json:"shop_points"`
	BraveExperience  uint64                     `json:"brave_experience"`
	GrowthExperience uint64                     `json:"growth_experience"`
	Month            string                     `json:"month"`
	Purchases        map[string]uint32          `json:"purchases,omitempty"`
	Expeditions      map[string]Expedition      `json:"expeditions,omitempty"`
}

type Expedition struct {
	Area       byte               `json:"area"`
	State      byte               `json:"state"`
	StartedAt  int64              `json:"started_at"`
	EndsAt     int64              `json:"ends_at"`
	Attributes []byte             `json:"attributes,omitempty"`
	Members    []ExpeditionMember `json:"members"`
	Reward     uint32             `json:"reward"`
}

type ExpeditionMember struct {
	CharacterID uint16 `json:"character_id"`
	Name        string `json:"name"`
	Level       uint32 `json:"level"`
	Job         byte   `json:"job"`
	GrowType    byte   `json:"grow_type"`
	Status      byte   `json:"status"`
}

func ParseRuntimeState(account dnfrepo.AccountRecord, config RuntimeConfig, now time.Time) (RuntimeState, error) {
	state := RuntimeState{Version: 1}
	value := strings.TrimSpace(account.Metadata[RuntimeStateMetadataKey])
	if value != "" {
		if err := json.Unmarshal([]byte(value), &state); err != nil {
			return RuntimeState{}, fmt.Errorf("%w: %v", ErrRuntimeStateInvalid, err)
		}
		if state.Version != 1 {
			return RuntimeState{}, fmt.Errorf("%w: version=%d", ErrRuntimeStateInvalid, state.Version)
		}
	}
	if state.Purchases == nil {
		state.Purchases = make(map[string]uint32)
	}
	if state.Expeditions == nil {
		state.Expeditions = make(map[string]Expedition)
	}
	state.Normalize(config, now)
	return state, nil
}

func (s *RuntimeState) Normalize(config RuntimeConfig, now time.Time) {
	if s == nil {
		return
	}
	s.Version = 1
	month := now.In(adventureGroupCalendarLocation).Format("2006-01")
	if s.Month != month {
		s.Month = month
		s.Purchases = make(map[string]uint32)
		for _, category := range config.ShopCategories {
			if category.ResetPointMonthly && int(category.Index) < len(s.ShopPoints) {
				s.ShopPoints[category.Index] = 0
			}
		}
	}
	if s.Purchases == nil {
		s.Purchases = make(map[string]uint32)
	}
	if s.Expeditions == nil {
		s.Expeditions = make(map[string]Expedition)
	}
	for _, category := range config.ShopCategories {
		if int(category.Index) < len(s.ShopPoints) && category.MaxPoint > 0 &&
			s.ShopPoints[category.Index] > category.MaxPoint {
			s.ShopPoints[category.Index] = category.MaxPoint
		}
	}
	if config.Capsule.MaximumExperience > 0 && s.GrowthExperience > config.Capsule.MaximumExperience {
		s.GrowthExperience = config.Capsule.MaximumExperience
	}
}

func (s RuntimeState) Marshal() (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRuntimeStateInvalid, err)
	}
	return string(data), nil
}

func (s RuntimeState) PurchaseCount(category byte, itemID uint32) uint32 {
	return s.Purchases[purchaseKey(category, itemID)]
}

func (s *RuntimeState) Spend(category ShopCategory, product ShopProduct) error {
	if s == nil || int(category.Index) >= len(s.ShopPoints) {
		return ErrRuntimeStateInvalid
	}
	count := s.PurchaseCount(category.Index, product.ItemID)
	if count >= product.MonthlyPurchaseLimit || s.ShopPoints[category.Index] < product.Cost {
		return ErrRuntimeStateInvalid
	}
	s.ShopPoints[category.Index] -= product.Cost
	s.Purchases[purchaseKey(category.Index, product.ItemID)] = count + 1
	return nil
}

func (s *RuntimeState) AddGlory(config RuntimeConfig, value uint32) {
	if s == nil || value == 0 {
		return
	}
	category, ok := config.Shop(ShopPointGlory)
	if !ok {
		return
	}
	s.ShopPoints[ShopPointGlory] = saturatingPointAdd(s.ShopPoints[ShopPointGlory], value, category.MaxPoint)
}

// AddMaximumLevelExperience accumulates both PVF systems owned by max-level
// dungeon settlement: Brave crystals with a carried remainder and the growth
// capsule gauge.
func (s *RuntimeState) AddMaximumLevelExperience(config RuntimeConfig, value uint32) {
	if s == nil || value == 0 {
		return
	}
	if category, ok := config.Shop(ShopPointBrave); ok && category.ExperiencePerPoint > 0 && category.PointPerExperience > 0 {
		total := s.BraveExperience + uint64(value)
		points := total / category.ExperiencePerPoint
		s.BraveExperience = total % category.ExperiencePerPoint
		if points > math.MaxUint32 {
			points = math.MaxUint32
		}
		add := uint64(category.PointPerExperience) * points
		if add > math.MaxUint32 {
			add = math.MaxUint32
		}
		s.ShopPoints[ShopPointBrave] = saturatingPointAdd(s.ShopPoints[ShopPointBrave], uint32(add), category.MaxPoint)
	}
	if config.Capsule.MaximumExperience > 0 {
		remaining := config.Capsule.MaximumExperience - minUint64(config.Capsule.MaximumExperience, s.GrowthExperience)
		add := uint64(value)
		if add > remaining {
			add = remaining
		}
		s.GrowthExperience += add
	}
}

func (s *RuntimeState) ConsumeCapsule(config RuntimeConfig) bool {
	if s == nil || config.Capsule.MinimumExperience == 0 || s.GrowthExperience < config.Capsule.MinimumExperience {
		return false
	}
	s.GrowthExperience -= config.Capsule.MinimumExperience
	return true
}

func purchaseKey(category byte, itemID uint32) string {
	return strconv.Itoa(int(category)) + ":" + strconv.FormatUint(uint64(itemID), 10)
}

func ExpeditionKey(area byte) string {
	return strconv.Itoa(int(area))
}

func saturatingPointAdd(current, value, maximum uint32) uint32 {
	total := uint64(current) + uint64(value)
	if maximum > 0 && total > uint64(maximum) {
		return maximum
	}
	if total > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(total)
}

func minUint64(left, right uint64) uint64 {
	if left < right {
		return left
	}
	return right
}
