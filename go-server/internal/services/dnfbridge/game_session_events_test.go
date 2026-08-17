package dnfbridge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"longheng.io/server/internal/platform/eventloop"
)

func TestGameSessionEventsSerializeInboundAndTimerWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := &Service{}
	session := &gameSession{
		connID:              "event-serialization",
		ctx:                 ctx,
		selectedCharacterID: 19,
		characterGeneration: 7,
	}
	if err := service.startGameSessionEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	defer stopTestGameSessionEvents(t, service, session)

	packetStarted := make(chan struct{})
	releasePacket := make(chan struct{})
	packetDone := make(chan error, 1)
	go func() {
		packetDone <- service.callGameSession(ctx, session, "blocked-inbound-packet", func() error {
			close(packetStarted)
			<-releasePacket
			return nil
		})
	}()
	waitTestSignal(t, packetStarted, "inbound packet did not start")

	timerRan := make(chan struct{})
	if err := service.postGameSessionCharacterEvent(
		session,
		"queued-timer",
		19,
		7,
		func() error {
			close(timerRan)
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	select {
	case <-timerRan:
		t.Fatal("timer callback ran concurrently with the inbound packet")
	default:
	}

	close(releasePacket)
	if err := <-packetDone; err != nil {
		t.Fatal(err)
	}
	waitTestSignal(t, timerRan, "queued timer did not run after the inbound packet")
}

func TestGameSessionEventsDropOldCharacterGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := &Service{}
	session := &gameSession{
		connID:              "event-generation",
		ctx:                 ctx,
		selectedCharacterID: 19,
		characterGeneration: 11,
	}
	if err := service.startGameSessionEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	defer stopTestGameSessionEvents(t, service, session)

	oldCharacterID, oldGeneration, err := gameSessionCharacterEventIdentity(session, 19)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.callGameSession(ctx, session, "switch-character", func() error {
		advanceGameSessionCharacterGeneration(session)
		session.selectedCharacterID = 20
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var staleRan atomic.Bool
	if err := service.postGameSessionCharacterEvent(
		session,
		"old-character-timer",
		oldCharacterID,
		oldGeneration,
		func() error {
			staleRan.Store(true)
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := service.callGameSession(ctx, session, "old-character-barrier", func() error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if staleRan.Load() {
		t.Fatal("old character timer crossed the character generation boundary")
	}

	currentCharacterID, currentGeneration, err := gameSessionCharacterEventIdentity(session, 20)
	if err != nil {
		t.Fatal(err)
	}
	currentRan := make(chan struct{})
	if err := service.postGameSessionCharacterEvent(
		session,
		"current-character-timer",
		currentCharacterID,
		currentGeneration,
		func() error {
			close(currentRan)
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	waitTestSignal(t, currentRan, "current character timer was not delivered")
}

func TestGameSessionCharacterLifecycleAdvancesGeneration(t *testing.T) {
	service := &Service{gameSessions: make(map[uint16]*gameSession)}
	session := &gameSession{selectedCharacterID: 19, characterGeneration: 5}
	service.gameSessions[19] = session

	service.bindGameSessionCharacter(session, 20)
	if session.selectedCharacterID != 20 || session.characterGeneration != 6 {
		t.Fatalf("character switch selected=%d generation=%d", session.selectedCharacterID, session.characterGeneration)
	}
	service.bindGameSessionCharacter(session, 20)
	if session.characterGeneration != 6 {
		t.Fatalf("same character rebind advanced generation=%d", session.characterGeneration)
	}

	previousCharacterID, _ := service.resetGameSessionForCharacterSelect(session)
	if previousCharacterID != 20 || session.selectedCharacterID != 0 || session.characterGeneration != 7 {
		t.Fatalf("return select previous=%d selected=%d generation=%d",
			previousCharacterID, session.selectedCharacterID, session.characterGeneration)
	}
}

func TestGameSessionEventsRejectTimerAfterStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := &Service{}
	session := &gameSession{
		connID:              "event-stop",
		ctx:                 ctx,
		selectedCharacterID: 19,
		characterGeneration: 3,
	}
	if err := service.startGameSessionEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	stopTestGameSessionEvents(t, service, session)

	var ran atomic.Bool
	err := service.postGameSessionCharacterEvent(session, "closed-timer", 19, 3, func() error {
		ran.Store(true)
		return nil
	})
	if !errors.Is(err, eventloop.ErrClosed) {
		t.Fatalf("post after stop error=%v want=%v", err, eventloop.ErrClosed)
	}
	if ran.Load() {
		t.Fatal("timer callback ran after the session event loop stopped")
	}
}

func TestDungeonDeathTimerRunsOnGameSessionEvents(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session.ctx = ctx
	if err := service.startGameSessionEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	defer stopTestGameSessionEvents(t, service, session)

	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	if err := service.callGameSession(ctx, session, "arm-death-return", func() error {
		return service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize))
	}); err != nil {
		t.Fatal(err)
	}
	task := queue.task(t, 0)
	session.conn.(*bufferConn).write.Reset()

	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- service.callGameSession(ctx, session, "blocked-session-owner", func() error {
			close(ownerStarted)
			<-releaseOwner
			return nil
		})
	}()
	waitTestSignal(t, ownerStarted, "session owner did not block")
	queue.fire(task, false)
	if got := session.conn.(*bufferConn).write.Len(); got != 0 {
		t.Fatalf("death timer bypassed session events and wrote %d bytes", got)
	}

	close(releaseOwner)
	if err := <-ownerDone; err != nil {
		t.Fatal(err)
	}
	if err := service.callGameSession(ctx, session, "death-return-barrier", func() error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := session.conn.(*bufferConn).write.Len(); got == 0 {
		t.Fatal("death timer did not execute after the session owner was released")
	}
}

func TestPetGrowthTimerRunsOnGameSessionEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service := &Service{gameplayTimers: queue}
	session := &gameSession{
		connID:              "pet-event-owner",
		ctx:                 ctx,
		selectedCharacterID: 19,
	}
	if err := service.startGameSessionEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	defer stopTestGameSessionEvents(t, service, session)
	if err := service.callGameSession(ctx, session, "arm-pet-growth", func() error {
		return service.switchCurrentPetGrowthClock(
			session,
			currentPetGrowthClockDungeon,
			queue.Now(),
			"test_event_owner",
		)
	}); err != nil {
		t.Fatal(err)
	}
	task := queue.task(t, 0)

	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- service.callGameSession(ctx, session, "blocked-pet-owner", func() error {
			close(ownerStarted)
			<-releaseOwner
			return nil
		})
	}()
	waitTestSignal(t, ownerStarted, "pet session owner did not block")
	queue.fire(task, false)
	scheduled, _, active := queue.counts()
	if scheduled != 1 || active != 0 {
		t.Fatalf("pet timer bypassed session events scheduled=%d active=%d", scheduled, active)
	}

	close(releaseOwner)
	if err := <-ownerDone; err != nil {
		t.Fatal(err)
	}
	if err := service.callGameSession(ctx, session, "pet-growth-barrier", func() error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scheduled, _, active = queue.counts()
	if scheduled != 2 || active != 1 {
		t.Fatalf("pet timer did not rearm on session owner scheduled=%d active=%d", scheduled, active)
	}
}

func stopTestGameSessionEvents(t *testing.T, service *Service, session *gameSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.stopGameSessionEvents(ctx, session); err != nil && !errors.Is(err, eventloop.ErrClosed) {
		t.Fatal(err)
	}
}

func waitTestSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}
