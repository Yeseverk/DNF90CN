// Package progression contains PVF-backed, protocol-independent character
// progression rules.
//
// The package deliberately performs no repository writes and emits no DNF
// packets. A dungeon/quest settlement owner must apply the returned character,
// quest, and skill-point changes in one unit of work before acknowledging the
// client.
package progression
