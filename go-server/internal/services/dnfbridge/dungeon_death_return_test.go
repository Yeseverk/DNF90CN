package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	"longheng.io/server/internal/modules/dnf/worldmap"
	"longheng.io/server/internal/platform/timequeue"
)

type fakeCurrentDungeonDeathTimerTask struct {
	name      string
	delay     time.Duration
	callback  timequeue.Callback
	cancelled bool
}

type fakeCurrentDungeonDeathTimerQueue struct {
	mu            sync.Mutex
	now           time.Time
	active        map[string]*fakeCurrentDungeonDeathTimerTask
	tasks         []*fakeCurrentDungeonDeathTimerTask
	scheduleCount int
	cancelCount   int
	scheduleErr   error
}

func newFakeCurrentDungeonDeathTimerQueue() *fakeCurrentDungeonDeathTimerQueue {
	return &fakeCurrentDungeonDeathTimerQueue{
		now:    time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC),
		active: make(map[string]*fakeCurrentDungeonDeathTimerTask),
	}
}

func (q *fakeCurrentDungeonDeathTimerQueue) Now() time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.now
}

func (q *fakeCurrentDungeonDeathTimerQueue) ScheduleAfter(
	name string,
	delay time.Duration,
	callback timequeue.Callback,
) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.scheduleErr != nil {
		return q.scheduleErr
	}
	if previous := q.active[name]; previous != nil {
		previous.cancelled = true
	}
	task := &fakeCurrentDungeonDeathTimerTask{name: name, delay: delay, callback: callback}
	q.active[name] = task
	q.tasks = append(q.tasks, task)
	q.scheduleCount++
	return nil
}

func (q *fakeCurrentDungeonDeathTimerQueue) Cancel(name string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	task := q.active[name]
	if task == nil {
		return false
	}
	delete(q.active, name)
	task.cancelled = true
	q.cancelCount++
	return true
}

func (q *fakeCurrentDungeonDeathTimerQueue) Start(ctx context.Context) {
	if ctx == nil {
		return
	}
	<-ctx.Done()
}

func (q *fakeCurrentDungeonDeathTimerQueue) task(t *testing.T, index int) *fakeCurrentDungeonDeathTimerTask {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	if index < 0 || index >= len(q.tasks) {
		t.Fatalf("death timer task[%d] missing; tasks=%d", index, len(q.tasks))
	}
	return q.tasks[index]
}

func (q *fakeCurrentDungeonDeathTimerQueue) counts() (scheduled, cancelled, active int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.scheduleCount, q.cancelCount, len(q.active)
}

func (q *fakeCurrentDungeonDeathTimerQueue) fire(task *fakeCurrentDungeonDeathTimerTask, forceStale bool) {
	q.mu.Lock()
	if q.active[task.name] == task {
		delete(q.active, task.name)
	}
	cancelled := task.cancelled
	due := q.now.Add(task.delay)
	q.mu.Unlock()
	if cancelled && !forceStale {
		return
	}
	task.callback(due)
}

