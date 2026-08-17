// 本文件提供 DNF 仓储的内存实现，用于本地开发和单元测试。
package memory

import (
	"longheng.io/server/internal/modules/dnf/repository"
	"sync"

	"longheng.io/server/internal/platform/db"
)

// NewMemoryGroup 创建完整的内存仓储聚合。
// 该实现不提供生产持久化，只用于本地开发、单测和 owner 接口验证。
func NewMemoryGroup() repository.Group {
	accounts := newMemoryAccountStore()
	accountInventory := db.NewMemoryStore(repository.AccountInventoryKey, repository.CloneAccountInventory)
	characters := newMemoryCharStore()
	inventory := db.NewMemoryStore(repository.InventoryKey, repository.CloneInventory)
	equipment := db.NewMemoryStore(repository.EquipmentKey, repository.CloneEquipment)
	pets := db.NewMemoryStore(repository.PetKey, repository.ClonePet)
	quests := db.NewMemoryStore(repository.QuestKey, repository.CloneQuest)
	skills := db.NewMemoryStore(repository.SkillKey, repository.CloneSkill)
	dungeonPermissions := newMemoryDungeonPermissionStore()
	settings := db.NewMemoryStore(repository.SettingsKey, repository.CloneSettings)
	mailboxes := db.NewMemoryStore(repository.MailboxKey, repository.CloneMailbox)
	aggregateTxMu := &sync.Mutex{}
	return repository.Group{
		Account:              accounts,
		AccountInventory:     accountInventory,
		Character:            characters,
		Inventory:            inventory,
		Equipment:            equipment,
		Pet:                  pets,
		Quest:                quests,
		Skill:                skills,
		DungeonPermission:    dungeonPermissions,
		PacketTemplate:       db.NewMemoryStore(repository.PacketTemplateKey, repository.ClonePacketTemplate),
		Settings:             settings,
		Mailbox:              mailboxes,
		CharacterCreate:      &memoryCharacterCreationUnitOfWork{},
		CharacterItems:       &memoryCharacterItemUnitOfWork{sharedMu: aggregateTxMu, inventory: inventory, equipment: equipment},
		CharacterTrade:       &memoryCharacterTradeUnitOfWork{sharedMu: aggregateTxMu, character: characters, inventory: inventory},
		CharacterPets:        &memoryCharacterPetUnitOfWork{sharedMu: aggregateTxMu, inventory: inventory, equipment: equipment, pets: pets},
		AccountItems:         &memoryAccountCharacterItemUnitOfWork{sharedMu: aggregateTxMu, accountInventory: accountInventory, inventory: inventory},
		CharacterAssets:      &memoryCharacterAssetUnitOfWork{sharedMu: aggregateTxMu, character: characters, inventory: inventory, equipment: equipment},
		MailboxAssets:        &memoryMailboxAssetUnitOfWork{sharedMu: aggregateTxMu, character: characters, inventory: inventory, mailbox: mailboxes},
		AccountAssets:        &memoryAccountCharacterAssetUnitOfWork{sharedMu: aggregateTxMu, accountInventory: accountInventory, character: characters, inventory: inventory, equipment: equipment},
		RentalAssets:         &memoryRentalAssetUnitOfWork{sharedMu: aggregateTxMu, account: accounts, character: characters, inventory: inventory, equipment: equipment},
		CeraShopAssets:       &memoryCeraShopAssetUnitOfWork{sharedMu: aggregateTxMu, account: accounts, character: characters, inventory: inventory, equipment: equipment, settings: settings},
		CharacterSkills:      &memoryCharacterSkillUnitOfWork{sharedMu: aggregateTxMu, skills: skills},
		CharacterProgression: &memoryCharacterProgressionUnitOfWork{sharedMu: aggregateTxMu, character: characters, skills: skills},
		CharacterSettlement:  &memoryCharacterSettlementUnitOfWork{sharedMu: aggregateTxMu, account: accounts, accountInventory: accountInventory, character: characters, quests: quests, inventory: inventory, equipment: equipment, skills: skills},
	}
}
