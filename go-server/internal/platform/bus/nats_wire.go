package bus

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

const (
	NATSWireBinary = "binary_v1"
	NATSWireJSON   = "json"
)

var (
	natsWireMagic = []byte{'L', 'H', 'B', '1'}
	natsWirePool  = sync.Pool{New: func() any { return new(bytes.Buffer) }}
)

type natsWireMessage struct {
	Topic       string
	Payload     []byte
	PublishedAt time.Time
	Reply       string
	RequestID   string
	Trace       map[string]string
}

type natsJSONWireMessage struct {
	Topic       string            `json:"topic"`
	Payload     json.RawMessage   `json:"payload"`
	PublishedAt time.Time         `json:"published_at"`
	Reply       string            `json:"reply,omitempty"`
	RequestID   string            `json:"request_id,omitempty"`
	Trace       map[string]string `json:"trace,omitempty"`
}

func isNATSWireEncoding(encoding string) bool {
	switch encoding {
	case NATSWireBinary, NATSWireJSON:
		return true
	default:
		return false
	}
}

func encodeNATSWire(msg natsWireMessage, encoding string) ([]byte, error) {
	if encoding == NATSWireJSON {
		return encodeNATSJSON(msg)
	}
	return encodeNATSBinary(msg)
}

func encodeNATSJSON(msg natsWireMessage) ([]byte, error) {
	return json.Marshal(natsJSONWireMessage{
		Topic:       msg.Topic,
		Payload:     json.RawMessage(msg.Payload),
		PublishedAt: msg.PublishedAt,
		Reply:       msg.Reply,
		RequestID:   msg.RequestID,
		Trace:       msg.Trace,
	})
}

func decodeNATSWire(data []byte) (natsWireMessage, error) {
	if bytes.HasPrefix(data, natsWireMagic) {
		return decodeNATSBinary(data)
	}
	var wire natsJSONWireMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		return natsWireMessage{}, err
	}
	return natsWireMessage{
		Topic:       wire.Topic,
		Payload:     append([]byte(nil), wire.Payload...),
		PublishedAt: wire.PublishedAt,
		Reply:       wire.Reply,
		RequestID:   wire.RequestID,
		Trace:       wire.Trace,
	}, nil
}

func encodeNATSBinary(msg natsWireMessage) ([]byte, error) {
	buf := natsWirePool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		buf.Reset()
		natsWirePool.Put(buf)
	}()

	buf.Write(natsWireMagic)
	writeVarint(buf, msg.PublishedAt.UnixNano())
	writeString(buf, msg.Topic)
	writeString(buf, msg.Reply)
	writeString(buf, msg.RequestID)
	writeTrace(buf, msg.Trace)
	writeBytes(buf, msg.Payload)
	return append([]byte(nil), buf.Bytes()...), nil
}

func decodeNATSBinary(data []byte) (natsWireMessage, error) {
	reader := bytes.NewReader(data)
	magic := make([]byte, len(natsWireMagic))
	if _, err := io.ReadFull(reader, magic); err != nil {
		return natsWireMessage{}, err
	}
	if !bytes.Equal(magic, natsWireMagic) {
		return natsWireMessage{}, fmt.Errorf("invalid nats wire magic")
	}
	unixNano, err := readVarint(reader)
	if err != nil {
		return natsWireMessage{}, err
	}
	topic, err := readString(reader)
	if err != nil {
		return natsWireMessage{}, err
	}
	reply, err := readString(reader)
	if err != nil {
		return natsWireMessage{}, err
	}
	requestID, err := readString(reader)
	if err != nil {
		return natsWireMessage{}, err
	}
	trace, err := readTrace(reader)
	if err != nil {
		return natsWireMessage{}, err
	}
	payload, err := readBytes(reader)
	if err != nil {
		return natsWireMessage{}, err
	}
	if reader.Len() != 0 {
		return natsWireMessage{}, fmt.Errorf("nats wire has %d trailing bytes", reader.Len())
	}
	var publishedAt time.Time
	if unixNano != 0 {
		publishedAt = time.Unix(0, unixNano).UTC()
	}
	return natsWireMessage{
		Topic:       topic,
		Payload:     payload,
		PublishedAt: publishedAt,
		Reply:       reply,
		RequestID:   requestID,
		Trace:       trace,
	}, nil
}

func writeVarint(buf *bytes.Buffer, value int64) {
	var scratch [binary.MaxVarintLen64]byte
	n := binary.PutVarint(scratch[:], value)
	buf.Write(scratch[:n])
}

func readVarint(reader *bytes.Reader) (int64, error) {
	value, err := binary.ReadVarint(reader)
	if err != nil {
		return 0, fmt.Errorf("read varint: %w", err)
	}
	return value, nil
}

func writeUvarint(buf *bytes.Buffer, value uint64) {
	var scratch [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(scratch[:], value)
	buf.Write(scratch[:n])
}

func readUvarint(reader *bytes.Reader) (uint64, error) {
	value, err := binary.ReadUvarint(reader)
	if err != nil {
		return 0, fmt.Errorf("read uvarint: %w", err)
	}
	return value, nil
}

func writeString(buf *bytes.Buffer, value string) {
	writeBytes(buf, []byte(value))
}

func readString(reader *bytes.Reader) (string, error) {
	value, err := readBytes(reader)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func writeBytes(buf *bytes.Buffer, value []byte) {
	writeUvarint(buf, uint64(len(value)))
	buf.Write(value)
}

func readBytes(reader *bytes.Reader) ([]byte, error) {
	length, err := readUvarint(reader)
	if err != nil {
		return nil, err
	}
	if length > uint64(reader.Len()) { //nolint:gosec // G115：bytes.Reader.Len 永远非负，这里只用于剩余长度上界比较。
		return nil, fmt.Errorf("nats wire length %d exceeds remaining %d", length, reader.Len())
	}
	value := make([]byte, int(length)) //nolint:gosec // G115：前面已确认 length 不超过 reader.Len。
	if _, err := io.ReadFull(reader, value); err != nil {
		return nil, err
	}
	return value, nil
}

func writeTrace(buf *bytes.Buffer, trace map[string]string) {
	if len(trace) == 0 {
		writeUvarint(buf, 0)
		return
	}
	keys := make([]string, 0, len(trace))
	for key := range trace {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writeUvarint(buf, uint64(len(keys)))
	for _, key := range keys {
		writeString(buf, key)
		writeString(buf, trace[key])
	}
}

func readTrace(reader *bytes.Reader) (map[string]string, error) {
	count, err := readUvarint(reader)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	maxEntries := uint64(reader.Len() / 2) //nolint:gosec // G115：bytes.Reader.Len 永远非负，这里估算剩余 trace 最多条目。
	if count > maxEntries {
		return nil, fmt.Errorf("nats wire trace count %d exceeds remaining %d", count, reader.Len())
	}
	trace := make(map[string]string, int(count)) //nolint:gosec // G115：前面已按剩余字节数约束 count。
	for i := uint64(0); i < count; i++ {
		key, err := readString(reader)
		if err != nil {
			return nil, err
		}
		value, err := readString(reader)
		if err != nil {
			return nil, err
		}
		trace[key] = value
	}
	return trace, nil
}
