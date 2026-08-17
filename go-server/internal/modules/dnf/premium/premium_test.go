package premium

import (
	"bytes"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestUpsertFreshRenewAndExpiredStack(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	account := &dnfrepo.AccountRecord{}

	Upsert(account, TypeOverEquip, 86400, 1, now)
	if got, want := ExpireAt(*account, TypeOverEquip), now.Unix()+86400; got != want {
		t.Fatalf("fresh expire = %d, want %d", got, want)
	}

	// Renewal while active stacks on the current expiry, not on now.
	Upsert(account, TypeOverEquip, 3*86400, 1, now)
	if got, want := ExpireAt(*account, TypeOverEquip), now.Unix()+4*86400; got != want {
		t.Fatalf("renew expire = %d, want %d", got, want)
	}

	// Renewal after expiry restarts from now.
	later := now.Add(10 * 86400 * time.Second)
	Upsert(account, TypeOverEquip, 86400, 1, later)
	if got, want := ExpireAt(*account, TypeOverEquip), later.Unix()+86400; got != want {
		t.Fatalf("expired renew expire = %d, want %d", got, want)
	}

	// Count multiplies the duration.
	Upsert(account, TypeOverSkill, 86400, 3, now)
	if got, want := ExpireAt(*account, TypeOverSkill), now.Unix()+3*86400; got != want {
		t.Fatalf("count expire = %d, want %d", got, want)
	}
}

func TestActiveAndExpireAtMalformed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	account := dnfrepo.AccountRecord{Metadata: map[string]string{
		MetadataKey(TypeCrystal): "garbage",
		MetadataKey(TypeLethe):   "-5",
	}}
	if ExpireAt(account, TypeCrystal) != 0 || ExpireAt(account, TypeLethe) != 0 {
		t.Fatalf("malformed metadata must read as 0: %+v", account.Metadata)
	}
	if Active(account, TypeCrystal, now) || Active(account, TypeLethe, now) || Active(account, TypeOverEquip, now) {
		t.Fatal("inactive premiums must not report active")
	}
}

func TestSelectAckEntriesFoldsDevilSlots(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	account := dnfrepo.AccountRecord{Metadata: map[string]string{}}
	Upsert(&account, TypeOverEquip, 86400, 1, now)
	Upsert(&account, DevilSlotType(DevilSlotAutoRepair), 3*86400, 1, now)
	Upsert(&account, DevilSlotType(DevilSlotGoldCard), 86400, 1, now)
	// Expired devil slot must not win the fold.
	account.Metadata[MetadataKey(DevilSlotType(DevilSlotQuestHelper))] = "1"

	got := SelectAckEntries(account, now)
	if got[0] != 2 {
		t.Fatalf("count = %d, want 2 (over-equip + folded devil): % X", got[0], got)
	}
	if len(got) != 1+2*9 {
		t.Fatalf("len = %d, want %d", len(got), 1+2*9)
	}
	if got[1] != byte(TypeOverEquip) {
		t.Fatalf("first entry type = %d, want %d", got[1], TypeOverEquip)
	}
	if got[10] != byte(DevilFolded) {
		t.Fatalf("second entry type = %d, want %d", got[10], DevilFolded)
	}
	var remaining int64
	for i := 0; i < 8; i++ {
		remaining |= int64(got[11+i]) << (8 * i)
	}
	if want := int64(3 * 86400); remaining != want {
		t.Fatalf("folded devil remaining = %d, want %d", remaining, want)
	}

	empty := SelectAckEntries(dnfrepo.AccountRecord{}, now)
	if !bytes.Equal(empty, []byte{0}) {
		t.Fatalf("empty entries = % X, want [0]", empty)
	}
}

func TestSelectAckEntriesKeepsAllActivatedRuntimePVFContractTypes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	account := dnfrepo.AccountRecord{Metadata: map[string]string{
		MetadataKey(100): "1700259200",
		MetadataKey(29):  "1700086400",
		MetadataKey(80):  "1700172800",
		// Malformed, expired, synthetic folded and out-of-wire-range keys must
		// not become select-character premium rows.
		MetadataKey(88):          "garbage",
		MetadataKey(92):          "1",
		MetadataKey(DevilFolded): "1700086400",
		MetadataKey(256):         "1700086400",
	}}

	got := SelectAckEntries(account, now)
	if got[0] != 3 {
		t.Fatalf("count = %d, want 3 generic runtime-PVF contracts: % X", got[0], got)
	}
	gotTypes := []byte{got[1], got[10], got[19]}
	wantTypes := []byte{29, 80, 100}
	if !bytes.Equal(gotTypes, wantTypes) {
		t.Fatalf("types = %v, want deterministic %v; body=% X", gotTypes, wantTypes, got)
	}
}

func TestSelectAckEntriesCountsTheFourCurrentCouponFamilies(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	account := dnfrepo.AccountRecord{Metadata: map[string]string{
		// Black Diamond is a runtime-PVF contract type, so it follows the
		// generic active-u8 path after the three named fixed types.
		MetadataKey(29):            "1700086400",
		MetadataKey(TypeOverEquip): "1700086400",
		MetadataKey(TypeOverSkill): "1700086400",
		MetadataKey(TypeBonusExp):  "1700086400",
	}}

	got := SelectAckEntries(account, now)
	if got[0] != 4 || len(got) != 1+4*9 {
		t.Fatalf("coupon count/body = %d/%d, want 4/%d: % X", got[0], len(got), 1+4*9, got)
	}
	gotTypes := []byte{got[1], got[10], got[19], got[28]}
	wantTypes := []byte{byte(TypeOverEquip), byte(TypeOverSkill), byte(TypeBonusExp), 29}
	if !bytes.Equal(gotTypes, wantTypes) {
		t.Fatalf("coupon types = %v, want %v; body=% X", gotTypes, wantTypes, got)
	}
}

