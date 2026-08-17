package dnfbridge

// currentDungeonSettlementPhase is the monotonic owner for the current EXE's
// completed-dungeon handshake. Packet builders remain protocol-specific, but
// no handler may advance more than one client-owned barrier at a time.
type currentDungeonSettlementPhase uint8

const (
	currentDungeonSettlementPhaseCombat currentDungeonSettlementPhase = iota
	currentDungeonSettlementPhaseClearEnabled
	currentDungeonSettlementPhaseResultShown
	currentDungeonSettlementPhaseCardScrollStarted
	currentDungeonSettlementPhaseCardsRevealed
	currentDungeonSettlementPhaseRewardCommitted
	currentDungeonSettlementPhaseEnding
)

func (phase currentDungeonSettlementPhase) String() string {
	switch phase {
	case currentDungeonSettlementPhaseCombat:
		return "combat"
	case currentDungeonSettlementPhaseClearEnabled:
		return "clear_enabled"
	case currentDungeonSettlementPhaseResultShown:
		return "result_shown"
	case currentDungeonSettlementPhaseCardScrollStarted:
		return "card_scroll_started"
	case currentDungeonSettlementPhaseCardsRevealed:
		return "cards_revealed"
	case currentDungeonSettlementPhaseRewardCommitted:
		return "reward_committed"
	case currentDungeonSettlementPhaseEnding:
		return "ending"
	default:
		return "unknown"
	}
}

func (runtime *runtimeDungeonState) advanceSettlementPhase(next currentDungeonSettlementPhase) {
	if runtime != nil && next > runtime.settlementPhase {
		runtime.settlementPhase = next
	}
}
