package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"sort"
	"strconv"
	"strings"
	"sync"

	"longheng.io/server/internal/platform/db"
)

type memoryCharacterCreationUnitOfWork struct {
	mu sync.Mutex
}

// memoryCreationStore buffers writes until the creation callback succeeds.
// NewMemoryGroup stores cannot fail after key validation, so committing the
// character first and the buffered aggregates afterwards cannot expose a
// partially initialized test character.
type memoryCreationStore[T any] struct {
	base    db.Store[T]
	keyFn   db.KeyFunc[T]
	cloneFn db.CloneFunc[T]
	pending map[string]T
}

func newMemoryCreationStore[T any](base db.Store[T], keyFn db.KeyFunc[T], cloneFn db.CloneFunc[T]) *memoryCreationStore[T] {
	if base == nil {
		return nil
	}
	return &memoryCreationStore[T]{
		base:    base,
		keyFn:   keyFn,
		cloneFn: cloneFn,
		pending: make(map[string]T),
	}
}

func (s *memoryCreationStore[T]) Load(ctx context.Context, key string) (T, bool, error) {
	if err := transactionContextError(ctx); err != nil {
		var zero T
		return zero, false, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		var zero T
		return zero, false, db.ErrRecordKeyRequired
	}
	if record, ok := s.pending[key]; ok {
		return s.cloneFn(record), true, nil
	}
	return s.base.Load(ctx, key)
}

func (s *memoryCreationStore[T]) Save(ctx context.Context, record T) error {
	if err := transactionContextError(ctx); err != nil {
		return err
	}
	key, err := db.RecordKey(s.keyFn, record)
	if err != nil {
		return err
	}
	s.pending[key] = s.cloneFn(record)
	return nil
}

func (s *memoryCreationStore[T]) commit() error {
	if s == nil || len(s.pending) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.pending))
	for key := range s.pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := s.base.Save(context.Background(), s.pending[key]); err != nil {
			return err
		}
	}
	return nil
}

type memoryCharacterCreationStore struct {
	base        repository.CharacterRepository
	expectedID  string
	pending     repository.CharacterRecord
	hasPending  bool
	createWrite bool
}

func (s *memoryCharacterCreationStore) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, s.expectedID, characterID); err != nil {
		return repository.CharacterRecord{}, false, err
	}
	if s.hasPending {
		return repository.CloneCharacter(s.pending), true, nil
	}
	return s.base.Load(ctx, s.expectedID)
}

func (s *memoryCharacterCreationStore) Save(ctx context.Context, record repository.CharacterRecord) error {
	if err := repository.TransactionKeyError(ctx, s.expectedID, record.CharacterID); err != nil {
		return err
	}
	s.pending = repository.CloneCharacter(record)
	s.hasPending = true
	return nil
}

func (s *memoryCharacterCreationStore) CreateCharacter(ctx context.Context, record repository.CharacterRecord) error {
	if err := repository.TransactionKeyError(ctx, s.expectedID, record.CharacterID); err != nil {
		return err
	}
	if _, exists, err := s.base.Load(ctx, s.expectedID); err != nil {
		return err
	} else if exists || s.hasPending {
		return repository.ErrCharacterIDExists
	}
	records, err := s.base.ListByAccount(ctx, record.AccountID, 0)
	if err != nil {
		return err
	}
	for _, existing := range records {
		if existing.Slot == record.Slot {
			return repository.ErrCharacterSlotOccupied
		}
	}
	s.pending = repository.CloneCharacter(record)
	s.hasPending = true
	s.createWrite = true
	return nil
}

func (s *memoryCharacterCreationStore) ListByAccount(ctx context.Context, accountID string, limit int) ([]repository.CharacterRecord, error) {
	if err := transactionContextError(ctx); err != nil {
		return nil, err
	}
	records, err := s.base.ListByAccount(ctx, accountID, 0)
	if err != nil {
		return nil, err
	}
	if s.hasPending && s.pending.AccountID == strings.TrimSpace(accountID) && characterDeleteFlag(s.pending) == 0 {
		replaced := false
		for idx := range records {
			if records[idx].CharacterID == s.pending.CharacterID {
				records[idx] = repository.CloneCharacter(s.pending)
				replaced = true
				break
			}
		}
		if !replaced {
			records = append(records, repository.CloneCharacter(s.pending))
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Slot == records[j].Slot {
			return records[i].CharacterID < records[j].CharacterID
		}
		return records[i].Slot < records[j].Slot
	})
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *memoryCharacterCreationStore) FindIDByName(ctx context.Context, name string) (string, bool, error) {
	if err := transactionContextError(ctx); err != nil {
		return "", false, err
	}
	name = strings.TrimSpace(name)
	if s.hasPending && s.pending.Name == name && characterDeleteFlag(s.pending) == 0 {
		return s.pending.CharacterID, true, nil
	}
	return s.base.FindIDByName(ctx, name)
}

