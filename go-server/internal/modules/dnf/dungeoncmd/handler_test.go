package dungeoncmd

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeSelectDungeonRequest(t *testing.T) {
	body := make([]byte, 23)
	binary.LittleEndian.PutUint32(body[0:4], 0x78563412)
	body[4] = 3
	binary.LittleEndian.PutUint16(body[5:7], 0x0504)
	body[7] = 6
	body[8] = 7
	binary.LittleEndian.PutUint16(body[9:11], 0x0908)
	binary.LittleEndian.PutUint32(body[11:15], 0x0d0c0b0a)
	body[15] = 0x0e
	binary.LittleEndian.PutUint32(body[16:20], 0x1211100f)
	body[20] = 0x13
	body[21], body[22] = 0xDE, 0xAD
	got, err := DecodeSelectDungeonRequest(body)
	if err != nil {
		t.Fatalf("DecodeSelectDungeonRequest error = %v", err)
	}
	if got.DungeonID != 0x78563412 || got.Difficulty != 3 || got.EntryOption != 0x0504 ||
		got.SelectionMode != 6 || got.RuntimeState != 7 || got.RuntimeToken != 0x0908 ||
		got.Reserved != 0x0d0c0b0a || got.PartyState != 0x0e ||
		got.LeaderObjectKey != 0x1211100f || got.SpecialMode != 0x13 ||
		len(got.OpaqueTail) != 2 || got.OpaqueTail[0] != 0xDE {
		t.Fatalf("got = %+v", got)
	}
	body[21] = 0
	if got.OpaqueTail[0] != 0xDE {
		t.Fatal("opaque select tail aliases request body")
	}
	if _, err := DecodeSelectDungeonRequest(make([]byte, 20)); err == nil {
		t.Fatal("20-byte select request accepted")
	}
}

func TestDecodeSelectDungeonRequestRejectsTruncatedFiveByteBody(t *testing.T) {
	if _, err := DecodeSelectDungeonRequest(make([]byte, 5)); err == nil {
		t.Fatal("truncated five-byte op16 request accepted")
	}
}

func TestDecodeGetItemRequestMatchesCurrentEXEOrdinaryTwentyByteWriter(t *testing.T) {
	body := make([]byte, GetItemRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], 0x44332211)
	body[4] = 0
	body[5] = 0x66
	for index, value := range []uint16{101, 102, 103, 201, 202, 301, 302} {
		binary.LittleEndian.PutUint16(body[6+index*2:], value)
	}
	request, err := DecodeGetItemRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.DropObjectKey != 0x44332211 || request.FixedZero != 0 || request.ObjectContext != 0x66 ||
		request.PlayerX != 101 || request.PlayerY != 102 || request.Token0 != 103 ||
		request.DropX != 201 || request.DropY != 202 || request.Token1 != 301 || request.Token2 != 302 {
		t.Fatalf("request = %+v", request)
	}
	for _, size := range []int{2, 18, GetItemRequestSize - 1, GetItemRequestSize + 1} {
		if _, err := DecodeGetItemRequest(make([]byte, size)); err == nil {
			t.Fatalf("op43 body size %d accepted", size)
		}
	}
}

func TestDecodeGetItemRequestMatchesLiveOrdinaryPickupCapture(t *testing.T) {
	// Full C2S body after the legacy game header from a live accepted op43.
	// Keep the byte at offset 4: an earlier truncated transcription omitted it
	// and incorrectly shifted ObjectContext into FixedZero.
	body, err := hex.DecodeString("ae01000000016502f500d8094b02e500876c9a10")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeGetItemRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.DropObjectKey != 430 || request.FixedZero != 0 || request.ObjectContext != 1 ||
		request.PlayerX != 613 || request.PlayerY != 245 || request.Token0 != 2520 ||
		request.DropX != 587 || request.DropY != 229 || request.Token1 != 27783 || request.Token2 != 4250 {
		t.Fatalf("request = %+v", request)
	}
}

