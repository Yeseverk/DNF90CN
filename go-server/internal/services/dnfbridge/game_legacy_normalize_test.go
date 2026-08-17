package dnfbridge

import (
	"bytes"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestNormalizeLegacyGameBodyStripsExactChangeSkillSlotTransportTrailer(t *testing.T) {
	semantic := []byte{0, 1, 199, 0xff, 0xff, 0xff, 0xff, 0}
	liveBody := append(append([]byte(nil), semantic...), 0x67, 0xc1, 0x19, 0x3b)

	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketChangeSkillslot), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op28 body = %x, want %x", got, semantic)
	}
	if &got[0] == &liveBody[0] {
		t.Fatal("normalized op28 body aliases the transport packet")
	}
}

func TestNormalizeLegacyGameBodyKeepsStrictChangeSkillSlotBoundaries(t *testing.T) {
	tests := []struct {
		name string
		typ  uint16
		body []byte
	}{
		{name: "semantic body", typ: uint16(dnfenum.CmdPacketChangeSkillslot), body: make([]byte, 8)},
		{name: "old opaque protected body", typ: uint16(dnfenum.CmdPacketChangeSkillslot), body: make([]byte, 16)},
		{name: "unrelated twelve byte body", typ: uint16(dnfenum.CmdPacketChangeSkillslot) + 1, body: make([]byte, 12)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLegacyGameBody(tt.typ, tt.body)
			if len(got) != len(tt.body) || !bytes.Equal(got, tt.body) {
				t.Fatalf("normalized body = %x (len %d), want unchanged len %d", got, len(got), len(tt.body))
			}
		})
	}
}

func TestNormalizeLegacyGameBodyStripsExactMoveItemspaceTransportTrailer(t *testing.T) {
	semantic := make([]byte, 28)
	semantic[0] = 0
	semantic[1] = 65
	semantic[11] = 0
	semantic[12] = 3
	liveBody := append(append([]byte(nil), semantic...), 0xde, 0xad, 0xbe, 0xef)

	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketMoveItemspace), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op19 body = %x, want %x", got, semantic)
	}
	if &got[0] == &liveBody[0] {
		t.Fatal("normalized op19 body aliases the transport packet")
	}
}

func TestNormalizeLegacyGameBodyKeepsStrictMoveItemspaceBoundaries(t *testing.T) {
	for _, body := range [][]byte{make([]byte, 28), make([]byte, 16)} {
		got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketMoveItemspace), body)
		if len(got) != len(body) || !bytes.Equal(got, body) {
			t.Fatalf("op19 body len %d normalized to len %d", len(body), len(got))
		}
	}

	unrelated := make([]byte, 32)
	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketMoveItemspace)+1, unrelated)
	if len(got) != len(unrelated) || !bytes.Equal(got, unrelated) {
		t.Fatalf("unrelated 32-byte body normalized to len %d", len(got))
	}
}

func TestNormalizeLegacyGameBodyStripsProvenBuySkillTransportTrailer(t *testing.T) {
	// Live trace: tree=0,count=1,skill=190,learn,+1,finalMode=0.
	semantic := []byte{0, 1, 0xbe, 0, 0, 1, 0}
	liveBody := append(append([]byte(nil), semantic...), 0xb8, 0x1a, 0, 0)
	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketBuySkill), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op29 body = %x, want %x", got, semantic)
	}
	if &got[0] == &liveBody[0] {
		t.Fatal("normalized op29 body aliases the transport packet")
	}
}

func TestNormalizeLegacyGameBodyKeepsBuySkillBoundariesWithoutCountProof(t *testing.T) {
	tests := [][]byte{
		{0, 1, 0xbe, 0, 0, 1, 0},
		make([]byte, 16),
		{0, 2, 0xbe, 0, 0, 1, 0, 1, 2, 3, 4},
	}
	for _, body := range tests {
		got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketBuySkill), body)
		if !bytes.Equal(got, body) || len(got) != len(body) {
			t.Fatalf("op29 body %x normalized to %x", body, got)
		}
	}
}