func TestCurrentDungeonDieCharacterSchedulesExactDeathStateAndReturnsAfterTenSeconds(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	body := make([]byte, currentDungeonDieCharacterBodySize)
	binary.LittleEndian.PutUint16(body[0:2], 0xffec) // signed i16 -20 on the wire
	binary.LittleEndian.PutUint16(body[2:4], 234)
	packet, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketDieCharacter), body, 8, dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, packet); err != nil {
		t.Fatal(err)
	}

	deathState, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if deathState.Header.MsgID != currentDungeonDeathStateMsgID || deathState.Header.Classification != 0 || len(rest) != 0 {
		t.Fatalf("death state header=%+v body=%x rest=%x", deathState.Header, deathState.Body, rest)
	}
	wantDeathState := []byte{99, 0, 0, 0}
	if !bytes.Equal(deathState.Body, wantDeathState) {
		t.Fatalf("death state body=%x want=%x", deathState.Body, wantDeathState)
	}
	task := queue.task(t, 0)
	if task.delay != currentDungeonDeathReturnDelay {
		t.Fatalf("death timer delay=%s want=%s", task.delay, currentDungeonDeathReturnDelay)
	}
	if !runtime.deathReturnWaiting || runtime.deathReturnGeneration == 0 ||
		runtime.deathReturnDueAt != queue.now.Add(currentDungeonDeathReturnDelay) || runtime.lifecycleToken == 0 {
		t.Fatalf("armed death state=%+v", runtime)
	}

	session.conn.(*bufferConn).write.Reset()
	queue.fire(task, false)
	op24, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if op24.Header.MsgID != currentSceneTransitionMsgID || op24.Header.Classification != 0 ||
		!bytes.Equal(op24.Body, wantDungeonTownTransitionBody()) || len(rest) != 0 {
		t.Fatalf("death return header=%+v body=%x rest=%x", op24.Header, op24.Body, rest)
	}
	if runtime.deathReturnWaiting || !runtime.townReturnPending || !runtime.townReturnOp24Sent ||
		runtime.townReturnRequestMsgID != uint16(dnfenum.CmdPacketDieCharacter) || session.dungeon.runtime != runtime {
		t.Fatalf("post-death return state=%+v owner=%p", runtime, session.dungeon.runtime)
	}
}

func TestLegacyDungeonDieCharacterZeroTailSchedulesDeathReturn(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue

	// Live 2026-07-19 op40 death packet reached the legacy decoder as
	// x:u16,y:u16,zero:u16. The first four bytes are the current EXE's normal
	// two coordinate fields; the zero tail is transport-only.
	body := []byte{0xf4, 0x02, 0xda, 0x00, 0x00, 0x00}
	packet := buildLegacyGamePacketForDeathTest(
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketDieCharacter),
		51,
		body,
	)
	if err := service.handleLegacyGamePacket(session, packet); err != nil {
		t.Fatal(err)
	}

	deathState, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if deathState.Header.MsgID != currentDungeonDeathStateMsgID || deathState.Header.Classification != 0 || len(rest) != 0 {
		t.Fatalf("legacy death state header=%+v body=%x rest=%x", deathState.Header, deathState.Body, rest)
	}
	if want := []byte{99, 0, 0, 0}; !bytes.Equal(deathState.Body, want) {
		t.Fatalf("legacy death state body=%x want=%x", deathState.Body, want)
	}
	task := queue.task(t, 0)
	if task.delay != currentDungeonDeathReturnDelay {
		t.Fatalf("legacy death timer delay=%s want=%s", task.delay, currentDungeonDeathReturnDelay)
	}
	if !runtime.deathReturnWaiting || runtime.deathReturnGeneration == 0 {
		t.Fatalf("legacy death did not arm return runtime=%+v", runtime)
	}

	session.conn.(*bufferConn).write.Reset()
	queue.fire(task, false)
	op24, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if op24.Header.MsgID != currentSceneTransitionMsgID || op24.Header.Classification != 0 ||
		!bytes.Equal(op24.Body, wantDungeonTownTransitionBody()) || len(rest) != 0 {
		t.Fatalf("legacy death return header=%+v body=%x rest=%x", op24.Header, op24.Body, rest)
	}
}

func buildLegacyGamePacketForDeathTest(cmd byte, typ uint16, sequence uint16, body []byte) []byte {
	packet := make([]byte, dnfproto.LegacyGameHeaderSize+len(body))
	packet[0] = cmd
	binary.LittleEndian.PutUint16(packet[1:3], typ)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	binary.LittleEndian.PutUint32(packet[7:11], 0)
	binary.LittleEndian.PutUint16(packet[11:13], sequence)
	copy(packet[dnfproto.LegacyGameHeaderSize:], body)
	return packet
}

