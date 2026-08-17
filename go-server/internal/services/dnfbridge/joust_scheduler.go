package dnfbridge

import (
	"context"
	"strconv"
	"time"

	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
)

const currentJoustPhaseTimerName = "joust:phase"

func (service *Service) startCurrentJoustScheduler(ctx context.Context) {
	if service == nil {
		return
	}
	if err := service.scheduleNextCurrentJoustBoundary(ctx); err != nil {
		service.logWarn("schedule joust boundary failed", "error", err)
	}
}

func (service *Service) scheduleNextCurrentJoustBoundary(ctx context.Context) error {
	queue := service.ensureGameplayTimeQueue()
	if queue == nil {
		return errGameplayTimeQueueUnavailable
	}
	now := queue.Now().UTC()
	delay := dnfjoust.NextBoundaryAfter(now).Sub(now)
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	return queue.ScheduleAfter(currentJoustPhaseTimerName, delay, func(due time.Time) {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		service.handleCurrentJoustBoundary(due.UTC())
		if err := service.scheduleNextCurrentJoustBoundary(ctx); err != nil && (ctx == nil || ctx.Err() == nil) {
			service.logWarn("reschedule joust boundary failed", "error", err)
		}
	})
}

func (service *Service) handleCurrentJoustBoundary(now time.Time) {
	timeline := dnfjoust.TimelineAt(now)
	sessions := service.currentJoustSessionSnapshot()
	if len(sessions) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*createWriteTimeout)
	defer cancel()

	if timeline.Phase == dnfjoust.PhaseSettled {
		accounts := make(map[string]struct{})
		for _, session := range sessions {
			accounts[service.accountIDForSession(session)] = struct{}{}
		}
		service.joustOperationMu.Lock()
		for accountID := range accounts {
			results, err := service.settleCurrentJoustAccount(ctx, accountID, now)
			if err != nil {
				service.logWarn("joust settlement deferred", "account", accountID, "round", timeline.Round, "error", err)
			}
			for _, result := range results {
				if !result.Won || result.MailID == "" {
					continue
				}
				characterID, parseErr := strconv.ParseUint(result.CharacterID, 10, 16)
				if parseErr != nil {
					continue
				}
				if session, found := service.onlineGameSession(uint16(characterID)); found {
					if alarmErr := service.sendMailboxAlarmToOnlineRecipient(session.selectedCharacterID); alarmErr != nil {
						service.logGameEvent(session, "game-joust-settlement-mail-alarm-deferred", "mail_id", result.MailID, "reason", alarmErr.Error())
					}
				}
			}
		}
		service.joustOperationMu.Unlock()
	}

	for _, session := range sessions {
		if err := service.pushCurrentJoustBoundarySnapshot(ctx, session, timeline); err != nil {
			service.logGameEvent(session, "game-joust-boundary-push-failed", "round", timeline.Round, "phase", timeline.Phase, "reason", err.Error())
		}
	}
}

func (service *Service) pushCurrentJoustBoundarySnapshot(
	ctx context.Context,
	session *gameSession,
	timeline dnfjoust.Timeline,
) error {
	// A state transition is the native animation trigger. In particular, the
	// semi-final still uses state 2 but needs a fresh state packet before its
	// stage-1 bracket snapshot, otherwise the current EXE paints the outcome
	// directly and skips its countdown/battle/horse-flash sequence.
	sendState := timeline.Phase == dnfjoust.PhaseBetting ||
		timeline.Phase == dnfjoust.PhaseQuarterFinal ||
		timeline.Phase == dnfjoust.PhaseSemiFinal ||
		(timeline.Phase == dnfjoust.PhaseFinal && timeline.Stage == 2) ||
		timeline.Phase == dnfjoust.PhaseSettled
	if sendState {
		if err := service.sendGameUpperRawClass(session, currentJoustStatePushMsgID, buildCurrentJoustState(timeline), 0); err != nil {
			return err
		}
	}
	if timeline.Phase == dnfjoust.PhaseBetting {
		return service.sendCurrentJoustOpeningRoster(session)
	}
	catalog, err := service.currentJoustCatalog(ctx)
	if err != nil {
		return err
	}
	tournament, err := catalog.TournamentFor(timeline.Round)
	if err != nil {
		return err
	}
	body, err := buildCurrentJoustMatchSnapshot(timeline.Round, timeline.Stage, tournament)
	if err != nil {
		return err
	}
	return service.sendGameUpperRawClass(session, currentJoustMatchPushMsgID, body, 0)
}

func (service *Service) currentJoustSessionSnapshot() []*gameSession {
	if service == nil {
		return nil
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	sessions := make([]*gameSession, 0, len(service.gameSessions))
	seen := make(map[*gameSession]struct{}, len(service.gameSessions))
	for _, session := range service.gameSessions {
		if session == nil || session.selectedCharacterID == 0 {
			continue
		}
		if _, duplicate := seen[session]; duplicate {
			continue
		}
		seen[session] = struct{}{}
		sessions = append(sessions, session)
	}
	return sessions
}
