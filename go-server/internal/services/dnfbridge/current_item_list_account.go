package dnfbridge

import (
	"context"
	"math"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) buildCurrentAccountCargoItemListBodyForSession(
	ctx context.Context,
	session *gameSession,
	repos dnfrepo.Group,
) ([]byte, string, int, bool) {
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID == "" || repos.Account == nil || repos.AccountInventory == nil {
		// There is no authenticated account owner to read.  A zero header is the
		// client's real uncreated-cargo state; do not borrow a character setting
		// or manufacture rows from another owner.
		return buildCurrentItemListBody(12, nil, dnfrepo.CharacterContainerState{}), "account_cargo_owner_unavailable_empty", 0, true
	}
	account, found, err := repos.Account.Load(ctx, accountID)
	if err != nil {
		s.logPacketEvent("game-upper-current-account-cargo-account-load-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"account_id", accountID,
			"err", err)
		return nil, "", 0, false
	}
	if !found {
		return buildCurrentItemListBody(12, nil, dnfrepo.CharacterContainerState{}), "account_cargo_account_absent_empty", 0, true
	}
	items, inventoryFound, err := repos.AccountInventory.Load(ctx, accountID)
	if err != nil {
		s.logPacketEvent("game-upper-current-account-cargo-items-load-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"account_id", accountID,
			"err", err)
		return nil, "", 0, false
	}
	state := currentAccountCargoContainerState(account)
	entries := make([]currentItemListEntry, 0)
	if inventoryFound {
		entries = currentItemListEntriesFromMap(items.Slots, 12)
	}
	sortCurrentItemListEntries(entries)
	patchedUsePeriods, usePeriodErr := s.applyCurrentPVFUsePeriodsToEntriesWithLoadedCatalog(ctx, entries)
	if usePeriodErr != nil {
		s.logPacketEvent("game-upper-current-account-cargo-use-period-wire-projection-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"account_id", accountID,
			"err", usePeriodErr)
	}
	source := "account_metadata+account_inventory"
	if !inventoryFound {
		source += "_absent_empty"
	}
	if patchedUsePeriods > 0 {
		source += "+runtime_pvf_stackable_use_period"
	}
	return buildCurrentItemListBody(12, entries, state), source, len(entries), true
}

func currentAccountCargoContainerState(account dnfrepo.AccountRecord) dnfrepo.CharacterContainerState {
	level := currentAccountCargoMetadataInt(account.Metadata, "account_cargo_level")
	gold := currentAccountCargoMetadataInt(account.Metadata, "account_cargo_gold")
	if level < 0 {
		level = 0
	}
	if level > math.MaxUint16 {
		level = math.MaxUint16
	}
	if gold < 0 {
		gold = 0
	}
	if gold > math.MaxUint32 {
		gold = math.MaxUint32
	}
	return dnfrepo.CharacterContainerState{
		AccountCargoSelectionKey: uint16(level),
		AccountCargoStateValue:   uint32(gold),
	}
}

func currentAccountCargoMetadataInt(metadata map[string]string, key string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(metadata[key]), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func currentGoldWalletItemListEntry(gold int64) currentItemListEntry {
	if gold < 0 {
		gold = 0
	}
	if gold > math.MaxInt32 {
		gold = math.MaxInt32
	}
	var entry currentItemListEntry
	// 86JP persists the character wallet as list0/slot0 with the balance in
	// stack_count/instance_value and item_template_id=0. It is a reserved
	// inventory row, not a fabricated PVF item.
	entry.patchCore(0, 0, uint32(gold))
	return entry
}
