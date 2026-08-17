// 本文件定义 DNF 仓储事务边界接口。
package repository

import (
	"context"
	"errors"
	"strings"

	"longheng.io/server/internal/platform/db"
)

var (
	ErrCharacterCreationTransactionUnavailable     = errors.New("character creation transaction is unavailable")
	ErrCharacterItemTransactionUnavailable         = errors.New("character item transaction is unavailable")
	ErrCharacterTradeTransactionUnavailable        = errors.New("character trade transaction is unavailable")
	ErrCharacterPetTransactionUnavailable          = errors.New("character pet transaction is unavailable")
	ErrAccountCharacterItemTransactionUnavailable  = errors.New("account and character item transaction is unavailable")
	ErrCharacterAssetTransactionUnavailable        = errors.New("character asset transaction is unavailable")
	ErrAccountCharacterAssetTransactionUnavailable = errors.New("account inventory and character asset transaction is unavailable")
	ErrRentalAssetTransactionUnavailable           = errors.New("rental asset transaction is unavailable")
	ErrCeraShopAssetTransactionUnavailable         = errors.New("cera shop asset transaction is unavailable")
	ErrCharacterSkillTransactionUnavailable        = errors.New("character skill transaction is unavailable")
	ErrCharacterProgressionTransactionUnavailable  = errors.New("character progression transaction is unavailable")
	ErrCharacterSettlementTransactionUnavailable   = errors.New("character settlement transaction is unavailable")
	ErrMailboxAssetTransactionUnavailable          = errors.New("mailbox asset transaction is unavailable")
)

// CharacterCreationUnitOfWork atomically persists a character aggregate. It
// is used by creation and by missing-only initialization repair. The base
// group is supplied explicitly so test/local repository overrides participate
// in the same transaction boundary.
type CharacterCreationUnitOfWork interface {
	WithinCharacterCreation(context.Context, string, Group, func(Group) error) error
}

// CharacterItemUnitOfWork supplies transaction-scoped inventory and equipment
// repositories for one character. The callback owns domain decisions; the
// repository only owns atomic commit and rollback.
type CharacterItemUnitOfWork interface {
	WithinCharacterItems(context.Context, string, func(InventoryRepository, EquipmentRepository) error) error
}

// CharacterTradeUnitOfWork atomically moves wallet and inventory assets
// between two distinct characters. Both transaction-scoped repositories
// accept only the two supplied character IDs; callers must load them in stable
// ID order before saving so concurrent trades acquire row locks consistently.
type CharacterTradeUnitOfWork interface {
	WithinCharacterTrade(context.Context, string, string, func(CharacterRepository, InventoryRepository) error) error
}

// CharacterPetUnitOfWork supplies one character-scoped transaction for the
// inventory pet container, worn creature slots, and durable creature state.
// Pet owners must use this boundary whenever one action can mutate more than
// one of those aggregates.
type CharacterPetUnitOfWork interface {
	WithinCharacterPets(context.Context, string, func(InventoryRepository, EquipmentRepository, PetRepository) error) error
}

// AccountCharacterItemUnitOfWork atomically persists the account-owned
// crystal/soul slots and the selected character's ordinary inventory. The two
// repositories are key-scoped for the supplied account and character.
type AccountCharacterItemUnitOfWork interface {
	WithinAccountCharacterItems(context.Context, string, string, func(AccountInventoryRepository, InventoryRepository) error) error
}

// CharacterAssetUnitOfWork supplies one transaction for the real character
// wallet fields and both item aggregates. Reward handlers must use this
// boundary when a single grant can change gold and inventory/equipment.
type CharacterAssetUnitOfWork interface {
	WithinCharacterAssets(context.Context, string, func(CharacterRepository, InventoryRepository, EquipmentRepository) error) error
}

// MailboxAssetUnitOfWork atomically moves wallet/inventory assets between one
// character and one mailbox aggregate. The two owner IDs may differ for send
// and are equal for attachment extraction.
type MailboxAssetUnitOfWork interface {
	WithinMailboxAssets(
		context.Context,
		string,
		string,
		func(CharacterRepository, InventoryRepository, MailboxRepository) error,
	) error
}

// AccountCharacterAssetUnitOfWork supplies one transaction for the
// account-scoped crystal/soul inventory, the selected character wallet, and
// both item aggregates. Upgrade and shop flows must use this boundary when
// one action consumes account-owned materials while also mutating character
// gold or equipment.
type AccountCharacterAssetUnitOfWork interface {
	WithinAccountCharacterAssets(context.Context, string, string, func(AccountInventoryRepository, CharacterRepository, InventoryRepository, EquipmentRepository) error) error
}

// RentalAssetUnitOfWork supplies one transaction for the account-scoped
// rental-point wallet, the selected character's gold wallet, and both item
// aggregates. Implementations must scope every repository to the two supplied
// owner keys so a forged request cannot mutate another account or character.
type RentalAssetUnitOfWork interface {
	WithinRentalAssets(context.Context, string, string, func(AccountRepository, CharacterRepository, InventoryRepository, EquipmentRepository) error) error
}

// CeraShopAssetUnitOfWork supplies one transaction for the account Cera
// wallet, selected character, inventory/equipment projections, and the exact
// character container-settings scope. Checkout owners use this boundary so a
// debit cannot commit without every purchased effect.
type CeraShopAssetUnitOfWork interface {
	WithinCeraShopAssets(
		context.Context,
		string,
		string,
		string,
		func(AccountRepository, CharacterRepository, InventoryRepository, EquipmentRepository, SettingsRepository) error,
	) error
}

// CharacterSkillUnitOfWork serializes and atomically commits one character's
// learned levels and SP/TP ledger.
type CharacterSkillUnitOfWork interface {
	WithinCharacterSkill(context.Context, string, func(SkillRepository) error) error
}

// CharacterProgressionUnitOfWork supplies one transaction-scoped character
// repository and skill repository for the same character. Experience/level
// and the SP/TP ledger must use this boundary so neither aggregate can commit
// without the other.
type CharacterProgressionUnitOfWork interface {
	WithinCharacterProgression(context.Context, string, func(CharacterRepository, SkillRepository) error) error
}

// CharacterSettlementUnitOfWork supplies one character-scoped transaction
// spanning every aggregate that a quest or dungeon settlement may mutate.
// The callback owns reward and progression rules; the repository owns atomic
// commit, rollback, and rejection of cross-character access.
type CharacterSettlementUnitOfWork interface {
	WithinCharacterSettlement(context.Context, string, func(Group) error) error
}

func TransactionKeyError(ctx context.Context, expected string, actual string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(actual) != expected {
		return db.ErrRecordKeyRequired
	}
	return nil
}
