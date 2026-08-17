package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	dnfaura "longheng.io/server/internal/modules/dnf/auraskin"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentOpenAuraSkinSlotRequestSize = 4
	currentOpenAuraSkinSlotFlagStat    = dnfaura.FlagStat
	currentAuraSkinSilentRestoreSource = "current_exe_town_ui_lifecycle_marked_restore"
)

var errCurrentOpenAuraSkinSlotInvalidRequest = errors.New("current aura skin slot request invalid")

type currentOpenAuraSkinSlotRequest struct {
	SourceSlot int16
}

func (s *Service) handleCurrentOpenAuraSkinSlot(session *gameSession, body []byte) error {
	request, err := decodeCurrentOpenAuraSkinSlotRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-current-open-aura-skin-slot-rejected",
			"body_len", len(body),
			"reason", err.Error())
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketOpenAuraSkinSlot), 4)
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		s.logGameEvent(session, "game-current-open-aura-skin-slot-rejected",
			"source_slot", request.SourceSlot,
			"reason", "pvf_catalog_unavailable",
			"error", err.Error())
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketOpenAuraSkinSlot), 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentOpenAuraSkinSlot(ctx, session, catalog, request)
	if err != nil {
		s.logGameEvent(session, "game-current-open-aura-skin-slot-rejected",
			"source_slot", request.SourceSlot,
			"reason", err.Error())
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketOpenAuraSkinSlot), 4)
	}
	// Current NoPack registration 0x1D3798F binds op863 to
	// sub_1D25590.  Its success path consumes only the common result marker;
	// it does not read a source slot or any other business byte.  Keep the
	// status success marker supplied by sendGameUpperSuccess, but do not append
	// the request's source slot to the response.
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketOpenAuraSkinSlot), nil); err != nil {
		return err
	}
	if result.SourceChanged {
		var sourceUpdate currentItemListEntry
		if result.SourceRemoved {
			sourceUpdate.patchCore(result.SourceSlot, math.MaxUint32, 0)
		} else {
			sourceUpdate = currentItemListEntryFromStack(
				dnfrepo.MainInventoryListType,
				result.SourceSlot,
				result.RemainingStack,
			)
		}
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, []currentItemListEntry{sourceUpdate}), 0); err != nil {
			return err
		}
	}
	// The ordinary success ACK intentionally reaches sub_1D25590 once so the
	// real expansion keeps its native effect. Follow it with the compatibility
	// marker that mirrors the committed state into sub_26905A0's durable bit;
	// the DLL consumes this marker without replaying the effect.
	if err := s.sendCurrentAuraSkinSlotPersistentStateMarker(session, true, "current_exe_op863_commit"); err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-open-aura-skin-slot-accepted",
		"source_slot", result.SourceSlot,
		"source_item_id", result.SourceItemID,
		"source_pvf_path", result.SourcePVFPath,
		"consumed", result.Consumed,
		"already_open", result.AlreadyOpen,
		"source_update", result.SourceChanged)
	return nil
}

func decodeCurrentOpenAuraSkinSlotRequest(body []byte) (currentOpenAuraSkinSlotRequest, error) {
	if len(body) != currentOpenAuraSkinSlotRequestSize {
		return currentOpenAuraSkinSlotRequest{}, fmt.Errorf("%w: body_len=%d", errCurrentOpenAuraSkinSlotInvalidRequest, len(body))
	}
	slot := binary.LittleEndian.Uint32(body)
	if slot > uint32(math.MaxInt16) {
		return currentOpenAuraSkinSlotRequest{}, fmt.Errorf("%w: source_slot=%d", errCurrentOpenAuraSkinSlotInvalidRequest, slot)
	}
	return currentOpenAuraSkinSlotRequest{SourceSlot: int16(slot)}, nil
}

func (s *Service) commitCurrentOpenAuraSkinSlot(ctx context.Context, session *gameSession, catalog *pvfDungeonDropCatalog, request currentOpenAuraSkinSlotRequest) (dnfaura.Result, error) {
	if s == nil || session == nil || catalog == nil {
		return dnfaura.Result{}, dnfaura.ErrOwnerUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return dnfaura.Result{}, dnfaura.ErrOwnerUnavailable
	}
	owner, err := dnfaura.NewOwner(repositories, currentAuraSkinCatalog{catalog: catalog})
	if err != nil {
		return dnfaura.Result{}, err
	}
	return owner.Open(ctx, dnfaura.Command{
		AccountID:           s.accountIDForSession(session),
		SelectedCharacterID: session.selectedCharacterID,
		SourceSlot:          request.SourceSlot,
	})
}

// sendCurrentAuraSkinSlotTownUIReadyState projects the durable aura-slot flag
// once per town UI lifecycle. Both open and closed state are explicit so a
// process that changes characters cannot inherit the previous actor's bit.
// The matching DLL consumes the marker without invoking op863's one-shot
// unlock-effect handler.
func (s *Service) sendCurrentAuraSkinSlotTownUIReadyState(session *gameSession) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	session.auraSkinMu.Lock()
	if session.auraSkinTownUIReadyStateSent {
		session.auraSkinMu.Unlock()
		s.logGameEvent(session, "game-current-aura-skin-slot-state-skipped",
			"source", currentAuraSkinSilentRestoreSource,
			"reason", "already_sent_for_current_town_scene")
		return nil
	}
	session.auraSkinTownUIReadyStateSent = true
	session.auraSkinMu.Unlock()

	resetGate := func() {
		session.auraSkinMu.Lock()
		session.auraSkinTownUIReadyStateSent = false
		session.auraSkinMu.Unlock()
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		resetGate()
		return dnfrepo.ErrRepoMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		resetGate()
		return err
	}
	if !found || strings.TrimSpace(character.CharacterID) != characterID {
		resetGate()
		return fmt.Errorf("aura skin state character %s not found", characterID)
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID != "" && strings.TrimSpace(character.AccountID) != accountID {
		resetGate()
		return fmt.Errorf("aura skin state character %s account mismatch", characterID)
	}
	opened := character.Stats != nil && character.Stats[currentOpenAuraSkinSlotFlagStat] != 0
	if err := s.sendCurrentAuraSkinSlotPersistentStateMarker(session, opened, currentAuraSkinSilentRestoreSource); err != nil {
		resetGate()
		return err
	}
	return nil
}

func (s *Service) sendCurrentAuraSkinSlotPersistentStateMarker(session *gameSession, opened bool, source string) error {
	state := byte(0)
	if opened {
		state = 1
	}
	body := []byte{state, 'A', 'U', 'R', 'A'}
	s.logGameEvent(session, "game-current-aura-skin-slot-state-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketOpenAuraSkinSlot),
		"classification", dnfproto.DefaultChannelClassification,
		"body_len", len(body),
		"opened", opened,
		"restore_mode", "marked_persistent_native_state_without_unlock_effect")
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketOpenAuraSkinSlot),
		body,
		dnfproto.DefaultChannelClassification,
	)
}

type currentAuraSkinCatalog struct {
	catalog *pvfDungeonDropCatalog
}

func (c currentAuraSkinCatalog) ResolveItem(itemID uint32) (dnfaura.ItemDefinition, error) {
	if c.catalog == nil {
		return dnfaura.ItemDefinition{}, dnfaura.ErrOwnerUnavailable
	}
	definition, err := c.catalog.ResolveItem(itemID)
	if err != nil {
		return dnfaura.ItemDefinition{}, err
	}
	return dnfaura.ItemDefinition{
		Kind:    string(definition.Kind),
		PVFPath: definition.PVFPath,
	}, nil
}
