package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentJoustHistoryRecordCount = 500
	currentJoustStatePushMsgID     = 1240
	currentJoustRosterPushMsgID    = 1241
	currentJoustPoolPushMsgID      = 1242
	currentJoustMatchPushMsgID     = 1243
	currentJoustRosterCount        = 8
	currentJoustRosterRecordSize   = 11
	currentJoustStateOpening       = 1
)

func joustGameplayModule() gameplayModuleDefinition {
	infoOpcode := uint16(dnfenum.CmdPacketJoustInfo)
	bettingOpcode := uint16(dnfenum.CmdPacketJoustBetting)
	historyOpcode := uint16(dnfenum.CmdPacketJoustMatchHistory)
	infoHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return handleCurrentJoustInfo(service, session, request.Body)
	}
	historyHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return handleCurrentJoustHistory(service, session, request.Body)
	}
	bettingHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentJoustBetting(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name: "joust",
		LegacyHandlers: map[uint16]gameplayHandler{
			infoOpcode:    infoHandler,
			bettingOpcode: bettingHandler,
			historyOpcode: historyHandler,
		},
		UpperHandlers: map[uint16]gameplayHandler{
			infoOpcode: defaultClassGameplayHandler(
				"game-joust-info-class-blocked",
				"current_exe_joust_info_requires_default_class",
				handleCurrentJoustInfo,
			),
			historyOpcode: defaultClassGameplayHandler(
				"game-joust-history-class-blocked",
				"current_exe_joust_history_requires_default_class",
				handleCurrentJoustHistory,
			),
			bettingOpcode: defaultClassGameplayHandler(
				"game-joust-betting-class-blocked",
				"current_exe_joust_betting_requires_default_class",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentJoustBetting(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			infoOpcode:    stripCurrentJoustOpaqueQueryPrefix,
			bettingOpcode: stripCurrentJoustOpaqueBettingPrefix,
			historyOpcode: stripCurrentJoustOpaqueQueryPrefix,
		},
	}
}

// Current NoPack's sub_E72300 and sub_F2B9D0 write exactly 13 bytes from a
// native-generated opaque buffer for op1291 and op1293. Neither query appends
// a gameplay field. Strip only that exact observed shape; every other length
// remains visible to the strict handlers and fails closed.
func stripCurrentJoustOpaqueQueryPrefix(body []byte) []byte {
	if len(body) != 13 {
		return body
	}
	return nil
}

// The live current client sent op1292 as the same 13-byte native transport
// prefix followed by the nine business bytes written by sub_F296C0. Preserve
// only the proved 22-byte shape and expose {u8 knight,u32 slot,u32 amount} to
// the strict betting decoder.
func stripCurrentJoustOpaqueBettingPrefix(body []byte) []byte {
	const (
		opaquePrefixSize = 13
		businessSize     = 9
	)
	if len(body) != opaquePrefixSize+businessSize {
		return body
	}
	return append([]byte(nil), body[opaquePrefixSize:]...)
}

// Current NoPack sends both queries with an empty logical body. The native
// handlers consume a u32 result after the upper success envelope. Result zero
// is the proved success value.
func handleCurrentJoustInfo(service *Service, session *gameSession, body []byte) error {
	if len(body) != 0 {
		service.logGameEvent(session, "game-joust-info-deferred",
			"body_len", len(body),
			"reason", "current_exe_joust_info_body_mismatch")
		return nil
	}
	var response packetWriter
	response.writeUint32(0)
	// Never replay op1240 here. Current EXE reacts to its "betting started"
	// notification by issuing op1291 again, so an op1240 replay creates an
	// unbounded request/notification loop that floods the UI. The initial-town
	// snapshot also arrives before the native joust window has been constructed:
	// its correct op1242 personal-support field is therefore not rendered until
	// a wager causes a later roster/pool push. Once the window asks op1291, its
	// result ACK is the safe readiness boundary. Republish only op1241/op1242
	// (and the current progressive bracket, when applicable) after that ACK.
	if err := service.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketJoustInfo), response.bytes()); err != nil {
		return err
	}
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if err := service.sendCurrentJoustOpeningRoster(session); err != nil {
		service.logGameEvent(session, "game-joust-info-refresh-skipped",
			"reason", err.Error(),
			"refresh", "post_op1291_roster_pool")
	}
	return nil
}

