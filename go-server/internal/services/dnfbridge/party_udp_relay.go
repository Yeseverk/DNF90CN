package dnfbridge

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
)

const (
	currentPartyUDPRelayMaxMembers  = 4
	currentPartyUDPRelayMaxDatagram = 2048
)

// currentPartyUDPRelayBinding is one authenticated half of a party transport.
// The TCP session's observed IPv4 is deliberately authoritative. A client may
// report an arbitrary inner/outer endpoint in CS op2, whereas the relay must
// never learn an endpoint from an unrelated Internet host.
type currentPartyUDPRelayBinding struct {
	characterID uint16
	generation  uint64
	address     [4]byte
}

type currentPartyUDPRelayLegKey struct {
	partyID uint16
	ownerID uint16
	peerID  uint16
}

type currentPartyUDPRelayLeg struct {
	key     currentPartyUDPRelayLegKey
	binding currentPartyUDPRelayBinding
	conn    *net.UDPConn
	port    uint16

	mu              sync.Mutex
	learnedEndpoint *net.UDPAddr
}

// currentPartyUDPRelay is a per-directed-pair UDP relay. For party members A
// and B it owns two ports: A sends to A->B; received payload is emitted from
// B->A to B's learned endpoint. This keeps the existing client P2P handshake
// shape while exposing only the server address to every participant.
//
// It is intentionally IPv4-only because PARTY_IP_INFO carries exactly four
// octets. The listener always binds a concrete configured address; wildcard
// UDP listeners would violate the local profile's network boundary.
type currentPartyUDPRelay struct {
	mu sync.Mutex

	enabled   bool
	bindIP    net.IP
	advertise [4]byte
	portStart int
	portCount int
	usedPorts map[int]struct{}
	legs      map[currentPartyUDPRelayLegKey]*currentPartyUDPRelayLeg
	closed    bool
	wg        sync.WaitGroup

	logWarn func(string, ...any)
}

func newCurrentPartyUDPRelay(enabled bool, advertiseIP string, portStart, portCount int, logWarn func(string, ...any)) (*currentPartyUDPRelay, error) {
	relay := &currentPartyUDPRelay{
		enabled: enabled,
		logWarn: logWarn,
	}
	if !enabled {
		return relay, nil
	}
	ip := net.ParseIP(advertiseIP)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() {
		return nil, fmt.Errorf("party UDP relay requires a concrete IPv4 advertise address, got %q", advertiseIP)
	}
	if portStart < 1 || portStart > 65535 || portCount < 12 || portCount > 1024 || portStart+portCount-1 > 65535 {
		return nil, fmt.Errorf("party UDP relay port range %d+%d is invalid", portStart, portCount)
	}
	ipv4 := ip.To4()
	relay.bindIP = append(net.IP(nil), ipv4...)
	copy(relay.advertise[:], ipv4)
	relay.portStart = portStart
	relay.portCount = portCount
	relay.usedPorts = make(map[int]struct{})
	relay.legs = make(map[currentPartyUDPRelayLegKey]*currentPartyUDPRelayLeg)
	return relay, nil
}

func (r *currentPartyUDPRelay) Enabled() bool {
	return r != nil && r.enabled
}

// Sync installs a complete, transactional directed-pair set for one party.
// It returns false without publishing a partial relay topology when a member
// is stale/offline or the configured UDP range cannot satisfy the room.
func (r *currentPartyUDPRelay) Sync(partyID uint16, bindings []currentPartyUDPRelayBinding) bool {
	if r == nil || !r.enabled {
		return false
	}
	bindings = currentPartyUDPRelayBindings(bindings)
	if partyID == 0 || len(bindings) < 2 || len(bindings) > currentPartyUDPRelayMaxMembers {
		return false
	}

	wanted := make(map[currentPartyUDPRelayLegKey]currentPartyUDPRelayBinding, len(bindings)*(len(bindings)-1))
	for _, owner := range bindings {
		for _, peer := range bindings {
			if owner.characterID == peer.characterID {
				continue
			}
			wanted[currentPartyUDPRelayLegKey{partyID: partyID, ownerID: owner.characterID, peerID: peer.characterID}] = owner
		}
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	newLegs := make(map[currentPartyUDPRelayLegKey]*currentPartyUDPRelayLeg)
	for key, binding := range wanted {
		if existing := r.legs[key]; existing != nil && existing.binding == binding {
			continue
		}
		leg, err := r.newLegLocked(key, binding)
		if err != nil {
			for _, created := range newLegs {
				r.releaseLegLocked(created)
			}
			r.mu.Unlock()
			r.log("party UDP relay synchronization deferred", "party_id", partyID, "error", err)
			return false
		}
		newLegs[key] = leg
	}

	toClose := make([]*currentPartyUDPRelayLeg, 0)
	for key, existing := range r.legs {
		if key.partyID != partyID {
			continue
		}
		if binding, wanted := wanted[key]; !wanted || existing.binding != binding {
			toClose = append(toClose, existing)
			delete(r.legs, key)
			delete(r.usedPorts, int(existing.port))
		}
	}
	for key, leg := range newLegs {
		r.legs[key] = leg
	}
	r.mu.Unlock()

	for _, leg := range toClose {
		_ = leg.conn.Close()
	}
	for _, leg := range newLegs {
		r.wg.Add(1)
		go r.readLoop(leg)
	}
	return true
}

// CloseParty releases all directed ports for a party immediately. It is used
// when the current client creates a new PartyID during leadership transfer.
func (r *currentPartyUDPRelay) CloseParty(partyID uint16) {
	if r == nil || partyID == 0 {
		return
	}
	r.mu.Lock()
	legs := r.removePartyLocked(partyID)
	r.mu.Unlock()
	for _, leg := range legs {
		_ = leg.conn.Close()
	}
}

func (r *currentPartyUDPRelay) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	legs := make([]*currentPartyUDPRelayLeg, 0, len(r.legs))
	for _, leg := range r.legs {
		legs = append(legs, leg)
	}
	r.legs = make(map[currentPartyUDPRelayLegKey]*currentPartyUDPRelayLeg)
	r.usedPorts = make(map[int]struct{})
	r.mu.Unlock()
	var closeErr error
	for _, leg := range legs {
		closeErr = errors.Join(closeErr, leg.conn.Close())
	}
	r.wg.Wait()
	return closeErr
}

