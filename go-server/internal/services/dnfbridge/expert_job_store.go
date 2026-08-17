package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentExpertJobDisjointStoreKind byte = 0
	currentExpertJobEnchantStoreKind  byte = 3

	currentExpertJobStoreCreateNotification  uint16 = 538
	currentExpertJobStoreCloseNotification   uint16 = 539
	currentExpertJobStoreUpdateNotification  uint16 = 544
	currentExpertJobEnchantOwnerNotification uint16 = 533
	currentExpertJobStoreVisibleAttachMode   byte   = 2
)

type currentExpertJobStore struct {
	OwnerCharacterID uint16
	OwnerSession     *gameSession
	Kind             byte
	Name             []byte
	Cost             int64
	TownID           byte
	AreaID           byte
	PositionX        int16
	PositionY        int16
	OpaqueObjectLink uint16
	MachineGrade     int
	Endurance        int64
	Qualifications   []byte
}

type currentExpertJobStoreCreateRequest struct {
	Kind             byte
	Name             []byte
	Cost             int64
	PositionX        int16
	PositionY        int16
	OpaqueObjectLink uint16
}

type currentExpertJobStoreDisjointRequest struct {
	OwnerID    uint16
	TargetSlot int16
	TargetList byte
}

type currentExpertJobStoreEnchantRequest struct {
	OwnerID    uint16
	RecipeID   int64
	TargetList byte
	TargetSlot int16
	CardList   byte
	CardSlot   int16
}

type currentExpertJobStoreUseResult struct {
	RequesterGold  int64
	OwnerGold      int64
	Endurance      int64
	Experience     int64
	LevelChanged   bool
	Success        bool
	TargetList     byte
	TargetSlot     int16
	Materials      []currentExpertJobExtractionMaterial
	Qualifications []byte
}

func parseCurrentExpertJobStoreCreateRequest(body []byte) (currentExpertJobStoreCreateRequest, error) {
	if len(body) < 15 {
		return currentExpertJobStoreCreateRequest{}, fmt.Errorf("expert store create body=%d", len(body))
	}
	nameLength := int(int32(binary.LittleEndian.Uint32(body[1:5])))
	if nameLength < 0 || nameLength > math.MaxUint8 || len(body) != 15+nameLength {
		return currentExpertJobStoreCreateRequest{}, errors.New("expert store name boundary is invalid")
	}
	offset := 5 + nameLength
	request := currentExpertJobStoreCreateRequest{
		Kind: body[0], Name: append([]byte(nil), body[5:offset]...),
		Cost:             int64(binary.LittleEndian.Uint32(body[offset : offset+4])),
		PositionX:        int16(binary.LittleEndian.Uint16(body[offset+4 : offset+6])),
		PositionY:        int16(binary.LittleEndian.Uint16(body[offset+6 : offset+8])),
		OpaqueObjectLink: binary.LittleEndian.Uint16(body[offset+8 : offset+10]),
	}
	if nameLength == 0 || (request.Kind != currentExpertJobDisjointStoreKind && request.Kind != currentExpertJobEnchantStoreKind) {
		return currentExpertJobStoreCreateRequest{}, errors.New("expert store create values are invalid")
	}
	return request, nil
}

func parseCurrentExpertJobStoreDisjointRequest(body []byte) (currentExpertJobStoreDisjointRequest, error) {
	if len(body) != 5 {
		return currentExpertJobStoreDisjointRequest{}, fmt.Errorf("store disjoint body=%d", len(body))
	}
	request := currentExpertJobStoreDisjointRequest{OwnerID: binary.LittleEndian.Uint16(body[0:2]), TargetSlot: int16(binary.LittleEndian.Uint16(body[2:4])), TargetList: body[4]}
	if request.OwnerID == 0 || request.TargetSlot < 0 || request.TargetList != dnfrepo.MainInventoryListType {
		return currentExpertJobStoreDisjointRequest{}, errors.New("store disjoint values are invalid")
	}
	return request, nil
}

func parseCurrentExpertJobStoreEnchantRequest(body []byte) (currentExpertJobStoreEnchantRequest, error) {
	if len(body) != 13 {
		return currentExpertJobStoreEnchantRequest{}, fmt.Errorf("store enchant body=%d", len(body))
	}
	request := currentExpertJobStoreEnchantRequest{
		OwnerID: binary.LittleEndian.Uint16(body[0:2]), RecipeID: int64(int32(binary.LittleEndian.Uint32(body[2:6]))),
		TargetList: body[7], TargetSlot: int16(binary.LittleEndian.Uint16(body[8:10])), CardList: body[10], CardSlot: int16(binary.LittleEndian.Uint16(body[11:13])),
	}
	if request.OwnerID == 0 || request.RecipeID <= 0 || body[6] != 2 || request.TargetList != dnfrepo.MainInventoryListType || request.CardList != dnfrepo.MainInventoryListType || request.TargetSlot < 0 || request.CardSlot < 0 || request.TargetSlot == request.CardSlot {
		return currentExpertJobStoreEnchantRequest{}, errors.New("store enchant values are invalid")
	}
	return request, nil
}

func (s *Service) handleCurrentExpertJobStoreCreate(session *gameSession, opcode uint16, body []byte) error {
	request, err := parseCurrentExpertJobStoreCreateRequest(body)
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	s.expertJobStoreOpMu.Lock()
	defer s.expertJobStoreOpMu.Unlock()
	store, err := s.createCurrentExpertJobStore(session, request)
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, currentExpertJobStoreError(err))
	}
	createBody := buildCurrentExpertJobStoreCreateNotification(store)
	if err := s.sendGameUpperRawClass(session, opcode, []byte{1}, dnfproto.DefaultChannelClassification); err != nil {
		s.removeCurrentExpertJobStore(store.OwnerCharacterID, store.OwnerSession)
		return err
	}
	// op598 creates the owner's local object. NOTI544 attaches enchanter
	// metadata to that object, while NOTI538 is strictly the peer projection.
	if store.Kind == currentExpertJobEnchantStoreKind {
		if err := s.sendGameUpperRawClass(session, currentExpertJobStoreUpdateNotification, buildCurrentExpertJobStoreUpdateNotification(store), 0); err != nil {
			s.removeCurrentExpertJobStore(store.OwnerCharacterID, store.OwnerSession)
			return err
		}
	}
	for _, peer := range s.onlinePlayers.PeersInSameArea(store.OwnerCharacterID) {
		if peer.Session != nil {
			_ = s.sendGameUpperRawClass(peer.Session, currentExpertJobStoreCreateNotification, createBody, 0)
		}
	}
	return nil
}

