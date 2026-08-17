package itemquality

import (
	"encoding/binary"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestApplyWritesOnlyDedicatedQualityFields(t *testing.T) {
	stack := dnfrepo.ItemStack{
		ItemID:   1001,
		Count:    1,
		RawEntry: make([]byte, 20),
		Extra:    map[string]string{"count_or_instance": "77"},
	}
	if err := Apply(&stack, 123456); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if stack.Extra["quality_seed"] != "123456" ||
		stack.Extra["count_or_instance"] != "77" ||
		binary.LittleEndian.Uint32(stack.RawEntry[6:10]) != 123456 {
		t.Fatalf("stack = %+v raw=%x", stack, stack.RawEntry)
	}
}

func TestNewRandomSeedExcludesTopSentinel(t *testing.T) {
	for range 32 {
		seed, err := NewRandomSeed()
		if err != nil {
			t.Fatalf("NewRandomSeed: %v", err)
		}
		if seed == 0 || seed > RandomSeedCount {
			t.Fatalf("seed = %d", seed)
		}
	}
}
