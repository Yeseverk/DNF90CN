package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// frame 加密参数使用 AES-GCM 推荐的 32 字节 key 和 12 字节 nonce。
const (
	FrameSessionKeySize = 32
	FrameNonceSize      = 12
)

// frame 加密和重放保护错误。
var (
	ErrFrameEncryptionKeyInvalid = errors.New("protocol frame encryption key is invalid")
	ErrFrameEncryptedPayload     = errors.New("protocol frame encrypted payload is invalid")
	ErrFrameNonceReplay          = errors.New("protocol frame nonce replay detected")
	ErrFrameReplayGuardFull      = errors.New("protocol frame replay guard is full")
)

// DeriveFrameSessionKey 从主密钥和会话 ID 派生 frame 会话密钥。
func DeriveFrameSessionKey(masterSecret, sessionID string) ([]byte, error) {
	if masterSecret == "" || sessionID == "" {
		return nil, ErrFrameEncryptionKeyInvalid
	}
	key, err := hkdf.Key(sha256.New, []byte(masterSecret), []byte(sessionID), "longheng:frame-session-key:v1", FrameSessionKeySize)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// EncryptFrame 加密 frame body，并把 nonce 放入密文前缀。
func EncryptFrame(frame Frame, key []byte, random io.Reader) (Frame, error) {
	if frame.Version == 0 {
		// 加密 AAD 必须与最终写出的帧头一致；WriteFrame 会把 0 规范成 FrameVersion1。
		frame.Version = FrameVersion1
	}
	if frame.Version != FrameVersion1 {
		return Frame{}, fmt.Errorf("%w: %d", ErrInvalidFrameVersion, frame.Version)
	}
	aead, err := frameAEAD(key)
	if err != nil {
		return Frame{}, err
	}
	if random == nil {
		random = rand.Reader
	}
	nonce := make([]byte, FrameNonceSize)
	binary.BigEndian.PutUint64(nonce[:8], frame.Sequence)
	if _, err := io.ReadFull(random, nonce[8:]); err != nil {
		return Frame{}, err
	}
	out := frame
	out.Flags |= FrameFlagEncrypted
	aad, err := frameAAD(frame)
	if err != nil {
		return Frame{}, err
	}
	out.Body = aead.Seal(nonce, nonce, frame.Body, aad)
	out.Checksum = 0
	return out, nil
}

// WriteEncryptedFrame 加密后写出 frame_v1。
func WriteEncryptedFrame(w io.Writer, frame Frame, key []byte, random io.Reader) error {
	encrypted, err := EncryptFrame(frame, key, random)
	if err != nil {
		return err
	}
	return WriteFrame(w, encrypted)
}

// DecryptFrame 解密 frame body 并执行可选 nonce 重放检查。
func DecryptFrame(frame Frame, key []byte, replay *FrameReplayGuard) (Frame, error) {
	if frame.Flags&FrameFlagEncrypted == 0 {
		return frame, nil
	}
	aead, err := frameAEAD(key)
	if err != nil {
		return Frame{}, err
	}
	if len(frame.Body) < FrameNonceSize+aead.Overhead() {
		return Frame{}, ErrFrameEncryptedPayload
	}
	nonce := append([]byte(nil), frame.Body[:FrameNonceSize]...)
	aad, err := frameAAD(frame)
	if err != nil {
		return Frame{}, err
	}
	plain, err := aead.Open(nil, nonce, frame.Body[FrameNonceSize:], aad)
	if err != nil {
		return Frame{}, fmt.Errorf("%w: %w", ErrFrameEncryptedPayload, err)
	}
	if replay != nil {
		if err := replay.Seen(nonce); err != nil {
			return Frame{}, err
		}
	}
	out := frame
	out.Flags &^= FrameFlagEncrypted
	out.Body = plain
	out.Checksum = 0
	return out, nil
}

// ReadEncryptedFrame 读取 frame_v1 后立即解密。
func ReadEncryptedFrame(r io.Reader, maxBodySize uint32, key []byte, replay *FrameReplayGuard) (Frame, error) {
	frame, err := ReadFrame(r, maxBodySize)
	if err != nil {
		return Frame{}, err
	}
	return DecryptFrame(frame, key, replay)
}

// EncryptEnvelope 把 envelope 按 frame_v1 规则加密。
func EncryptEnvelope(envelope Envelope, key []byte, random io.Reader) (Envelope, error) {
	if NormalizeWireFormat(envelope.Format) != WireFormatFrameV1 {
		return Envelope{}, ErrInvalidWireFormat
	}
	frame := Frame{
		Version:  envelope.Frame.Version,
		Flags:    envelope.Frame.Flags,
		Codec:    envelope.Frame.Codec,
		PacketID: envelope.Packet.PacketID,
		MsgID:    envelope.Packet.MsgID,
		Sequence: envelope.Frame.Sequence,
		Body:     append([]byte(nil), envelope.Packet.Body...),
		Metadata: CloneFrameMetadata(envelope.Frame.Metadata),
	}
	if envelope.Packet.Compressed {
		frame.Flags |= FrameFlagCompressed
	}
	encrypted, err := EncryptFrame(frame, key, random)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Format: WireFormatFrameV1,
		Packet: packetFromFrame(encrypted),
		Frame:  encrypted,
	}, nil
}

