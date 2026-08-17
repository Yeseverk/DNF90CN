package redeem

import "longheng.io/server/internal/platform/runtimeguard"

func (*MemoryStore) BackendKind() runtimeguard.Backend {
	return runtimeguard.BackendMemory
}

func (*PersistentStore) BackendKind() runtimeguard.Backend {
	return runtimeguard.BackendMemory
}
