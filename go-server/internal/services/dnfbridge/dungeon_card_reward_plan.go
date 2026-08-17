package dnfbridge

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"strings"
)

// newDungeonCardRewardPlan freezes already-resolved rewards for one run. It
// does not choose item ids, rarity, gold values, or PVF paths; callers must
// supply values resolved from the runtime catalogs and real run state.
func newDungeonCardRewardPlan(
	identity dungeonCardPlanIdentity,
	source string,
	free dungeonCardRewardBundle,
	paid dungeonCardRewardBundle,
) (dungeonCardRewardPlan, error) {
	identity.CharacterID = strings.TrimSpace(identity.CharacterID)
	source = strings.TrimSpace(source)
	if identity.CharacterID == "" || identity.DungeonID <= 0 || identity.MazeIndex < 0 || source == "" {
		return dungeonCardRewardPlan{}, errDungeonCardPlanInvalid
	}

	var numeric [16]byte
	binary.LittleEndian.PutUint64(numeric[0:8], uint64(identity.DungeonID))
	binary.LittleEndian.PutUint32(numeric[8:12], uint32(identity.MazeIndex))
	binary.LittleEndian.PutUint32(numeric[12:16], identity.RunSeed)
	hash := sha256.New()
	_, _ = hash.Write([]byte("dnf-card-plan-v2\x00"))
	_, _ = hash.Write([]byte(identity.CharacterID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(numeric[:])
	_, _ = hash.Write([]byte(source))
	writeDungeonCardBundleIdentity(hash, free)
	writeDungeonCardBundleIdentity(hash, paid)
	id := hex.EncodeToString(hash.Sum(nil)[:16])

	plan := dungeonCardRewardPlan{
		ID:          id,
		CharacterID: identity.CharacterID,
		Source:      source,
		Sides: [dungeonCardSideCount]dungeonCardRewardBundle{
			cloneDungeonCardRewardBundle(free),
			cloneDungeonCardRewardBundle(paid),
		},
	}
	if err := validateDungeonCardPlan(plan); err != nil {
		return dungeonCardRewardPlan{}, fmt.Errorf("freeze dungeon card plan: %w", err)
	}
	return plan, nil
}

func writeDungeonCardBundleIdentity(digest hash.Hash, bundle dungeonCardRewardBundle) {
	var numeric [8]byte
	binary.LittleEndian.PutUint64(numeric[:], uint64(bundle.Gold))
	_, _ = digest.Write(numeric[:])
	binary.LittleEndian.PutUint64(numeric[:], uint64(len(bundle.Items)))
	_, _ = digest.Write(numeric[:])
	for _, item := range bundle.Items {
		binary.LittleEndian.PutUint64(numeric[:], uint64(item.ItemID))
		_, _ = digest.Write(numeric[:])
		binary.LittleEndian.PutUint64(numeric[:], uint64(item.Count))
		_, _ = digest.Write(numeric[:])
		flags := byte(0)
		if item.Stackable {
			flags |= 1
		}
		if item.Bind {
			flags |= 2
		}
		_, _ = digest.Write([]byte{flags})
		binary.LittleEndian.PutUint16(numeric[0:2], uint16(item.SlotStart))
		binary.LittleEndian.PutUint16(numeric[2:4], uint16(item.SlotEnd))
		_, _ = digest.Write(numeric[0:4])
		binary.LittleEndian.PutUint64(numeric[:], uint64(item.ExpireAt.Unix()))
		_, _ = digest.Write(numeric[:])
		binary.LittleEndian.PutUint64(numeric[:], uint64(len(item.RawEntry)))
		_, _ = digest.Write(numeric[:])
		_, _ = digest.Write(item.RawEntry)
		keys := make([]string, 0, len(item.Extra))
		for key := range item.Extra {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		binary.LittleEndian.PutUint64(numeric[:], uint64(len(keys)))
		_, _ = digest.Write(numeric[:])
		for _, key := range keys {
			value := item.Extra[key]
			binary.LittleEndian.PutUint64(numeric[:], uint64(len(key)))
			_, _ = digest.Write(numeric[:])
			_, _ = digest.Write([]byte(key))
			binary.LittleEndian.PutUint64(numeric[:], uint64(len(value)))
			_, _ = digest.Write(numeric[:])
			_, _ = digest.Write([]byte(value))
		}
	}
}
