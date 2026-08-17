// Package dproto defines the server-side boundary for the native DNF
// protected transport. It deliberately does not implement TerSafe crypto:
// callers must inject a verified server-role provider.
package dproto

import (
	"context"
	"errors"
)

var (
	ErrProviderUnavailable = errors.New("dnf dproto server-role provider is unavailable")
	ErrEmptyProviderOutput = errors.New("dnf dproto provider returned an empty wire packet")
)

// ConnectionInfo identifies one game TCP connection. A Provider must create
// independent state for every call to Open; DPROTO sequence, handshake and
// cipher state must never be shared between game connections.
type ConnectionInfo struct {
	ConnectionID string
	RemoteAddr   string
	LocalAddr    string
	ChannelID    int
}

// DecodeResult contains complete plaintext upper packets plus any complete
// server wire packets produced by the protected-transport control state.
// Returned byte slices are owned by the caller.
type DecodeResult struct {
	InnerPackets    [][]byte
	OutboundPackets [][]byte
}

// EncodeResult is one complete server wire packet. Protected is true only
// when Packet is a native S2C op1467 envelope; otherwise Packet must be the
// unmodified direct inner packet supplied to EncodeServer.
type EncodeResult struct {
	Packet    []byte
	Protected bool
}

// Provider opens a server-role DPROTO session for one game connection.
type Provider interface {
	Open(context.Context, ConnectionInfo) (Session, error)
}

// Session is the ordered, stateful server-side DPROTO peer. Implementations
// are not required to be concurrency-safe; dnfbridge serializes every method
// call and the resulting socket write for a connection.
type Session interface {
	DecodeClient(context.Context, []byte) (DecodeResult, error)
	EncodeServer(context.Context, []byte) (EncodeResult, error)
	HandleClientControl(context.Context, uint16, []byte) ([][]byte, error)
	Close() error
}
