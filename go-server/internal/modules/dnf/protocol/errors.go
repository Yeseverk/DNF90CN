package protocol

import "errors"

var (
	ErrPacketTooShort  = errors.New("dnf packet too short")
	ErrPacketLength    = errors.New("dnf packet length invalid")
	ErrPacketTooLarge  = errors.New("dnf packet too large")
	ErrChecksumInvalid = errors.New("dnf packet checksum invalid")
	ErrInnerKind       = errors.New("dnf latest game inner kind invalid")
)