func (r *currentPartyUDPRelay) Endpoint(partyID, ownerID, peerID uint16) ([4]byte, uint16, bool) {
	if r == nil || !r.enabled || partyID == 0 || ownerID == 0 || peerID == 0 || ownerID == peerID {
		return [4]byte{}, 0, false
	}
	r.mu.Lock()
	leg := r.legs[currentPartyUDPRelayLegKey{partyID: partyID, ownerID: ownerID, peerID: peerID}]
	address := r.advertise
	r.mu.Unlock()
	if leg == nil {
		return [4]byte{}, 0, false
	}
	return address, leg.port, true
}

func (r *currentPartyUDPRelay) newLegLocked(key currentPartyUDPRelayLegKey, binding currentPartyUDPRelayBinding) (*currentPartyUDPRelayLeg, error) {
	for offset := 0; offset < r.portCount; offset++ {
		port := r.portStart + offset
		if _, used := r.usedPorts[port]; used {
			continue
		}
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: append(net.IP(nil), r.bindIP...), Port: port})
		if err != nil {
			continue
		}
		r.usedPorts[port] = struct{}{}
		return &currentPartyUDPRelayLeg{key: key, binding: binding, conn: conn, port: uint16(port)}, nil
	}
	return nil, fmt.Errorf("no free ports in configured range %d-%d", r.portStart, r.portStart+r.portCount-1)
}

func (r *currentPartyUDPRelay) releaseLegLocked(leg *currentPartyUDPRelayLeg) {
	if leg == nil {
		return
	}
	delete(r.usedPorts, int(leg.port))
	_ = leg.conn.Close()
}

func (r *currentPartyUDPRelay) removePartyLocked(partyID uint16) []*currentPartyUDPRelayLeg {
	legs := make([]*currentPartyUDPRelayLeg, 0)
	for key, leg := range r.legs {
		if key.partyID != partyID {
			continue
		}
		delete(r.legs, key)
		delete(r.usedPorts, int(leg.port))
		legs = append(legs, leg)
	}
	return legs
}

func (r *currentPartyUDPRelay) readLoop(leg *currentPartyUDPRelayLeg) {
	defer r.wg.Done()
	buffer := make([]byte, currentPartyUDPRelayMaxDatagram)
	for {
		n, source, err := leg.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		if n <= 0 || !currentPartyUDPRelaySourceMatches(source, leg.binding.address) {
			continue
		}
		leg.mu.Lock()
		leg.learnedEndpoint = cloneCurrentPartyUDPRelayAddr(source)
		leg.mu.Unlock()

		r.mu.Lock()
		opposite := r.legs[currentPartyUDPRelayLegKey{
			partyID: leg.key.partyID,
			ownerID: leg.key.peerID,
			peerID:  leg.key.ownerID,
		}]
		r.mu.Unlock()
		if opposite == nil {
			continue
		}
		opposite.mu.Lock()
		destination := cloneCurrentPartyUDPRelayAddr(opposite.learnedEndpoint)
		opposite.mu.Unlock()
		if destination == nil {
			continue
		}
		if _, err := opposite.conn.WriteToUDP(buffer[:n], destination); err != nil {
			r.log("party UDP relay forward failed", "party_id", leg.key.partyID, "owner_id", leg.key.ownerID, "peer_id", leg.key.peerID, "error", err)
		}
	}
}

func (r *currentPartyUDPRelay) log(message string, args ...any) {
	if r != nil && r.logWarn != nil {
		r.logWarn(message, args...)
	}
}

func currentPartyUDPRelayBindings(bindings []currentPartyUDPRelayBinding) []currentPartyUDPRelayBinding {
	unique := make(map[uint16]currentPartyUDPRelayBinding, len(bindings))
	for _, binding := range bindings {
		if binding.characterID == 0 || binding.generation == 0 {
			return nil
		}
		if _, exists := unique[binding.characterID]; exists {
			return nil
		}
		unique[binding.characterID] = binding
	}
	result := make([]currentPartyUDPRelayBinding, 0, len(unique))
	for _, binding := range unique {
		result = append(result, binding)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].characterID < result[right].characterID })
	return result
}

func currentPartyUDPRelaySourceMatches(source *net.UDPAddr, expected [4]byte) bool {
	if source == nil || source.IP == nil {
		return false
	}
	ip := source.IP.To4()
	return len(ip) == net.IPv4len && ip[0] == expected[0] && ip[1] == expected[1] && ip[2] == expected[2] && ip[3] == expected[3]
}

func cloneCurrentPartyUDPRelayAddr(source *net.UDPAddr) *net.UDPAddr {
	if source == nil {
		return nil
	}
	return &net.UDPAddr{IP: append(net.IP(nil), source.IP...), Port: source.Port, Zone: source.Zone}
}
