package protocol

import (
	"encoding/binary"
	"errors"
)

const (
	DprotoServerControlOpcode  = uint16(1466)
	DprotoServerEnvelopeOpcode = uint16(1467)
	DprotoClientEnvelopeOpcode = uint16(1517)
	DprotoCallbackOpcode       = uint16(1518)
)

var (
	ErrDprotoEnvelopeOpcode = errors.New("dnf dproto envelope opcode is invalid")
	ErrDprotoEnvelopeBody   = errors.New("dnf dproto envelope body is invalid")
	ErrDprotoProtectedSize  = errors.New("dnf dproto protected payload length is invalid")
)

// DprotoClientEnvelope is the verified C2S class1/op1517 outer packet. The
// protected bytes remain opaque until a real server-role provider consumes
// the complete Raw packet and its per-connection session state.
type DprotoClientEnvelope struct {
	Header    ChannelHeader
	Protected []byte
	Raw       []byte
}

// ParseDprotoClientEnvelope verifies the ordinary outer upper checksum and
// the u32 protected-length field observed at body offset zero.
func ParseDprotoClientEnvelope(raw []byte, maxProtectedBytes int) (DprotoClientEnvelope, error) {
	packet, err := ParseChannelPacket(raw)
	if err != nil {
		return DprotoClientEnvelope{}, err
	}
	if packet.Header.Classification != DefaultChannelClassification ||
		packet.Header.MsgID != DprotoClientEnvelopeOpcode {
		return DprotoClientEnvelope{}, ErrDprotoEnvelopeOpcode
	}
	if len(packet.Body) < 4 {
		return DprotoClientEnvelope{}, ErrDprotoEnvelopeBody
	}
	protectedLength := int(binary.LittleEndian.Uint32(packet.Body[:4]))
	if protectedLength <= 0 || protectedLength != len(packet.Body)-4 {
		return DprotoClientEnvelope{}, ErrDprotoProtectedSize
	}
	if maxProtectedBytes > 0 && protectedLength > maxProtectedBytes {
		return DprotoClientEnvelope{}, ErrPacketTooLarge
	}
	return DprotoClientEnvelope{
		Header:    packet.Header,
		Protected: cloneBytes(packet.Body[4:]),
		Raw:       cloneBytes(raw),
	}, nil
}

// BuildDprotoClientEnvelope exists for provider integration tests and native
// transport fixtures. Production C2S envelopes are built by the client.
func BuildDprotoClientEnvelope(protected []byte, seq uint16) ([]byte, error) {
	if len(protected) == 0 {
		return nil, ErrDprotoProtectedSize
	}
	body := make([]byte, 4+len(protected))
	binary.LittleEndian.PutUint32(body[:4], uint32(len(protected)))
	copy(body[4:], protected)
	return BuildChannelPacket(DprotoClientEnvelopeOpcode, body, seq, DefaultChannelClassification)
}