func (s *Service) createCurrentExpertJobStore(session *gameSession, request currentExpertJobStoreCreateRequest) (*currentExpertJobStore, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || s.onlinePlayers == nil || currentExpertJobSessionInDungeon(session) || runtimePartyStateSnapshot(session).PartyID != 0 {
		return nil, dnfexpertjob.ErrMachineInvalid
	}
	player, ok := s.onlinePlayers.PlayerForCharacter(session.selectedCharacterID)
	if !ok || player.Session != session || player.TownID == 0 {
		return nil, dnfexpertjob.ErrMachineInvalid
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return nil, dnfexpertjob.ErrOwnerUnavailable
	}
	catalog, err := s.currentExpertJobCatalog()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil || !found || character.CharacterID != characterID {
		return nil, dnfexpertjob.ErrCharacterNotFound
	}
	jobType, experience, err := currentExpertJobCharacterState(&character)
	if err != nil {
		return nil, err
	}
	store := &currentExpertJobStore{OwnerCharacterID: session.selectedCharacterID, OwnerSession: session, Kind: request.Kind, Name: request.Name, Cost: request.Cost, TownID: player.TownID, AreaID: player.AreaID, PositionX: request.PositionX, PositionY: request.PositionY, OpaqueObjectLink: request.OpaqueObjectLink}
	switch request.Kind {
	case currentExpertJobDisjointStoreKind:
		config, ok := catalog.Disjointer()
		if !ok || jobType != dnfexpertjob.DisjointerType || request.Cost > config.MaximumStoreCharge {
			return nil, dnfexpertjob.ErrMachineInvalid
		}
		store.MachineGrade = int(currentExpertJobStatDefault(character.Stats, currentExpertJobMachineGradeStat, 1))
		store.Endurance = currentExpertJobStatDefault(character.Stats, currentExpertJobMachineEnduranceStat, config.InitialEndurance)
		if _, ok := config.RepairRule(store.MachineGrade); !ok || store.Endurance <= 0 {
			return nil, dnfexpertjob.ErrMachineEndurance
		}
	case currentExpertJobEnchantStoreKind:
		config, ok := catalog.Enchanter()
		if !ok || jobType != dnfexpertjob.EnchanterType || request.Cost > config.MaximumStoreCharge {
			return nil, dnfexpertjob.ErrMachineInvalid
		}
		store.Endurance = currentExpertJobStatDefault(character.Stats, currentExpertJobMachineEnduranceStat, config.InitialEndurance)
		store.Qualifications = config.Qualifications(experience)
		if store.Endurance <= 0 {
			return nil, dnfexpertjob.ErrMachineEndurance
		}
	default:
		return nil, dnfexpertjob.ErrMachineInvalid
	}
	s.expertJobStoreMu.Lock()
	defer s.expertJobStoreMu.Unlock()
	if s.expertJobStores == nil {
		s.expertJobStores = make(map[uint16]*currentExpertJobStore)
	}
	if _, exists := s.expertJobStores[store.OwnerCharacterID]; exists {
		return nil, dnfexpertjob.ErrMachineInvalid
	}
	s.expertJobStores[store.OwnerCharacterID] = store
	return cloneCurrentExpertJobStore(store), nil
}

