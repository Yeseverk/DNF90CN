package dungeon

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	"longheng.io/server/internal/modules/dnf/honor"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	settlementReceiptPrefix  = "dungeon_settlement_receipt_"
	settlementReceiptVersion = int64(1)
)

var ErrSettlementStateInvalid = errors.New("dungeon settlement persisted state is invalid")

type SettlementCommand struct {
	AccountID               string
	CharacterID             string
	CompletionKey           string
	Tables                  *progression.Tables
	Experience              uint32
	RecommendedDungeonClear bool
	AdventureRuntime        *adventuregroup.RuntimeConfig
	HonorExpertTables       *honor.Tables
	MaximumCharacterLevel   int
	UpdatedAt               time.Time
}

type SettlementResult struct {
	Character       dnfrepo.CharacterRecord
	Skill           dnfrepo.SkillRecord
	Inventory       dnfrepo.InventoryRecord
	ExperienceGain  uint32
	HonorExpertGain uint32
	SPGain          int
	TPGain          int
	Replayed        bool
}

// CommitSettlement atomically advances experience and the SP/TP ledger and
// writes an idempotency receipt tied to the accepted dungeon completion.
func (o *Owner) CommitSettlement(ctx context.Context, cmd SettlementCommand) (SettlementResult, error) {
	if o == nil || o.settlements == nil || strings.TrimSpace(cmd.CharacterID) == "" ||
		strings.TrimSpace(cmd.AccountID) == "" || strings.TrimSpace(cmd.CompletionKey) == "" ||
		cmd.Tables == nil || cmd.Experience == 0 {
		return SettlementResult{}, ErrOwnerUnavailable
	}
	ctx = contextOrBackground(ctx)
	wantHash := sha256.Sum256([]byte(cmd.CompletionKey))
	now := updatedAtOrNow(cmd.UpdatedAt)

	var result SettlementResult
	err := o.settlements.WithinCharacterSettlement(ctx, cmd.CharacterID, func(tx dnfrepo.Group) error {
		character, found, err := tx.Character.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found || character.CharacterID != cmd.CharacterID ||
			strings.TrimSpace(character.AccountID) != strings.TrimSpace(cmd.AccountID) {
			return ErrSettlementStateInvalid
		}
		skill, found, err := tx.Skill.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found || skill.CharacterID != cmd.CharacterID {
			return ErrSettlementStateInvalid
		}
		inventory, found, err := tx.Inventory.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found || inventory.CharacterID != cmd.CharacterID {
			return ErrSettlementStateInvalid
		}
		character = dnfrepo.CloneCharacter(character)
		skill = dnfrepo.CloneSkill(skill)
		inventory = dnfrepo.CloneInventory(inventory)
		if settlementReceiptMatches(character.Stats, wantHash) {
			receiptGain, receiptSP, err := settlementReceiptValues(character, skill)
			if err != nil {
				return err
			}
			result = SettlementResult{
				Character: character, Skill: skill, Inventory: inventory,
				ExperienceGain: receiptGain, SPGain: receiptSP, Replayed: true,
			}
			return nil
		}
		experience, present := character.Stats["exp"]
		if !present || experience < 0 || uint64(experience) > math.MaxUint32 ||
			character.Level <= 0 || skill.Points.SyncedLevel != character.Level {
			return ErrSettlementStateInvalid
		}
		maximumLevel := cmd.MaximumCharacterLevel
		if maximumLevel <= 0 {
			maximumLevel = math.MaxInt
		}
		planned, err := PlanSettlementProgressionAtCap(
			cmd.Tables,
			character.Level,
			uint32(experience),
			cmd.Experience,
			skill.Points,
			maximumLevel,
		)
		if err != nil {
			return err
		}
		maximumLevelExperience := maximumLevelExperienceGain(
			character.Level,
			uint32(experience),
			cmd.Experience,
			cmd.MaximumCharacterLevel,
			cmd.Tables,
		)
		character.Level = planned.Experience.NewLevel
		character.Stats["exp"] = int64(planned.Experience.NewExperience)
		appliedHonorExpertGain := uint32(0)
		if maximumLevelExperience > 0 && cmd.HonorExpertTables != nil {
			currentExpert, err := honor.ExpertProgressFromStats(character.Stats)
			if err != nil {
				return err
			}
			nextExpert, err := cmd.HonorExpertTables.AdvanceExpert(currentExpert, uint64(maximumLevelExperience))
			if err != nil {
				return err
			}
			expertStats, err := honor.ExpertStats(nextExpert)
			if err != nil {
				return err
			}
			for key, value := range expertStats {
				character.Stats[key] = value
			}
			appliedHonorExpertGain = maximumLevelExperience
		}
		if cmd.RecommendedDungeonClear {
			count := character.Stats[adventuregroup.RecommendedDungeonClearStatKey]
			if count < 0 {
				return ErrSettlementStateInvalid
			}
			if count < math.MaxUint16 {
				character.Stats[adventuregroup.RecommendedDungeonClearStatKey] = count + 1
			}
		}
		writeSettlementReceipt(
			character.Stats,
			wantHash,
			cmd.Experience,
			planned.SkillPoints.SPGain,
			planned.Experience.NewLevel,
			planned.Experience.NewExperience,
		)
		character.UpdatedAt = now
		skill.Points = planned.SkillPoints.New
		skill.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, tx.Character, character, dnfrepo.CharacterFieldBase, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		if err := dnfrepo.SaveSkillFields(ctx, tx.Skill, skill, dnfrepo.SkillFieldPoints); err != nil {
			return err
		}
		if maximumLevelExperience > 0 && cmd.AdventureRuntime != nil {
			if tx.Account == nil {
				return ErrSettlementStateInvalid
			}
			account, accountFound, err := tx.Account.Load(ctx, cmd.AccountID)
			if err != nil {
				return err
			}
			if !accountFound || strings.TrimSpace(account.AccountID) != strings.TrimSpace(cmd.AccountID) {
				return ErrSettlementStateInvalid
			}
			account = dnfrepo.CloneAccount(account)
			runtimeState, err := adventuregroup.ParseRuntimeState(account, *cmd.AdventureRuntime, now)
			if err != nil {
				return err
			}
			runtimeState.AddMaximumLevelExperience(*cmd.AdventureRuntime, maximumLevelExperience)
			encoded, err := runtimeState.Marshal()
			if err != nil {
				return err
			}
			if account.Metadata == nil {
				account.Metadata = make(map[string]string)
			}
			account.Metadata[adventuregroup.RuntimeStateMetadataKey] = encoded
			if account.HonorExp > math.MaxUint64-uint64(maximumLevelExperience) {
				account.HonorExp = math.MaxUint64
			} else {
				account.HonorExp += uint64(maximumLevelExperience)
			}
			account.UpdatedAt = now
			if err := tx.Account.Save(ctx, account); err != nil {
				return err
			}
		}
		result = SettlementResult{
			Character: character, Skill: skill, Inventory: inventory,
			ExperienceGain:  cmd.Experience,
			HonorExpertGain: appliedHonorExpertGain,
			SPGain:          planned.SkillPoints.SPGain,
			TPGain:          planned.SkillPoints.TPGain,
		}
		return nil
	})
	if err != nil {
		return SettlementResult{}, err
	}
	return result, nil
}