func TestCurrentDungeonDeathOwnerUsesSelectedProtocolIDWithoutParsingRecordID(t *testing.T) {
	_, session, runtime := newBackToVillageRuntime(t)
	if runtime.Character.CharacterID != "99" {
		t.Fatalf("fixture record id=%q", runtime.Character.CharacterID)
	}
	characterID, reason := currentDungeonDeathRuntimeOwner(runtime, session)
	if reason != "" || characterID != uint16(99) {
		t.Fatalf("death owner id=%d reason=%q", characterID, reason)
	}

	// The repository id remains a string owner. A mismatched protocol id is
	// rejected by the existing runtime owner check rather than parsed/coerced.
	session.selectedCharacterID = 100
	if characterID, reason = currentDungeonDeathRuntimeOwner(runtime, session); characterID != 0 ||
		reason != "active_dungeon_runtime_character_owner_mismatch" {
		t.Fatalf("mismatched death owner id=%d reason=%q", characterID, reason)
	}
}

func TestCurrentDungeonDeathTimerIsCancelledByRoomMove(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, false)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	session := &gameSession{
		conn:                &bufferConn{},
		connID:              "death-room-move-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime, runToken: 1},
	}
	runtime.lifecycleToken = 1
	armTestCurrentDungeonDeathReturn(t, service, session, runtime)

	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = 1
	if err := service.handleDungeonMoveMap(session, body); err != nil {
		t.Fatal(err)
	}
	scheduled, cancelled, active := queue.counts()
	if scheduled != 1 || cancelled != 1 || active != 0 || runtime.deathReturnWaiting {
		t.Fatalf("move cancellation scheduled=%d cancelled=%d active=%d runtime=%+v",
			scheduled, cancelled, active, runtime)
	}
	if scene, ok := runtime.Session.Scene(); !ok || scene.Coordinate != (worldmap.RoomCoordinate{X: 1, Y: 0}) {
		t.Fatalf("room move did not commit scene=%+v ok=%t", scene, ok)
	}
}

func TestCurrentDungeonDeathTimerIsCancelledBySettlementWithoutStartingSettlementTimer(t *testing.T) {
	service, runtime, session, conn, _ := prepareCompletedSettlementRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	if runtime.lifecycleToken == 0 {
		runtime.lifecycleToken = 1
		session.dungeon.runToken = 1
	}
	armTestCurrentDungeonDeathReturn(t, service, session, runtime)
	conn.write.Reset()

	session.dungeon.mu.Lock()
	err := service.sendCurrentDungeonSettlementEntryLocked(session, runtime, "test_settlement_replay_cancels_death")
	session.dungeon.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	scheduled, cancelled, active := queue.counts()
	if scheduled != 1 || cancelled != 1 || active != 0 || runtime.deathReturnWaiting {
		t.Fatalf("settlement cancellation scheduled=%d cancelled=%d active=%d runtime=%+v",
			scheduled, cancelled, active, runtime)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("settlement replay emitted packet=%x", conn.write.Bytes())
	}

	// Replaying the normal settlement entry never creates a replacement timer.
	session.dungeon.mu.Lock()
	err = service.sendCurrentDungeonSettlementEntryLocked(session, runtime, "test_normal_settlement_has_no_timer")
	session.dungeon.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	scheduled, _, active = queue.counts()
	if scheduled != 1 || active != 0 {
		t.Fatalf("normal settlement invented timer scheduled=%d active=%d", scheduled, active)
	}
}

