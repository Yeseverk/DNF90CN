// Package onlineevent owns protocol-independent daily online-time progress and
// reward claims.
//
// The package deliberately does not register a game command or encode a
// packet. A transport owner must first prove the current EXE's activity
// descriptor, progress, and claim layouts. It may then feed server-observed
// presence intervals into Owner.Observe and supply a PVF/current-client item
// allocator to Owner.Claim. AttendancePVFCatalog exposes only the raw proved
// PVF values; it does not activate an event or assign protocol semantics.
package onlineevent
