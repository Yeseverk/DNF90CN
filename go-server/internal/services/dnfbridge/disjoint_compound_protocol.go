package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentDisjointItemRequestSize       = 5
	currentAvatarDisjointRequestMinSize  = 2
	currentAvatarDisjointRequestFullSize = 6
	currentAvatarInventoryListType       = 1
	currentEmblemCompoundMinInputs       = 2
	currentEmblemCompoundMaxInputs       = 5
	currentEmblemCompoundInputSize       = 6
	currentDisjointMaxRewards            = 32
)

var (
	errCurrentDisjointRequestInvalid = errors.New("dnf current disjoint request is invalid")
	errCurrentDisjointUnavailable    = errors.New("dnf current disjoint source or PVF mapping is unavailable")
	errCurrentDisjointSourceInvalid  = errors.New("dnf current disjoint source item is invalid")
	errCurrentDisjointRewardInvalid  = errors.New("dnf current disjoint reward mapping is invalid")
)

// currentDisjointItemRequest is the exact semantic body written by the
// current client's op26 sender: target slot, item-space, portable-disjoint
// slot, and (on some UI paths) a u32 context.  The reply reader consumes the
// first three fields again before it reads result rows.
type currentDisjointItemRequest struct {
	SourceSlot int16
	ListType   byte
	ToolSlot   int16
	Context    uint32
}

type currentAvatarDisjointRequest struct {
	SourceSlot     int16
	ExpectedItemID uint32
}

type currentEmblemCompoundInput struct {
	ItemID uint32
	Slot   int16
}

type currentEmblemCompoundRequest struct {
	Inputs []currentEmblemCompoundInput
}

type currentDisjointReward struct {
	ItemID uint32
	Count  uint32
}

type currentDisjointRewardSlot struct {
	Slot    int16
	ItemID  uint32
	Count   uint32 // final stack amount, which is what the current readers apply.
	Granted uint32
}

type currentDisjointResult struct {
	Rewards []currentDisjointRewardSlot
	Updates []currentItemListEntry
}

type currentDisjointAdditionalConst struct {
	GreatDivisor  float64
	NormalDivisor float64
	GreatChance   float64
}

type currentDisjointExpand struct {
	Enabled      bool
	ItemID       uint32
	LevelDivisor float64
	GreatChance  float64
	NormalChance float64
}

type currentDisjointPVFConfig struct {
	CubeItemID         uint32
	CubeBase           float64
	CubeMultipliers    []float64
	Additional         [][]uint32
	AdditionalConsts   []currentDisjointAdditionalConst
	Expands            []currentDisjointExpand
	AvatarByJob        map[string][]uint32             // legacy disjoint.etc [avatar disjoint info], kept for config validation
	AvatarEmblemTables []currentAvatarEmblemGradeTable // etc/avatardisjoint/emblemlistinfo_<grade>.etc
	EmblemBoosters     map[[2]int]uint32               // (emblem grade,input count) -> booster item
}

// currentAvatarEmblemGradeTable is one grade's avatar disjoint reward table
// (86JP AvatarDisjointConfigProvider grammar): each [result info] section is
// one pool whose first value is the pick count, followed by
// item/weight/count/special 4-tuples.
type currentAvatarEmblemGradeTable struct {
	Pools []currentAvatarEmblemPool
}

type currentAvatarEmblemPool struct {
	PickCount int
	Rewards   []currentAvatarEmblemReward
}

type currentAvatarEmblemReward struct {
	ItemID  uint32
	Weight  uint32
	Count   uint32
	Special bool
}

func parseCurrentDisjointItemRequest(body []byte) (currentDisjointItemRequest, error) {
	if len(body) != currentDisjointItemRequestSize && len(body) != currentDisjointItemRequestSize+4 {
		return currentDisjointItemRequest{}, fmt.Errorf("%w: op26 body=%d", errCurrentDisjointRequestInvalid, len(body))
	}
	request := currentDisjointItemRequest{
		SourceSlot: int16(binary.LittleEndian.Uint16(body[0:2])),
		ListType:   body[2],
		ToolSlot:   int16(binary.LittleEndian.Uint16(body[3:5])),
	}
	if len(body) == currentDisjointItemRequestSize+4 {
		request.Context = binary.LittleEndian.Uint32(body[5:9])
	}
	if request.ListType != dnfrepo.MainInventoryListType || request.SourceSlot <= 0 ||
		dnfrepo.IsAccountSharedInventorySlot(request.ListType, request.SourceSlot) {
		return currentDisjointItemRequest{}, fmt.Errorf("%w: op26 list=%d slot=%d", errCurrentDisjointRequestInvalid, request.ListType, request.SourceSlot)
	}
	return request, nil
}