func TestCurrentDungeonDeathTimerStaleCallbackCannotCrossNewRun(t *testing.T) {
	service, session, oldRuntime := newBackToVillageRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	if err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize)); err != nil {
		t.Fatal(err)
	}
	oldTask := queue.task(t, 0)
	oldToken := oldRuntime.lifecycleToken
	if err := oldRuntime.Session.Abandon(); err != nil {
		t.Fatal(err)
	}
	newRuntime, _, err := service.prepareDungeonRuntime(
		context.Background(), session, dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := freezeCurrentDungeonTownReturnOrigin(session, newRuntime); err != nil {
		t.Fatal(err)
	}
	if err := service.commitDungeonRuntime(session, newRuntime); err != nil {
		t.Fatal(err)
	}
	if newRuntime.lifecycleToken == 0 || newRuntime.lifecycleToken == oldToken || session.dungeon.runtime != newRuntime {
		t.Fatalf("new run token old=%d new=%d owner=%p", oldToken, newRuntime.lifecycleToken, session.dungeon.runtime)
	}
	if oldRuntime.deathReturnWaiting {
		t.Fatal("new run retained old death timer")
	}

	session.conn.(*bufferConn).write.Reset()
	queue.fire(oldTask, true)
	if session.conn.(*bufferConn).write.Len() != 0 || session.dungeon.runtime != newRuntime || newRuntime.townReturnPending {
		t.Fatalf("stale callback crossed run wrote=%x owner=%p new=%+v",
			session.conn.(*bufferConn).write.Bytes(), session.dungeon.runtime, newRuntime)
	}
}

func TestCurrentDungeonDeathTimerIsCancelledByGiveupDisconnectAndCommittedRevive(t *testing.T) {
	t.Run("giveup", func(t *testing.T) {
		service, session, runtime := newBackToVillageRuntime(t)
		queue := newFakeCurrentDungeonDeathTimerQueue()
		service.gameplayTimers = queue
		if err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize)); err != nil {
			t.Fatal(err)
		}
		stale := queue.task(t, 0)
		session.conn.(*bufferConn).write.Reset()
		if err := service.handleDungeonGiveupGame(session, nil); err != nil {
			t.Fatal(err)
		}
		if runtime.deathReturnWaiting || !runtime.townReturnPending {
			t.Fatalf("giveup state=%+v", runtime)
		}
		before := session.conn.(*bufferConn).write.Len()
		queue.fire(stale, true)
		if session.conn.(*bufferConn).write.Len() != before {
			t.Fatalf("stale death timer resent giveup return before=%d after=%d", before, session.conn.(*bufferConn).write.Len())
		}
	})

	t.Run("disconnect", func(t *testing.T) {
		service, session, runtime := newBackToVillageRuntime(t)
		queue := newFakeCurrentDungeonDeathTimerQueue()
		service.gameplayTimers = queue
		armTestCurrentDungeonDeathReturn(t, service, session, runtime)
		stale := queue.task(t, 0)
		service.unbindGameSession(session)
		_, cancelled, active := queue.counts()
		if cancelled != 1 || active != 0 || runtime.deathReturnWaiting {
			t.Fatalf("disconnect cancellation cancelled=%d active=%d runtime=%+v", cancelled, active, runtime)
		}
		queue.fire(stale, true)
		if session.conn.(*bufferConn).write.Len() != 0 || runtime.townReturnPending {
			t.Fatalf("dequeued death callback survived disconnect wrote=%x runtime=%+v",
				session.conn.(*bufferConn).write.Bytes(), runtime)
		}
	})

	t.Run("character switch", func(t *testing.T) {
		service, session, runtime := newBackToVillageRuntime(t)
		queue := newFakeCurrentDungeonDeathTimerQueue()
		service.gameplayTimers = queue
		armTestCurrentDungeonDeathReturn(t, service, session, runtime)
		service.bindGameSessionCharacter(session, 100)
		_, cancelled, active := queue.counts()
		if cancelled != 1 || active != 0 || runtime.deathReturnWaiting || session.selectedCharacterID != 100 {
			t.Fatalf("character-switch cancellation cancelled=%d active=%d selected=%d runtime=%+v",
				cancelled, active, session.selectedCharacterID, runtime)
		}
	})

	t.Run("committed revive hook", func(t *testing.T) {
		service, session, runtime := newBackToVillageRuntime(t)
		queue := newFakeCurrentDungeonDeathTimerQueue()
		service.gameplayTimers = queue
		armTestCurrentDungeonDeathReturn(t, service, session, runtime)
		session.dungeon.mu.Lock()
		service.completeCurrentDungeonReviveLocked(session, runtime, "test_authoritative_revive_committed")
		session.dungeon.mu.Unlock()
		_, cancelled, active := queue.counts()
		if cancelled != 1 || active != 0 || runtime.deathReturnWaiting {
			t.Fatalf("revive cancellation cancelled=%d active=%d runtime=%+v", cancelled, active, runtime)
		}
	})
}