func TestNormalizeLegacyGameBodySkillInitStrictBoundaries(t *testing.T) {
	semantic := []byte{0, 0}
	liveBody := append(append([]byte(nil), semantic...), 1, 2, 3, 4)
	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSkillInit), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op491 body = %x, want %x", got, semantic)
	}
	for _, body := range [][]byte{semantic, make([]byte, 8)} {
		got = normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSkillInit), body)
		if !bytes.Equal(got, body) || len(got) != len(body) {
			t.Fatalf("op491 boundary len %d normalized to len %d", len(body), len(got))
		}
	}
}

func TestNormalizeLegacyGameBodyPreservesLiveSkillTreeSwitchTail(t *testing.T) {
	liveBody := []byte{0, 0x3e, 0x74, 0x5e, 0x7a}
	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketChangeAnotherSkillTree), liveBody)
	if !bytes.Equal(got, liveBody) {
		t.Fatalf("normalized op260 body = %x, want full live body %x", got, liveBody)
	}
}

func TestNormalizeLegacyGameBodyDoesNotGuessTownPositionTransportTrailer(t *testing.T) {
	semantic := []byte{0x3c, 0x03, 0xd3, 0x00, 0x08, 0x64, 0x00}
	possibleTrailerBody := append(append([]byte(nil), semantic...), 1, 2, 3, 4)

	for _, body := range [][]byte{semantic, possibleTrailerBody} {
		got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSetUserPosition), body)
		if !bytes.Equal(got, body) || len(got) != len(body) {
			t.Fatalf("unproved op35 transport body normalized from %x to %x", body, got)
		}
	}
}

func TestNormalizeLegacyGameBodyStripsExactUseStackableTransportTrailer(t *testing.T) {
	semantic := []byte{
		3, 0, 0,
		0x44, 0x33, 0x22, 0x11,
		0x1a, 0x21, 0, 0,
		0, 0, 0, 0,
	}
	liveBody := append(append([]byte(nil), semantic...), 0xde, 0xad, 0xbe, 0xef)

	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketUseStackable), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op44 body = %x, want %x", got, semantic)
	}
	if &got[0] == &liveBody[0] {
		t.Fatal("normalized op44 body aliases the transport packet")
	}
}

func TestNormalizeLegacyGameBodyKeepsStrictUseStackableBoundaries(t *testing.T) {
	tests := []struct {
		name string
		typ  uint16
		body []byte
	}{
		{name: "semantic body", typ: uint16(dnfenum.CmdPacketUseStackable), body: make([]byte, 15)},
		{name: "opaque protected body", typ: uint16(dnfenum.CmdPacketUseStackable), body: make([]byte, 16)},
		{name: "unrelated nineteen byte body", typ: uint16(dnfenum.CmdPacketUseStackable) + 1, body: make([]byte, 19)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLegacyGameBody(tt.typ, tt.body)
			if len(got) != len(tt.body) || !bytes.Equal(got, tt.body) {
				t.Fatalf("normalized body = %x (len %d), want unchanged len %d", got, len(got), len(tt.body))
			}
		})
	}
}

func TestNormalizeLegacyGameBodyStripsExactGuardianGemTransportTrailer(t *testing.T) {
	semantic := make([]byte, currentGuardianGemUseRequestWireSize)
	semantic[0] = 0x90
	semantic[4] = 32
	semantic[6] = 0xf1
	semantic[10] = 3
	liveBody := append(append([]byte(nil), semantic...), 0xde, 0xad, 0xbe, 0xef)

	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketUseGem), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op829 body = %x, want %x", got, semantic)
	}
	if &got[0] == &liveBody[0] {
		t.Fatal("normalized op829 body aliases the transport packet")
	}
}

func TestNormalizeLegacyGameBodyKeepsStrictGuardianGemBoundaries(t *testing.T) {
	for _, body := range [][]byte{
		make([]byte, currentGuardianGemUseRequestWireSize),
		make([]byte, currentGuardianGemUseRequestWireSize+5),
		make([]byte, 16),
	} {
		got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketUseGem), body)
		if len(got) != len(body) || !bytes.Equal(got, body) {
			t.Fatalf("op829 body len %d normalized to len %d", len(body), len(got))
		}
	}
}

