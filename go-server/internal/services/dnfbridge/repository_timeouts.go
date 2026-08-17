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
)
