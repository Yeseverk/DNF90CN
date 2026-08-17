package dnfbridge

import (
	"time"

	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// Account premium contracts live in dnf_accounts.metadata_json as
// premium_expire_<type> = unix seconds, following the same account-scoped
// metadata precedent as account_cera / account_cargo_*. The storage and wire
// contract is owned by internal/modules/dnf/premium so the inventory, skill,
// equipment and cera-shop modules share one implementation; these aliases
// keep the dnfbridge call sites short.
const (
	currentPremiumTypeOverEquip  = premium.TypeOverEquip
	currentPremiumTypeOverSkill  = premium.TypeOverSkill
	currentPremiumTypeBonusExp   = premium.TypeBonusExp
	currentPremiumTypeCrystal    = premium.TypeCrystal
	currentPremiumTypeLethe      = premium.TypeLethe
	currentPremiumDevilTypeBase  = premium.DevilTypeBase
	currentPremiumDevilFolded    = premium.DevilFolded
	currentPremiumDevilSlotCount = premium.DevilSlotCount

	// currentPremiumActivatedMsgID is the current-EXE class0 premium
	// activation notification (sub_1D61460, registered at 0x1D7EBA8). Its
	// premium type field is u8, so internal devil slots 580..587 never use it.
	currentPremiumActivatedMsgID uint16 = 0x42
)

func currentPremiumExpireAt(account dnfrepo.AccountRecord, premiumType int64) int64 {
	return premium.ExpireAt(account, premiumType)
}

func currentPremiumActive(account dnfrepo.AccountRecord, premiumType int64, now time.Time) bool {
	return premium.Active(account, premiumType, now)
}

func currentPremiumDevilSlotType(slot int64) int64 {
	return premium.DevilSlotType(slot)
}

func currentSelectAckPremiumEntries(account dnfrepo.AccountRecord, now time.Time) []byte {
	return premium.SelectAckEntries(account, now)
}

func buildCurrentPremiumActivatedBody(premiumType int64, remainingSeconds int64) []byte {
	return premium.BuildActivatedBody(premiumType, remainingSeconds)
}

func buildCurrentPremiumServiceDataBody(
	account dnfrepo.AccountRecord,
	character dnfrepo.CharacterRecord,
	now time.Time,
) []byte {
	return premium.BuildServiceDataBody(account, character, now)
}