func TestDecodeMoveMapRequestCurrentEXELayoutAndBoundary(t *testing.T) {
	if _, err := DecodeMoveMapRequest(make([]byte, MoveMapRequestSize-1)); err == nil {
		t.Fatal("99-byte move-map request accepted")
	}
	if _, err := DecodeMoveMapRequest(make([]byte, MoveMapRequestSize+1)); err == nil {
		t.Fatal("101-byte move-map request accepted")
	}
	body := make([]byte, MoveMapRequestSize)
	body[0], body[1] = 1, 2
	binary.LittleEndian.PutUint32(body[2:6], 0x11223344)
	binary.LittleEndian.PutUint32(body[6:10], 0x55667788)
	body[10] = 4
	binary.LittleEndian.PutUint16(body[11:13], 0x1234)
	offset := 13
	for index := 0; index < 8; index++ {
		binary.LittleEndian.PutUint16(body[offset:offset+2], uint16(100+index))
		offset += 2
	}
	for index := 0; index < 8; index++ {
		binary.LittleEndian.PutUint32(body[offset:offset+4], uint32(1000+index))
		offset += 4
	}
	binary.LittleEndian.PutUint16(body[61:63], 0xBEEF)
	for index := 63; index < 99; index++ {
		body[index] = byte(index)
	}
	body[99] = 9
	full, err := DecodeMoveMapRequest(body)
	if err != nil {
		t.Fatalf("DecodeMoveMapRequest full error = %v", err)
	}
	if full.NextX != 1 || full.NextY != 2 || full.PositionX != 0x11223344 ||
		full.PositionY != 0x55667788 || full.MoveKind != 4 || full.TimingToken != 0x1234 ||
		full.ShortState[7] != 107 || full.IntegerState[7] != 1007 ||
		full.SequenceToken != 0xBEEF || full.RuntimeState != 9 ||
		full.RuntimeTail[0] != 63 || full.RuntimeTail[35] != 98 || len(full.OpaqueTail) != 0 {
		t.Fatalf("full = %+v", full)
	}
	body[63] = 0
	if full.RuntimeTail[0] != 63 {
		t.Fatal("runtime tail aliases request body")
	}
}

func TestDecodeDieMonsterRequestCurrentEXELayoutAndBoundary(t *testing.T) {
	if _, err := DecodeDieMonsterRequest(make([]byte, 61)); err == nil {
		t.Fatal("61-byte die-monster request accepted")
	}
	body := make([]byte, DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], 0x11223344)
	binary.LittleEndian.PutUint16(body[4:6], 0x5566)
	for index := 0; index < 4; index++ {
		binary.LittleEndian.PutUint32(body[6+index*4:], uint32(100+index))
	}
	body[22] = 7
	binary.LittleEndian.PutUint16(body[23:25], 201)
	binary.LittleEndian.PutUint16(body[25:27], 202)
	copy(body[27:31], []byte{8, 9, 10, 11})
	binary.LittleEndian.PutUint16(body[31:33], 0x7788)
	body[33] = 1
	binary.LittleEndian.PutUint32(body[34:38], 0x99AABBCC)
	binary.LittleEndian.PutUint64(body[38:46], 0x0102030405060708)
	for index := 0; index < 7; index++ {
		binary.LittleEndian.PutUint16(body[46+index*2:], uint16(300+index))
	}
	body[60], body[61] = 12, 13

	got, err := DecodeDieMonsterRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.RuntimeObjectKey != 0x11223344 || got.OwnerObjectKey != 0x5566 ||
		got.CombatState[3] != 103 || got.ShortState[1] != 202 || got.ByteState[3] != 11 ||
		got.RandomState != 0x7788 || got.HasActorState != 1 || got.ActorState != 0x99AABBCC ||
		got.ActorValue != 0x0102030405060708 || got.ActorShortState[6] != 306 ||
		got.Layout != DieMonsterRequestLayoutFixed62 || got.TailState != [2]byte{12, 13} || len(got.OpaqueTail) != 0 {
		t.Fatalf("got = %+v", got)
	}
	if _, err := DecodeDieMonsterRequest(append(append([]byte(nil), body...), 0xFA)); err == nil {
		t.Fatal("unsupported 63-byte die-monster boundary accepted")
	}
}

func TestDecodeDieMonsterRequestCurrentTutorialAPCVariableLayout(t *testing.T) {
	body, err := hex.DecodeString("9301000094010000000000000000910d0000130000000211019401bf0b00000d001101130094010000060064024d01000000001e3c0001000000430d00000000000064024d0164024d0112002f0003000000300280d6")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeDieMonsterRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != DieMonsterRequestLayoutVariableCombat ||
		got.RuntimeObjectKey != 403 || got.OwnerObjectKey != 404 ||
		got.CombatEntryCount != 2 || len(got.CombatEntries) != 20 ||
		len(got.RuntimeTail) != DieMonsterVariableRuntimeTailSize || len(got.OpaqueTail) != 0 {
		t.Fatalf("got = %+v", got)
	}
	body[23] = 0
	if got.CombatEntries[0] != 0x11 {
		t.Fatal("variable combat entries alias request body")
	}
	withTail := append(append([]byte(nil), body...), 0xFA, 0xCE)
	got, err = DecodeDieMonsterRequest(withTail)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.OpaqueTail) != 2 || got.OpaqueTail[0] != 0xFA || got.OpaqueTail[1] != 0xCE {
		t.Fatalf("opaque tail = %x", got.OpaqueTail)
	}
	truncated := append([]byte(nil), body[:len(body)-1]...)
	truncated[22] = 2
	if _, err := DecodeDieMonsterRequest(truncated); err == nil {
		t.Fatal("truncated variable die-monster body accepted")
	}
}

