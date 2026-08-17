package dnfbridge

import (
	"encoding/binary"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestAdventureGameplayModuleOwnsLegacyAndUpperRuntimeOpcodes(t *testing.T) {
	module := adventureGameplayModule()
	for _, opcode := range []uint16{
		uint16(dnfenum.CmdPacketMercenaryInfo),
		uint16(dnfenum.CmdPacketMercenaryCompetition),
		uint16(dnfenum.CmdPacketMercenaryCompetitionCancle),
		uint16(dnfenum.CmdPacketMercenaryCompetitionRewardRequest),
		uint16(dnfenum.CmdPacketMercenaryPointRecalculate),
		uint16(dnfenum.CmdPacketAdventurerShopPurchase),
		uint16(dnfenum.CmdPacketAdventureGrowthcapsuleExp),
	} {
		if module.LegacyHandlers[opcode] == nil || module.UpperHandlers[opcode] == nil {
			t.Fatalf("opcode %d is not registered for both current transports", opcode)
		}
	}
}

func TestParseCurrentAdventureExpeditionStartRequest(t *testing.T) {
	body := make([]byte, 13)
	body[0] = 2
	binary.LittleEndian.PutUint32(body[1:5], 6*60*60)
	binary.LittleEndian.PutUint16(body[5:7], 2)
	copy(body[7:], []byte{1, 0, 2, 3, 1, 4})
	request, err := parseCurrentAdventureExpeditionStartRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Area != 2 || request.DurationSeconds != 6*60*60 || len(request.Members) != 2 ||
		request.Members[1] != (currentAdventureExpeditionMemberRequest{Selector: 3, Job: 1, GrowType: 4}) {
		t.Fatalf("request=%+v", request)
	}
	if _, err := parseCurrentAdventureExpeditionStartRequest(body[:12]); err == nil {
		t.Fatal("truncated request accepted")
	}
}

func TestBuildCurrentAdventureExpeditionStateBodyMatchesCurrentReader(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	state := adventuregroup.RuntimeState{
		Expeditions: map[string]adventuregroup.Expedition{
			"2": {
				Area:       2,
				State:      1,
				StartedAt:  now.Unix() - 10,
				EndsAt:     now.Unix() + 10,
				Attributes: []byte{4, 7},
				Members: []adventuregroup.ExpeditionMember{{
					CharacterID: 3,
					Name:        "牛牛",
					Level:       90,
					Job:         1,
					GrowType:    2,
				}},
			},
		},
	}
	body := buildCurrentAdventureExpeditionStateBody(state, now)
	offset := 0
	if body[offset] != 1 {
		t.Fatalf("area count=%d", body[offset])
	}
	offset++
	if body[offset] != 2 || body[offset+1] != 1 {
		t.Fatalf("area/state=%x", body[offset:offset+2])
	}
	offset += 2 + 4 + 4
	if body[offset] != 2 || body[offset+1] != 4 || body[offset+2] != 7 {
		t.Fatalf("attributes=%x", body[offset:offset+3])
	}
	offset += 3
	if binary.LittleEndian.Uint16(body[offset:offset+2]) != 1 {
		t.Fatalf("member count=%x", body[offset:offset+2])
	}
	offset += 2
	if binary.LittleEndian.Uint16(body[offset:offset+2]) != 3 {
		t.Fatalf("member id=%x", body[offset:offset+2])
	}
	offset += 2
	nameLength := int(binary.LittleEndian.Uint32(body[offset : offset+4]))
	if nameLength != 4 {
		t.Fatalf("GB18030 name length=%d body=%x", nameLength, body)
	}
}

func TestResolveCurrentAdventureShopProductAcceptsCurrentItemIDAndZeroBasedIndex(t *testing.T) {
	category := adventuregroup.ShopCategory{Products: []adventuregroup.ShopProduct{
		{ItemID: 100},
		{ItemID: 200},
	}}
	if product, ok := resolveCurrentAdventureShopProduct(category, 200); !ok || product.ItemID != 200 {
		t.Fatalf("item-id selector product=%+v ok=%v", product, ok)
	}
	if product, ok := resolveCurrentAdventureShopProduct(category, 0); !ok || product.ItemID != 100 {
		t.Fatalf("index selector product=%+v ok=%v", product, ok)
	}
}