func maximumLevelExperienceGain(
	previousLevel int,
	previousExperience uint32,
	gain uint32,
	maximumLevel int,
	tables *progression.Tables,
) uint32 {
	if maximumLevel <= 0 || previousLevel <= 0 || previousLevel > maximumLevel || gain == 0 || tables == nil {
		return 0
	}
	if previousLevel >= maximumLevel {
		return gain
	}
	entryExperience, err := tables.ThresholdToNext(maximumLevel - 1)
	if err != nil {
		return 0
	}
	total := uint64(previousExperience) + uint64(gain)
	if total <= uint64(entryExperience) {
		return 0
	}
	excess := total - uint64(entryExperience)
	if excess > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(excess)
}

func PlanSettlementProgression(
	tables *progression.Tables,
	level int,
	total uint32,
	gain uint32,
	points dnfrepo.SkillPointState,
) (progression.ExperienceSkillPointPlan, error) {
	return PlanSettlementProgressionAtCap(tables, level, total, gain, points, math.MaxInt)
}

func PlanSettlementProgressionAtCap(
	tables *progression.Tables,
	level int,
	total uint32,
	gain uint32,
	points dnfrepo.SkillPointState,
	maximumLevel int,
) (progression.ExperienceSkillPointPlan, error) {
	if tables == nil || level <= 0 || points.SyncedLevel != level || maximumLevel <= 0 || level > maximumLevel {
		return progression.ExperienceSkillPointPlan{}, ErrSettlementStateInvalid
	}
	newTotal := uint64(total) + uint64(gain)
	if newTotal > math.MaxUint32 {
		newTotal = math.MaxUint32
	}
	result := progression.ExperienceResult{
		PreviousLevel:      level,
		PreviousExperience: total,
		Gain:               gain,
		NewLevel:           level,
		NewExperience:      uint32(newTotal),
	}
	for result.NewLevel < maximumLevel {
		threshold, err := tables.ThresholdToNext(result.NewLevel)
		if err != nil {
			return progression.ExperienceSkillPointPlan{}, err
		}
		if result.NewExperience < threshold {
			break
		}
		if _, ok := tables.SkillPointsAtLevel(result.NewLevel + 1); !ok {
			return progression.ExperienceSkillPointPlan{}, ErrSettlementStateInvalid
		}
		result.NewLevel++
	}
	result.LevelsGained = result.NewLevel - result.PreviousLevel
	advance, err := tables.AdvanceSkillPoints(points, result.NewLevel)
	if err != nil {
		return progression.ExperienceSkillPointPlan{}, err
	}
	return progression.ExperienceSkillPointPlan{Experience: result, SkillPoints: advance}, nil
}

