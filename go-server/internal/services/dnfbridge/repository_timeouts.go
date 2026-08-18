package dnfbridge

import "time"

const (
	createWriteTimeout = 3 * time.Second
	// Selected-character initialization builds several real repository
	// snapshots before the first scene is usable. It must not share the short
	// mutation/write deadline: a cold PVF/catalog or MySQL pool wakeup can
	// otherwise cancel the complete actor-bound container bootstrap.
	currentRepositorySnapshotTimeout   = 15 * time.Second
	currentItemListPVFReconcileTimeout = 2 * time.Second
	// A dungeon pickup commits while session.dungeon.mu is held, so an
	// unbounded write does not merely delay one item: it freezes the whole
	// dungeon session, and the client can no longer settle the run or return
	// to town. Bounding it turns a stalled MySQL into a rejected pickup that
	// leaves the drop available, which the client already retries.
	currentDungeonPickupWriteTimeout = 5 * time.Second
)
