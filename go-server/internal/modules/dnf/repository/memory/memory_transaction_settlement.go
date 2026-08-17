package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"sync"
)

// memoryCharacterSettlementUnitOfWork buffers all settlement aggregates
// before committing them under the shared aggregate transaction mutex.
type memoryCharacterSettlementUnitOfWork struct {
	mu               sync.Mutex
	sharedMu         *sync.Mutex
	account          repository.AccountRepository
	accountInventory repository.AccountInventoryRepository
	character        repository.CharacterRepository
	quests           repository.QuestRepository
	inventory        repository.InventoryRepository
	equipment        repository.EquipmentRepository
	skills           repository.SkillRepository
}

func (u *memoryCharacterSettlementUnitOfWork) WithinCharacterSettlement(
	ctx context.Context,
	characterID string,
	apply func(repository.Group) error,
) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil || u.character == nil ||
		u.quests == nil || u.inventory == nil || u.equipment == nil || u.skills == nil {
		return repository.ErrCharacterSettlementTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	lock := &u.mu
	if u.sharedMu != nil {
		lock = u.sharedMu
	}
	lock.Lock()
	defer lock.Unlock()

	character, characterExists, err := u.character.Load(ctx, characterID)
	if err != nil {
		return err
	}
	accountID := strings.TrimSpace(character.AccountID)
	var account repository.AccountRecord
	var accountExists bool
	if accountID != "" && u.account != nil {
		account, accountExists, err = u.account.Load(ctx, accountID)
		if err != nil {
			return err
		}
	}
	var accountInventory repository.AccountInventoryRecord
	var accountInventoryExists bool
	if accountID != "" && u.accountInventory != nil {
		accountInventory, accountInventoryExists, err = u.accountInventory.Load(ctx, accountID)
		if err != nil {
			return err
		}
	}
	questRecord, questExists, err := u.quests.Load(ctx, characterID)
	if err != nil {
		return err
	}
	inventory, inventoryExists, err := u.inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	equipment, equipmentExists, err := u.equipment.Load(ctx, characterID)
	if err != nil {
		return err
	}
	skills, skillExists, err := u.skills.Load(ctx, characterID)
	if err != nil {
		return err
	}

	characterTx := &memoryCharacterTransaction{
		characterID: characterID,
		record:      repository.CloneCharacter(character),
		exists:      characterExists,
	}
	questTx := &memoryQuestTransaction{
		characterID: characterID,
		record:      repository.CloneQuest(questRecord),
		exists:      questExists,
	}
	inventoryTx := &memoryInventoryTransaction{
		characterID: characterID,
		record:      repository.CloneInventory(inventory),
		exists:      inventoryExists,
	}
	equipmentTx := &memoryEquipmentTransaction{
		characterID: characterID,
		record:      repository.CloneEquipment(equipment),
		exists:      equipmentExists,
	}
	skillTx := &memorySkillTransaction{
		characterID: characterID,
		record:      repository.CloneSkill(skills),
		exists:      skillExists,
	}
	accountInventoryTx := &memoryAccountInventoryTransaction{
		accountID: accountID,
		record:    repository.CloneAccountInventory(accountInventory),
		exists:    accountInventoryExists,
	}
	var accountTx *memoryAccountTransaction
	if u.account != nil {
		accountTx = &memoryAccountTransaction{
			accountID: accountID,
			record:    repository.CloneAccount(account),
			exists:    accountExists,
		}
	}
	txGroup := repository.Group{
		Account:          accountTx,
		AccountInventory: accountInventoryTx,
		Character:        characterTx,
		Quest:            questTx,
		Inventory:        inventoryTx,
		Equipment:        equipmentTx,
		Skill:            skillTx,
	}
	if err := apply(txGroup); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if characterTx.dirty {
		if err := u.character.Save(ctx, characterTx.record); err != nil {
			return errors.Join(
				err,
				restoreCharacterRecord(u.character, characterID, character, characterExists, true),
			)
		}
	}
	if questTx.dirty {
		if err := u.quests.Save(ctx, questTx.record); err != nil {
			return errors.Join(
				err,
				restoreQuestRecordIfDirty(u.quests, characterID, questRecord, questExists, true),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	if skillTx.dirty {
		if err := u.skills.Save(ctx, skillTx.record); err != nil {
			return errors.Join(
				err,
				restoreSkillRecord(u.skills, characterID, skills, skillExists, true),
				restoreQuestRecordIfDirty(u.quests, characterID, questRecord, questExists, questTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, true),
				restoreSkillRecord(u.skills, characterID, skills, skillExists, skillTx.dirty),
				restoreQuestRecordIfDirty(u.quests, characterID, questRecord, questExists, questTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	if equipmentTx.dirty {
		if err := u.equipment.Save(ctx, equipmentTx.record); err != nil {
			return errors.Join(
				err,
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, true),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
				restoreSkillRecord(u.skills, characterID, skills, skillExists, skillTx.dirty),
				restoreQuestRecordIfDirty(u.quests, characterID, questRecord, questExists, questTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	if accountInventoryTx.dirty {
		if u.accountInventory == nil {
			return repository.ErrCharacterSettlementTransactionUnavailable
		}
		if err := u.accountInventory.Save(ctx, accountInventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreAccountInventoryRecord(u.accountInventory, accountID, accountInventory, accountInventoryExists),
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, equipmentTx.dirty),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
				restoreSkillRecord(u.skills, characterID, skills, skillExists, skillTx.dirty),
				restoreQuestRecordIfDirty(u.quests, characterID, questRecord, questExists, questTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	if accountTx != nil && accountTx.dirty {
		if err := u.account.Save(ctx, accountTx.record); err != nil {
			return errors.Join(
				err,
				restoreAccountRecord(u.account, accountID, account, accountExists, true),
				restoreAccountInventoryRecordIfDirty(u.accountInventory, accountID, accountInventory, accountInventoryExists, accountInventoryTx.dirty),
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, equipmentTx.dirty),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
				restoreSkillRecord(u.skills, characterID, skills, skillExists, skillTx.dirty),
				restoreQuestRecordIfDirty(u.quests, characterID, questRecord, questExists, questTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	return nil
}

type memoryQuestTransaction struct {
	characterID string
	record      repository.QuestRecord
	exists      bool
	dirty       bool
}

func (tx *memoryQuestTransaction) Load(ctx context.Context, characterID string) (repository.QuestRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.characterID, characterID); err != nil {
		return repository.QuestRecord{}, false, err
	}
	return repository.CloneQuest(tx.record), tx.exists, nil
}

func (tx *memoryQuestTransaction) Save(ctx context.Context, record repository.QuestRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	tx.record = repository.CloneQuest(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func restoreQuestRecordIfDirty(
	repo repository.QuestRepository,
	characterID string,
	record repository.QuestRecord,
	existed bool,
	dirty bool,
) error {
	if !dirty {
		return nil
	}
	if existed {
		return repo.Save(context.Background(), record)
	}
	deleter, ok := repo.(interface {
		Delete(context.Context, string) error
	})
	if !ok {
		return errors.New("quest rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), characterID)
}