func TestBuildActivatedBodyExactBytes(t *testing.T) {
	got := BuildActivatedBody(TypeOverEquip, 86400)
	want := []byte{2, 0, 22, 0x80, 0x51, 0x01, 0, 0, 0, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("body = % X, want % X", got, want)
	}
}

func TestCanNotifyActivationRejectsInternalDevilSlots(t *testing.T) {
	if !CanNotifyActivation(TypeOverEquip) {
		t.Fatal("ordinary u8 premium type must use the current activation notification")
	}
	for slot := int64(0); slot < DevilSlotCount; slot++ {
		if CanNotifyActivation(DevilSlotType(slot)) {
			t.Fatalf("internal devil slot %d type=%d must not be truncated into class0/op66", slot, DevilSlotType(slot))
		}
	}
	if CanNotifyActivation(0) || CanNotifyActivation(256) {
		t.Fatal("zero and values outside u8 must not use the current activation notification")
	}
}

func TestBuildServiceDataBodyDevilOffsets(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	account := dnfrepo.AccountRecord{Metadata: map[string]string{
		MetadataKey(DevilSlotType(DevilSlotAutoRepair)): "1700604800",
		MetadataKey(DevilSlotType(DevilSlotGoldCard)):   "1699999999",
	}}
	character := dnfrepo.CharacterRecord{Stats: map[string]int64{
		dailyUsageDayKey:                   ServiceDayID(now),
		DailyUsageKey(DevilSlotAutoRepair): 4,
	}}
	got := BuildServiceDataBody(account, character, now)
	if len(got) != 3+74 {
		t.Fatalf("len = %d, want 77", len(got))
	}
	if got[0] != 1 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("prefix = % X, want 01 01 00", got[:3])
	}
	offset := 3 + 6 + DevilSlotAutoRepair*9
	var expireAt uint32
	for i := 0; i < 4; i++ {
		expireAt |= uint32(got[int(offset)+i]) << (8 * i)
	}
	if expireAt != 1_700_604_800 {
		t.Fatalf("slot6 absolute expiry = %d, want %d", expireAt, 1_700_604_800)
	}
	usedOffset := 3 + 10 + DevilSlotAutoRepair*9
	var used uint32
	for i := 0; i < 4; i++ {
		used |= uint32(got[int(usedOffset)+i]) << (8 * i)
	}
	if used != 4 {
		t.Fatalf("slot6 used = %d, want 4", used)
	}
	// Expired and unused slots stay zero.
	other := 3 + 6 + DevilSlotGoldCard*9
	if got[other] != 0 || got[other+1] != 0 || got[other+2] != 0 || got[other+3] != 0 {
		t.Fatalf("slot0 expired absolute expiry must be zero: % X", got[other:other+4])
	}
}

func TestDevilServiceDurationAndDailyUsageRules(t *testing.T) {
	now := time.Date(2026, time.July, 27, 5, 59, 59, 0, time.FixedZone("CST", 8*60*60))
	account := &dnfrepo.AccountRecord{}

	Upsert(account, DevilSlotType(DevilSlotGoldCard), 7*24*60*60, 1, now)
	Upsert(account, DevilSlotType(DevilSlotGoldCard), 30*24*60*60, 1, now)
	if got, want := ExpireAt(*account, DevilSlotType(DevilSlotGoldCard)), now.Unix()+37*24*60*60; got != want {
		t.Fatalf("7-day plus 30-day expiry = %d, want %d", got, want)
	}

	character := &dnfrepo.CharacterRecord{}
	for count := int64(0); count < DailyLimit(DevilSlotGoldCard); count++ {
		if !TryConsumeDaily(character, DevilSlotGoldCard, now) {
			t.Fatalf("consume %d unexpectedly rejected", count+1)
		}
	}
	if TryConsumeDaily(character, DevilSlotGoldCard, now) {
		t.Fatal("eleventh gold-card use must be rejected")
	}
	if got := DailyUsage(*character, DevilSlotGoldCard, now); got != 10 {
		t.Fatalf("gold-card usage = %d, want 10", got)
	}

	afterReset := now.Add(time.Second)
	if got := DailyUsage(*character, DevilSlotGoldCard, afterReset); got != 0 {
		t.Fatalf("usage after 06:00 reset = %d, want 0", got)
	}
	if !TryConsumeDaily(character, DevilSlotGoldCard, afterReset) {
		t.Fatal("first use after reset must be accepted")
	}
	if got := DailyUsage(*character, DevilSlotGoldCard, afterReset); got != 1 {
		t.Fatalf("usage after reset consume = %d, want 1", got)
	}
}

func TestDailyLimitsMatchPVF(t *testing.T) {
	want := map[int64]int64{
		DevilSlotGoldCard:     10,
		DevilSlotDoubleJar:    8,
		DevilSlotQuestHelper:  0,
		DevilSlotSevenBuff:    10,
		DevilSlotHpMpRecover:  0,
		DevilSlotFreeWeakness: 10,
		DevilSlotAutoRepair:   6,
		DevilSlotFastJarOpen:  0,
	}
	for slot, limit := range want {
		if got := DailyLimit(slot); got != limit {
			t.Fatalf("slot %d limit = %d, want %d", slot, got, limit)
		}
	}
}
