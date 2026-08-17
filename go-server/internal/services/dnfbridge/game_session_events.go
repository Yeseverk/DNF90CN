package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"longheng.io/server/internal/platform/eventloop"
)

const (
	gameSessionEventQueueSize = 512
	gameSessionShutdownWait   = 2 * createWriteTimeout
)

var errInvalidGameSessionEvent = errors.New("dnf game session event is invalid")

type gameSessionEvents struct {
	loop   *eventloop.Loop
	active atomic.Bool
}

type gameSessionEventTask struct {
	source              string
	characterID         uint16
	characterGeneration uint64
	async               bool
	run                 func() error
}

func (s *Service) startGameSessionEvents(ctx context.Context, session *gameSession) error {
	if s == nil || session == nil {
		return errInvalidGameSessionEvent
	}
	if session.events != nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if session.characterGeneration == 0 {
		session.characterGeneration = 1
	}
	events := &gameSessionEvents{}
	events.loop = eventloop.New(
		"dnf-game-session-"+session.connID,
		eventloop.ProcessorFunc(func(_ context.Context, event eventloop.Event) eventloop.Result {
			task, ok := event.Data.(gameSessionEventTask)
			if !ok || task.run == nil {
				return eventloop.Result{Err: errInvalidGameSessionEvent}
			}
			if task.async {
				if !events.active.Load() {
					return eventloop.Result{}
				}
				if task.characterGeneration != 0 &&
					(task.characterGeneration != session.characterGeneration ||
						task.characterID == 0 ||
						task.characterID != session.selectedCharacterID) {
					s.logGameEvent(session, "game-session-event-stale",
						"source", task.source,
						"event_char_id", task.characterID,
						"selected_char_id", session.selectedCharacterID,
						"event_generation", task.characterGeneration,
						"current_generation", session.characterGeneration,
						"reason", "selected_character_or_generation_changed")
					return eventloop.Result{}
				}
			}
			return eventloop.Result{Err: task.run()}
		}),
		eventloop.Options{
			QueueSize:         gameSessionEventQueueSize,
			CallbackQueueSize: gameSessionEventQueueSize,
			CallbackTimeout:   gameSessionShutdownWait,
			SlowThreshold:     createWriteTimeout,
			Logger:            s.logger,
		},
	)
	if err := events.loop.Start(ctx); err != nil {
		return err
	}
	events.active.Store(true)
	session.events = events
	s.logGameEvent(session, "game-session-events-started",
		"character_generation", session.characterGeneration,
		"queue_size", gameSessionEventQueueSize)
	return nil
}

func (s *Service) beginGameSessionClose(session *gameSession) {
	if session == nil || session.events == nil {
		return
	}
	session.events.active.Store(false)
}

func (s *Service) stopGameSessionEvents(ctx context.Context, session *gameSession) error {
	if session == nil || session.events == nil || session.events.loop == nil {
		return nil
	}
	session.events.active.Store(false)
	if ctx == nil {
		ctx = context.Background()
	}
	return session.events.loop.Stop(ctx)
}

func (s *Service) callGameSession(
	ctx context.Context,
	session *gameSession,
	source string,
	run func() error,
) error {
	if run == nil {
		return errInvalidGameSessionEvent
	}
	if session == nil {
		return errInvalidGameSessionEvent
	}
	if session.events == nil || session.events.loop == nil {
		return run()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callback := make(chan eventloop.Result, 1)
	event := eventloop.Event{
		Type:     source,
		Data:     gameSessionEventTask{source: source, run: run},
		Priority: eventloop.PriorityMedium,
		Callback: callback,
		Context:  ctx,
	}
	if err := session.events.loop.SubmitBlocking(ctx, event); err != nil {
		return err
	}
	select {
	case result := <-callback:
		return result.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) postGameSessionCharacterEvent(
	session *gameSession,
	source string,
	characterID uint16,
	characterGeneration uint64,
	run func() error,
) error {
	if run == nil {
		return errInvalidGameSessionEvent
	}
	if session == nil {
		return errInvalidGameSessionEvent
	}
	if session.events == nil || session.events.loop == nil {
		return run()
	}
	ctx := session.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return session.events.loop.SubmitBlocking(ctx, eventloop.Event{
		Type: source,
		Data: gameSessionEventTask{
			source:              source,
			characterID:         characterID,
			characterGeneration: characterGeneration,
			async:               true,
			run:                 run,
		},
		Priority: eventloop.PriorityMedium,
		Context:  ctx,
	})
}

func (s *Service) shutdownGameSessionEvents(session *gameSession, closeDproto bool) {
	if session == nil {
		return
	}
	s.beginGameSessionClose(session)

	ctx, cancel := context.WithTimeout(context.Background(), gameSessionShutdownWait)
	defer cancel()
	var cleanupCompleted atomic.Bool
	cleanup := func() error {
		if closeDproto {
			s.closeGameDprotoSession(session)
		}
		s.cleanupOnlinePlayer(session)
		s.unbindGameSession(session)
		cleanupCompleted.Store(true)
		return nil
	}
	cleanupErr := s.callGameSession(ctx, session, "game-session-shutdown", cleanup)
	stopErr := s.stopGameSessionEvents(ctx, session)
	if !cleanupCompleted.Load() && stopErr == nil {
		cleanupErr = errors.Join(cleanupErr, cleanup())
	}
	if cleanupErr != nil || stopErr != nil {
		s.logGameEvent(session, "game-session-events-shutdown-failed",
			"cleanup_error", cleanupErr,
			"stop_error", stopErr)
		return
	}
	s.logGameEvent(session, "game-session-events-stopped",
		"character_generation", session.characterGeneration)
}

func advanceGameSessionCharacterGeneration(session *gameSession) uint64 {
	if session == nil {
		return 0
	}
	session.characterGeneration++
	if session.characterGeneration == 0 {
		session.characterGeneration++
	}
	return session.characterGeneration
}

func gameSessionCharacterEventIdentity(session *gameSession, characterID uint16) (uint16, uint64, error) {
	if session == nil || characterID == 0 || characterID != session.selectedCharacterID {
		return 0, 0, fmt.Errorf("%w: selected character changed", errInvalidGameSessionEvent)
	}
	return characterID, session.characterGeneration, nil
}

func isClosedGameSessionEventError(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, eventloop.ErrClosed) ||
		errors.Is(err, eventloop.ErrDraining) ||
		errors.Is(err, eventloop.ErrNotStarted)
}
