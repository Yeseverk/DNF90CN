package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func newPremiumServiceTestRuntime(
	t *testing.T,
	activeSlots ...int64,
) (*Service, *gameSession, dnfrepo.Group, *bufferConn) {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Now().UTC()
	account := dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  make(map[string]string),
	}
	for _, slot := range activeSlots {
		premium.Upsert(&account, premium.DevilSlotType(slot), 7*24*60*60, 1, now)
	}
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   account.AccountID,
		Stats:       make(map[string]int64),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       make(map[string]dnfrepo.ItemStack),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries:     make(map[string]dnfrepo.EquipmentEntry),
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          account.AccountID,
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	return service, &gameSession{
		conn:                connection,
		connID:              "premium-service-test",
		selectedCharacterID: 19,
	}, repositories, connection
}

func premiumServicePacketUsage(t *testing.T, packetBytes []byte, slot int64) uint8 {
	t.Helper()
	packet, trailing := splitGameServerUpperPacket(t, packetBytes)
	if len(trailing) != 0 ||
		packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketPremiumService) {
		t.Fatalf("premium packet header=%+v trailing=%d", packet.Header, len(trailing))
	}
	if len(packet.Body) != 77 || !bytes.Equal(packet.Body[:3], []byte{1, 1, 0}) {
		t.Fatalf("premium packet body=%x", packet.Body)
	}
	return packet.Body[3+10+slot*9]
}

func TestCurrentPremiumServiceExplicitRequestReturnsSingleOp903State(t *testing.T) {
	service, session, _, connection := newPremiumServiceTestRuntime(
		t,
		premium.DevilSlotAutoRepair,
	)

	if err := service.handleCurrentPremiumService(session, nil); err != nil {
		t.Fatal(err)
	}

	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 ||
		packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketPremiumService) {
		t.Fatalf("explicit premium request packet header=%+v trailing=%x", packet.Header, trailing)
	}
	if len(packet.Body) != 77 || !bytes.Equal(packet.Body[:3], []byte{1, 1, 0}) {
		t.Fatalf("explicit premium request body=%x", packet.Body)
	}
	expiryOffset := 3 + 6 + premium.DevilSlotAutoRepair*9
	if got := binary.LittleEndian.Uint32(packet.Body[expiryOffset : expiryOffset+4]); got == 0 {
		t.Fatalf("explicit premium request auto-repair expiry=%d, want active absolute expiry", got)
	}
}

func TestCurrentPremiumSevenBuffUseConsumesOneDailyUse(t *testing.T) {
	service, session, repositories, connection := newPremiumServiceTestRuntime(
		t,
		premium.DevilSlotSevenBuff,
	)
	body := make([]byte, 2+currentPremiumServiceBlobSize)
	binary.LittleEndian.PutUint16(body[:2], 1)
	binary.LittleEndian.PutUint16(body[2:4], uint16(premium.DevilSlotSevenBuff))

	if err := service.handleCurrentPremiumService(session, body); err != nil {
		t.Fatal(err)
	}
	if got := premiumServicePacketUsage(t, connection.write.Bytes(), premium.DevilSlotSevenBuff); got != 1 {
		t.Fatalf("wire seven-buff usage=%d, want 1", got)
	}
	character, found, err := repositories.Character.Load(context.Background(), strconv.Itoa(int(session.selectedCharacterID)))
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if got := premium.DailyUsage(character, premium.DevilSlotSevenBuff, time.Now().UTC()); got != 1 {
		t.Fatalf("persisted seven-buff usage=%d, want 1", got)
	}
}

func TestCurrentPremiumFreeWeaknessConsumesOnlyFreeOp9Path(t *testing.T) {
	service, session, repositories, connection := newPremiumServiceTestRuntime(
		t,
		premium.DevilSlotFreeWeakness,
	)
	if err := service.handleCurrentPremiumFreeWeakness(session, []byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("paid weakness recovery unexpectedly sent %d bytes", connection.write.Len())
	}

	if err := service.handleCurrentPremiumFreeWeakness(session, []byte{1, 0}); err != nil {
		t.Fatal(err)
	}
	if got := premiumServicePacketUsage(t, connection.write.Bytes(), premium.DevilSlotFreeWeakness); got != 1 {
		t.Fatalf("wire free-weakness usage=%d, want 1", got)
	}
	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if got := premium.DailyUsage(character, premium.DevilSlotFreeWeakness, time.Now().UTC()); got != 1 {
		t.Fatalf("persisted free-weakness usage=%d, want 1", got)
	}
}
