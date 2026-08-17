package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/inventory"
	"longheng.io/server/internal/modules/dnf/itemtrade"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentItemTradeListType   byte   = 4
	currentItemTradeChangeMsg  uint16 = 15
	currentItemTradeCancelMsg  uint16 = 16
	currentItemTradeStateMsg   uint16 = 17
	currentItemTradeFinishMsg  uint16 = 18
	currentItemTradeMoveMsg    uint16 = 19
	currentItemTradeReadyMsg   uint16 = uint16(dnfenum.CmdPacketSetItemtradeState)
	currentItemTradeAddingDone byte   = 1
	currentItemTradeOfferAck   byte   = 2
	currentItemTradeReady      byte   = 3
	currentItemTradeAddRequest byte   = 5
	currentItemTradeOfferFirst uint16 = 3
	currentItemTradeOfferLast  uint16 = 11
)

type onlineItemTradeOffer struct {
	tradeSlot  uint16
	sourceList byte
	sourceSlot int16
	count      int32
	itemID     int64
	stack      dnfrepo.ItemStack
}

type onlineItemTrade struct {
	participants [2]uint16
	offers       map[uint16]map[uint16]onlineItemTradeOffer
	gold         map[uint16]int64
	states       map[uint16]byte
	committing   bool
}

func (t *onlineItemTrade) other(characterID uint16) (uint16, bool) {
	if t == nil {
		return 0, false
	}
	if t.participants[0] == characterID {
		return t.participants[1], true
	}
	if t.participants[1] == characterID {
		return t.participants[0], true
	}
	return 0, false
}

func (s *Service) beginOnlineItemTrade(first, second *gameSession) {
	if s == nil || first == nil || second == nil || first == second || first.selectedCharacterID == 0 || second.selectedCharacterID == 0 {
		return
	}
	firstID, secondID := first.selectedCharacterID, second.selectedCharacterID
	if s.onlinePlayers == nil || !s.onlinePlayers.PeerInSameArea(firstID, secondID) {
		return
	}
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	if s.itemTrades == nil {
		s.itemTrades = make(map[uint16]*onlineItemTrade)
	}
	for _, id := range []uint16{firstID, secondID} {
		if old := s.itemTrades[id]; old != nil {
			delete(s.itemTrades, old.participants[0])
			delete(s.itemTrades, old.participants[1])
		}
	}
	trade := &onlineItemTrade{
		participants: [2]uint16{firstID, secondID},
		offers: map[uint16]map[uint16]onlineItemTradeOffer{
			firstID:  make(map[uint16]onlineItemTradeOffer),
			secondID: make(map[uint16]onlineItemTradeOffer),
		},
		gold:   map[uint16]int64{firstID: 0, secondID: 0},
		states: map[uint16]byte{firstID: 0, secondID: 0},
	}
	s.itemTrades[firstID] = trade
	s.itemTrades[secondID] = trade
}

func (s *Service) handleOnlineItemTradeCommand(session *gameSession, typ uint16, body []byte) (bool, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return false, nil
	}
	trade := s.currentOnlineItemTrade(session.selectedCharacterID)
	if trade == nil {
		return false, nil
	}
	switch typ {
	case currentItemTradeMoveMsg:
		request, err := inventory.DecodeMoveItemspaceRequest(body)
		if err != nil {
			return true, s.sendGameUpperFailure(session, typ, 1)
		}
		if request.SourceListType != currentItemTradeListType && request.DestinationListType != currentItemTradeListType {
			return false, nil
		}
		return true, s.handleOnlineItemTradeMove(session, request)
	case currentItemTradeReadyMsg:
		if len(body) != 1 {
			return true, s.sendGameUpperFailure(session, typ, 1)
		}
		return true, s.handleOnlineItemTradeState(session, body[0])
	default:
		return false, nil
	}
}

func (s *Service) currentOnlineItemTrade(characterID uint16) *onlineItemTrade {
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	return s.itemTrades[characterID]
}

func (s *Service) handleOnlineItemTradeMove(session *gameSession, request inventory.MoveItemspaceRequest) error {
	if request.DestinationListType == currentItemTradeListType && request.SourceListType != currentItemTradeListType {
		if isOnlineItemTradeGoldDeposit(request) {
			return s.addOnlineItemTradeGold(session, request)
		}
		return s.addOnlineItemTradeOffer(session, request)
	}
	if request.SourceListType == currentItemTradeListType && request.DestinationListType != currentItemTradeListType {
		if isOnlineItemTradeGoldWithdrawal(request) {
			return s.removeOnlineItemTradeGold(session, request)
		}
		return s.removeOnlineItemTradeOffer(session, request)
	}
	return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
}

