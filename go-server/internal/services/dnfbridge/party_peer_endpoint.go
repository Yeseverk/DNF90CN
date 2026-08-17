package dnfbridge

import (
	"encoding/binary"
	"fmt"
)

const currentPartyPeerRegistrationBodySize = 31

const (
	currentPartyPeerMinimumMTU = 576
	currentPartyPeerMaximumMTU = 9000
)

// currentPartyPeerEndpointRegistration is the current client's CS op2
// SET_UDP_IP_PORT report. The first byte is the client character attribute,
// followed by inner/outer IPv4, a network-order UDP port, little-endian MTU,
// and a length-prefixed machine token. Only the endpoint fields are retained;
// the token is deliberately ignored.
type currentPartyPeerEndpointRegistration struct {
	CharacterAttribute byte
	InnerIPv4          [4]byte
	OuterIPv4          [4]byte
	Port               uint16
	MTU                uint32
}

func parseCurrentPartyPeerEndpointRegistration(body []byte) (currentPartyPeerEndpointRegistration, error) {
	if len(body) != currentPartyPeerRegistrationBodySize {
		return currentPartyPeerEndpointRegistration{}, fmt.Errorf(
			"current party peer registration body length %d, want %d",
			len(body),
			currentPartyPeerRegistrationBodySize,
		)
	}
	registration := currentPartyPeerEndpointRegistration{
		CharacterAttribute: body[0],
		Port:               binary.BigEndian.Uint16(body[9:11]),
		MTU:                binary.LittleEndian.Uint32(body[11:15]),
	}
	copy(registration.InnerIPv4[:], body[1:5])
	copy(registration.OuterIPv4[:], body[5:9])
	tokenLength := int(binary.LittleEndian.Uint32(body[15:19]))
	if tokenLength < 0 || tokenLength > len(body)-19 {
		return currentPartyPeerEndpointRegistration{}, fmt.Errorf(
			"current party peer registration token length %d exceeds %d",
			tokenLength,
			len(body)-19,
		)
	}
	return registration, nil
}

func (s *Service) captureCurrentPartyPeerEndpointRegistration(session *gameSession, body []byte) {
	if session == nil {
		return
	}
	registration, err := parseCurrentPartyPeerEndpointRegistration(body)
	if err != nil {
		s.logGameEvent(session, "game-party-peer-endpoint-registration-rejected",
			"body_len", len(body),
			"reason", "current_exe_op2_31_byte_layout_mismatch",
			"error", err)
		return
	}
	// A zero-filled 31-byte reconnect probe is used by tests and by older
	// clients before their UDP socket is ready. Preserve the existing endpoint
	// until a real non-zero port arrives.
	if registration.Port == 0 {
		return
	}
	session.partyPeerMu.Lock()
	session.partyPeer = registration
	session.partyPeerMu.Unlock()
	s.logGameEvent(session, "game-party-peer-endpoint-registered",
		"char_id", session.selectedCharacterID,
		"inner_ipv4", fmt.Sprintf("%d.%d.%d.%d",
			registration.InnerIPv4[0], registration.InnerIPv4[1], registration.InnerIPv4[2], registration.InnerIPv4[3]),
		"outer_ipv4", fmt.Sprintf("%d.%d.%d.%d",
			registration.OuterIPv4[0], registration.OuterIPv4[1], registration.OuterIPv4[2], registration.OuterIPv4[3]),
		"udp_port", registration.Port,
		"mtu", registration.MTU,
		"advertised_mtu", currentPartyPeerAdvertisedMTU(registration),
		"character_attribute", registration.CharacterAttribute,
		"source", "current_exe_cs_op2_dynamic_udp_endpoint")
}

func currentPartyPeerPort(session *gameSession) uint16 {
	return currentPartyPeerEndpointSnapshot(session).Port
}

func currentPartyPeerEndpointSnapshot(session *gameSession) currentPartyPeerEndpointRegistration {
	if session == nil {
		return currentPartyPeerEndpointRegistration{}
	}
	session.partyPeerMu.Lock()
	defer session.partyPeerMu.Unlock()
	return session.partyPeer
}

func currentPartyPeerAdvertisedMTU(registration currentPartyPeerEndpointRegistration) uint32 {
	if registration.MTU < currentPartyPeerMinimumMTU ||
		registration.MTU > currentPartyPeerMaximumMTU {
		return 0
	}
	return registration.MTU
}