func (service *Service) sendCurrentJoustOpeningState(session *gameSession) error {
	if service == nil || session == nil {
		return nil
	}
	if session.selectedCharacterID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		defer cancel()
		service.joustOperationMu.Lock()
		result, settleErr := service.settleCurrentJoustForSession(ctx, session, service.gameplayNow().UTC())
		service.joustOperationMu.Unlock()
		if settleErr != nil {
			service.logGameEvent(session, "game-joust-reconnect-settlement-deferred", "reason", settleErr.Error())
		} else if result.Settled && result.Won && result.MailID != "" {
			if err := service.sendMailboxAlarmToOnlineRecipient(session.selectedCharacterID); err != nil {
				// The settlement and mail are already durable. Mailbox open will
				// refresh the native list, so a notice failure is never retried as
				// a second payout.
				service.logGameEvent(session, "game-joust-settlement-mail-alarm-deferred", "mail_id", result.MailID, "reason", err.Error())
			}
		}
	}
	return service.sendGameUpperRawClass(
		session,
		currentJoustStatePushMsgID,
		buildCurrentJoustState(dnfjoust.TimelineAt(service.gameplayNow())),
		0,
	)
}

// buildCurrentJoustOpeningState matches sub_E727F0 exactly: u16 round then u8
// state. State 1 initializes the type-73 activity and its owner-608 companion;
// zero leaves owner 609's native open gate disabled even when event 2365 is in
// the active-event map.
func buildCurrentJoustOpeningState(round uint16) []byte {
	return buildCurrentJoustState(dnfjoust.Timeline{Round: round, State: 1})
}

func buildCurrentJoustState(timeline dnfjoust.Timeline) []byte {
	var body packetWriter
	body.writeUint16(timeline.Round)
	body.writeByte(timeline.State)
	return body.bytes()
}

func (service *Service) sendCurrentJoustOpeningRoster(session *gameSession) error {
	if service == nil || session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	now := service.gameplayNow().UTC()
	opening, err := service.currentJoustOpening(ctx, session, now)
	if err != nil {
		return err
	}
	roster, err := buildCurrentJoustOpeningRoster(opening)
	if err != nil {
		return err
	}
	if err := service.sendGameUpperRawClass(session, currentJoustRosterPushMsgID, roster, 0); err != nil {
		return err
	}
	personalSupport, err := service.currentJoustPersonalSupport(ctx, session, opening.Number)
	if err != nil {
		return err
	}
	pool, err := buildCurrentJoustPool(opening, personalSupport)
	if err != nil {
		return err
	}
	if err := service.sendGameUpperRawClass(session, currentJoustPoolPushMsgID, pool, 0); err != nil {
		return err
	}
	timeline := dnfjoust.TimelineAt(now)
	if timeline.Phase == dnfjoust.PhaseBetting {
		return nil
	}
	catalog, err := service.currentJoustCatalog(ctx)
	if err != nil {
		return err
	}
	tournament, err := catalog.TournamentFor(opening.Number)
	if err != nil {
		return err
	}
	matchBody, err := buildCurrentJoustMatchSnapshot(opening.Number, timeline.Stage, tournament)
	if err != nil {
		return err
	}
	return service.sendGameUpperRawClass(session, currentJoustMatchPushMsgID, matchBody, 0)
}