func settlementReceiptMatches(stats map[string]int64, want [sha256.Size]byte) bool {
	if stats == nil || stats[settlementReceiptPrefix+"version"] != settlementReceiptVersion {
		return false
	}
	for index := 0; index < 4; index++ {
		if uint64(stats[fmt.Sprintf("%shash_%d", settlementReceiptPrefix, index)]) !=
			binary.LittleEndian.Uint64(want[index*8:(index+1)*8]) {
			return false
		}
	}
	return true
}

func writeSettlementReceipt(
	stats map[string]int64,
	hash [sha256.Size]byte,
	gain uint32,
	spGain int,
	newLevel int,
	newExperience uint32,
) {
	stats[settlementReceiptPrefix+"version"] = settlementReceiptVersion
	for index := 0; index < 4; index++ {
		stats[fmt.Sprintf("%shash_%d", settlementReceiptPrefix, index)] =
			int64(binary.LittleEndian.Uint64(hash[index*8 : (index+1)*8]))
	}
	stats[settlementReceiptPrefix+"experience_gain"] = int64(gain)
	stats[settlementReceiptPrefix+"sp_gain"] = int64(spGain)
	stats[settlementReceiptPrefix+"new_level"] = int64(newLevel)
	stats[settlementReceiptPrefix+"new_experience"] = int64(newExperience)
}

func settlementReceiptValues(
	character dnfrepo.CharacterRecord,
	skill dnfrepo.SkillRecord,
) (uint32, int, error) {
	stats := character.Stats
	gain := stats[settlementReceiptPrefix+"experience_gain"]
	spGain := stats[settlementReceiptPrefix+"sp_gain"]
	if gain <= 0 || gain > math.MaxUint32 || spGain < 0 || spGain > math.MaxInt ||
		stats[settlementReceiptPrefix+"new_level"] != int64(character.Level) ||
		stats[settlementReceiptPrefix+"new_experience"] != stats["exp"] ||
		skill.Points.SyncedLevel != character.Level {
		return 0, 0, ErrSettlementStateInvalid
	}
	return uint32(gain), int(spGain), nil
}