func (s *memoryCharacterCreationStore) NextNumericID(ctx context.Context) (int, error) {
	next, err := s.base.NextNumericID(ctx)
	if err != nil {
		return 0, err
	}
	if s.hasPending {
		if id, parseErr := strconv.Atoi(s.pending.CharacterID); parseErr == nil && id >= next {
			return id + 1, nil
		}
	}
	return next, nil
}

func (s *memoryCharacterCreationStore) commit() error {
	if !s.hasPending {
		return nil
	}
	if s.createWrite {
		return repository.CreateCharacter(context.Background(), s.base, s.pending)
	}
	return s.base.Save(context.Background(), s.pending)
}

func (u *memoryCharacterCreationUnitOfWork) WithinCharacterCreation(ctx context.Context, characterID string, base repository.Group, apply func(repository.Group) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || base.Character == nil {
		return repository.ErrCharacterCreationTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	characters := &memoryCharacterCreationStore{base: base.Character, expectedID: characterID}
	accounts := newMemoryCreationStore[repository.AccountRecord](base.Account, repository.AccountKey, repository.CloneAccount)
	inventory := newMemoryCreationStore[repository.InventoryRecord](base.Inventory, repository.InventoryKey, repository.CloneInventory)
	equipment := newMemoryCreationStore[repository.EquipmentRecord](base.Equipment, repository.EquipmentKey, repository.CloneEquipment)
	skills := newMemoryCreationStore[repository.SkillRecord](base.Skill, repository.SkillKey, repository.CloneSkill)
	quests := newMemoryCreationStore[repository.QuestRecord](base.Quest, repository.QuestKey, repository.CloneQuest)
	pets := newMemoryCreationStore[repository.PetRecord](base.Pet, repository.PetKey, repository.ClonePet)
	settings := newMemoryCreationStore[repository.SettingsRecord](base.Settings, repository.SettingsKey, repository.CloneSettings)
	mailboxes := newMemoryCreationStore[repository.MailboxRecord](base.Mailbox, repository.MailboxKey, repository.CloneMailbox)
	txGroup := repository.Group{Character: characters}
	if accounts != nil {
		txGroup.Account = accounts
	}
	if inventory != nil {
		txGroup.Inventory = inventory
	}
	if equipment != nil {
		txGroup.Equipment = equipment
	}
	if skills != nil {
		txGroup.Skill = skills
	}
	if quests != nil {
		txGroup.Quest = quests
	}
	if pets != nil {
		txGroup.Pet = pets
	}
	if settings != nil {
		txGroup.Settings = settings
	}
	if mailboxes != nil {
		txGroup.Mailbox = mailboxes
	}
	if err := apply(txGroup); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := characters.commit(); err != nil {
		return err
	}
	for _, commit := range []func() error{
		accounts.commit,
		inventory.commit,
		equipment.commit,
		skills.commit,
		quests.commit,
		pets.commit,
		settings.commit,
		mailboxes.commit,
	} {
		if err := commit(); err != nil {
			return err
		}
	}
	return nil
}

func transactionContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

type memoryCharacterItemUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	inventory repository.InventoryRepository
	equipment repository.EquipmentRepository
}

type memoryCharacterTradeUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	character repository.CharacterRepository
	inventory repository.InventoryRepository
}

type memoryCharacterAssetUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	character repository.CharacterRepository
	inventory repository.InventoryRepository
	equipment repository.EquipmentRepository
}

type memoryRentalAssetUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	account   repository.AccountRepository
	character repository.CharacterRepository
	inventory repository.InventoryRepository
	equipment repository.EquipmentRepository
}

type memoryCeraShopAssetUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	account   repository.AccountRepository
	character repository.CharacterRepository
	inventory repository.InventoryRepository
	equipment repository.EquipmentRepository
	settings  repository.SettingsRepository
}

type memoryCharacterSkillUnitOfWork struct {
	mu       sync.Mutex
	sharedMu *sync.Mutex
	skills   repository.SkillRepository
}

type memoryCharacterProgressionUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	character repository.CharacterRepository
	skills    repository.SkillRepository
}

func (u *memoryCharacterSkillUnitOfWork) WithinCharacterSkill(ctx context.Context, characterID string, apply func(repository.SkillRepository) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil || u.skills == nil {
		return repository.ErrCharacterSkillTransactionUnavailable
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

	record, exists, err := u.skills.Load(ctx, characterID)
	if err != nil {
		return err
	}
	tx := &memorySkillTransaction{
		characterID: characterID,
		record:      repository.CloneSkill(record),
		exists:      exists,
	}
	if err := apply(tx); err != nil {
		return err
	}
	if tx.dirty {
		return u.skills.Save(ctx, tx.record)
	}
	return nil
}

func (u *memoryCharacterProgressionUnitOfWork) WithinCharacterProgression(
	ctx context.Context,
	characterID string,
	apply func(repository.CharacterRepository, repository.SkillRepository) error,
) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil || u.character == nil || u.skills == nil {
		return repository.ErrCharacterProgressionTransactionUnavailable
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
	skills, skillExists, err := u.skills.Load(ctx, characterID)
	if err != nil {
		return err
	}
	characterTx := &memoryCharacterTransaction{
		characterID: characterID,
		record:      repository.CloneCharacter(character),
		exists:      characterExists,
	}
	skillTx := &memorySkillTransaction{
		characterID: characterID,
		record:      repository.CloneSkill(skills),
		exists:      skillExists,
	}
	if err := apply(characterTx, skillTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if characterTx.dirty {
		if err := u.character.Save(ctx, characterTx.record); err != nil {
			return errors.Join(err, restoreCharacterRecord(u.character, characterID, character, characterExists, true))
		}
	}
	if skillTx.dirty {
		if err := u.skills.Save(ctx, skillTx.record); err != nil {
			return errors.Join(
				err,
				restoreSkillRecord(u.skills, characterID, skills, skillExists, true),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	return nil
}

type memorySkillTransaction struct {
	characterID string
	record      repository.SkillRecord
	exists      bool
	dirty       bool
}

func (tx *memorySkillTransaction) Load(ctx context.Context, characterID string) (repository.SkillRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.characterID, characterID); err != nil {
		return repository.SkillRecord{}, false, err
	}
	return repository.CloneSkill(tx.record), tx.exists, nil
}

func (tx *memorySkillTransaction) Save(ctx context.Context, record repository.SkillRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	tx.record = repository.CloneSkill(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func (tx *memorySkillTransaction) SaveFields(ctx context.Context, record repository.SkillRecord, fields ...repository.SkillField) error {
	return tx.Save(ctx, record)
}

func (u *memoryCharacterItemUnitOfWork) WithinCharacterItems(ctx context.Context, characterID string, apply func(repository.InventoryRepository, repository.EquipmentRepository) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil || u.inventory == nil || u.equipment == nil {
		return repository.ErrCharacterItemTransactionUnavailable
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

	inventory, inventoryExists, err := u.inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	equipment, equipmentExists, err := u.equipment.Load(ctx, characterID)
	if err != nil {
		return err
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
	if err := apply(inventoryTx, equipmentTx); err != nil {
		return err
	}
	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return err
		}
	}
	if equipmentTx.dirty {
		if err := u.equipment.Save(ctx, equipmentTx.record); err != nil {
			if !inventoryTx.dirty {
				return err
			}
			return errors.Join(err, restoreInventoryRecord(u.inventory, characterID, inventory, inventoryExists))
		}
	}
	return nil
}

func (u *memoryCharacterTradeUnitOfWork) WithinCharacterTrade(
	ctx context.Context,
	firstCharacterID string,
	secondCharacterID string,
	apply func(repository.CharacterRepository, repository.InventoryRepository) error,
) error {
	firstCharacterID = strings.TrimSpace(firstCharacterID)
	secondCharacterID = strings.TrimSpace(secondCharacterID)
	if firstCharacterID == "" || secondCharacterID == "" || firstCharacterID == secondCharacterID ||
		apply == nil || u == nil || u.character == nil || u.inventory == nil {
		return repository.ErrCharacterTradeTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ids := []string{firstCharacterID, secondCharacterID}
	sort.Strings(ids)

	lock := &u.mu
	if u.sharedMu != nil {
		lock = u.sharedMu
	}
	lock.Lock()
	defer lock.Unlock()

	characterRecords := make(map[string]repository.CharacterRecord, 2)
	characterExists := make(map[string]bool, 2)
	inventoryRecords := make(map[string]repository.InventoryRecord, 2)
	inventoryExists := make(map[string]bool, 2)
	for _, characterID := range ids {
		character, found, err := u.character.Load(ctx, characterID)
		if err != nil {
			return err
		}
		characterRecords[characterID] = repository.CloneCharacter(character)
		characterExists[characterID] = found
		inventory, found, err := u.inventory.Load(ctx, characterID)
		if err != nil {
			return err
		}
		inventoryRecords[characterID] = repository.CloneInventory(inventory)
		inventoryExists[characterID] = found
	}
	allowed := map[string]struct{}{ids[0]: {}, ids[1]: {}}
	characterTx := &memoryCharacterTradeCharacterTransaction{
		allowed: allowed,
		records: characterRecords,
		exists:  characterExists,
		dirty:   make(map[string]bool, 2),
	}
	inventoryTx := &memoryCharacterTradeInventoryTransaction{
		allowed: allowed,
		records: inventoryRecords,
		exists:  inventoryExists,
		dirty:   make(map[string]bool, 2),
	}
	if err := apply(characterTx, inventoryTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	type committedTradeRecord struct {
		characterID string
		wallet      bool
	}
	committed := make([]committedTradeRecord, 0, 4)
	rollback := func(cause error) error {
		errs := []error{cause}
		for index := len(committed) - 1; index >= 0; index-- {
			row := committed[index]
			if row.wallet {
				errs = append(errs, restoreCharacterRecord(u.character, row.characterID, characterRecords[row.characterID], characterExists[row.characterID], true))
			} else {
				errs = append(errs, restoreInventoryRecord(u.inventory, row.characterID, inventoryRecords[row.characterID], inventoryExists[row.characterID]))
			}
		}
		return errors.Join(errs...)
	}
	for _, characterID := range ids {
		if !characterTx.dirty[characterID] {
			continue
		}
		if err := u.character.Save(ctx, characterTx.records[characterID]); err != nil {
			return rollback(err)
		}
		committed = append(committed, committedTradeRecord{characterID: characterID, wallet: true})
	}
	for _, characterID := range ids {
		if !inventoryTx.dirty[characterID] {
			continue
		}
		if err := u.inventory.Save(ctx, inventoryTx.records[characterID]); err != nil {
			return rollback(err)
		}
		committed = append(committed, committedTradeRecord{characterID: characterID})
	}
	return nil
}

type memoryCharacterTradeCharacterTransaction struct {
	allowed map[string]struct{}
	records map[string]repository.CharacterRecord
	exists  map[string]bool
	dirty   map[string]bool
}

func (tx *memoryCharacterTradeCharacterTransaction) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if err := transactionContextError(ctx); err != nil {
		return repository.CharacterRecord{}, false, err
	}
	if _, allowed := tx.allowed[characterID]; !allowed {
		return repository.CharacterRecord{}, false, db.ErrRecordKeyRequired
	}
	return repository.CloneCharacter(tx.records[characterID]), tx.exists[characterID], nil
}

func (tx *memoryCharacterTradeCharacterTransaction) Save(ctx context.Context, record repository.CharacterRecord) error {
	characterID := strings.TrimSpace(record.CharacterID)
	if err := transactionContextError(ctx); err != nil {
		return err
	}
	if _, allowed := tx.allowed[characterID]; !allowed {
		return db.ErrRecordKeyRequired
	}
	tx.records[characterID] = repository.CloneCharacter(record)
	tx.exists[characterID] = true
	tx.dirty[characterID] = true
	return nil
}

func (tx *memoryCharacterTradeCharacterTransaction) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, repository.ErrCharacterTradeTransactionUnavailable
}

func (tx *memoryCharacterTradeCharacterTransaction) FindIDByName(context.Context, string) (string, bool, error) {
	return "", false, repository.ErrCharacterTradeTransactionUnavailable
}

func (tx *memoryCharacterTradeCharacterTransaction) NextNumericID(context.Context) (int, error) {
	return 0, repository.ErrCharacterTradeTransactionUnavailable
}

type memoryCharacterTradeInventoryTransaction struct {
	allowed map[string]struct{}
	records map[string]repository.InventoryRecord
	exists  map[string]bool
	dirty   map[string]bool
}

func (tx *memoryCharacterTradeInventoryTransaction) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if err := transactionContextError(ctx); err != nil {
		return repository.InventoryRecord{}, false, err
	}
	if _, allowed := tx.allowed[characterID]; !allowed {
		return repository.InventoryRecord{}, false, db.ErrRecordKeyRequired
	}
	return repository.CloneInventory(tx.records[characterID]), tx.exists[characterID], nil
}

func (tx *memoryCharacterTradeInventoryTransaction) Save(ctx context.Context, record repository.InventoryRecord) error {
	characterID := strings.TrimSpace(record.CharacterID)
	if err := transactionContextError(ctx); err != nil {
		return err
	}
	if _, allowed := tx.allowed[characterID]; !allowed {
		return db.ErrRecordKeyRequired
	}
	tx.records[characterID] = repository.CloneInventory(record)
	tx.exists[characterID] = true
	tx.dirty[characterID] = true
	return nil
}

func (u *memoryCharacterAssetUnitOfWork) WithinCharacterAssets(ctx context.Context, characterID string, apply func(repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil || u.character == nil || u.inventory == nil || u.equipment == nil {
		return repository.ErrCharacterAssetTransactionUnavailable
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
	if !characterExists {
		return repository.ErrCharacterAssetTransactionUnavailable
	}
	inventory, inventoryExists, err := u.inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	equipment, equipmentExists, err := u.equipment.Load(ctx, characterID)
	if err != nil {
		return err
	}
	characterTx := &memoryCharacterTransaction{characterID: characterID, record: repository.CloneCharacter(character), exists: true}
	inventoryTx := &memoryInventoryTransaction{characterID: characterID, record: repository.CloneInventory(inventory), exists: inventoryExists}
	equipmentTx := &memoryEquipmentTransaction{characterID: characterID, record: repository.CloneEquipment(equipment), exists: equipmentExists}
	if err := apply(characterTx, inventoryTx, equipmentTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if characterTx.dirty {
		if err := u.character.Save(ctx, characterTx.record); err != nil {
			return errors.Join(err, restoreCharacterRecord(u.character, characterID, character, characterExists, true))
		}
	}
	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, true),
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
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	return nil
}

func (u *memoryRentalAssetUnitOfWork) WithinRentalAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(repository.AccountRepository, repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" || characterID == "" || apply == nil || u == nil ||
		u.account == nil || u.character == nil || u.inventory == nil || u.equipment == nil {
		return repository.ErrRentalAssetTransactionUnavailable
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

	account, accountExists, err := u.account.Load(ctx, accountID)
	if err != nil {
		return err
	}
	character, characterExists, err := u.character.Load(ctx, characterID)
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

	accountTx := &memoryAccountTransaction{accountID: accountID, record: repository.CloneAccount(account), exists: accountExists}
	characterTx := &memoryCharacterTransaction{characterID: characterID, record: repository.CloneCharacter(character), exists: characterExists}
	inventoryTx := &memoryInventoryTransaction{characterID: characterID, record: repository.CloneInventory(inventory), exists: inventoryExists}
	equipmentTx := &memoryEquipmentTransaction{characterID: characterID, record: repository.CloneEquipment(equipment), exists: equipmentExists}
	if err := apply(accountTx, characterTx, inventoryTx, equipmentTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if accountTx.dirty {
		if err := u.account.Save(ctx, accountTx.record); err != nil {
			return errors.Join(err, restoreAccountRecord(u.account, accountID, account, accountExists, true))
		}
	}
	if characterTx.dirty {
		if err := u.character.Save(ctx, characterTx.record); err != nil {
			return errors.Join(
				err,
				restoreCharacterRecord(u.character, characterID, character, characterExists, true),
				restoreAccountRecord(u.account, accountID, account, accountExists, accountTx.dirty),
			)
		}
	}
	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, true),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
				restoreAccountRecord(u.account, accountID, account, accountExists, accountTx.dirty),
			)
		}
	}
	if equipmentTx.dirty {
		if err := u.equipment.Save(ctx, equipmentTx.record); err != nil {
			return errors.Join(
				err,
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, true),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
				restoreAccountRecord(u.account, accountID, account, accountExists, accountTx.dirty),
			)
		}
	}
	return nil
}

func (u *memoryCeraShopAssetUnitOfWork) WithinCeraShopAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	settingsScope string,
	apply func(repository.AccountRepository, repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository, repository.SettingsRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	settingsScope = strings.TrimSpace(settingsScope)
	if accountID == "" || characterID == "" || settingsScope == "" || apply == nil || u == nil ||
		u.account == nil || u.character == nil || u.inventory == nil || u.equipment == nil || u.settings == nil {
		return repository.ErrCeraShopAssetTransactionUnavailable
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

	account, accountExists, err := u.account.Load(ctx, accountID)
	if err != nil {
		return err
	}
	character, characterExists, err := u.character.Load(ctx, characterID)
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
	settings, settingsExists, err := u.settings.Load(ctx, settingsScope)
	if err != nil {
		return err
	}

	accountTx := &memoryAccountTransaction{accountID: accountID, record: repository.CloneAccount(account), exists: accountExists}
	characterTx := &memoryCharacterTransaction{characterID: characterID, record: repository.CloneCharacter(character), exists: characterExists}
	inventoryTx := &memoryInventoryTransaction{characterID: characterID, record: repository.CloneInventory(inventory), exists: inventoryExists}
	equipmentTx := &memoryEquipmentTransaction{characterID: characterID, record: repository.CloneEquipment(equipment), exists: equipmentExists}
	settingsTx := &memorySettingsTransaction{scope: settingsScope, record: repository.CloneSettings(settings), exists: settingsExists}
	if err := apply(accountTx, characterTx, inventoryTx, equipmentTx, settingsTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if accountTx.dirty {
		if err := u.account.Save(ctx, accountTx.record); err != nil {
			return errors.Join(err, restoreAccountRecord(u.account, accountID, account, accountExists, true))
		}
	}
	if characterTx.dirty {
		if err := u.character.Save(ctx, characterTx.record); err != nil {
			return errors.Join(
				err,
				restoreCharacterRecord(u.character, characterID, character, characterExists, true),
				restoreAccountRecord(u.account, accountID, account, accountExists, accountTx.dirty),
			)
		}
	}
	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, true),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
				restoreAccountRecord(u.account, accountID, account, accountExists, accountTx.dirty),
			)
		}
	}
	if equipmentTx.dirty {
		if err := u.equipment.Save(ctx, equipmentTx.record); err != nil {
			return errors.Join(
				err,
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, true),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
				restoreAccountRecord(u.account, accountID, account, accountExists, accountTx.dirty),
			)
		}
	}
	if settingsTx.dirty {
		if err := u.settings.Save(ctx, settingsTx.record); err != nil {
			return errors.Join(
				err,
				restoreSettingsRecord(u.settings, settingsScope, settings, settingsExists, true),
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, equipmentTx.dirty),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
				restoreCharacterRecord(u.character, characterID, character, characterExists, characterTx.dirty),
				restoreAccountRecord(u.account, accountID, account, accountExists, accountTx.dirty),
			)
		}
	}
	return nil
}

