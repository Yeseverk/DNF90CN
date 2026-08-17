package repository

import (
	"context"
	"errors"

	"longheng.io/server/internal/platform/db"
)

var ErrRepoMissing = errors.New("dnf repository is missing")

type Group struct {
	Account              AccountRepository
	AccountInventory     AccountInventoryRepository
	Character            CharacterRepository
	Inventory            InventoryRepository
	Equipment            EquipmentRepository
	Pet                  PetRepository
	Quest                QuestRepository
	Skill                SkillRepository
	DungeonPermission    DungeonPermissionRepository
	PacketTemplate       PacketTemplateRepository
	Settings             SettingsRepository
	Mailbox              MailboxRepository
	LegacyUserInfo       LegacyUserInfoRepository
	LegacyInventory      LegacyInventoryRepository
	CharacterCreate      CharacterCreationUnitOfWork
	CharacterItems       CharacterItemUnitOfWork
	CharacterTrade       CharacterTradeUnitOfWork
	CharacterPets        CharacterPetUnitOfWork
	AccountItems         AccountCharacterItemUnitOfWork
	CharacterAssets      CharacterAssetUnitOfWork
	MailboxAssets        MailboxAssetUnitOfWork
	AccountAssets        AccountCharacterAssetUnitOfWork
	RentalAssets         RentalAssetUnitOfWork
	CeraShopAssets       CeraShopAssetUnitOfWork
	CharacterSkills      CharacterSkillUnitOfWork
	CharacterProgression CharacterProgressionUnitOfWork
	CharacterSettlement  CharacterSettlementUnitOfWork
}

type RepositoryGroup = Group

func (g Group) Check(ctx context.Context) error {
	repos := []any{
		g.Account,
		g.AccountInventory,
		g.Character,
		g.Inventory,
		g.Equipment,
		g.Pet,
		g.Quest,
		g.Skill,
		g.DungeonPermission,
		g.PacketTemplate,
		g.Settings,
		g.Mailbox,
	}
	for _, repo := range repos {
		if repo == nil {
			return ErrRepoMissing
		}
		if err := db.Check(ctx, repo); err != nil {
			return err
		}
	}
	if g.LegacyUserInfo != nil {
		if err := db.Check(ctx, g.LegacyUserInfo); err != nil {
			return err
		}
	}
	return nil
}