func TestNormalizeLegacyGameBodyStripsExactRentalTransportTrailer(t *testing.T) {
	semantic := make([]byte, currentRentalRequestWireSize)
	semantic[13] = 1
	semantic[17] = 3
	liveBody := append(append([]byte(nil), semantic...), 0xde, 0xad, 0xbe, 0xef)
	for _, typ := range []uint16{
		uint16(dnfenum.CmdPacketRentEquipmentItem),
		uint16(dnfenum.CmdPacketChargeRentpoint),
	} {
		got := normalizeLegacyGameBody(typ, liveBody)
		if !bytes.Equal(got, semantic) {
			t.Fatalf("normalized rental op%d body = %x, want %x", typ, got, semantic)
		}
		if &got[0] == &liveBody[0] {
			t.Fatalf("normalized rental op%d aliases transport packet", typ)
		}
	}
}

func TestNormalizeLegacyGameBodyKeepsOpaqueRentalBoundaries(t *testing.T) {
	for _, typ := range []uint16{
		uint16(dnfenum.CmdPacketRentEquipmentItem),
		uint16(dnfenum.CmdPacketChargeRentpoint),
	} {
		for _, body := range [][]byte{make([]byte, currentRentalRequestWireSize), make([]byte, 24)} {
			got := normalizeLegacyGameBody(typ, body)
			if len(got) != len(body) || !bytes.Equal(got, body) {
				t.Fatalf("rental op%d body len %d normalized to len %d", typ, len(body), len(got))
			}
		}
	}
}

func TestNormalizeLegacyGameBodyStripsExactSelectCardTransportTrailer(t *testing.T) {
	semantic := []byte{0, 3}
	liveBody := append(append([]byte(nil), semantic...), 0xde, 0xad, 0xbe, 0xef)

	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSelectCard), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op71 body = %x, want %x", got, semantic)
	}
	if &got[0] == &liveBody[0] {
		t.Fatal("normalized op71 body aliases the transport packet")
	}
}

func TestNormalizeLegacyGameBodyKeepsStrictSelectCardBoundaries(t *testing.T) {
	tests := []struct {
		name string
		typ  uint16
		body []byte
	}{
		{name: "single semantic byte clone", typ: uint16(dnfenum.CmdPacketSelectCard), body: make([]byte, 5)},
		{name: "semantic body", typ: uint16(dnfenum.CmdPacketSelectCard), body: make([]byte, 2)},
		{name: "old opaque protected body", typ: uint16(dnfenum.CmdPacketSelectCard), body: make([]byte, 8)},
		{name: "unrelated six byte body", typ: uint16(dnfenum.CmdPacketSelectCard) + 2, body: make([]byte, 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLegacyGameBody(tt.typ, tt.body)
			if len(got) != len(tt.body) || !bytes.Equal(got, tt.body) {
				t.Fatalf("normalized body = %x (len %d), want unchanged len %d", got, len(got), len(tt.body))
			}
		})
	}
}

func TestNormalizeLegacyGameBodyStripsExactEplpTransportTrailer(t *testing.T) {
	semantic := []byte{1, 2}
	liveBody := append(append([]byte(nil), semantic...), 0xde, 0xad, 0xbe, 0xef)

	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketEplpCommand), liveBody)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op72 body = %x, want %x", got, semantic)
	}
	if &got[0] == &liveBody[0] {
		t.Fatal("normalized op72 body aliases the transport packet")
	}
}

func TestNormalizeLegacyGameBodyKeepsStrictEplpBoundaries(t *testing.T) {
	tests := []struct {
		name string
		typ  uint16
		body []byte
	}{
		{name: "semantic body", typ: uint16(dnfenum.CmdPacketEplpCommand), body: make([]byte, 2)},
		{name: "old opaque protected body", typ: uint16(dnfenum.CmdPacketEplpCommand), body: make([]byte, 16)},
		{name: "unrelated six byte body", typ: uint16(dnfenum.CmdPacketEplpCommand) + 1, body: make([]byte, 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLegacyGameBody(tt.typ, tt.body)
			if len(got) != len(tt.body) || !bytes.Equal(got, tt.body) {
				t.Fatalf("normalized body = %x (len %d), want unchanged len %d", got, len(got), len(tt.body))
			}
		})
	}
}