func (s *Service) handleCurrentExpertJobStoreEnter(session *gameSession, opcode uint16, body []byte) error {
	if len(body) != 2 || session == nil || session.selectedCharacterID == 0 || currentExpertJobSessionInDungeon(session) {
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	ownerID := binary.LittleEndian.Uint16(body)
	store, ok := s.enterCurrentExpertJobStore(session, ownerID)
	if !ok {
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	return s.sendGameUpperRawClass(session, opcode, buildCurrentExpertJobStoreEnterSuccess(store), dnfproto.DefaultChannelClassification)
}

func (s *Service) enterCurrentExpertJobStore(session *gameSession, ownerID uint16) (*currentExpertJobStore, bool) {
	if s == nil || session == nil || ownerID == 0 || s.onlinePlayers == nil {
		return nil, false
	}
	visitor, visitorOK := s.onlinePlayers.PlayerForCharacter(session.selectedCharacterID)
	s.expertJobStoreMu.Lock()
	defer s.expertJobStoreMu.Unlock()
	store := s.expertJobStores[ownerID]
	if !visitorOK || visitor.Session != session || store == nil || store.OwnerSession == nil || store.TownID != visitor.TownID || store.AreaID != visitor.AreaID {
		return nil, false
	}
	if s.expertJobVisitors == nil {
		s.expertJobVisitors = make(map[*gameSession]uint16)
	}
	s.expertJobVisitors[session] = ownerID
	return cloneCurrentExpertJobStore(store), true
}

func (s *Service) handleCurrentExpertJobStoreClose(session *gameSession, opcode uint16, body []byte) error {
	if len(body) != 0 || session == nil || session.selectedCharacterID == 0 {
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	s.expertJobStoreOpMu.Lock()
	defer s.expertJobStoreOpMu.Unlock()
	store := s.removeCurrentExpertJobStore(session.selectedCharacterID, session)
	if store != nil {
		s.broadcastCurrentExpertJobStoreClose(store, true)
		return nil
	}
	s.expertJobStoreMu.Lock()
	_, visitor := s.expertJobVisitors[session]
	delete(s.expertJobVisitors, session)
	s.expertJobStoreMu.Unlock()
	if visitor {
		return nil
	}
	return s.sendGameUpperFailure(session, opcode, 19)
}

func (s *Service) removeCurrentExpertJobStore(ownerID uint16, ownerSession *gameSession) *currentExpertJobStore {
	if s == nil || ownerID == 0 {
		return nil
	}
	s.expertJobStoreMu.Lock()
	defer s.expertJobStoreMu.Unlock()
	store := s.expertJobStores[ownerID]
	if store == nil || (ownerSession != nil && store.OwnerSession != ownerSession) {
		return nil
	}
	delete(s.expertJobStores, ownerID)
	for visitor, enteredOwner := range s.expertJobVisitors {
		if enteredOwner == ownerID {
			delete(s.expertJobVisitors, visitor)
		}
	}
	return cloneCurrentExpertJobStore(store)
}

func (s *Service) closeCurrentExpertJobStoreSession(session *gameSession, includeOwner bool) {
	if s == nil || session == nil {
		return
	}
	s.expertJobStoreOpMu.Lock()
	defer s.expertJobStoreOpMu.Unlock()
	store := s.removeCurrentExpertJobStore(session.selectedCharacterID, session)
	s.expertJobStoreMu.Lock()
	delete(s.expertJobVisitors, session)
	s.expertJobStoreMu.Unlock()
	if store != nil {
		s.broadcastCurrentExpertJobStoreClose(store, includeOwner)
	}
}

func (s *Service) broadcastCurrentExpertJobStoreClose(store *currentExpertJobStore, includeOwner bool) {
	if s == nil || store == nil || s.onlinePlayers == nil {
		return
	}
	body := buildCurrentExpertJobStoreCloseNotification(store.OwnerCharacterID)
	for _, player := range s.onlinePlayers.GetAreaPlayers(store.TownID, store.AreaID) {
		if player.Session == nil || (!includeOwner && player.CharacterID == store.OwnerCharacterID) {
			continue
		}
		_ = s.sendGameUpperRawClass(player.Session, currentExpertJobStoreCloseNotification, body, 0)
	}
}

func (s *Service) replayCurrentExpertJobStores(session *gameSession, townID, areaID byte) {
	if s == nil || session == nil {
		return
	}
	s.expertJobStoreMu.Lock()
	stores := make([]*currentExpertJobStore, 0)
	for _, store := range s.expertJobStores {
		if store.TownID == townID && store.AreaID == areaID && store.OwnerSession != session {
			stores = append(stores, cloneCurrentExpertJobStore(store))
		}
	}
	s.expertJobStoreMu.Unlock()
	for _, store := range stores {
		_ = s.sendGameUpperRawClass(session, currentExpertJobStoreCreateNotification, buildCurrentExpertJobStoreCreateNotification(store), 0)
	}
}

func cloneCurrentExpertJobStore(store *currentExpertJobStore) *currentExpertJobStore {
	if store == nil {
		return nil
	}
	clone := *store
	clone.Name = append([]byte(nil), store.Name...)
	clone.Qualifications = append([]byte(nil), store.Qualifications...)
	return &clone
}

func currentExpertJobSessionInDungeon(session *gameSession) bool {
	if session == nil {
		return true
	}
	session.dungeon.mu.Lock()
	active := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	return active
}

func buildCurrentExpertJobStoreCreateNotification(store *currentExpertJobStore) []byte {
	w := packetWriter{}
	w.writeByte(store.Kind)
	w.writeUint16(store.OwnerCharacterID)
	w.writeRawDstr(store.Name)
	w.writeByte(store.TownID)
	w.writeUint32(uint32(store.AreaID))
	w.writeUint16(uint16(store.PositionX))
	w.writeUint16(uint16(store.PositionY))
	w.writeUint32(uint32(store.Cost))
	w.writeByte(currentExpertJobStoreVisibleAttachMode)
	if store.Kind == currentExpertJobEnchantStoreKind {
		w.writeByte(byte(len(store.Qualifications)))
		w.writeBytes(store.Qualifications)
	}
	return w.bytes()
}

func buildCurrentExpertJobStoreUpdateNotification(store *currentExpertJobStore) []byte {
	w := packetWriter{}
	w.writeByte(store.Kind)
	w.writeUint16(store.OwnerCharacterID)
	w.writeRawDstr(store.Name)
	if store.Kind == currentExpertJobEnchantStoreKind {
		w.writeByte(byte(len(store.Qualifications)))
		w.writeBytes(store.Qualifications)
	}
	return w.bytes()
}

func (s *Service) broadcastCurrentExpertJobStoreUpdate(store *currentExpertJobStore) {
	if s == nil || store == nil || s.onlinePlayers == nil {
		return
	}
	body := buildCurrentExpertJobStoreUpdateNotification(store)
	for _, player := range s.onlinePlayers.GetAreaPlayers(store.TownID, store.AreaID) {
		if player.Session != nil {
			_ = s.sendGameUpperRawClass(player.Session, currentExpertJobStoreUpdateNotification, body, 0)
		}
	}
}

func buildCurrentExpertJobStoreCloseNotification(ownerID uint16) []byte {
	w := packetWriter{}
	w.writeUint16(ownerID)
	return w.bytes()
}

func buildCurrentExpertJobStoreEnterSuccess(store *currentExpertJobStore) []byte {
	w := packetWriter{}
	w.writeByte(1)
	w.writeByte(store.Kind)
	if store.Kind == currentExpertJobEnchantStoreKind {
		w.writeUint16(store.OwnerCharacterID)
		w.writeInt32(int(store.Endurance))
	} else {
		w.writeByte(byte(store.MachineGrade))
		w.writeInt32(int(store.Cost))
		w.writeInt32(int(store.Endurance))
	}
	return w.bytes()
}

func currentExpertJobStoreError(err error) byte {
	switch {
	case errors.Is(err, dnfexpertjob.ErrInsufficientGold):
		return 21
	case errors.Is(err, dnfexpertjob.ErrLevelTooLow):
		return 14
	case errors.Is(err, dnfexpertjob.ErrMachineEndurance):
		return 189
	case errors.Is(err, dnfexpertjob.ErrMachineGradeTooLow):
		return 0xD4
	case errors.Is(err, errDungeonPickupInventoryFull), errors.Is(err, errCurrentDisjointRewardInvalid):
		return 4
	default:
		return 19
	}
}

func (s *Service) handleCurrentExpertJobStoreRepair(session *gameSession, opcode uint16, body []byte) error {
	if len(body) != 0 {
		return s.sendGameUpperFailure(session, opcode, 60)
	}
	s.expertJobStoreOpMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	result, jobType, err := s.commitCurrentExpertJobMachineRepair(ctx, session)
	cancel()
	s.expertJobStoreOpMu.Unlock()
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, currentExpertJobRepairError(err))
	}
	w := packetWriter{}
	w.writeByte(1)
	w.writeInt32(int(result.Gold))
	w.writeInt32(int(result.Endurance))
	if err := s.sendGameUpperRawClass(session, opcode, w.bytes(), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	return s.sendCurrentExpertJobInfoFromRepository(session, jobType, true)
}

func (s *Service) commitCurrentExpertJobMachineRepair(ctx context.Context, session *gameSession) (dnfexpertjob.MachineRepairPlan, byte, error) {
	jobCatalog, _, owner, accountID, characterID, err := s.currentExpertJobMutationContext(session)
	if err != nil || currentExpertJobSessionInDungeon(session) {
		return dnfexpertjob.MachineRepairPlan{}, 0, dnfexpertjob.ErrMachineInvalid
	}
	s.expertJobStoreMu.Lock()
	_, busy := s.expertJobStores[session.selectedCharacterID]
	s.expertJobStoreMu.Unlock()
	if busy {
		return dnfexpertjob.MachineRepairPlan{}, 0, dnfexpertjob.ErrMachineInvalid
	}
	var result dnfexpertjob.MachineRepairPlan
	var resultJobType byte
	err = owner.Machine(ctx, dnfexpertjob.Command{AccountID: accountID, CharacterID: characterID, UpdatedAt: s.gameplayNow(), Project: func(assets *dnfexpertjob.Assets) (dnfexpertjob.Changes, error) {
		jobType, experience, stateErr := currentExpertJobCharacterState(assets.Character)
		if stateErr != nil || (jobType != dnfexpertjob.EnchanterType && jobType != dnfexpertjob.DisjointerType) {
			return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
		}
		grade := 1
		var rule dnfexpertjob.RepairRule
		var configured bool
		initial := int64(0)
		if jobType == dnfexpertjob.DisjointerType {
			config, ok := jobCatalog.Disjointer()
			if !ok {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
			}
			grade = int(currentExpertJobStatDefault(assets.Character.Stats, currentExpertJobMachineGradeStat, 1))
			rule, configured = config.RepairRule(grade)
			initial = config.InitialEndurance
		} else {
			config, ok := jobCatalog.Enchanter()
			if !ok {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
			}
			level := config.Recipes.Level(experience)
			if level > len(config.RepairRules) {
				level = len(config.RepairRules)
			}
			if level > 0 {
				rule, configured = config.RepairRules[level-1], true
			}
			initial = config.InitialEndurance
		}
		if !configured {
			return dnfexpertjob.Changes{}, dnfexpertjob.ErrMachineInvalid
		}
		gold, goldErr := currentExpertJobWalletGold(assets.Inventory)
		if goldErr != nil {
			return dnfexpertjob.Changes{}, goldErr
		}
		current := currentExpertJobStatDefault(assets.Character.Stats, currentExpertJobMachineEnduranceStat, initial)
		plan, planErr := dnfexpertjob.PlanMachineRepair(gold, current, rule)
		if planErr != nil {
			return dnfexpertjob.Changes{}, planErr
		}
		currentExpertJobSetWalletGold(assets.Inventory, plan.Gold)
		if assets.Character.Stats == nil {
			assets.Character.Stats = make(map[string]int64)
		}
		assets.Character.Stats[currentExpertJobMachineEnduranceStat] = plan.Endurance
		result, resultJobType = plan, jobType
		return dnfexpertjob.Changes{Character: true, Inventory: true}, nil
	}})
	return result, resultJobType, err
}

func (s *Service) handleCurrentExpertJobStoreUpgrade(session *gameSession, opcode uint16, body []byte) error {
	if len(body) != 0 {
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	s.expertJobStoreOpMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	result, err := s.commitCurrentDisjointerMachineUpgrade(ctx, session)
	cancel()
	s.expertJobStoreOpMu.Unlock()
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, currentExpertJobUpgradeError(err))
	}
	w := packetWriter{}
	w.writeByte(1)
	w.writeInt32(int(result.Gold))
	w.writeInt32(result.Grade)
	w.writeInt32(int(result.Endurance))
	if err := s.sendGameUpperRawClass(session, opcode, w.bytes(), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	return s.sendCurrentExpertJobInfoFromRepository(session, dnfexpertjob.DisjointerType, true)
}

func (s *Service) commitCurrentDisjointerMachineUpgrade(ctx context.Context, session *gameSession) (dnfexpertjob.DisjointerUpgradePlan, error) {
	jobCatalog, _, owner, accountID, characterID, err := s.currentExpertJobMutationContext(session)
	if err != nil || currentExpertJobSessionInDungeon(session) {
		return dnfexpertjob.DisjointerUpgradePlan{}, dnfexpertjob.ErrMachineInvalid
	}
	s.expertJobStoreMu.Lock()
	_, busy := s.expertJobStores[session.selectedCharacterID]
	s.expertJobStoreMu.Unlock()
	if busy {
		return dnfexpertjob.DisjointerUpgradePlan{}, dnfexpertjob.ErrMachineInvalid
	}
	var result dnfexpertjob.DisjointerUpgradePlan
	err = owner.Machine(ctx, dnfexpertjob.Command{AccountID: accountID, CharacterID: characterID, UpdatedAt: s.gameplayNow(), Project: func(assets *dnfexpertjob.Assets) (dnfexpertjob.Changes, error) {
		jobType, experience, stateErr := currentExpertJobCharacterState(assets.Character)
		if stateErr != nil || jobType != dnfexpertjob.DisjointerType {
			return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
		}
		config, ok := jobCatalog.Disjointer()
		if !ok {
			return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
		}
		gold, goldErr := currentExpertJobWalletGold(assets.Inventory)
		if goldErr != nil {
			return dnfexpertjob.Changes{}, goldErr
		}
		grade := int(currentExpertJobStatDefault(assets.Character.Stats, currentExpertJobMachineGradeStat, 1))
		endurance := currentExpertJobStatDefault(assets.Character.Stats, currentExpertJobMachineEnduranceStat, config.InitialEndurance)
		plan, planErr := config.PlanUpgrade(gold, experience, endurance, grade, assets.Character.Level)
		if planErr != nil {
			return dnfexpertjob.Changes{}, planErr
		}
		currentExpertJobSetWalletGold(assets.Inventory, plan.Gold)
		if assets.Character.Stats == nil {
			assets.Character.Stats = make(map[string]int64)
		}
		assets.Character.Stats[currentExpertJobMachineGradeStat], assets.Character.Stats[currentExpertJobMachineEnduranceStat] = int64(plan.Grade), plan.Endurance
		result = plan
		return dnfexpertjob.Changes{Character: true, Inventory: true}, nil
	}})
	return result, err
}

func currentExpertJobWalletGold(inventory *dnfrepo.InventoryRecord) (int64, error) {
	if inventory == nil {
		return 0, dnfexpertjob.ErrInventoryNotFound
	}
	wallet, ok := inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, 0)]
	if !ok || wallet.ItemID != 0 || wallet.Count < 0 {
		return 0, dnfexpertjob.ErrInsufficientGold
	}
	return wallet.Count, nil
}

