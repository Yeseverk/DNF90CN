package dnfbridge

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestCurrentPartyUDPRelayForwardsOnlyAuthenticatedDirectedLegs(t *testing.T) {
	portStart := testCurrentPartyUDPRelayPortStart(t)
	relay, err := newCurrentPartyUDPRelay(true, "127.0.0.1", portStart, 12, nil)
	if err != nil {
		t.Fatalf("new UDP relay: %v", err)
	}
	defer func() {
		if err := relay.Close(); err != nil {
			t.Fatalf("close UDP relay: %v", err)
		}
	}()

	bindings := []currentPartyUDPRelayBinding{
		{characterID: 101, generation: 1, address: [4]byte{127, 0, 0, 1}},
		{characterID: 202, generation: 1, address: [4]byte{127, 0, 0, 1}},
	}
	if !relay.Sync(77, bindings) {
		t.Fatal("relay did not create a complete two-member room")
	}
	addressAB, portAB, ok := relay.Endpoint(77, 101, 202)
	if !ok {
		t.Fatal("missing A-to-B relay endpoint")
	}
	addressBA, portBA, ok := relay.Endpoint(77, 202, 101)
	if !ok || portAB == portBA {
		t.Fatalf("invalid directed relay endpoints: AB=%d BA=%d ok=%v", portAB, portBA, ok)
	}

	clientA := testCurrentPartyUDPClient(t, "127.0.0.1")
	defer clientA.Close()
	clientB := testCurrentPartyUDPClient(t, "127.0.0.1")
	defer clientB.Close()
	wrongSource := testCurrentPartyUDPClient(t, "127.0.0.2")
	defer wrongSource.Close()

	endpointAB := &net.UDPAddr{IP: net.IPv4(addressAB[0], addressAB[1], addressAB[2], addressAB[3]), Port: int(portAB)}
	endpointBA := &net.UDPAddr{IP: net.IPv4(addressBA[0], addressBA[1], addressBA[2], addressBA[3]), Port: int(portBA)}
	if _, err := wrongSource.WriteToUDP([]byte("forged"), endpointAB); err != nil {
		t.Fatalf("send forged endpoint registration: %v", err)
	}
	if _, err := clientB.WriteToUDP([]byte("b-ready"), endpointBA); err != nil {
		t.Fatalf("send B handshake: %v", err)
	}
	if _, err := clientA.WriteToUDP([]byte("A-payload"), endpointAB); err != nil {
		t.Fatalf("send A payload: %v", err)
	}

	buffer := make([]byte, 128)
	if err := clientB.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set client B deadline: %v", err)
	}
	n, _, err := clientB.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("receive relayed payload: %v", err)
	}
	if !bytes.Equal(buffer[:n], []byte("A-payload")) {
		t.Fatalf("relayed payload = %q, want A-payload", buffer[:n])
	}
}

func TestCurrentPartyUDPRelaySyncRemovesFormerMemberLegs(t *testing.T) {
	portStart := testCurrentPartyUDPRelayPortStart(t)
	relay, err := newCurrentPartyUDPRelay(true, "127.0.0.1", portStart, 12, nil)
	if err != nil {
		t.Fatalf("new UDP relay: %v", err)
	}
	defer relay.Close()

	first := []currentPartyUDPRelayBinding{
		{characterID: 101, generation: 1, address: [4]byte{127, 0, 0, 1}},
		{characterID: 202, generation: 1, address: [4]byte{127, 0, 0, 1}},
		{characterID: 303, generation: 1, address: [4]byte{127, 0, 0, 1}},
	}
	if !relay.Sync(77, first) {
		t.Fatal("initial relay sync failed")
	}
	if _, _, ok := relay.Endpoint(77, 101, 303); !ok {
		t.Fatal("initial member endpoint missing")
	}
	if !relay.Sync(77, first[:2]) {
		t.Fatal("member removal relay sync failed")
	}
	if _, _, ok := relay.Endpoint(77, 101, 303); ok {
		t.Fatal("former member endpoint remained routable")
	}
	if _, _, ok := relay.Endpoint(77, 101, 202); !ok {
		t.Fatal("remaining pair endpoint was removed")
	}
}

func testCurrentPartyUDPRelayPortStart(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		if err != nil {
			t.Fatalf("allocate test UDP port: %v", err)
		}
		port := listener.LocalAddr().(*net.UDPAddr).Port
		_ = listener.Close()
		if port <= 65523 {
			return port
		}
	}
	t.Fatal("could not allocate a usable twelve-port UDP test range")
	return 0
}

func testCurrentPartyUDPClient(t *testing.T, ip string) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(ip), Port: 0})
	if err != nil {
		t.Fatalf("listen UDP client %s: %v", ip, err)
	}
	return conn
}
