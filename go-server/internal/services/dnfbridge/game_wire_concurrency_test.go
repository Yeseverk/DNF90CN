package dnfbridge

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func TestConcurrentGameTransportSendsPreserveWireSequence(t *testing.T) {
	const sendCount = 128

	conn := &bufferConn{}
	service := &Service{}
	session := &gameSession{conn: conn, connID: "transport-concurrency"}

	runConcurrentGameSends(t, sendCount, func(index int) error {
		return service.sendGame(session, 1, uint16(300+index), []byte{byte(index)})
	})

	stream := conn.write.Bytes()
	for want := uint32(0); want < sendCount; want++ {
		if len(stream) < dnfproto.MinTCPFrameSize {
			t.Fatalf("transport frame %d is missing: remaining=%d", want, len(stream))
		}
		frameLength := int(binary.LittleEndian.Uint16(stream[2:4]))
		if frameLength > len(stream) {
			t.Fatalf("transport frame %d length=%d remaining=%d", want, frameLength, len(stream))
		}
		records, err := dnfproto.ParseLatestGameTCPRecords(stream[:frameLength])
		if err != nil {
			t.Fatalf("parse transport frame %d: %v", want, err)
		}
		if len(records) != 1 {
			t.Fatalf("transport frame %d records=%d want=1", want, len(records))
		}
		if got := records[0].TransportHeader.Sequence; got != want {
			t.Fatalf("transport wire sequence=%d want=%d", got, want)
		}
		stream = stream[frameLength:]
	}
	if len(stream) != 0 {
		t.Fatalf("transport stream has %d trailing bytes", len(stream))
	}
	if got := session.sequence; got != sendCount {
		t.Fatalf("transport session sequence=%d want=%d", got, sendCount)
	}
}

func TestConcurrentGameUpperSendsPreserveWireSequence(t *testing.T) {
	const sendCount = 128

	conn := &bufferConn{}
	service := &Service{options: options{
		gameUpperHeader:    gameUpperHeaderChannel13,
		gameUpperBodyCodec: gameUpperBodyCodecPlain,
	}}
	session := &gameSession{conn: conn, connID: "upper-concurrency"}

	runConcurrentGameSends(t, sendCount, func(index int) error {
		return service.sendGameUpperRawClassNoCodec(
			session,
			uint16(300+index),
			[]byte{byte(index)},
			dnfproto.DefaultChannelClassification,
		)
	})

	stream := conn.write.Bytes()
	for want := uint16(0); want < sendCount; want++ {
		packet, rest := splitGameServerUpperPacket(t, stream)
		if got := packet.Header.Seq; got != want {
			t.Fatalf("upper wire sequence=%d want=%d", got, want)
		}
		stream = rest
	}
	if len(stream) != 0 {
		t.Fatalf("upper stream has %d trailing bytes", len(stream))
	}
	if got := session.upperSeq; got != sendCount {
		t.Fatalf("upper session sequence=%d want=%d", got, sendCount)
	}
}

func TestGamePacketLogSequenceIsAtomic(t *testing.T) {
	const logCount = 256

	service := &Service{}
	session := &gameSession{connID: "packet-log-concurrency"}

	runConcurrentGameSends(t, logCount, func(index int) error {
		service.logGamePacket(session, "SEND", "concurrency-test", []byte{byte(index)})
		return nil
	})

	if got := atomic.LoadUint64(&session.packetSeq); got != logCount {
		t.Fatalf("packet log sequence=%d want=%d", got, logCount)
	}
}

func runConcurrentGameSends(t *testing.T, count int, send func(index int) error) {
	t.Helper()

	start := make(chan struct{})
	errors := make(chan error, count)
	var group sync.WaitGroup
	group.Add(count)
	for index := 0; index < count; index++ {
		index := index
		go func() {
			defer group.Done()
			<-start
			if err := send(index); err != nil {
				errors <- fmt.Errorf("send %d: %w", index, err)
			}
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
}