type memoryAccountTransaction struct {
	accountID string
	record    repository.AccountRecord
	exists    bool
	dirty     bool
}

func (tx *memoryAccountTransaction) Load(ctx context.Context, accountID string) (repository.AccountRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.accountID, accountID); err != nil {
		return repository.AccountRecord{}, false, err
	}
	return repository.CloneAccount(tx.record), tx.exists, nil
}

func (tx *memoryAccountTransaction) Save(ctx context.Context, record repository.AccountRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.accountID, record.AccountID); err != nil {
		return err
	}
	tx.record = repository.CloneAccount(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

type memoryCharacterTransaction struct {
	characterID string
	record      repository.CharacterRecord
	exists      bool
	dirty       bool
}

type memorySettingsTransaction struct {
	scope  string
	record repository.SettingsRecord
	exists bool
	dirty  bool
}

func (tx *memorySettingsTransaction) Load(ctx context.Context, scope string) (repository.SettingsRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.scope, scope); err != nil {
		return repository.SettingsRecord{}, false, err
	}
	return repository.CloneSettings(tx.record), tx.exists, nil
}

func (tx *memorySettingsTransaction) Save(ctx context.Context, record repository.SettingsRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.scope, record.Scope); err != nil {
		return err
	}
	tx.record = repository.CloneSettings(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func (tx *memoryCharacterTransaction) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.characterID, characterID); err != nil {
		return repository.CharacterRecord{}, false, err
	}
	return repository.CloneCharacter(tx.record), tx.exists, nil
}