func currentExpertJobSetWalletGold(inventory *dnfrepo.InventoryRecord, gold int64) {
	key := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, 0)
	wallet := inventory.Slots[key]
	wallet.ItemID, wallet.Count = 0, gold
	inventory.Slots[key] = wallet
}

func currentExpertJobRepairError(err error) byte {
	if errors.Is(err, dnfexpertjob.ErrMachineInvalid) {
		return 60
	}
	return 22
}

func currentExpertJobUpgradeError(err error) byte {
	if errors.Is(err, dnfexpertjob.ErrLevelTooLow) {
		return 14
	}
	if errors.Is(err, dnfexpertjob.ErrInsufficientGold) {
		return 22
	}
	return 19
}

func currentExpertJobStoreStringContainsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(needle)) {
			return true
		}
	}
	return false
}

func currentExpertJobStoreIntContains(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func (s *Service) handleCurrentExpertJobStoreDisjoint(session *gameSession, opcode uint16, body []byte) error {
	request, err := parseCurrentExpertJobStoreDisjointRequest(body)
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	s.expertJobStoreOpMu.Lock()
	store, ok := s.currentExpertJobEnteredStore(session, request.OwnerID, currentExpertJobDisjointStoreKind)
	if !ok {
		s.expertJobStoreOpMu.Unlock()
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	result, err := s.commitCurrentExpertJobStoreDisjoint(ctx, session, store, request)
	cancel()
	if err == nil {
		s.updateCurrentExpertJobStoreMachine(store, result.Endurance, store.MachineGrade)
	}
	s.expertJobStoreOpMu.Unlock()
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, currentExpertJobStoreError(err))
	}
	if err := s.sendGameUpperRawClass(session, opcode, buildCurrentExpertJobStoreDisjointSuccessBody(request, result), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentContainerListsRefresh(session, "expert_job_store_disjoint_after_ack"); err != nil {
		return err
	}
	if store.OwnerSession != nil {
		if store.OwnerSession != session {
			_ = s.sendGameUpperRawClass(
				store.OwnerSession,
				uint16(dnfenum.CmdPacketWalkoutPartyMember),
				buildCurrentGoldStateBody(result.OwnerGold),
				0,
			)
		}
		_ = s.sendCurrentExpertJobInfoFromRepository(store.OwnerSession, dnfexpertjob.DisjointerType, true)
	}
	return nil
}

func (s *Service) commitCurrentExpertJobStoreDisjoint(ctx context.Context, session *gameSession, store *currentExpertJobStore, request currentExpertJobStoreDisjointRequest) (currentExpertJobStoreUseResult, error) {
	jobCatalog, err := s.currentExpertJobCatalog()
	if err != nil {
		return currentExpertJobStoreUseResult{}, err
	}
	items, err := s.currentPVFItemCatalog()
	if err != nil {
		return currentExpertJobStoreUseResult{}, err
	}
	var result currentExpertJobStoreUseResult
	err = s.mutateCurrentExpertJobStorePair(ctx, session, store, func(requesterCharacter *dnfrepo.CharacterRecord, requesterInventory *dnfrepo.InventoryRecord, ownerCharacter *dnfrepo.CharacterRecord, ownerInventory *dnfrepo.InventoryRecord) error {
		jobType, experience, stateErr := currentExpertJobCharacterState(ownerCharacter)
		if stateErr != nil || jobType != dnfexpertjob.DisjointerType {
			return dnfexpertjob.ErrJobUnsupported
		}
		config, configured := jobCatalog.Disjointer()
		if !configured {
			return dnfexpertjob.ErrJobUnsupported
		}
		grade := int(currentExpertJobStatDefault(ownerCharacter.Stats, currentExpertJobMachineGradeStat, 1))
		endurance := currentExpertJobStatDefault(ownerCharacter.Stats, currentExpertJobMachineEnduranceStat, config.InitialEndurance)
		if grade != store.MachineGrade || endurance != store.Endurance {
			return dnfexpertjob.ErrMachineInvalid
		}
		targetKey := currentCeraShopInventorySlotKey(request.TargetList, request.TargetSlot)
		target, found := requesterInventory.Slots[targetKey]
		if !found || target.ItemID <= 0 || target.ItemID > math.MaxUint32 || target.Count != 1 || currentNPCShopItemLocked(target) {
			return dnfexpertjob.ErrExtractionInvalid
		}
		metadata, metadataErr := jobCatalog.Equipment(target.ItemID)
		if metadataErr != nil || metadata.DisjointForbidden || metadata.AttachType == "trade delete" {
			return dnfexpertjob.ErrExtractionInvalid
		}
		metadata.State = currentExpertJobEquipmentState(target)
		plan, planErr := jobCatalog.PlanDisjointer(experience, grade, metadata, requesterCharacter.CharacterID == ownerCharacter.CharacterID, rand.IntN)
		if planErr != nil {
			return planErr
		}
		if endurance < plan.EnduranceReduction {
			return dnfexpertjob.ErrMachineEndurance
		}
		requesterGold, goldErr := currentExpertJobWalletGold(requesterInventory)
		if goldErr != nil {
			return goldErr
		}
		ownerGold, ownerGoldErr := currentExpertJobWalletGold(ownerInventory)
		if ownerGoldErr != nil {
			return ownerGoldErr
		}
		self := requesterCharacter.CharacterID == ownerCharacter.CharacterID
		if !self {
			if requesterGold < store.Cost {
				return dnfexpertjob.ErrInsufficientGold
			}
			if ownerGold > math.MaxInt32-store.Cost {
				return dnfexpertjob.ErrInsufficientGold
			}
			requesterGold -= store.Cost
			ownerGold += store.Cost
			currentExpertJobSetWalletGold(requesterInventory, requesterGold)
			currentExpertJobSetWalletGold(ownerInventory, ownerGold)
		}
		delete(requesterInventory.Slots, targetKey)
		for _, reward := range plan.Materials {
			if reward.ItemID <= 0 || reward.ItemID > math.MaxUint32 || reward.Count <= 0 || reward.Count > math.MaxUint32 {
				return dnfexpertjob.ErrExtractionInvalid
			}
			definition, resolveErr := items.ResolveItem(uint32(reward.ItemID))
			if resolveErr != nil || definition.Kind != dungeonDropItemStackable {
				return dnfexpertjob.ErrExtractionInvalid
			}
			slots, grantErr := grantCurrentCeraShopProduct(requesterInventory, definition, uint32(reward.Count))
			if grantErr != nil {
				return grantErr
			}
			if len(slots) == 0 {
				return dnfexpertjob.ErrExtractionInvalid
			}
			result.Materials = append(result.Materials, currentExpertJobExtractionMaterial{Slot: int16(slots[0]), ItemID: uint32(reward.ItemID), Count: uint32(reward.Count)})
		}
		if ownerCharacter.Stats == nil {
			ownerCharacter.Stats = make(map[string]int64)
		}
		ownerCharacter.Stats["expert_job_exp"] = plan.FinalExperience
		ownerCharacter.Stats[currentExpertJobMachineEnduranceStat] = endurance - plan.EnduranceReduction
		result.RequesterGold, result.OwnerGold, result.Endurance, result.Experience, result.LevelChanged = requesterGold, ownerGold, endurance-plan.EnduranceReduction, plan.ExperienceGain, plan.LevelChanged
		return nil
	})
	return result, err
}

func (s *Service) handleCurrentExpertJobStoreEnchant(session *gameSession, opcode uint16, body []byte) error {
	request, err := parseCurrentExpertJobStoreEnchantRequest(body)
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	s.expertJobStoreOpMu.Lock()
	store, ok := s.currentExpertJobEnteredStore(session, request.OwnerID, currentExpertJobEnchantStoreKind)
	if !ok {
		s.expertJobStoreOpMu.Unlock()
		return s.sendGameUpperFailure(session, opcode, 19)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	result, err := s.commitCurrentExpertJobStoreEnchant(ctx, session, store, request)
	cancel()
	var updatedStore, closedStore *currentExpertJobStore
	qualificationsChanged := false
	if err == nil {
		s.expertJobStoreMu.Lock()
		live := s.expertJobStores[store.OwnerCharacterID]
		if live != nil && live.OwnerSession == store.OwnerSession {
			qualificationsChanged = !bytes.Equal(live.Qualifications, result.Qualifications)
			live.Endurance = result.Endurance
			live.Qualifications = append([]byte(nil), result.Qualifications...)
			updatedStore = cloneCurrentExpertJobStore(live)
		}
		s.expertJobStoreMu.Unlock()
		if result.Endurance == 0 {
			closedStore = s.removeCurrentExpertJobStore(store.OwnerCharacterID, store.OwnerSession)
		}
	}
	s.expertJobStoreOpMu.Unlock()
	if err != nil {
		return s.sendGameUpperFailure(session, opcode, currentExpertJobStoreError(err))
	}
	if err := s.sendGameUpperRawClass(session, opcode, buildCurrentExpertJobStoreEnchantSuccessBody(result), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentContainerListsRefresh(session, "expert_job_store_enchant_after_ack"); err != nil {
		return err
	}
	if store.OwnerSession != nil && store.OwnerSession != session {
		owner := packetWriter{}
		owner.writeInt32(int(result.OwnerGold))
		owner.writeInt32(int(result.Endurance))
		_ = s.sendGameUpperRawClass(store.OwnerSession, currentExpertJobEnchantOwnerNotification, owner.bytes(), 0)
		_ = s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentGoldStateBody(result.RequesterGold), 0)
		_ = s.sendGameUpperRawClass(store.OwnerSession, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentGoldStateBody(result.OwnerGold), 0)
	}
	if store.OwnerSession != nil {
		_ = s.sendCurrentExpertJobInfoFromRepository(store.OwnerSession, dnfexpertjob.EnchanterType, true)
	}
	if qualificationsChanged && closedStore == nil && updatedStore != nil {
		s.broadcastCurrentExpertJobStoreUpdate(updatedStore)
	}
	if closedStore != nil {
		s.broadcastCurrentExpertJobStoreClose(closedStore, true)
	}
	return nil
}

func (s *Service) commitCurrentExpertJobStoreEnchant(ctx context.Context, session *gameSession, store *currentExpertJobStore, request currentExpertJobStoreEnchantRequest) (currentExpertJobStoreUseResult, error) {
	jobCatalog, err := s.currentExpertJobCatalog()
	if err != nil {
		return currentExpertJobStoreUseResult{}, err
	}
	items, err := s.currentPVFItemCatalog()
	if err != nil {
		return currentExpertJobStoreUseResult{}, err
	}
	s.initialEquipmentMu.Lock()
	source, sourceErr := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if sourceErr != nil {
		return currentExpertJobStoreUseResult{}, sourceErr
	}
	var result currentExpertJobStoreUseResult
	err = s.mutateCurrentExpertJobStorePair(ctx, session, store, func(requesterCharacter *dnfrepo.CharacterRecord, requesterInventory *dnfrepo.InventoryRecord, ownerCharacter *dnfrepo.CharacterRecord, ownerInventory *dnfrepo.InventoryRecord) error {
		jobType, experience, stateErr := currentExpertJobCharacterState(ownerCharacter)
		if stateErr != nil || jobType != dnfexpertjob.EnchanterType {
			return dnfexpertjob.ErrJobUnsupported
		}
		config, configured := jobCatalog.Enchanter()
		if !configured {
			return dnfexpertjob.ErrJobUnsupported
		}
		endurance := currentExpertJobStatDefault(ownerCharacter.Stats, currentExpertJobMachineEnduranceStat, config.InitialEndurance)
		if endurance != store.Endurance {
			return dnfexpertjob.ErrMachineInvalid
		}
		targetKey := currentCeraShopInventorySlotKey(request.TargetList, request.TargetSlot)
		cardKey := currentCeraShopInventorySlotKey(request.CardList, request.CardSlot)
		target, targetFound := requesterInventory.Slots[targetKey]
		card, cardFound := requesterInventory.Slots[cardKey]
		if !targetFound || target.ItemID <= 0 || target.ItemID > math.MaxUint32 || target.Count != 1 || currentNPCShopItemLocked(target) || !cardFound || card.ItemID <= 0 || card.ItemID > math.MaxUint32 || card.Count <= 0 || currentNPCShopItemLocked(card) {
			return dnfexpertjob.ErrExtractionInvalid
		}
		plan, planErr := jobCatalog.PlanEnchanterStore(experience, request.RecipeID, card.ItemID, endurance, rand.IntN)
		if planErr != nil {
			return planErr
		}
		resolution, resolutionErr := resolveCurrentEnchantCardMetadata(items, source, card.ItemID, target.ItemID)
		if resolutionErr != nil {
			return resolutionErr
		}
		upgradeCount := currentExpertJobEnchantUpgradeCount(card)
		if resolution.CardItemID != card.ItemID || resolution.TargetKind != string(dungeonDropItemEquipment) || !currentExpertJobStoreStringContainsFold(resolution.AllowedEquipmentTypes, resolution.TargetEquipmentType) || (len(resolution.UpgradeCounts) > 0 && !currentExpertJobStoreIntContains(resolution.UpgradeCounts, int64(upgradeCount))) || (len(resolution.UpgradeCounts) == 0 && upgradeCount != 0) {
			return dnfexpertjob.ErrExtractionInvalid
		}
		requesterGold, goldErr := currentExpertJobWalletGold(requesterInventory)
		if goldErr != nil {
			return goldErr
		}
		ownerGold, ownerGoldErr := currentExpertJobWalletGold(ownerInventory)
		if ownerGoldErr != nil {
			return ownerGoldErr
		}
		self := requesterCharacter.CharacterID == ownerCharacter.CharacterID
		if !self {
			if requesterGold < store.Cost || ownerGold > math.MaxInt32-store.Cost {
				return dnfexpertjob.ErrInsufficientGold
			}
			requesterGold -= store.Cost
			ownerGold += store.Cost
			currentExpertJobSetWalletGold(requesterInventory, requesterGold)
			currentExpertJobSetWalletGold(ownerInventory, ownerGold)
		}
		if plan.Success {
			currentExpertJobSetEnchantFields(&target, card.ItemID, upgradeCount)
			requesterInventory.Slots[targetKey] = target
		}
		card.Count--
		if card.Count == 0 {
			delete(requesterInventory.Slots, cardKey)
		} else {
			entry := currentItemListEntryFromStack(request.CardList, request.CardSlot, card)
			card.RawEntry = append([]byte(nil), entry.data[:]...)
			requesterInventory.Slots[cardKey] = card
		}
		if ownerCharacter.Stats == nil {
			ownerCharacter.Stats = make(map[string]int64)
		}
		ownerCharacter.Stats["expert_job_exp"] = plan.FinalExperience
		ownerCharacter.Stats[currentExpertJobMachineEnduranceStat] = endurance - plan.EnduranceReduction
		result = currentExpertJobStoreUseResult{RequesterGold: requesterGold, OwnerGold: ownerGold, Endurance: endurance - plan.EnduranceReduction, Experience: plan.FinalExperience, LevelChanged: plan.LevelChanged, Success: plan.Success, TargetList: request.TargetList, TargetSlot: request.TargetSlot, Qualifications: config.Qualifications(plan.FinalExperience)}
		return nil
	})
	return result, err
}

func (s *Service) currentExpertJobEnteredStore(session *gameSession, ownerID uint16, kind byte) (*currentExpertJobStore, bool) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || currentExpertJobSessionInDungeon(session) || s.onlinePlayers == nil {
		return nil, false
	}
	visitor, visitorOK := s.onlinePlayers.PlayerForCharacter(session.selectedCharacterID)
	s.expertJobStoreMu.Lock()
	defer s.expertJobStoreMu.Unlock()
	store := s.expertJobStores[ownerID]
	if !visitorOK || visitor.Session != session || s.expertJobVisitors[session] != ownerID || store == nil || store.Kind != kind || store.OwnerSession == nil || store.TownID != visitor.TownID || store.AreaID != visitor.AreaID {
		return nil, false
	}
	return cloneCurrentExpertJobStore(store), true
}

func (s *Service) updateCurrentExpertJobStoreMachine(snapshot *currentExpertJobStore, endurance int64, grade int) {
	if s == nil || snapshot == nil {
		return
	}
	s.expertJobStoreMu.Lock()
	defer s.expertJobStoreMu.Unlock()
	store := s.expertJobStores[snapshot.OwnerCharacterID]
	if store != nil && store.OwnerSession == snapshot.OwnerSession {
		store.Endurance, store.MachineGrade = endurance, grade
	}
}

func buildCurrentExpertJobStoreDisjointSuccessBody(request currentExpertJobStoreDisjointRequest, result currentExpertJobStoreUseResult) []byte {
	w := packetWriter{}
	w.writeByte(1)
	w.writeUint16(uint16(request.TargetSlot))
	w.writeByte(request.TargetList)
	w.writeUint16(request.OwnerID)
	w.writeByte(byte(len(result.Materials)))
	for _, material := range result.Materials {
		w.writeUint16(uint16(material.Slot))
		w.writeUint32(material.ItemID)
		w.writeUint32(material.Count)
	}
	w.writeUint32(uint32(result.RequesterGold))
	w.writeUint32(uint32(result.Endurance))
	return w.bytes()
}

func buildCurrentExpertJobStoreEnchantSuccessBody(result currentExpertJobStoreUseResult) []byte {
	w := packetWriter{}
	w.writeByte(1)
	if result.Success {
		w.writeByte(1)
	} else {
		w.writeByte(0)
	}
	w.writeUint32(uint32(result.Experience))
	w.writeByte(0)
	w.writeUint32(uint32(result.Endurance))
	return w.bytes()
}

func (s *Service) mutateCurrentExpertJobStorePair(ctx context.Context, requesterSession *gameSession, store *currentExpertJobStore, apply func(*dnfrepo.CharacterRecord, *dnfrepo.InventoryRecord, *dnfrepo.CharacterRecord, *dnfrepo.InventoryRecord) error) error {
	if s == nil || requesterSession == nil || store == nil || apply == nil || requesterSession.selectedCharacterID == 0 {
		return dnfexpertjob.ErrOwnerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return dnfexpertjob.ErrOwnerUnavailable
	}
	requesterID := strconv.FormatUint(uint64(requesterSession.selectedCharacterID), 10)
	ownerID := strconv.FormatUint(uint64(store.OwnerCharacterID), 10)
	if requesterID == ownerID {
		owner, ownerErr := dnfexpertjob.NewOwner(repositories)
		if ownerErr != nil {
			return ownerErr
		}
		accountID := strings.TrimSpace(s.accountIDForSession(requesterSession))
		return owner.Machine(ctx, dnfexpertjob.Command{AccountID: accountID, CharacterID: requesterID, UpdatedAt: s.gameplayNow(), Project: func(assets *dnfexpertjob.Assets) (dnfexpertjob.Changes, error) {
			if err := apply(assets.Character, assets.Inventory, assets.Character, assets.Inventory); err != nil {
				return dnfexpertjob.Changes{}, err
			}
			return dnfexpertjob.Changes{Character: true, Inventory: true}, nil
		}})
	}
	if repositories.CharacterTrade == nil {
		return dnfrepo.ErrCharacterTradeTransactionUnavailable
	}
	return repositories.CharacterTrade.WithinCharacterTrade(ctx, requesterID, ownerID, func(characters dnfrepo.CharacterRepository, inventories dnfrepo.InventoryRepository) error {
		ids := []string{requesterID, ownerID}
		if ids[1] < ids[0] {
			ids[0], ids[1] = ids[1], ids[0]
		}
		characterRows := make(map[string]dnfrepo.CharacterRecord, 2)
		inventoryRows := make(map[string]dnfrepo.InventoryRecord, 2)
		for _, id := range ids {
			character, found, err := characters.Load(ctx, id)
			if err != nil {
				return err
			}
			if !found || character.CharacterID != id {
				return dnfexpertjob.ErrCharacterNotFound
			}
			inventory, found, err := inventories.Load(ctx, id)
			if err != nil {
				return err
			}
			if !found || inventory.CharacterID != id || inventory.Slots == nil {
				return dnfexpertjob.ErrInventoryNotFound
			}
			characterRows[id], inventoryRows[id] = dnfrepo.CloneCharacter(character), dnfrepo.CloneInventory(inventory)
		}
		requesterCharacter, requesterInventory := characterRows[requesterID], inventoryRows[requesterID]
		ownerCharacter, ownerInventory := characterRows[ownerID], inventoryRows[ownerID]
		if err := apply(&requesterCharacter, &requesterInventory, &ownerCharacter, &ownerInventory); err != nil {
			return err
		}
		characterRows[requesterID], inventoryRows[requesterID], characterRows[ownerID], inventoryRows[ownerID] = requesterCharacter, requesterInventory, ownerCharacter, ownerInventory
		now := s.gameplayNow()
		for _, id := range ids {
			character := characterRows[id]
			character.UpdatedAt = now
			if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
				return err
			}
			inventory := inventoryRows[id]
			inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		return nil
	})
}

func currentExpertJobSetEnchantFields(stack *dnfrepo.ItemStack, cardItemID int64, upgradeCount byte) {
	if stack == nil {
		return
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 4)
	}
	stack.Extra["value_a"], stack.Extra["enchant_card_id"] = strconv.FormatInt(cardItemID, 10), strconv.FormatInt(cardItemID, 10)
	stack.Extra["byte_12"], stack.Extra["enchant_upgrade_count"] = strconv.Itoa(int(upgradeCount)), strconv.Itoa(int(upgradeCount))
	if len(stack.RawEntry) == currentItemListEntryWireSize {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		value := uint32(cardItemID)
		binary.LittleEndian.PutUint32(stack.RawEntry[0x0E:0x12], value)
		stack.RawEntry[0x12] = upgradeCount
	}
}
