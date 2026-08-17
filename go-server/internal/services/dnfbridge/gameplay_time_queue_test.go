package dnfbridge

import (
	"testing"
	"time"
)

func TestPetGrowthCountersUseSharedGameplayTimeQueue(t *testing.T) {
	tests := []struct {
		name  string
		mode  currentPetGrowthClockMode
		delay time.Duration
	}{
		{name: "dungeon decay", mode: currentPetGrowthClockDungeon, delay: currentPetGrowthDungeonTick},
		{name: "town recovery", mode: currentPetGrowthClockTown, delay: currentPetGrowthTownTick},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := newFakeCurrentDungeonDeathTimerQueue()
			service := &Service{gameplayTimers: queue}
			session := &gameSession{
				connID:              "shared-pet-timer",
				selectedCharacterID: 19,
			}
			if err := service.switchCurrentPetGrowthClock(session, test.mode, time.Time{}, "test"); err != nil {
				t.Fatal(err)
			}
			first := queue.task(t, 0)
			if first.name != "dnf-pet-growth:shared-pet-timer" || first.delay != test.delay {
				t.Fatalf("task=%+v want name/delay dnf-pet-growth:shared-pet-timer/%s", first, test.delay)
			}
			if !session.petGrowth.anchor.Equal(queue.Now()) {
				t.Fatalf("anchor=%s want queue now=%s", session.petGrowth.anchor, queue.Now())
			}

			if err := service.settleCurrentPetGrowthClock(session, queue.Now(), "zero_elapsed_rearm"); err != nil {
				t.Fatal(err)
			}
			second := queue.task(t, 1)
			if !first.cancelled || second.cancelled || second.delay != test.delay {
				t.Fatalf("replacement first=%+v second=%+v", first, second)
			}
			service.stopCurrentPetGrowthClock(session, "test_stop")
			scheduled, cancelled, active := queue.counts()
			if scheduled != 2 || cancelled != 1 || active != 0 || !second.cancelled {
				t.Fatalf("queue scheduled=%d cancelled=%d active=%d second=%+v", scheduled, cancelled, active, second)
			}
		})
	}
}