func parseCurrentAvatarDisjointRequest(body []byte) (currentAvatarDisjointRequest, error) {
	if len(body) != currentAvatarDisjointRequestMinSize && len(body) != currentAvatarDisjointRequestFullSize {
		return currentAvatarDisjointRequest{}, fmt.Errorf("%w: op202 body=%d", errCurrentDisjointRequestInvalid, len(body))
	}
	request := currentAvatarDisjointRequest{SourceSlot: int16(binary.LittleEndian.Uint16(body[0:2]))}
	if len(body) == currentAvatarDisjointRequestFullSize {
		request.ExpectedItemID = binary.LittleEndian.Uint32(body[2:6])
	}
	if request.SourceSlot < 0 {
		return currentAvatarDisjointRequest{}, fmt.Errorf("%w: op202 slot=%d", errCurrentDisjointRequestInvalid, request.SourceSlot)
	}
	return request, nil
}

func parseCurrentEmblemCompoundRequest(body []byte) (currentEmblemCompoundRequest, error) {
	if len(body) < 1 {
		return currentEmblemCompoundRequest{}, errCurrentDisjointRequestInvalid
	}
	count := int(body[0])
	if count < currentEmblemCompoundMinInputs || count > currentEmblemCompoundMaxInputs || len(body) != 1+count*currentEmblemCompoundInputSize {
		return currentEmblemCompoundRequest{}, fmt.Errorf("%w: op256 count=%d body=%d", errCurrentDisjointRequestInvalid, count, len(body))
	}
	request := currentEmblemCompoundRequest{Inputs: make([]currentEmblemCompoundInput, 0, count)}
	for index := 0; index < count; index++ {
		offset := 1 + index*currentEmblemCompoundInputSize
		input := currentEmblemCompoundInput{ItemID: binary.LittleEndian.Uint32(body[offset : offset+4]), Slot: int16(binary.LittleEndian.Uint16(body[offset+4 : offset+6]))}
		if input.ItemID == 0 || input.Slot <= 0 || dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, input.Slot) {
			return currentEmblemCompoundRequest{}, fmt.Errorf("%w: op256 input=%d item=%d slot=%d", errCurrentDisjointRequestInvalid, index, input.ItemID, input.Slot)
		}
		request.Inputs = append(request.Inputs, input)
	}
	return request, nil
}

func buildCurrentDisjointItemSuccessBody(request currentDisjointItemRequest, rewards []currentDisjointRewardSlot) []byte {
	var body packetWriter
	body.writeByte(1)
	body.writeUint16(uint16(request.SourceSlot))
	body.writeByte(request.ListType)
	body.writeUint16(uint16(request.ToolSlot))
	body.writeByte(byte(len(rewards)))
	for _, reward := range rewards {
		body.writeUint16(uint16(reward.Slot))
		body.writeUint32(reward.ItemID)
		body.writeUint32(reward.Granted)
	}
	return body.bytes()
}

func buildCurrentAvatarDisjointSuccessBody(request currentAvatarDisjointRequest, rewards []currentDisjointRewardSlot) []byte {
	var body packetWriter
	body.writeByte(1)
	body.writeUint16(uint16(request.SourceSlot))
	body.writeUint16(uint16(len(rewards)))
	for _, reward := range rewards {
		body.writeUint16(uint16(reward.Slot))
		body.writeUint32(reward.ItemID)
		body.writeUint32(reward.Count)
	}
	return body.bytes()
}

func buildCurrentEmblemCompoundSuccessBody(reward currentDisjointRewardSlot) []byte {
	var body packetWriter
	body.writeByte(1)
	body.writeByte(1)
	body.writeUint32(reward.ItemID)
	body.writeUint32(reward.Granted)
	return body.bytes()
}