func isOnlineItemTradeGoldDeposit(request inventory.MoveItemspaceRequest) bool {
	return request.SourceListType == dnfrepo.MainInventoryListType &&
		request.SourceSlotIndex == 0 && request.SourceInstanceValue == 0 &&
		request.MoveCount > 0 && request.DestinationListType == currentItemTradeListType &&
		request.DestinationSlotIndex == 0
}

func isOnlineItemTradeGoldWithdrawal(request inventory.MoveItemspaceRequest) bool {
	return request.SourceListType == currentItemTradeListType && request.SourceSlotIndex == 0 &&
		request.DestinationListType == dnfrepo.MainInventoryListType && request.DestinationSlotIndex == 0
}

func (s *Service) addOnlineItemTradeGold(session *gameSession, request inventory.MoveItemspaceRequest) error {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	character, found, err := repositories.Character.Load(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	if err != nil || !found || int64(request.MoveCount) > character.Stats["gold"] {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}

	s.tradeMu.Lock()
	trade := s.itemTrades[session.selectedCharacterID]
	peerID, inTrade := trade.other(session.selectedCharacterID)
	valid := inTrade && !trade.committing && trade.states[session.selectedCharacterID] != currentItemTradeReady
	previousGold := int64(0)
	if valid {
		previousGold = trade.gold[session.selectedCharacterID]
		trade.gold[session.selectedCharacterID] = int64(request.MoveCount)
	}
	s.tradeMu.Unlock()
	peer, peerOnline := s.onlineGameSession(peerID)
	if !valid || !peerOnline || s.onlinePlayers == nil || !s.onlinePlayers.PeerInSameArea(session.selectedCharacterID, peerID) {
		if valid {
			s.tradeMu.Lock()
			if current := s.itemTrades[session.selectedCharacterID]; current == trade && !current.committing {
				current.gold[session.selectedCharacterID] = previousGold
			}
			s.tradeMu.Unlock()
		}
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	if err := s.sendGameUpperSuccess(session, currentItemTradeMoveMsg, buildOnlineItemTradeMoveAck(request)); err != nil {
		return err
	}
	entry := currentGoldWalletItemListEntry(int64(request.MoveCount))
	if err := s.sendGameUpperRawClassCodec(peer, currentItemTradeChangeMsg, entry.data[:], 0, true); err != nil {
		return err
	}
	s.logGameEvent(session, "game-item-trade-gold-added",
		"char_id", session.selectedCharacterID, "peer_char_id", peerID, "gold", request.MoveCount)
	return nil
}

func (s *Service) removeOnlineItemTradeGold(session *gameSession, request inventory.MoveItemspaceRequest) error {
	s.tradeMu.Lock()
	trade := s.itemTrades[session.selectedCharacterID]
	peerID, inTrade := trade.other(session.selectedCharacterID)
	gold := int64(0)
	if inTrade && !trade.committing && trade.states[session.selectedCharacterID] != currentItemTradeReady {
		gold = trade.gold[session.selectedCharacterID]
		if gold > 0 {
			trade.gold[session.selectedCharacterID] = 0
		}
	}
	s.tradeMu.Unlock()
	if gold <= 0 {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	if err := s.sendGameUpperSuccess(session, currentItemTradeMoveMsg, buildOnlineItemTradeMoveAck(request)); err != nil {
		return err
	}
	peer, peerOnline := s.onlineGameSession(peerID)
	if !peerOnline {
		return nil
	}
	return s.sendGameUpperRawClassCodec(peer, currentItemTradeChangeMsg, buildOnlineItemTradeRemovedEntry(0), 0, true)
}

func (s *Service) addOnlineItemTradeOffer(session *gameSession, request inventory.MoveItemspaceRequest) error {
	if request.MoveCount <= 0 || request.DestinationSlotIndex < 0 || request.SourceSlotIndex < 0 ||
		(request.SourceListType != dnfrepo.MainInventoryListType && request.SourceListType != 1) {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Inventory == nil {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	record, found, err := repositories.Inventory.Load(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	if err != nil || !found {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	sourceKey := fmt.Sprintf("%d:%d", request.SourceListType, request.SourceSlotIndex)
	stack, found := record.Slots[sourceKey]
	if !found || stack.ItemID <= 0 || stack.Count < int64(request.MoveCount) || stack.Bind ||
		(!stack.ExpireAt.IsZero() && !stack.ExpireAt.After(time.Now())) ||
		dnfrepo.IsAccountSharedInventorySlot(request.SourceListType, request.SourceSlotIndex) {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	// The request's source-instance field is an opaque value read from the
	// current client's live item object.  It is not the PVF item ID and is not
	// stable across every equipment/stackable representation.  The repository
	// slot and authoritative count above are sufficient to prevent item
	// injection, so a projection mismatch must not reject a valid trade.
	projectedSourceValue := currentItemListStackAmount(request.SourceListType, stack)
	if request.SourceInstanceValue != 0 && uint32(request.SourceInstanceValue) != projectedSourceValue {
		s.logGameEvent(session, "game-item-trade-source-instance-projection-mismatch",
			"char_id", session.selectedCharacterID,
			"source_list", request.SourceListType,
			"source_slot", request.SourceSlotIndex,
			"client_value", request.SourceInstanceValue,
			"projected_value", projectedSourceValue)
	}

	s.tradeMu.Lock()
	trade := s.itemTrades[session.selectedCharacterID]
	peerID, inTrade := trade.other(session.selectedCharacterID)
	valid := inTrade && !trade.committing && trade.states[session.selectedCharacterID] != currentItemTradeReady
	tradeSlot := uint16(0)
	slotAvailable := false
	if valid {
		// Current NoPack's trade container has twelve native slots. Live current-
		// EXE memory and sub_2339FB0 show 0..2 are occupied pseudo rows (gold and
		// the other trade currencies); the nine visible item rows are 3..11.
		// Targeting 0 renders a count-one item as +1 gold, while 1/2 collide with
		// the remaining pseudo rows and leave the item list blank.
		for slot := currentItemTradeOfferFirst; slot <= currentItemTradeOfferLast; slot++ {
			if _, occupied := trade.offers[session.selectedCharacterID][slot]; !occupied {
				tradeSlot = slot
				slotAvailable = true
				break
			}
		}
		for _, existing := range trade.offers[session.selectedCharacterID] {
			if existing.sourceList == request.SourceListType && existing.sourceSlot == request.SourceSlotIndex {
				valid = false
				break
			}
		}
	}
	valid = valid && slotAvailable
	offer := onlineItemTradeOffer{
		tradeSlot: tradeSlot, sourceList: request.SourceListType, sourceSlot: request.SourceSlotIndex,
		count: request.MoveCount, itemID: stack.ItemID, stack: cloneOnlineItemTradeStack(stack),
	}
	if valid {
		trade.offers[session.selectedCharacterID][tradeSlot] = offer
	}
	s.tradeMu.Unlock()
	peer, peerOnline := s.onlineGameSession(peerID)
	if !valid || !peerOnline || s.onlinePlayers == nil || !s.onlinePlayers.PeerInSameArea(session.selectedCharacterID, peerID) {
		if valid {
			s.removeOnlineItemTradeOfferState(session.selectedCharacterID, tradeSlot)
		}
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}

	// The request always targets pseudo-container slot 0 because the client does
	// not allocate the visible row itself. Echo the server-assigned native item
	// row in the success ACK; 0..2 are pseudo rows and item rows are 3..11.
	ackRequest := request
	ackRequest.DestinationSlotIndex = int16(tradeSlot)
	if err := s.sendGameUpperSuccess(session, currentItemTradeMoveMsg, buildOnlineItemTradeMoveAck(ackRequest)); err != nil {
		return err
	}
	offer.stack.Count = int64(offer.count)
	entry := currentItemListEntryFromStack(offer.sourceList, int16(offer.tradeSlot), offer.stack)
	if err := s.sendGameUpperRawClassCodec(peer, currentItemTradeChangeMsg, entry.data[:], 0, true); err != nil {
		return err
	}
	s.logGameEvent(session, "game-item-trade-offer-added",
		"char_id", session.selectedCharacterID, "peer_char_id", peerID,
		"source_list", offer.sourceList, "source_slot", offer.sourceSlot,
		"trade_slot", offer.tradeSlot, "item_id", offer.itemID, "count", offer.count)
	return nil
}

func (s *Service) removeOnlineItemTradeOffer(session *gameSession, request inventory.MoveItemspaceRequest) error {
	tradeSlot := uint16(request.SourceSlotIndex)
	s.tradeMu.Lock()
	trade := s.itemTrades[session.selectedCharacterID]
	peerID, inTrade := trade.other(session.selectedCharacterID)
	var offer onlineItemTradeOffer
	var found bool
	if inTrade && !trade.committing && trade.states[session.selectedCharacterID] != currentItemTradeReady {
		offer, found = trade.offers[session.selectedCharacterID][tradeSlot]
		if found {
			delete(trade.offers[session.selectedCharacterID], tradeSlot)
		}
	}
	s.tradeMu.Unlock()
	if !found || request.DestinationListType != offer.sourceList {
		return s.sendGameUpperFailure(session, currentItemTradeMoveMsg, 1)
	}
	if err := s.sendGameUpperSuccess(session, currentItemTradeMoveMsg, buildOnlineItemTradeMoveAck(request)); err != nil {
		return err
	}
	peer, peerOnline := s.onlineGameSession(peerID)
	if !peerOnline {
		return nil
	}
	return s.sendGameUpperRawClassCodec(peer, currentItemTradeChangeMsg, buildOnlineItemTradeRemovedEntry(tradeSlot), 0, true)
}

func buildOnlineItemTradeRemovedEntry(tradeSlot uint16) []byte {
	removed := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint16(removed[0:2], tradeSlot)
	binary.LittleEndian.PutUint32(removed[2:6], ^uint32(0))
	return removed
}

func (s *Service) removeOnlineItemTradeOfferState(characterID, tradeSlot uint16) {
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	if trade := s.itemTrades[characterID]; trade != nil && !trade.committing {
		delete(trade.offers[characterID], tradeSlot)
	}
}

func (s *Service) handleOnlineItemTradeState(session *gameSession, state byte) error {
	if state != 0 && state != currentItemTradeAddingDone && state != currentItemTradeOfferAck && state != currentItemTradeReady && state != currentItemTradeAddRequest {
		return s.sendGameUpperFailure(session, currentItemTradeReadyMsg, 1)
	}
	characterID := session.selectedCharacterID
	s.tradeMu.Lock()
	trade := s.itemTrades[characterID]
	peerID, inTrade := trade.other(characterID)
	if !inTrade || trade.committing {
		s.tradeMu.Unlock()
		return s.sendGameUpperFailure(session, currentItemTradeReadyMsg, 1)
	}
	peerState := trade.states[peerID]
	if state == currentItemTradeOfferAck {
		currentState := trade.states[characterID]
		if currentState == currentItemTradeReady {
			s.tradeMu.Unlock()
			return s.sendGameUpperFailure(session, currentItemTradeReadyMsg, 1)
		}
		s.tradeMu.Unlock()
		s.logGameEvent(session, "game-item-trade-offer-change-acknowledged",
			"char_id", characterID,
			"peer_char_id", peerID,
			"retained_state", currentState,
			"peer_state", peerState)
		return s.sendGameUpperSuccess(session, currentItemTradeReadyMsg, nil)
	}
	projectedState := state
	// Live current-EXE traffic distinguishes the two buttons even though the
	// server-facing state table does not: "adding complete" submits 5 and must
	// be projected to class0/op17 state 1; choosing Yes in resource 727 submits
	// 3 and must be projected as state 3.  A second value 5 is retained only as
	// a compatibility spelling of final consent for older clients.  Neither
	// spelling may skip the shared state-1 confirmation boundary.
	switch state {
	case currentItemTradeAddRequest:
		if trade.states[characterID] == 0 {
			projectedState = currentItemTradeAddingDone
		} else if trade.states[characterID] == currentItemTradeAddingDone &&
			(peerState == currentItemTradeAddingDone || peerState == currentItemTradeReady) {
			projectedState = currentItemTradeReady
		} else {
			s.tradeMu.Unlock()
			return s.sendGameUpperFailure(session, currentItemTradeReadyMsg, 1)
		}
	case currentItemTradeReady:
		if trade.states[characterID] != currentItemTradeAddingDone ||
			(peerState != currentItemTradeAddingDone && peerState != currentItemTradeReady) {
			s.tradeMu.Unlock()
			return s.sendGameUpperFailure(session, currentItemTradeReadyMsg, 1)
		}
	case currentItemTradeAddingDone:
		if trade.states[characterID] != 0 {
			s.tradeMu.Unlock()
			return s.sendGameUpperFailure(session, currentItemTradeReadyMsg, 1)
		}
	case 0:
		if trade.states[characterID] == currentItemTradeReady {
			s.tradeMu.Unlock()
			return s.sendGameUpperFailure(session, currentItemTradeReadyMsg, 1)
		}
	}
	trade.states[characterID] = projectedState
	peerState = trade.states[peerID]
	ready := projectedState == currentItemTradeReady && peerState == currentItemTradeReady
	addingDone := projectedState == currentItemTradeAddingDone && peerState == currentItemTradeAddingDone
	if ready {
		trade.committing = true
	}
	s.tradeMu.Unlock()
	s.logGameEvent(session, "game-item-trade-state-updated",
		"char_id", characterID,
		"peer_char_id", peerID,
		"inbound_state", state,
		"projected_state", projectedState,
		"peer_state", peerState,
		"both_adding_done", addingDone,
		"both_ready", ready)

	if err := s.sendGameUpperSuccess(session, currentItemTradeReadyMsg, nil); err != nil {
		return err
	}
	// The current client moves itself to "waiting for the other participant's
	// final confirmation" as soon as it submits state 3. Broadcasting op17/3
	// here would make sub_2340B10 close resource 727 on the peer that has not
	// answered yet, preventing the second consent. Keep final consent server-
	// side only; the second consent is followed directly by the atomic op18
	// completion (or the existing op16 rollback on failure).
	if projectedState == currentItemTradeReady {
		if ready {
			return s.commitOnlineItemTrade(trade)
		}
		return nil
	}
	stateBody := buildOnlineItemTradeStateBody(characterID, projectedState)
	if err := s.sendGameUpperRawClassCodec(session, currentItemTradeStateMsg, stateBody, 0, true); err != nil {
		return err
	}
	peer, peerOnline := s.onlineGameSession(peerID)
	if peerOnline {
		if err := s.sendGameUpperRawClassCodec(peer, currentItemTradeStateMsg, stateBody, 0, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) commitOnlineItemTrade(trade *onlineItemTrade) error {
	if trade == nil {
		return nil
	}
	firstID, secondID := trade.participants[0], trade.participants[1]
	firstOffers, secondOffers := snapshotOnlineItemTradeOffers(trade, firstID), snapshotOnlineItemTradeOffers(trade, secondID)
	firstGold, secondGold := snapshotOnlineItemTradeGold(trade, firstID), snapshotOnlineItemTradeGold(trade, secondID)
	repositories, ok := s.repositoryGroup()
	if !ok {
		s.cancelOnlineItemTrade(trade, "repository_unavailable")
		return nil
	}
	owner, err := itemtrade.NewOwner(repositories)
	if err != nil {
		s.cancelOnlineItemTrade(trade, "transaction_owner_unavailable")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	result, err := owner.Exchange(ctx,
		itemtrade.Participant{CharacterID: strconv.Itoa(int(firstID)), Offers: itemTradeDomainOffers(firstOffers), Gold: firstGold},
		itemtrade.Participant{CharacterID: strconv.Itoa(int(secondID)), Offers: itemTradeDomainOffers(secondOffers), Gold: secondGold},
	)
	if err != nil {
		s.logInfo("dnfbridge item trade atomic commit rejected", "first_char_id", firstID, "second_char_id", secondID, "error", err)
		s.cancelOnlineItemTrade(trade, "atomic_commit_rejected")
		return nil
	}
	s.clearOnlineItemTrade(trade)
	for _, characterID := range []uint16{firstID, secondID} {
		session, online := s.onlineGameSession(characterID)
		if !online {
			continue
		}
		body := buildOnlineItemTradeFinishBody(result.Received[strconv.Itoa(int(characterID))])
		if err := s.sendGameUpperRawClassCodec(session, currentItemTradeFinishMsg, body, 0, true); err != nil {
			return err
		}
		if character, changed := result.Characters[strconv.Itoa(int(characterID))]; changed {
			if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentGoldStateBody(character.Stats["gold"]), 0); err != nil {
				return err
			}
		}
		if err := s.sendSelectedCurrentContainerListsRefresh(session, "current_exe_item_trade_atomic_commit"); err != nil {
			return err
		}
	}
	s.logInfo("dnfbridge item trade atomic commit completed", "first_char_id", firstID, "second_char_id", secondID,
		"first_offer_count", len(firstOffers), "second_offer_count", len(secondOffers),
		"first_gold", firstGold, "second_gold", secondGold)
	return nil
}

func snapshotOnlineItemTradeGold(trade *onlineItemTrade, characterID uint16) int64 {
	if trade == nil {
		return 0
	}
	return trade.gold[characterID]
}

func snapshotOnlineItemTradeOffers(trade *onlineItemTrade, characterID uint16) []onlineItemTradeOffer {
	offers := make([]onlineItemTradeOffer, 0, len(trade.offers[characterID]))
	for _, offer := range trade.offers[characterID] {
		offers = append(offers, offer)
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].tradeSlot < offers[j].tradeSlot })
	return offers
}

func itemTradeDomainOffers(offers []onlineItemTradeOffer) []itemtrade.Offer {
	rows := make([]itemtrade.Offer, 0, len(offers))
	for _, offer := range offers {
		rows = append(rows, itemtrade.Offer{TradeSlot: offer.tradeSlot, SourceList: offer.sourceList,
			SourceSlot: offer.sourceSlot, Count: int64(offer.count), ExpectedItem: offer.itemID})
	}
	return rows
}

func (s *Service) clearOnlineItemTrade(trade *onlineItemTrade) {
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	if trade != nil {
		for _, characterID := range trade.participants {
			if s.itemTrades[characterID] == trade {
				delete(s.itemTrades, characterID)
			}
		}
	}
}

func (s *Service) cancelOnlineItemTrade(trade *onlineItemTrade, reason string) {
	if trade == nil {
		return
	}
	firstID, secondID := trade.participants[0], trade.participants[1]
	firstOffers, secondOffers := snapshotOnlineItemTradeOffers(trade, firstID), snapshotOnlineItemTradeOffers(trade, secondID)
	firstGold, secondGold := snapshotOnlineItemTradeGold(trade, firstID), snapshotOnlineItemTradeGold(trade, secondID)
	s.clearOnlineItemTrade(trade)
	for _, row := range []struct {
		characterID uint16
		offers      []onlineItemTradeOffer
		gold        int64
	}{{firstID, firstOffers, firstGold}, {secondID, secondOffers, secondGold}} {
		if session, online := s.onlineGameSession(row.characterID); online {
			_ = s.sendGameUpperRawClassCodec(session, currentItemTradeCancelMsg, buildOnlineItemTradeCancelBody(row.offers, row.gold), 0, true)
		}
	}
	s.logInfo("dnfbridge item trade cancelled", "first_char_id", firstID, "second_char_id", secondID, "reason", reason)
}

func (s *Service) detachOnlineItemTrade(session *gameSession, reason string) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return
	}
	s.tradeMu.Lock()
	trade := s.itemTrades[session.selectedCharacterID]
	s.tradeMu.Unlock()
	if trade != nil {
		s.cancelOnlineItemTrade(trade, reason)
	}
}

func buildOnlineItemTradeMoveAck(request inventory.MoveItemspaceRequest) []byte {
	var writer packetWriter
	writer.writeByte(request.SourceListType)
	writer.writeUint16(uint16(request.SourceSlotIndex))
	writer.writeInt32(int(request.MoveCount))
	writer.writeByte(request.DestinationListType)
	writer.writeUint16(uint16(request.DestinationSlotIndex))
	return writer.bytes()
}

func buildOnlineItemTradeStateBody(characterID uint16, state byte) []byte {
	var writer packetWriter
	writer.writeUint16(characterID)
	writer.writeByte(state)
	return writer.bytes()
}

func buildOnlineItemTradeCancelBody(offers []onlineItemTradeOffer, gold int64) []byte {
	var writer packetWriter
	count := len(offers)
	if gold > 0 {
		count++
	}
	writer.writeUint16(uint16(count))
	if gold > 0 {
		writer.writeUint16(0)
		writer.writeByte(dnfrepo.MainInventoryListType)
		writer.writeUint16(0)
	}
	for _, offer := range offers {
		writer.writeUint16(offer.tradeSlot)
		writer.writeByte(offer.sourceList)
		writer.writeUint16(uint16(offer.sourceSlot))
	}
	return writer.bytes()
}

func buildOnlineItemTradeFinishBody(transfers []itemtrade.Transfer) []byte {
	var writer packetWriter
	writer.writeUint16(uint16(len(transfers)))
	for _, transfer := range transfers {
		writer.writeUint16(transfer.TradeSlot)
		writer.writeUint16(uint16(transfer.DestinationSlot))
	}
	return writer.bytes()
}

func cloneOnlineItemTradeStack(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	if stack.Extra != nil {
		extra := make(map[string]string, len(stack.Extra))
		for key, value := range stack.Extra {
			extra[key] = value
		}
		stack.Extra = extra
	}
	return stack
}
