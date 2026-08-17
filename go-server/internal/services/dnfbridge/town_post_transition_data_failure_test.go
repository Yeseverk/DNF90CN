package dnfbridge

import (
	"context"
	"errors"
	"reflect"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type failingTownPostEquipmentRepository struct {
	dnfrepo.EquipmentRepository
	err error
}

func (repository *failingTownPostEquipmentRepository) Load(
	context.Context,
	string,
) (dnfrepo.EquipmentRecord, bool, error) {
	return dnfrepo.EquipmentRecord{}, false, repository.err
}

type missingTownPostCharacterRepository struct {
	dnfrepo.CharacterRepository
}

func (repository *missingTownPostCharacterRepository) Load(
	context.Context,
	string,
) (dnfrepo.CharacterRecord, bool, error) {
	return dnfrepo.CharacterRecord{}, false, nil
}

func TestTownPostTransitionDoesNotAdvanceMode0OnEquipmentLoadFailure(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	wantErr := errors.New("equipment load failed")
	failing := repositories
	failing.Equipment = &failingTownPostEquipmentRepository{
		EquipmentRepository: repositories.Equipment,
		err:                 wantErr,
	}
	service.repositoryProvider = func() (dnfrepo.Group, bool) { return failing, true }
	service.armCurrentTownPostTransitionPlayerState(session, "test_equipment_failure")
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	err := service.sendCurrentTownPostTransitionPlayerState(session, "test_equipment_failure")
	if !errors.Is(err, wantErr) {
		t.Fatalf("equipment failure=%v want=%v", err, wantErr)
	}
	if session.townPostTransition.stage != currentTownPostTransitionPending {
		t.Fatalf("equipment failure advanced stage=%d", session.townPostTransition.stage)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("equipment failure wrote stale mode0=%x", connection.write.Bytes())
	}

	service.repositoryProvider = func() (dnfrepo.Group, bool) { return repositories, true }
	if err := service.sendCurrentTownPostTransitionPlayerState(session, "test_equipment_resume"); err != nil {
		t.Fatalf("equipment resume: %v", err)
	}
	if got := townPostTransitionPacketSignatures(t, connection.write.Bytes()); !reflect.DeepEqual(
		got,
		[]string{"mode0", "mode1", "op105", "op37", "op30", "op19", "op120"},
	) {
		t.Fatalf("equipment resume=%v", got)
	}
}

func TestTownPostTransitionDoesNotAdvanceCreatureStageOnEquipmentLoadFailure(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	wantErr := errors.New("creature equipment load failed")
	failing := repositories
	failing.Equipment = &failingTownPostEquipmentRepository{
		EquipmentRepository: repositories.Equipment,
		err:                 wantErr,
	}
	service.repositoryProvider = func() (dnfrepo.Group, bool) { return failing, true }
	armTownPostTransitionAtStage(session, currentTownPostTransitionCreatureTableSent)
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	err := service.sendCurrentTownPostTransitionPlayerState(session, "test_creature_failure")
	if !errors.Is(err, wantErr) {
		t.Fatalf("creature failure=%v want=%v", err, wantErr)
	}
	if session.townPostTransition.stage != currentTownPostTransitionCreatureTableSent {
		t.Fatalf("creature failure advanced stage=%d", session.townPostTransition.stage)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("creature failure wrote suffix=%x", connection.write.Bytes())
	}

	service.repositoryProvider = func() (dnfrepo.Group, bool) { return repositories, true }
	if err := service.sendCurrentTownPostTransitionPlayerState(session, "test_creature_resume"); err != nil {
		t.Fatalf("creature resume: %v", err)
	}
	if got := townPostTransitionPacketSignatures(t, connection.write.Bytes()); !reflect.DeepEqual(
		got,
		[]string{"op37", "op30", "op19", "op120"},
	) {
		t.Fatalf("creature resume=%v", got)
	}
}

func TestTownPostTransitionDoesNotAdvanceFinishStateWhenCharacterSnapshotMissing(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	missing := repositories
	missing.Character = &missingTownPostCharacterRepository{
		CharacterRepository: repositories.Character,
	}
	service.repositoryProvider = func() (dnfrepo.Group, bool) { return missing, true }
	armTownPostTransitionAtStage(session, currentTownPostTransitionCreatureGrowthSent)
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendCurrentTownPostTransitionPlayerState(session, "test_character_missing"); err == nil {
		t.Fatal("missing finish-state character returned nil")
	}
	if session.townPostTransition.stage != currentTownPostTransitionCreatureGrowthSent {
		t.Fatalf("missing character advanced stage=%d", session.townPostTransition.stage)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("missing character wrote suffix=%x", connection.write.Bytes())
	}

	service.repositoryProvider = func() (dnfrepo.Group, bool) { return repositories, true }
	if err := service.sendCurrentTownPostTransitionPlayerState(session, "test_character_resume"); err != nil {
		t.Fatalf("character resume: %v", err)
	}
	if got := townPostTransitionPacketSignatures(t, connection.write.Bytes()); !reflect.DeepEqual(
		got,
		[]string{"op37", "op30", "op19", "op120"},
	) {
		t.Fatalf("character resume=%v", got)
	}
}

func TestTownPostTransitionDoesNotAdvanceSkillStageWhenProjectionUnavailable(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	catalog := service.skillCatalog
	service.skillCatalog = nil
	armTownPostTransitionAtStage(session, currentTownPostTransitionFinishCompletionSent)
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendCurrentTownPostTransitionPlayerState(session, "test_skill_missing"); err == nil {
		t.Fatal("missing skill projection returned nil")
	}
	if session.townPostTransition.stage != currentTownPostTransitionFinishCompletionSent {
		t.Fatalf("missing skill projection advanced stage=%d", session.townPostTransition.stage)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("missing skill projection wrote op120=%x", connection.write.Bytes())
	}

	service.skillCatalog = catalog
	if err := service.sendCurrentTownPostTransitionPlayerState(session, "test_skill_resume"); err != nil {
		t.Fatalf("skill resume: %v", err)
	}
	if got := townPostTransitionPacketSignatures(t, connection.write.Bytes()); !reflect.DeepEqual(
		got,
		[]string{"op19", "op120"},
	) {
		t.Fatalf("skill resume=%v", got)
	}
}

func armTownPostTransitionAtStage(
	session *gameSession,
	stage currentTownPostTransitionStage,
) {
	session.townPostTransition.characterID = session.selectedCharacterID
	session.townPostTransition.ownerChannel = currentTownActorOwnerContext(session)
	session.townPostTransition.stage = stage
	session.townPostTransition.source = "test_manual_stage"
}
