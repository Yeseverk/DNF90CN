package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	currentRentalRequestWireSize = 21
	currentRentalStateMsgID      = 985
)

type currentRentEquipmentRequest struct {
	ItemID        uint32
	PackedJobTier uint32
}

type currentChargeRentalPointRequest struct {
	ChargeType uint32
	Count      uint32
}

type currentRentalActiveEntry struct {
	ItemID     uint32
	ExpireUnix uint32
}

func decodeCurrentRentEquipmentRequest(body []byte) (currentRentEquipmentRequest, error) {
	if len(body) != currentRentalRequestWireSize {
		return currentRentEquipmentRequest{}, fmt.Errorf("rent equipment body length=%d want=%d", len(body), currentRentalRequestWireSize)
	}
	request := currentRentEquipmentRequest{
		ItemID:        binary.LittleEndian.Uint32(body[13:17]),
		PackedJobTier: binary.LittleEndian.Uint32(body[17:21]),
	}
	if request.ItemID == 0 {
		return currentRentEquipmentRequest{}, errors.New("rent equipment item id is zero")
	}
	return request, nil
}

func decodeCurrentChargeRentalPointRequest(body []byte) (currentChargeRentalPointRequest, error) {
	if len(body) != currentRentalRequestWireSize {
		return currentChargeRentalPointRequest{}, fmt.Errorf("charge rental point body length=%d want=%d", len(body), currentRentalRequestWireSize)
	}
	request := currentChargeRentalPointRequest{
		ChargeType: binary.LittleEndian.Uint32(body[13:17]),
		Count:      binary.LittleEndian.Uint32(body[17:21]),
	}
	if request.ChargeType != 1 || request.Count == 0 {
		return currentChargeRentalPointRequest{}, fmt.Errorf("unsupported rental point charge type=%d count=%d", request.ChargeType, request.Count)
	}
	return request, nil
}

func (s *Service) sendCurrentRentEquipmentResult(session *gameSession, result uint32) error {
	var body packetWriter
	body.writeUint32(result)
	return s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketRentEquipmentItem), body.bytes())
}

func buildCurrentRentalPointStateBody(points uint32, active []currentRentalActiveEntry) []byte {
	var body packetWriter
	body.writeUint32(points)
	body.writeUint32(uint32(len(active)))
	for _, entry := range active {
		body.writeUint32(entry.ItemID)
		body.writeUint32(entry.ExpireUnix)
	}
	return body.bytes()
}
