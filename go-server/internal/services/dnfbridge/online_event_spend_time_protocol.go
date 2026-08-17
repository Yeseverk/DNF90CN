package dnfbridge

import (
	"fmt"
	"math"
)

const (
	// The legacy command enum names both values from the opposite packet
	// direction. Keep current-EXE notification semantics local to this owner.
	currentSpendTimeEventInfoMsgID uint16 = 108
	currentSpendTimeProgressMsgID  uint16 = 1206
	currentSpendTimeEventID        uint32 = 2347
	currentJoustEventID            uint32 = 2365

	currentSpendTimeDescriptorParamCount = 12
	currentSpendTimeMaxRewardItems       = 10
	currentSpendTimeProgressRawSize      = 0x20
	currentSpendTimeUnusedParam          = math.MaxUint32
	currentSpendTimeBaseCatalogValue     = 1
	currentSpendTimeEventInfoCodec       = "current_op108_spend_time_and_all_day_joust_catalog_zlib"
	currentSpendTimeEventRoute           = "spend time event/0/0"
)

// buildCurrentSpendTimeEventInfoBody matches the current NoPack key-9 op108
// body consumed by sub_20457F0:
//
//	u16 base_catalog_count,
//	base_catalog_count * { u16 event_id, u32 value, dstr title,
//	                          dstr description, u8 has_extended, ... },
//	u8 active_count, active_count * { u16 event_id, 12 * u32 param }
//
// sub_2043290 reads a retired list only when the already-installed base table
// contains special key 9. This catalog contains events 2347 and 2365, neither
// of which is that special key, so a retired-count byte must not be inserted
// before active_count.
//
// The base row is required even though event 2347 owns a fixed client UI.
// sub_2045130 inserts it into the global event table; without that row,
// sub_2045E30 never places 2347 in the active-event map and sub_F3EDF0 hides
// the HUD entry. Event 2347 also requires its exact extended action route.
// sub_2036D50 maps "spend time event" to action 0x6AD, and sub_2042550 marks
// the row clickable and stores its two route arguments. Without that route the
// compact HUD entry is visible, but clicking it cannot open the fixed
// four-stage ItemReward.xui panel.
//
// Event 2347 owns params 0..N-1 as reward item IDs, param 10 as N, and param
// 11 as the total duration of all four stages. Current sub_F3FDA0 treats -1,
// not zero, as the unused reward-slot sentinel.
//
// Event 2365 is also present as a base row and an active row with the current
// EXE's generic twelve-parameter record. It has no schedule extension, which
// makes the joust activity available for the full server uptime instead of
// depending on a historical date window. The remaining indirect sub_20457F0
// finishers consume no bytes for this minimum body.
func buildCurrentSpendTimeEventInfoBody(
	rewardItemIDs []uint32,
	totalStageSeconds uint32,
) ([]byte, error) {
	if len(rewardItemIDs) == 0 || len(rewardItemIDs) > currentSpendTimeMaxRewardItems {
		return nil, fmt.Errorf("current spend-time reward count=%d is outside 1..%d", len(rewardItemIDs), currentSpendTimeMaxRewardItems)
	}
	if totalStageSeconds == 0 {
		return nil, fmt.Errorf("current spend-time total stage seconds is zero")
	}
	params := [currentSpendTimeDescriptorParamCount]uint32{}
	for index := 0; index < currentSpendTimeMaxRewardItems; index++ {
		params[index] = currentSpendTimeUnusedParam
	}
	for index, itemID := range rewardItemIDs {
		if itemID == 0 {
			return nil, fmt.Errorf("current spend-time reward[%d] item id is zero", index)
		}
		params[index] = itemID
	}
	params[10] = uint32(len(rewardItemIDs))
	params[11] = totalStageSeconds

	var writer packetWriter
	writer.writeUint16(2) // base_catalog_count
	writer.writeUint16(uint16(currentSpendTimeEventID))
	writer.writeUint32(currentSpendTimeBaseCatalogValue)
	writer.writeRawDstr(nil) // generic title; the fixed event UI owns visible text
	writer.writeRawDstr(nil) // generic description
	writer.writeByte(1)      // has_extended: installs the native click action below
	writer.writeByte(0)      // generic category flags
	writer.writeByte(0)      // generic presentation flags
	writer.writeRawDstr(nil) // optional title override
	writer.writeRawDstr(nil) // optional subtitle override
	writer.writeRawDstr(nil) // optional description override
	writer.writeUint32(0)    // optional start timestamp
	writer.writeUint32(0)    // optional end timestamp
	writer.writeAsciiDstr(currentSpendTimeEventRoute)
	writer.writeRawDstr(nil) // optional status text
	writer.writeByte(0)      // optional presentation tail
	writer.writeUint16(uint16(currentJoustEventID))
	writer.writeUint32(currentSpendTimeBaseCatalogValue)
	writer.writeRawDstr(nil) // generic title; the fixed joust UI owns visible text
	writer.writeRawDstr(nil) // generic description
	writer.writeByte(0)      // no schedule/action extension: permanently active below
	writer.writeByte(2)      // active_count
	writer.writeUint16(uint16(currentSpendTimeEventID))
	for _, value := range params {
		writer.writeUint32(value)
	}
	writer.writeUint16(uint16(currentJoustEventID))
	for index := 0; index < currentSpendTimeDescriptorParamCount; index++ {
		writer.writeUint32(0)
	}
	return writer.bytes(), nil
}

// buildCurrentSpendTimeProgressBody matches current NoPack op1206 dispatch:
//
//	u32 event_id, u8 wide_flag, raw[wide_flag ? 0xA0 : 0x20]
//
// Event 2347 uses the narrow record. Its first two u32 values are authoritative
// elapsed seconds and the number of automatically completed reward stages.
func buildCurrentSpendTimeProgressBody(elapsedSeconds uint64, completedStages uint32) ([]byte, error) {
	if elapsedSeconds > math.MaxUint32 {
		return nil, fmt.Errorf("current spend-time elapsed seconds=%d overflows u32", elapsedSeconds)
	}
	var writer packetWriter
	writer.writeUint32(currentSpendTimeEventID)
	writer.writeByte(0) // narrow 0x20-byte record
	writer.writeUint32(uint32(elapsedSeconds))
	writer.writeUint32(completedStages)
	writer.writeBytes(make([]byte, currentSpendTimeProgressRawSize-8))
	return writer.bytes(), nil
}