func TestCurrentDungeonDeathRearmReplacesOldGeneration(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	if err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize)); err != nil {
		t.Fatal(err)
	}
	firstTask := queue.task(t, 0)
	firstGeneration := runtime.deathReturnGeneration
	session.conn.(*bufferConn).write.Reset()
	if err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize)); err != nil {
		t.Fatal(err)
	}
	if runtime.deathReturnGeneration == firstGeneration || !runtime.deathReturnWaiting {
		t.Fatalf("rearm generation first=%d current=%d state=%+v",
			firstGeneration, runtime.deathReturnGeneration, runtime)
	}
	scheduled, cancelled, active := queue.counts()
	if scheduled != 2 || cancelled != 1 || active != 1 {
		t.Fatalf("rearm queue scheduled=%d cancelled=%d active=%d", scheduled, cancelled, active)
	}
	session.conn.(*bufferConn).write.Reset()
	queue.fire(firstTask, true)
	if session.conn.(*bufferConn).write.Len() != 0 || !runtime.deathReturnWaiting || runtime.townReturnPending {
		t.Fatalf("old rearm callback changed state wrote=%x runtime=%+v",
			session.conn.(*bufferConn).write.Bytes(), runtime)
	}
	secondTask := queue.task(t, 1)
	queue.fire(secondTask, false)
	op24, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if op24.Header.MsgID != currentSceneTransitionMsgID || op24.Header.Classification != 0 ||
		!bytes.Equal(op24.Body, wantDungeonTownTransitionBody()) || len(rest) != 0 {
		t.Fatalf("replacement death timer return header=%+v body=%x rest=%x", op24.Header, op24.Body, rest)
	}
	if runtime.deathReturnWaiting || !runtime.townReturnPending || !runtime.townReturnOp24Sent {
		t.Fatalf("replacement death timer did not own one final return runtime=%+v", runtime)
	}
}

func TestCurrentDungeonDeathReturnUsesFrozenTownSelectorOrigin(t *testing.T) {
	service, session, runtime := newBackToVillageRuntimeAtOrigin(t, 640, 240)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue

	if err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize)); err != nil {
		t.Fatal(err)
	}
	if runtime.deathReturnTransition.PositionX != 640 || runtime.deathReturnTransition.PositionY != 240 ||
		runtime.deathReturnTransition.PositionSource != "current_exe_op35_runtime_origin_snapshot" {
		t.Fatalf("death timer transition did not use frozen selector origin: %+v", runtime.deathReturnTransition)
	}

	// Discard op32 and inspect the timer's typed op24 town transition.
	session.conn.(*bufferConn).write.Reset()
	queue.fire(queue.task(t, 0), false)
	op24, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	want := []byte{7, 3, 1, 0, 99, 0, 0x80, 0x02, 0xf0, 0x00, 5, 3}
	if op24.Header.MsgID != currentSceneTransitionMsgID || op24.Header.Classification != 0 ||
		!bytes.Equal(op24.Body, want) || len(rest) != 0 {
		t.Fatalf("death origin op24 header=%+v body=%x rest=%x want=%x", op24.Header, op24.Body, rest, want)
	}
}

