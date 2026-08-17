package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

const (
	currentBoosterSelectionBodySize = 10
	currentBoosterRandomBodySize    = 2
	currentBoosterMultiBodyBaseSize = 6
	currentBoosterMultiEntrySize    = 9
	currentBoosterMaxRewardRows     = 128
	currentBoosterMaxRewardUnits    = 1_000_000
	currentBoosterPetArtifactStart  = int16(140)
	currentBoosterPetArtifactEnd    = int16(188)
)

var (
	errCurrentBoosterRequestMalformed = errors.New("dnf booster request is malformed")
	errCurrentBoosterOwnerUnavailable = errors.New("dnf booster selected character is unavailable")
	errCurrentBoosterSourceMissing    = errors.New("dnf booster source item is unavailable")
	errCurrentBoosterPVFInvalid       = errors.New("dnf booster runtime PVF data is invalid")
	errCurrentBoosterSelectionInvalid = errors.New("dnf booster selection is invalid")
	errCurrentBoosterExpired          = errors.New("dnf booster source item has expired")
)

type currentBoosterRequestKind byte

const (
	currentBoosterRequestRandom currentBoosterRequestKind = iota
	currentBoosterRequestSelection
)

// currentBoosterSelectionRequest is one PVF-backed item/attribute pair written
// by the current NoPack op160 selection UI.
type currentBoosterSelectionRequest struct {
	ItemID uint32
	Option byte
}

// currentBoosterOpenRequest follows the current NoPack op160 writers seen on
// the live connection. Plain [booster] sends only u16 sourceSlot. A one-choice
// [booster selection] sends u16 slot, u16 category context, u32 selected item,
// u8 option and one trailing UI byte. A selection_num=0 category sends every
// category item twice: first N*u32 item IDs, then u8 N and N*{u32 item,u8
// option}, followed by one trailing UI byte.
type currentBoosterOpenRequest struct {
	Kind                currentBoosterRequestKind
	SourceSlot          int16
	SelectionContext    uint16
	Selections          []currentBoosterSelectionRequest
	SelectedItemID      uint32
	SelectionOption     byte
	TrailingUIByte      byte
	RewardMultiplier    uint32
	ConsumePremiumDaily bool
	PremiumDailySlot    int64
}

type currentBoosterSelectionCandidate struct {
	ItemID       uint32
	Count        uint32
	CategoryKind int64
	Option       byte
}

type currentBoosterRewardPlacement byte

const (
	currentBoosterRewardMain currentBoosterRewardPlacement = iota
	currentBoosterRewardAvatar
	currentBoosterRewardPetBody
	currentBoosterRewardPetArtifact
	currentBoosterRewardPetConsumable
)

type currentBoosterReward struct {
	Definition dungeonDropItemDefinition
	Count      uint32
	Option     byte
	Seal       bool
	Placement  currentBoosterRewardPlacement
}

type currentBoosterGrantedReward struct {
	ItemID uint32
	Count  uint32
}

type currentBoosterDefinition struct {
	Source            dungeonDropItemDefinition
	StackableType     string
	SelectionRequired int
	Selection         []currentBoosterSelectionCandidate
	SelectionCategory map[uint16][]currentBoosterSelectionCandidate
	MaterialItemID    int64
	MaterialCount     int64
	Random            alignedcmd.MagicBoxResolution
}

type currentBoosterCommitResult struct {
	SourceItemID    uint32
	SourceSlot      int16
	SourceRemaining uint32
	Rewards         []currentBoosterGrantedReward
	ChangedLists    []byte
}

func parseCurrentBoosterOpenRequest(body []byte) (currentBoosterOpenRequest, error) {
	switch len(body) {
	case currentBoosterRandomBodySize:
		return currentBoosterOpenRequest{
			Kind:       currentBoosterRequestRandom,
			SourceSlot: int16(binary.LittleEndian.Uint16(body[0:2])),
		}, nil
	case currentBoosterSelectionBodySize:
		selectedItemID := binary.LittleEndian.Uint32(body[4:8])
		if selectedItemID == 0 {
			return currentBoosterOpenRequest{}, errCurrentBoosterRequestMalformed
		}
		return currentBoosterOpenRequest{
			Kind:             currentBoosterRequestSelection,
			SourceSlot:       int16(binary.LittleEndian.Uint16(body[0:2])),
			SelectionContext: binary.LittleEndian.Uint16(body[2:4]),
			Selections: []currentBoosterSelectionRequest{{
				ItemID: selectedItemID,
				Option: body[8],
			}},
			SelectedItemID:  selectedItemID,
			SelectionOption: body[8],
			TrailingUIByte:  body[9],
		}, nil
	default:
		return parseCurrentBoosterMultiSelectionRequest(body)
	}
}

