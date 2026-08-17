package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentAvatarEmblemAttachOpcode uint16 = uint16(dnfenum.CmdPacketUseEmblem)
	currentAvatarSocketOpenOpcode   uint16 = uint16(dnfenum.CmdPacketAddAvatarSocket)

	// Current NoPack sends ordinary-equipment emblem attachment on class1/op913
	// with target slot/template followed by the selected emblem rows.
	currentEquipmentEmblemAttachOpcode uint16 = uint16(dnfenum.CmdPacketUseEmblemForEquipment)

	// Current NoPack sub_156ED20 writes class1/op796 with zero business bytes.
	// It is a no-state control/ack path; it is not a closed equipment-emblem
	// mutation request.
	currentNoBody796Opcode uint16 = 0x031C

	// Current NoPack C2S writer sub_2112D50 sends the equipment socket opener
	// on class1/op914 (CmdPacketAddEquipmentSocket), not the old op797 route.
	currentEquipmentSocketOpenOpcode uint16 = uint16(dnfenum.CmdPacketAddEquipmentSocket)

	currentSocketListMain      byte = dnfrepo.MainInventoryListType
	currentSocketListAvatar    byte = 1
	currentSocketListEquipment byte = 3

	// Current NoPack sub_225CD00 extracts the ordinary-equipment emblem vector
	// from raw+0x3C: one count byte followed by two contiguous u32 emblem IDs.
	// sub_22D2280 then installs every nonzero entry in the equipment wrapper.
	currentEquipmentVectorOffset = 0x3C

	// An earlier server projection incorrectly treated the unrelated
	// sub_21792A0 record vector at raw+0x27..+0x37 as emblem state. Keep this
	// offset only for the exact, bounded migration of rows written by that
	// interim build.
	currentLegacyWrongEquipmentVectorOffset = 0x27

	currentEquipmentLegacyEmblemDataOffset = 0x2F
	currentEquipmentEmblemDataBytes        = 9
	currentEquipmentTailDataBytes          = 37
	currentAvatarSocketBytes               = 30
	currentAvatarSocketCount               = 5
	currentAvatarSocketStride              = 6

	currentEquipmentSocketOpenToolPVFPath = "stackable/emblem/chn_makesocket.stk"
)

var currentEquipmentEmblemDataExtraKeys = []string{
	"equipment_emblem_data",
	"equipment_socket_data",
	"current_equipment_emblem_data",
	"current_equipment_socket_data",
}

var currentEquipmentTailDataExtraKeys = []string{"tail_data_2f", "tailData2F", "tail2f", "raw_data_2f"}

var (
	errCurrentSocketSelectedCharacterMissing = errors.New("current socket selected character is missing")
	errCurrentSocketRepositoryMissing        = errors.New("current socket repository is missing")
	errCurrentSocketTransactionMissing       = errors.New("current socket item transaction is missing")
	errCurrentSocketInventoryMissing         = errors.New("current socket inventory is missing")
	errCurrentSocketTargetMissing            = errors.New("current socket target item is missing")
	errCurrentSocketTargetKindMismatch       = errors.New("current socket target item kind does not match socket operation")
	errCurrentSocketMaterialMissing          = errors.New("current socket material item is missing")
	errCurrentSocketMaterialInvalid          = errors.New("current socket material is not an equipment socket opener")
	errCurrentSocketTargetAlreadyOpen        = errors.New("current socket target already has open sockets")
	errCurrentSocketTargetAmbiguous          = errors.New("current socket target item is ambiguous")
	errCurrentSocketNoOpenSockets            = errors.New("current socket target has no open sockets")
	errCurrentSocketIndexInvalid             = errors.New("current socket index is invalid")
	errCurrentSocketEmblemMissing            = errors.New("current socket emblem item is missing")
	errCurrentSocketPVFInvalid               = errors.New("current socket PVF metadata is unavailable")
	errCurrentSocketTypeMismatch             = errors.New("current socket type mismatch")

	currentAvatarSocketRE = regexp.MustCompile(`(?i)\[\s*([ABCDSM])\s+socket\s*\]`)
)

type currentSocketOpenRequest struct {
	TargetSlot   int16
	TargetItemID int64
	MaterialSlot int16
}

type currentEmblemAttachRequest struct {
	TargetSlot   int16
	TargetItemID int64
	Emblems      []currentEmblemApplyRequest
}

type currentEmblemApplyRequest struct {
	EmblemSlot   int16
	EmblemItemID int64
	SocketIndex  byte
}

type currentSocketChangedSlot struct {
	ListType byte
	Slot     int16
}

type currentSocketMutationResult struct {
	Target         currentSocketChangedSlot
	TargetEquipped bool
	Consumed       []currentSocketChangedSlot
}

func decodeCurrentSocketOpenRequest(body []byte) (currentSocketOpenRequest, error) {
	if len(body) < 8 {
		return currentSocketOpenRequest{}, fmt.Errorf("socket-open body too short: %d", len(body))
	}
	return currentSocketOpenRequest{
		TargetSlot:   int16(binary.LittleEndian.Uint16(body[0:2])),
		TargetItemID: int64(binary.LittleEndian.Uint32(body[2:6])),
		MaterialSlot: int16(binary.LittleEndian.Uint16(body[6:8])),
	}, nil
}