// buildCurrentJoustOpeningRoster returns the exact 90-byte layout consumed by
// sub_E72490: u16 round followed by eight packed 11-byte records. Current EXE
// proves each record as {u8 knight,u8 attack_type,f32 multiplier,u16 wins,
// u16 losses,u8 status}: sub_F2C990 reads the float at offset 2, sub_F2BF70
// reads the two history counters at offsets 6/8 and the status at offset 10.
func buildCurrentJoustOpeningRoster(opening dnfjoust.OpeningRound) ([]byte, error) {
	if len(opening.Riders) != currentJoustRosterCount {
		return nil, fmt.Errorf("joust opening riders=%d want=%d", len(opening.Riders), currentJoustRosterCount)
	}
	var body packetWriter
	body.writeUint16(opening.Number)
	for _, rider := range opening.Riders {
		if rider.Multiplier <= 0 || math.IsNaN(float64(rider.Multiplier)) || math.IsInf(float64(rider.Multiplier), 0) {
			return nil, fmt.Errorf("joust rider=%d invalid multiplier=%v", rider.ID, rider.Multiplier)
		}
		body.writeByte(rider.ID)
		body.writeByte(rider.AttackType)
		body.writeUint32(math.Float32bits(rider.Multiplier))
		body.writeUint16(rider.Wins)
		body.writeUint16(rider.Losses)
		body.writeByte(rider.Status)
	}
	if len(body.bytes()) != 2+currentJoustRosterCount*currentJoustRosterRecordSize {
		return nil, fmt.Errorf("joust roster size=%d", len(body.bytes()))
	}
	return body.bytes(), nil
}

// buildCurrentJoustPool matches sub_E72540's exact 46-byte read: u16 round,
// u32 selected-character support, then eight packed {u8 knight,u32 support}
// public-pool records. sub_F297B0 subtracts the selected-character value from
// PVF [max betting], so writing the public total there makes the UI negative.
func buildCurrentJoustPool(opening dnfjoust.OpeningRound, personalSupport uint32) ([]byte, error) {
	if len(opening.Riders) != currentJoustRosterCount || opening.TotalSupport == 0 {
		return nil, fmt.Errorf("joust pool riders=%d total=%d", len(opening.Riders), opening.TotalSupport)
	}
	if personalSupport > dnfjoust.MaximumBetPerRound {
		return nil, fmt.Errorf("joust personal support=%d", personalSupport)
	}
	var body packetWriter
	body.writeUint16(opening.Number)
	body.writeUint32(personalSupport)
	for _, rider := range opening.Riders {
		if rider.Support == 0 {
			return nil, fmt.Errorf("joust rider=%d has zero support", rider.ID)
		}
		body.writeByte(rider.ID)
		body.writeUint32(rider.Support)
	}
	if len(body.bytes()) != 46 {
		return nil, fmt.Errorf("joust pool size=%d", len(body.bytes()))
	}
	return body.bytes(), nil
}

