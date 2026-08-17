package inventory

import "fmt"

const (
	investAmplifyActionInvest   byte = 0
	investAmplifyActionTwist    byte = 1
	investAmplifyActionPureGold byte = 2
)

// PurifyItemRequest is the exact 12-byte current-NoPack class1/op204 request.
type PurifyItemRequest struct {
	TargetSlotIndex        int16
	TargetItemTemplateID   int32
	MaterialSlotIndex      int16
	MaterialItemTemplateID int32
}

// InvestItemAmplifyOptionRequest is the current-NoPack class1/op205 request.
// Action 2 appends one i32-length ASCII DSTR after the 14-byte fixed prefix.
type InvestItemAmplifyOptionRequest struct {
	Action                 byte
	TargetSlotIndex        int16
	TargetItemTemplateID   int32
	MaterialSlotIndex      int16
	MaterialItemTemplateID int32
	SelectedOption         byte
	TargetItemName         string
}

func DecodePurifyItemRequest(body []byte) (PurifyItemRequest, error) {
	const size = 12
	if len(body) != size {
		return PurifyItemRequest{}, fmt.Errorf("purify item body length %d, want %d", len(body), size)
	}
	request := PurifyItemRequest{
		TargetSlotIndex:        readI16(body, 0),
		TargetItemTemplateID:   readI32(body, 2),
		MaterialSlotIndex:      readI16(body, 6),
		MaterialItemTemplateID: readI32(body, 8),
	}
	if request.TargetSlotIndex < 0 || request.MaterialSlotIndex < 0 || request.TargetItemTemplateID <= 0 || request.MaterialItemTemplateID <= 0 {
		return PurifyItemRequest{}, fmt.Errorf("purify item identity invalid: target=(%d,%d) material=(%d,%d)", request.TargetSlotIndex, request.TargetItemTemplateID, request.MaterialSlotIndex, request.MaterialItemTemplateID)
	}
	return request, nil
}

func DecodeInvestItemAmplifyOptionRequest(body []byte) (InvestItemAmplifyOptionRequest, error) {
	const fixedSize = 14
	if len(body) < fixedSize {
		return InvestItemAmplifyOptionRequest{}, shortBodyError(len(body), fixedSize)
	}
	request := InvestItemAmplifyOptionRequest{
		Action:                 body[0],
		TargetSlotIndex:        readI16(body, 1),
		TargetItemTemplateID:   readI32(body, 3),
		MaterialSlotIndex:      readI16(body, 7),
		MaterialItemTemplateID: readI32(body, 9),
		SelectedOption:         body[13],
	}
	if request.Action > investAmplifyActionPureGold {
		return InvestItemAmplifyOptionRequest{}, fmt.Errorf("invest amplify action %d invalid", request.Action)
	}
	if request.TargetSlotIndex < 0 || request.MaterialSlotIndex < 0 || request.TargetItemTemplateID <= 0 || request.MaterialItemTemplateID <= 0 {
		return InvestItemAmplifyOptionRequest{}, fmt.Errorf("invest amplify identity invalid: target=(%d,%d) material=(%d,%d)", request.TargetSlotIndex, request.TargetItemTemplateID, request.MaterialSlotIndex, request.MaterialItemTemplateID)
	}
	if request.Action != investAmplifyActionPureGold {
		if len(body) != fixedSize {
			return InvestItemAmplifyOptionRequest{}, fmt.Errorf("invest amplify action %d body length %d, want %d", request.Action, len(body), fixedSize)
		}
		return request, nil
	}
	if len(body) < fixedSize+4 {
		return InvestItemAmplifyOptionRequest{}, shortBodyError(len(body), fixedSize+4)
	}
	nameLength := readI32(body, fixedSize)
	if nameLength < 0 {
		return InvestItemAmplifyOptionRequest{}, fmt.Errorf("invest amplify target name length %d invalid", nameLength)
	}
	end := fixedSize + 4 + int(nameLength)
	if end < fixedSize+4 || len(body) != end {
		return InvestItemAmplifyOptionRequest{}, fmt.Errorf("invest amplify action 2 body length %d, want %d", len(body), end)
	}
	request.TargetItemName = string(body[fixedSize+4 : end])
	return request, nil
}