func TestCurrentDungeonDeathDoesNotConfirmWithoutTimerOrAfterSocketFailure(t *testing.T) {
	t.Run("schedule failure", func(t *testing.T) {
		service, session, runtime := newBackToVillageRuntime(t)
		queue := newFakeCurrentDungeonDeathTimerQueue()
		queue.scheduleErr = errors.New("schedule failed")
		service.gameplayTimers = queue
		if err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize)); err != nil {
			t.Fatal(err)
		}
		if session.conn.(*bufferConn).write.Len() != 0 || runtime.deathReturnWaiting {
			t.Fatalf("schedule failure confirmed death wrote=%x runtime=%+v",
				session.conn.(*bufferConn).write.Bytes(), runtime)
		}
	})

	t.Run("op32 socket failure", func(t *testing.T) {
		service, session, runtime := newBackToVillageRuntime(t)
		queue := newFakeCurrentDungeonDeathTimerQueue()
		service.gameplayTimers = queue
		wantErr := errors.New("op32 write failed")
		session.conn = &failNthDungeonWriteConn{failAt: 1, err: wantErr}
		err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize))
		if !errors.Is(err, wantErr) {
			t.Fatalf("op32 write error=%v want=%v", err, wantErr)
		}
		_, cancelled, active := queue.counts()
		if cancelled != 1 || active != 0 || runtime.deathReturnWaiting {
			t.Fatalf("op32 failure timer cancelled=%d active=%d runtime=%+v", cancelled, active, runtime)
		}
	})
}

func TestCurrentDungeonUseCoinRequestDoesNotFakeReviveOrCancelTimer(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	if err := service.handleDungeonDieCharacter(session, make([]byte, currentDungeonDieCharacterBodySize)); err != nil {
		t.Fatal(err)
	}
	session.conn.(*bufferConn).write.Reset()
	body := make([]byte, currentDungeonUseCoinBodySize)
	binary.LittleEndian.PutUint16(body, currentSceneActorObjectKey(session.selectedCharacterID))
	packet, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketUseCoin), body, 9, dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, packet); err != nil {
		t.Fatal(err)
	}
	// With free-revive policy the op41 cancels the death timer and sends ACK.
	_, cancelled, active := queue.counts()
	if runtime.deathReturnWaiting || cancelled == 0 || active != 0 {
		t.Fatalf("op41 free revive did not cancel timer: cancelled=%d active=%d waiting=%t",
			cancelled, active, runtime.deathReturnWaiting)
	}
	if session.conn.(*bufferConn).write.Len() == 0 {
		t.Fatal("op41 free revive did not send ACK")
	}
}

func TestCurrentDungeonDeathAndUseCoinRejectUnprovedShapesAndClasses(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue

	for _, body := range [][]byte{make([]byte, 3), make([]byte, 5)} {
		if err := service.handleDungeonDieCharacter(session, body); err != nil {
			t.Fatal(err)
		}
	}
	wrongClass, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketDieCharacter), make([]byte, currentDungeonDieCharacterBodySize), 0, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, wrongClass); err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{{0}, {0, 0, 0}} {
		if err := service.handleDungeonUseCoin(session, body); err != nil {
			t.Fatal(err)
		}
	}
	if session.conn.(*bufferConn).write.Len() != 0 || runtime.deathReturnWaiting {
		t.Fatalf("rejected death/revive changed state wrote=%x runtime=%+v",
			session.conn.(*bufferConn).write.Bytes(), runtime)
	}
	if scheduled, _, active := queue.counts(); scheduled != 0 || active != 0 {
		t.Fatalf("rejected requests scheduled timers=%d active=%d", scheduled, active)
	}
}

func armTestCurrentDungeonDeathReturn(
	t *testing.T,
	service *Service,
	session *gameSession,
	runtime *runtimeDungeonState,
) {
	t.Helper()
	if runtime.lifecycleToken == 0 {
		session.dungeon.runToken = nextCurrentDungeonDeathGeneration(session.dungeon.runToken)
		runtime.lifecycleToken = session.dungeon.runToken
	}
	session.dungeon.mu.Lock()
	err := service.armCurrentDungeonDeathReturnLocked(
		session,
		runtime,
		currentDungeonTownTransition{TownID: 7, AreaID: 3, Body: wantDungeonTownTransitionBody()},
	)
	session.dungeon.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}