func (service *Service) currentJoustPersonalSupport(
	ctx context.Context,
	session *gameSession,
	round uint16,
) (uint32, error) {
	if service == nil || session == nil || session.selectedCharacterID == 0 {
		return 0, nil
	}
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Character == nil {
		return 0, nil
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	storedRound, _, amount, valid := currentJoustBettingLedgerForCharacter(character)
	if !valid || storedRound != round {
		return 0, nil
	}
	return amount, nil
}

func (service *Service) currentJoustOpening(
	ctx context.Context,
	session *gameSession,
	now time.Time,
) (dnfjoust.OpeningRound, error) {
	catalog, err := service.currentJoustCatalog(ctx)
	if err != nil {
		return dnfjoust.OpeningRound{}, err
	}
	round := dnfjoust.RoundNumberAt(now)
	accountID := service.accountIDForSession(session)
	return service.currentJoustOpeningForRound(ctx, catalog, accountID, round)
}

func (service *Service) currentJoustOpeningForRound(
	ctx context.Context,
	catalog *dnfjoust.Catalog,
	accountID string,
	round uint16,
) (dnfjoust.OpeningRound, error) {
	ledgers := make([]dnfjoust.BettingLedger, 0, 8)
	if repositories, ok := service.repositoryGroup(); ok && repositories.Character != nil {
		characters, err := repositories.Character.ListByAccount(ctx, accountID, 32)
		if err != nil {
			return dnfjoust.OpeningRound{}, err
		}
		for _, character := range characters {
			ledgers = append(ledgers, currentJoustBettingLedgersForCharacter(character)...)
		}
	}
	opening, err := catalog.OpeningWithLedgers(round, ledgers)
	if err != nil {
		return dnfjoust.OpeningRound{}, err
	}
	counters, err := service.currentJoustRecordedWinLoss(ctx, accountID, catalog)
	if err != nil {
		return dnfjoust.OpeningRound{}, err
	}
	for index := range opening.Riders {
		opening.Riders[index].Wins = counters[opening.Riders[index].ID][0]
		opening.Riders[index].Losses = counters[opening.Riders[index].ID][1]
	}
	return opening, nil
}

func currentJoustBettingLedgersForCharacter(character dnfrepo.CharacterRecord) []dnfjoust.BettingLedger {
	if character.Stats == nil {
		return nil
	}
	roundValue := character.Stats[dnfjoust.RoundStat]
	if roundValue < 0 || roundValue > math.MaxUint16 {
		return nil
	}
	round := uint16(roundValue)
	bets, _, valid := dnfjoust.CurrentRoundBets(character.Stats, round)
	if !valid || len(bets) == 0 {
		return nil
	}
	knights := make([]int, 0, len(bets))
	for knight := range bets {
		knights = append(knights, int(knight))
	}
	sort.Ints(knights)
	ledgers := make([]dnfjoust.BettingLedger, 0, len(knights))
	for _, knight := range knights {
		amount := bets[byte(knight)]
		if amount != 0 {
			ledgers = append(ledgers, dnfjoust.BettingLedger{Round: round, Knight: byte(knight), Amount: amount, Valid: true})
		}
	}
	return ledgers
}

// buildCurrentJoustMatchSnapshot matches sub_E72600/sub_F34350: u16 round,
// u8 stage and seven packed DWORD matches. Each DWORD is winner id/profile then
// loser id/profile. The profile byte is the current EXE's 0..3 opponent attack
// type timeline key, not a random entry inside the seven-value PVF timeline.
// The four quarter-finals, two semi-finals and final are always present; stage
// controls the client's progressive reveal animation.
func buildCurrentJoustMatchSnapshot(round uint16, stage byte, tournament dnfjoust.Tournament) ([]byte, error) {
	if stage > 3 || tournament.Round != round {
		return nil, fmt.Errorf("joust match snapshot round=%d tournament=%d stage=%d", round, tournament.Round, stage)
	}
	var body packetWriter
	body.writeUint16(round)
	body.writeByte(stage)
	for _, match := range tournament.Matches {
		body.writeByte(match.Winner)
		body.writeByte(match.WinnerAction)
		body.writeByte(match.Loser)
		body.writeByte(match.LoserAction)
	}
	if len(body.bytes()) != 31 {
		return nil, fmt.Errorf("joust match snapshot size=%d", len(body.bytes()))
	}
	return body.bytes(), nil
}

// The current client always reads the complete fixed 500-entry history array
// after a zero result. Each entry is u16 sequence, u8 winner, f32 multiplier. A zero
// sequence is the native empty-list sentinel; the remaining fixed bytes still
// have to be present for the parser to stay aligned.
func handleCurrentJoustHistory(service *Service, session *gameSession, body []byte) error {
	if len(body) != 0 {
		service.logGameEvent(session, "game-joust-history-deferred",
			"body_len", len(body),
			"reason", "current_exe_joust_history_body_mismatch")
		return nil
	}
	var response packetWriter
	response.writeUint32(0)
	records, err := service.currentJoustHistory(context.Background(), session, service.gameplayNow().UTC())
	if err != nil {
		service.logGameEvent(session, "game-joust-history-fallback", "reason", err.Error())
	}
	for _, record := range records {
		response.writeUint16(record.Round)
		response.writeByte(record.Winner)
		response.writeUint32(math.Float32bits(record.Multiplier))
	}
	if remaining := currentJoustHistoryRecordCount - len(records); remaining > 0 {
		response.writeBytes(make([]byte, remaining*7))
	}
	return service.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketJoustMatchHistory), response.bytes())
}