func parseCurrentBoosterMultiSelectionRequest(body []byte) (currentBoosterOpenRequest, error) {
	if len(body) <= currentBoosterSelectionBodySize ||
		len(body) < currentBoosterMultiBodyBaseSize+currentBoosterMultiEntrySize ||
		(len(body)-currentBoosterMultiBodyBaseSize)%currentBoosterMultiEntrySize != 0 {
		return currentBoosterOpenRequest{}, fmt.Errorf(
			"%w: body_len=%d want=2_10_or_6_plus_9n",
			errCurrentBoosterRequestMalformed,
			len(body),
		)
	}
	selectionCount := (len(body) - currentBoosterMultiBodyBaseSize) / currentBoosterMultiEntrySize
	if selectionCount < 1 || selectionCount > currentBoosterMaxRewardRows || selectionCount > math.MaxUint8 {
		return currentBoosterOpenRequest{}, fmt.Errorf("%w: selection_count=%d", errCurrentBoosterRequestMalformed, selectionCount)
	}
	countOffset := 4 + selectionCount*4
	if int(body[countOffset]) != selectionCount {
		return currentBoosterOpenRequest{}, fmt.Errorf(
			"%w: selection_count_byte=%d want=%d",
			errCurrentBoosterRequestMalformed,
			body[countOffset],
			selectionCount,
		)
	}
	recordOffset := countOffset + 1
	selections := make([]currentBoosterSelectionRequest, 0, selectionCount)
	seen := make(map[uint32]struct{}, selectionCount)
	for index := 0; index < selectionCount; index++ {
		prefixItemID := binary.LittleEndian.Uint32(body[4+index*4 : 8+index*4])
		entryOffset := recordOffset + index*5
		recordItemID := binary.LittleEndian.Uint32(body[entryOffset : entryOffset+4])
		if prefixItemID == 0 || recordItemID != prefixItemID {
			return currentBoosterOpenRequest{}, fmt.Errorf(
				"%w: selection_index=%d prefix_item=%d record_item=%d",
				errCurrentBoosterRequestMalformed,
				index,
				prefixItemID,
				recordItemID,
			)
		}
		if _, duplicate := seen[recordItemID]; duplicate {
			return currentBoosterOpenRequest{}, fmt.Errorf("%w: duplicate_item=%d", errCurrentBoosterRequestMalformed, recordItemID)
		}
		seen[recordItemID] = struct{}{}
		selections = append(selections, currentBoosterSelectionRequest{
			ItemID: recordItemID,
			Option: body[entryOffset+4],
		})
	}
	return currentBoosterOpenRequest{
		Kind:             currentBoosterRequestSelection,
		SourceSlot:       int16(binary.LittleEndian.Uint16(body[0:2])),
		SelectionContext: binary.LittleEndian.Uint16(body[2:4]),
		Selections:       selections,
		SelectedItemID:   selections[0].ItemID,
		SelectionOption:  selections[0].Option,
		TrailingUIByte:   body[len(body)-1],
	}, nil
}

// buildCurrentBoosterSuccessBody matches current NoPack sub_1D2BFF0 after
// the shared success byte: u32 source item, u16 source slot, u32 remaining,
// u32 special-result mode (zero for ordinary boxes), u16 reward count, then
// rewardCount * {u32 item, u32 display count}.
func buildCurrentBoosterSuccessBody(result currentBoosterCommitResult) []byte {
	var writer packetWriter
	writer.writeUint32(result.SourceItemID)
	writer.writeUint16(uint16(result.SourceSlot))
	writer.writeUint32(result.SourceRemaining)
	writer.writeUint32(0)
	writer.writeUint16(uint16(len(result.Rewards)))
	for _, reward := range result.Rewards {
		writer.writeUint32(reward.ItemID)
		writer.writeUint32(reward.Count)
	}
	return writer.bytes()
}
