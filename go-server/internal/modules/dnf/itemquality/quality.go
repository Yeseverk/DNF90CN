package itemquality

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	// TopSeed is the current-client top-quality sentinel used by the gold
	// kaleido box.
	TopSeed uint32 = 999999998
	// RandomSeedCount excludes the dedicated top-quality sentinel.
	RandomSeedCount uint32 = TopSeed - 1
)

func NewRandomSeed() (uint32, error) {
	const acceptLimit = ^uint32(0) - (^uint32(0) % RandomSeedCount)
	var raw [4]byte
	for {
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, fmt.Errorf("generate equipment quality seed: %w", err)
		}
		value := binary.LittleEndian.Uint32(raw[:])
		if value < acceptLimit {
			return value%RandomSeedCount + 1, nil
		}
	}
}

func Valid(seed uint32) bool {
	return seed > 0 && seed <= TopSeed
}

// Apply persists a quality seed in both the relational item metadata and a
// present raw item projection. It never writes the independent stack-count or
// instance-value aliases.
func Apply(stack *dnfrepo.ItemStack, seed uint32) error {
	if stack == nil || !Valid(seed) {
		return fmt.Errorf("invalid equipment quality seed: %d", seed)
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 4)
	}
	stack.Extra["quality_seed"] = strconv.FormatUint(uint64(seed), 10)
	if len(stack.RawEntry) >= 10 {
		binary.LittleEndian.PutUint32(stack.RawEntry[6:10], seed)
	}
	return nil
}