func decodeCurrentEmblemAttachRequest(body []byte) (currentEmblemAttachRequest, error) {
	return decodeCurrentEmblemAttachRequestAt(body, 0)
}

func decodeCurrentAvatarEmblemAttachRequest(body []byte) (currentEmblemAttachRequest, error) {
	if len(body) >= 8 && body[0] == currentSocketListAvatar {
		return decodeCurrentEmblemAttachRequestAt(body, 1)
	}
	return decodeCurrentEmblemAttachRequest(body)
}

func decodeCurrentEmblemAttachRequestAt(body []byte, offset int) (currentEmblemAttachRequest, error) {
	if offset < 0 || len(body) < offset+7 {
		return currentEmblemAttachRequest{}, fmt.Errorf("emblem body too short: len=%d offset=%d", len(body), offset)
	}
	count := int(body[offset+6])
	end := offset + 7 + count*7
	if len(body) < end {
		return currentEmblemAttachRequest{}, fmt.Errorf("emblem body incomplete: len=%d want=%d count=%d", len(body), end, count)
	}
	request := currentEmblemAttachRequest{
		TargetSlot:   int16(binary.LittleEndian.Uint16(body[offset : offset+2])),
		TargetItemID: int64(binary.LittleEndian.Uint32(body[offset+2 : offset+6])),
		Emblems:      make([]currentEmblemApplyRequest, 0, count),
	}
	pos := offset + 7
	for index := 0; index < count; index++ {
		request.Emblems = append(request.Emblems, currentEmblemApplyRequest{
			EmblemSlot:   int16(binary.LittleEndian.Uint16(body[pos : pos+2])),
			EmblemItemID: int64(binary.LittleEndian.Uint32(body[pos+2 : pos+6])),
			SocketIndex:  body[pos+6],
		})
		pos += 7
	}
	return request, nil
}

func stripLegacySocketOpenTransportTrailer(body []byte) []byte {
	return stripLegacyTransportTrailer(body, 8)
}

func stripLegacyNoBody796TransportTrailer(body []byte) []byte {
	if len(body) == 0 || len(body) == 4 {
		return nil
	}
	return body
}

func stripLegacyEmblemAttachTransportTrailer(body []byte) []byte {
	if semantic, ok := currentEmblemAttachSemanticLength(body, 0); ok {
		return stripLegacyTransportTrailer(body, semantic)
	}
	if len(body) >= 1 && body[0] == currentSocketListAvatar {
		if semantic, ok := currentEmblemAttachSemanticLength(body, 1); ok {
			return stripLegacyTransportTrailer(body, semantic)
		}
	}
	return body
}

func currentEmblemAttachSemanticLength(body []byte, offset int) (int, bool) {
	if offset < 0 || len(body) < offset+7 {
		return 0, false
	}
	count := int(body[offset+6])
	semantic := offset + 7 + count*7
	if len(body) == semantic || len(body) == semantic+4 {
		return semantic, true
	}
	return 0, false
}

func buildCurrentSocketOpenAckBody(request currentSocketOpenRequest) []byte {
	var writer packetWriter
	writer.writeUint16(uint16(request.TargetSlot))
	writer.writeUint32(uint32(request.TargetItemID))
	writer.writeUint16(uint16(request.MaterialSlot))
	return writer.bytes()
}

func buildCurrentEquipmentSocketOpenAckBody(request currentSocketOpenRequest) []byte {
	var writer packetWriter
	writer.writeUint16(uint16(request.TargetSlot))
	writer.writeUint16(uint16(request.MaterialSlot))
	return writer.bytes()
}

// Current NoPack sub_1D24BD0 consumes no op796 business bytes after the shared
// success envelope.
func buildCurrentNoBody796AckBody() []byte {
	return nil
}

// Current NoPack op913 success handler sub_1D20750 reads one u8 row count,
// followed by [u16 emblem slot][u32 emblem template] for every consumed row.
func buildCurrentEquipmentEmblemAttachAckBody(request currentEmblemAttachRequest) []byte {
	var writer packetWriter
	count := len(request.Emblems)
	if count > math.MaxUint8 {
		count = math.MaxUint8
	}
	writer.writeByte(byte(count))
	for _, emblem := range request.Emblems[:count] {
		writer.writeUint16(uint16(emblem.EmblemSlot))
		writer.writeUint32(uint32(emblem.EmblemItemID))
	}
	return writer.bytes()
}

func buildCurrentAvatarEmblemAttachAckBody(request currentEmblemAttachRequest) []byte {
	var writer packetWriter
	writer.writeUint16(uint16(request.TargetSlot))
	writer.writeUint32(uint32(request.TargetItemID))
	if len(request.Emblems) > math.MaxUint8 {
		writer.writeByte(math.MaxUint8)
	} else {
		writer.writeByte(byte(len(request.Emblems)))
	}
	return writer.bytes()
}

func (s *Service) sendCurrentSocketParseFailure(session *gameSession, opcode uint16) error {
	return s.sendGameUpperRawClass(session, opcode, []byte{0}, dnfproto.DefaultChannelClassification)
}