func TestDecodeBossDieCheckRequestCurrentEXELayoutAndBoundary(t *testing.T) {
	for _, size := range []int{BossDieCheckRequestSize - 1, BossDieCheckRequestSize + 1} {
		if _, err := DecodeBossDieCheckRequest(make([]byte, size)); err == nil {
			t.Fatalf("%d-byte boss-die-check request accepted", size)
		}
	}
	body := make([]byte, BossDieCheckRequestSize)
	binary.LittleEndian.PutUint16(body[0:2], 0x1122)
	binary.LittleEndian.PutUint16(body[2:4], 0x3344)
	binary.LittleEndian.PutUint32(body[4:8], 0)
	binary.LittleEndian.PutUint32(body[8:12], 0x55667788)
	body[12] = 0
	binary.LittleEndian.PutUint32(body[13:17], 0x99aabbcc)
	binary.LittleEndian.PutUint64(body[17:25], 0x0102030405060708)
	for index := 0; index < 7; index++ {
		binary.LittleEndian.PutUint16(body[25+index*2:], uint16(0x1200+index))
	}

	got, err := DecodeBossDieCheckRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.RelatedActorObjectKey != 0x1122 || got.TargetObjectKey != 0x3344 ||
		got.ReservedZero != 0 || got.Field08 != 0x55667788 || got.Field12 != 0 ||
		got.Field13 != 0x99aabbcc || got.Field17 != 0x0102030405060708 ||
		got.Field25[0] != 0x1200 || got.Field25[6] != 0x1206 {
		t.Fatalf("got = %+v", got)
	}
}

func TestHandlerBlocksDungeonAck(t *testing.T) {
	body := make([]byte, GetItemRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], 0x11223344)
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketGetItem),
		Body:   body,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if got.Operation != "get_item" {
		t.Fatalf("operation = %q", got.Operation)
	}
}

func TestHandlerUsesOwnerPreflightAndStillBlocksDungeonAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "acc",
		Level:       86,
		Stats:       map[string]int64{"fatigue": 121},
		Location:    dnfrepo.CharacterLocation{TownID: 3, DungeonID: 4001, RoomID: "2:3"},
	}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 1001, Count: 2},
		},
	}); err != nil {
		t.Fatalf("Inventory.Save error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSelectDungeon),
		SelectedCharacterID: 99,
		Repositories:        repos,
		Body: func() []byte {
			body := make([]byte, 21)
			binary.LittleEndian.PutUint32(body, 4001)
			body[4] = 3
			return body
		}(),
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	for _, want := range []string{"dungeon owner verified", "level=86", "fatigue=121", "requestDungeon=4001", "difficulty=3", `room="2:3"`} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
}

func TestHandlerMoveMapRequiresLiveRuntimeBeforeResponse(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "acc"}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	body := make([]byte, MoveMapRequestSize)
	body[0], body[1] = 1, 2
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveMap),
		SelectedCharacterID: 99,
		Repositories:        repos,
		Body:                body,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || got.Operation != "move_map" {
		t.Fatalf("result = %+v, want blocked move_map", got)
	}
	for _, want := range []string{"active dungeon runtime session required", "target-room actor/object packet chain is not proven"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
}

func TestDecodeChangeTutorialFlagRequestFromCurrentEXEWriter(t *testing.T) {
	got, err := DecodeChangeTutorialFlagRequest([]byte{
		0x00,
		0x1f, 0x00, 0x00, 0x00,
		0x01,
	})
	if err != nil {
		t.Fatalf("DecodeChangeTutorialFlagRequest error = %v", err)
	}
	if got.Prefix != 0 || got.Progress != 31 || got.CommitFlag != 1 {
		t.Fatalf("request = %+v", got)
	}
}

func TestDecodeChangeTutorialFlagRequestRequiresExactBoundary(t *testing.T) {
	for _, body := range [][]byte{make([]byte, 5), make([]byte, 7), make([]byte, 16)} {
		if _, err := DecodeChangeTutorialFlagRequest(body); err == nil {
			t.Fatalf("body len %d unexpectedly decoded", len(body))
		}
	}
}

func TestTutorialCommandRecordsMCPOwnerGap(t *testing.T) {
	cmd := NewTutorialCommand(alignedcmd.Request{
		AccountID:           " acc ",
		SelectedCharacterID: 99,
		Body:                make([]byte, ChangeTutorialFlagRequestSize),
	}, ChangeTutorialFlagRequest{
		Progress:   31,
		CommitFlag: 1,
	})
	summary := cmd.String()
	for _, want := range []string{`account="acc"`, "char=99", "progress=31", "commit=1", "rawLen=6", "tutorial owner", "EXE"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}