func (tx *memoryCharacterTransaction) Save(ctx context.Context, record repository.CharacterRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	tx.record = repository.CloneCharacter(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func (tx *memoryCharacterTransaction) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, repository.ErrCharacterAssetTransactionUnavailable
}

func (tx *memoryCharacterTransaction) FindIDByName(context.Context, string) (string, bool, error) {
	return "", false, repository.ErrCharacterAssetTransactionUnavailable
}

func (tx *memoryCharacterTransaction) NextNumericID(context.Context) (int, error) {
	return 0, repository.ErrCharacterAssetTransactionUnavailable
}

func restoreCharacterRecord(repo repository.CharacterRepository, characterID string, record repository.CharacterRecord, existed bool, dirty bool) error {
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
		return errors.New("character rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), characterID)
}

func restoreAccountRecord(repo repository.AccountRepository, accountID string, record repository.AccountRecord, existed bool, dirty bool) error {
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
		return errors.New("account rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), accountID)
}

func restoreSettingsRecord(repo repository.SettingsRepository, scope string, record repository.SettingsRecord, existed bool, dirty bool) error {
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
		return errors.New("settings rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), scope)
}

func restoreSkillRecord(repo repository.SkillRepository, characterID string, record repository.SkillRecord, existed bool, dirty bool) error {
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
		return errors.New("skill rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), characterID)
}

func restoreInventoryRecordIfDirty(repo repository.InventoryRepository, characterID string, record repository.InventoryRecord, existed bool, dirty bool) error {
	if !dirty {
		return nil
	}
	return restoreInventoryRecord(repo, characterID, record, existed)
}

func restoreEquipmentRecordIfDirty(repo repository.EquipmentRepository, characterID string, record repository.EquipmentRecord, existed bool, dirty bool) error {
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
		return errors.New("equipment rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), characterID)
}

func restoreInventoryRecord(repo repository.InventoryRepository, characterID string, record repository.InventoryRecord, existed bool) error {
	if existed {
		return repo.Save(context.Background(), record)
	}
	deleter, ok := repo.(interface {
		Delete(context.Context, string) error
	})
	if !ok {
		return errors.New("inventory rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), characterID)
}

type memoryInventoryTransaction struct {
	characterID string
	record      repository.InventoryRecord
	exists      bool
	dirty       bool
}

func (tx *memoryInventoryTransaction) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.characterID, characterID); err != nil {
		return repository.InventoryRecord{}, false, err
	}
	return repository.CloneInventory(tx.record), tx.exists, nil
}

func (tx *memoryInventoryTransaction) Save(ctx context.Context, record repository.InventoryRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	tx.record = repository.CloneInventory(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

type memoryEquipmentTransaction struct {
	characterID string
	record      repository.EquipmentRecord
	exists      bool
	dirty       bool
}

func (tx *memoryEquipmentTransaction) Load(ctx context.Context, characterID string) (repository.EquipmentRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.characterID, characterID); err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	return repository.CloneEquipment(tx.record), tx.exists, nil
}

func (tx *memoryEquipmentTransaction) Save(ctx context.Context, record repository.EquipmentRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	tx.record = repository.CloneEquipment(record)
	tx.exists = true
	tx.dirty = true
	return nil
}
