package dnfbridge

import (
	"bytes"
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentActorUserStateBodyAndRepositoryBackedSend(t *testing.T) {
	if got, want := buildCurrentActorUserStateBody(0x1234, 0x13), []byte{0x34, 0x12, 0x13}; !bytes.Equal(got, want) {
		t.Fatalf("user-state body=%x want=%x", got, want)
	}

	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Stats:       map[string]int64{"user_state_bits": 0x13},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                connection,
		connID:              "actor-user-state",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}
	if err := service.sendSelectedActorUserStateRefresh(session, "test"); err != nil {
		t.Fatal(err)
	}
	packet, trailing := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.MsgID != currentActorUserStateMsgID ||
		packet.Header.Classification != 0 ||
		!bytes.Equal(packet.Body, []byte{19, 0, 0x13}) ||
		len(trailing) != 0 {
		t.Fatalf("user-state packet header=%+v body=%x trailing=%x", packet.Header, packet.Body, trailing)
	}
}
