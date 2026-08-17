package notice

import "longheng.io/server/internal/platform/runtimeguard"

func (*MemoryStore) BackendKind() runtimeguard.Backend {
	return runtimeguard.BackendMemory
}

func (*SQLStore) BackendKind() runtimeguard.Backend {
	return runtimeguard.BackendMySQL
}