// DecryptEnvelope 解密 frame_v1 envelope，旧 packet envelope 直接透传。
func DecryptEnvelope(envelope Envelope, key []byte, replay *FrameReplayGuard) (Envelope, error) {
	switch NormalizeWireFormat(envelope.Format) {
	case WireFormatPacket:
		return envelope, nil
	case WireFormatFrameV1:
	default:
		return Envelope{}, ErrInvalidWireFormat
	}
	if envelope.Frame.Flags&FrameFlagEncrypted == 0 {
		return envelope, nil
	}
	decrypted, err := DecryptFrame(envelope.Frame, key, replay)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Format: WireFormatFrameV1,
		Packet: packetFromFrame(decrypted),
		Frame:  decrypted,
	}, nil
}

// FrameReplayGuard 在有界序号窗口内精确记录 nonce，防止同一会话内加密帧重放。
//
// nonce 前 8 字节是大端 frame sequence，后 4 字节是随机后缀。同一 sequence
// 可能因响应和 push 编号空间重叠而出现不同 nonce，因此窗口内按完整 nonce
// 去重；一旦 sequence 滑出窗口，其任何 nonce 都永久按重放拒绝。
type FrameReplayGuard struct {
	mu          sync.Mutex
	max         int
	initialized bool
	highest     uint64
	total       int
	seen        map[[FrameNonceSize]byte]struct{}
	slots       []frameReplaySlot
}

type frameReplaySlot struct {
	sequence uint64
	active   bool
	nonces   [][FrameNonceSize]byte
}

// NewFrameReplayGuard 创建固定序号窗口和 nonce 容量的重放保护器。
func NewFrameReplayGuard(maxEntries int) *FrameReplayGuard {
	if maxEntries <= 0 {
		maxEntries = 4096
	}
	return &FrameReplayGuard{
		max:   maxEntries,
		seen:  make(map[[FrameNonceSize]byte]struct{}, maxEntries),
		slots: make([]frameReplaySlot, maxEntries),
	}
}

// Seen 记录 nonce；重复 nonce 或已滑出窗口的旧序号会返回重放错误。
func (g *FrameReplayGuard) Seen(nonce []byte) error {
	if g == nil {
		return ErrFrameEncryptedPayload
	}
	if len(nonce) != FrameNonceSize {
		return ErrFrameEncryptedPayload
	}
	var key [FrameNonceSize]byte
	copy(key[:], nonce)
	sequence := binary.BigEndian.Uint64(key[:8])
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureReady()
	window := replayWindow(g.max)

	if g.initialized && sequence <= g.highest && g.highest-sequence >= window {
		return ErrFrameNonceReplay
	}
	if !g.initialized {
		g.initialized = true
		g.highest = sequence
	} else if sequence > g.highest {
		g.advance(sequence)
	}
	if _, ok := g.seen[key]; ok {
		return ErrFrameNonceReplay
	}
	if g.total >= g.max {
		return ErrFrameReplayGuardFull
	}

	slot := &g.slots[sequence%window]
	if slot.active && slot.sequence != sequence {
		g.clearSlot(slot)
	}
	if !slot.active {
		slot.sequence = sequence
		slot.active = true
	}
	slot.nonces = append(slot.nonces, key)
	g.seen[key] = struct{}{}
	g.total++
	return nil
}

func (g *FrameReplayGuard) ensureReady() {
	if g.max <= 0 {
		g.max = 4096
	}
	if g.seen == nil {
		g.seen = make(map[[FrameNonceSize]byte]struct{}, g.max)
	}
	if len(g.slots) != g.max {
		g.slots = make([]frameReplaySlot, g.max)
		g.seen = make(map[[FrameNonceSize]byte]struct{}, g.max)
		g.initialized = false
		g.highest = 0
		g.total = 0
	}
}

func (g *FrameReplayGuard) advance(sequence uint64) {
	delta := sequence - g.highest
	window := replayWindow(g.max)
	if delta >= window {
		g.slots = make([]frameReplaySlot, g.max)
		g.seen = make(map[[FrameNonceSize]byte]struct{}, g.max)
		g.total = 0
		g.highest = sequence
		return
	}
	start := g.highest % window
	for step := uint64(1); step <= delta; step++ {
		g.clearSlot(&g.slots[(start+step)%window])
	}
	g.highest = sequence
}

func replayWindow(maxEntries int) uint64 {
	if maxEntries <= 0 {
		return 4096
	}
	// int 的正值范围始终可由 uint64 表示；这里集中转换，避免窗口运算散落窄化风险。
	return uint64(maxEntries) // #nosec G115 -- maxEntries 已验证为正数，转换不会溢出。
}

func (g *FrameReplayGuard) clearSlot(slot *frameReplaySlot) {
	if slot == nil || !slot.active {
		return
	}
	for _, nonce := range slot.nonces {
		delete(g.seen, nonce)
		g.total--
	}
	if g.total < 0 {
		g.total = 0
	}
	*slot = frameReplaySlot{}
}

func frameAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != FrameSessionKeySize {
		return nil, ErrFrameEncryptionKeyInvalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func frameAAD(frame Frame) ([]byte, error) {
	var aad [21]byte
	aad[0] = frame.Version
	binary.BigEndian.PutUint16(aad[1:3], frame.Flags&^FrameFlagEncrypted)
	binary.BigEndian.PutUint16(aad[3:5], frame.Codec)
	binary.BigEndian.PutUint32(aad[5:9], packetIDToWire(frame.PacketID))
	binary.BigEndian.PutUint32(aad[9:13], frame.MsgID)
	binary.BigEndian.PutUint64(aad[13:21], frame.Sequence)
	metadata, err := encodeFrameMeta(frame.Metadata)
	if err != nil {
		return nil, err
	}
	if len(metadata) == 0 {
		return aad[:], nil
	}
	out := make([]byte, 0, len(aad)+len(metadata))
	out = append(out, aad[:]...)
	out = append(out, metadata...)
	return out, nil
}
