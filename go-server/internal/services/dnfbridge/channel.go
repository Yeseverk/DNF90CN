// channel.go 负责 DNF bridge 频道服 7001 的握手、兼容包解析和频道目录响应。
package dnfbridge

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

type channelSession struct {
	conn                         net.Conn
	connID                       string
	packetSeq                    uint64
	seq                          uint16
	scriptSent                   bool
	closeAfterBootstrapDirectory bool
}

const channelReadIdleTimeout = 8 * time.Second

func (s *Service) handleChannelConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	session := &channelSession{conn: conn, connID: s.nextPacketConnID("channel")}
	reader := bufio.NewReader(conn)
	s.logInfo("dnfbridge channel connection accepted",
		"conn_id", session.connID,
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String())
	s.logPacketEvent("channel-connect",
		"conn_id", session.connID,
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String())
	defer s.logPacketEvent("channel-close",
		"conn_id", session.connID,
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String())
	setChannelReadDeadline(conn)
	if err := s.acceptDofLoginPreface(reader, session); err != nil {
		s.logChannelReadError(ctx, conn, err)
		return
	}
	for {
		setChannelReadDeadline(conn)
		handled, err := s.handleLegacyChannelRequest(reader, session)
		if err != nil {
			s.logChannelReadError(ctx, conn, err)
			return
		}
		if handled {
			if session.closeAfterBootstrapDirectory {
				s.logPacketEvent("channel-bootstrap-directory-committed",
					"conn_id", session.connID,
					"remote", session.conn.RemoteAddr().String())
				return
			}
			continue
		}
		setChannelReadDeadline(conn)
		packet, err := s.readChannelPacket(reader, session)
		if err != nil {
			s.logChannelReadError(ctx, conn, err)
			return
		}
		if err := s.handleChannelPacket(session, packet); err != nil {
			s.logWarn("dnfbridge handle channel packet failed", "msg_id", packet.Header.MsgID, "error", err)
			return
		}
	}
}

func setChannelReadDeadline(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(channelReadIdleTimeout))
}

func (s *Service) acceptDofLoginPreface(reader *bufio.Reader, session *channelSession) error {
	header, err := reader.Peek(dnfproto.LegacyChannelHeaderSize)
	if err != nil {
		return err
	}
	if !dnfproto.IsDofLoginPrefacePrefix(header) {
		return nil
	}
	raw := make([]byte, dnfproto.DofLoginPrefaceSize)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return err
	}
	s.logChannelPacket(session, "RECV", "channel-dof-preface", raw)
	ack := dnfproto.BuildDofLoginAck(s.aesKey())
	s.logChannelPacket(session, "SEND", "channel-dof-ack", ack)
	_, err = session.conn.Write(ack)
	return err
}

func (s *Service) handleLegacyChannelRequest(reader *bufio.Reader, session *channelSession) (bool, error) {
	header, err := reader.Peek(dnfproto.LegacyChannelHeaderSize)
	if err != nil {
		return false, err
	}
	if dnfproto.IsDofChannelProbePrefix(header) {
		raw := make([]byte, dnfproto.LegacyChannelHeaderSize)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return false, err
		}
		s.logChannelPacket(session, "RECV", "channel-dof-get-script", raw)
		return true, s.sendLegacyScript(session)
	}
	if !dnfproto.IsDofAskChannelPrefix(header) {
		return false, nil
	}
	raw := make([]byte, dnfproto.DofAskChannelSize)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return false, err
	}
	s.logChannelPacket(session, "RECV", "channel-dof-ask", raw)
	bootstrap := session.scriptSent
	body, err := s.buildChannelInfoBodyFor(dnfproto.LegacyPayload(raw), bootstrap)
	if err != nil {
		return true, err
	}
	packet := dnfproto.BuildLegacyChannelPacket(dnfenum.LegacyMsgChannelInfo, body)
	s.logChannelPacket(session, "SEND", "channel-dof-info", packet, "msg_id", dnfenum.LegacyMsgChannelInfo)
	if _, err := session.conn.Write(packet); err != nil {
		return true, err
	}
	s.logPacketEvent("channel-dof-info-sent",
		"conn_id", session.connID,
		"remote", session.conn.RemoteAddr().String(),
		"local", session.conn.LocalAddr().String(),
		"bootstrap", bootstrap)
	session.closeAfterBootstrapDirectory = bootstrap
	return true, nil
}

func (s *Service) sendLegacyScript(session *channelSession) error {
	body, err := s.buildScriptBody()
	if err != nil {
		return err
	}
	packet := dnfproto.BuildLegacyChannelPacket(dnfenum.LegacyMsgDofScript, body)
	s.logChannelPacket(session, "SEND", "channel-dof-script", packet, "msg_id", dnfenum.LegacyMsgDofScript)
	if _, err := session.conn.Write(packet); err != nil {
		return err
	}
	session.scriptSent = true
	return nil
}

func (s *Service) readChannelPacket(reader io.Reader, session *channelSession) (dnfproto.ChannelPacket, error) {
	header := make([]byte, dnfproto.ChannelHeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return dnfproto.ChannelPacket{}, err
	}
	if packet, ok, err := s.tryReadMixedLegacyChannelPacket(reader, header, session); ok || err != nil {
		return packet, err
	}
	length := binary.LittleEndian.Uint32(header[3:7])
	maxPacketBytes := s.options.maxPacketBytes
	if maxPacketBytes <= 0 {
		maxPacketBytes = defaultMaxPacketBytes
	}
	if length < dnfproto.ChannelHeaderSize || int(length) > maxPacketBytes {
		s.logChannelPacket(session, "RECV", "channel-invalid-header", header)
		return dnfproto.ChannelPacket{}, dnfproto.ErrPacketLength
	}
	packet := make([]byte, int(length))
	copy(packet, header)
	if _, err := io.ReadFull(reader, packet[dnfproto.ChannelHeaderSize:]); err != nil {
		return dnfproto.ChannelPacket{}, err
	}
	parsed, err := dnfproto.ParseChannelPacket(packet)
	if err != nil {
		s.logChannelPacket(session, "RECV", "channel-invalid", packet)
		return dnfproto.ChannelPacket{}, err
	}
	s.logChannelPacket(session, "RECV", "channel", packet,
		"msg_id", parsed.Header.MsgID,
		"header_seq", parsed.Header.Seq,
		"body_len", len(parsed.Body))
	return parsed, nil
}

func (s *Service) tryReadMixedLegacyChannelPacket(reader io.Reader, header []byte, session *channelSession) (dnfproto.ChannelPacket, bool, error) {
	if len(header) < dnfproto.ChannelHeaderSize || header[0] == dnfproto.DefaultChannelClassification {
		return dnfproto.ChannelPacket{}, false, nil
	}
	bodyLen := int(binary.BigEndian.Uint16(header[0:2]))
	msgID := binary.LittleEndian.Uint16(header[2:4])
	total := 4 + bodyLen
	if !isMixedLegacyChannelMsg(msgID) || total < dnfproto.ChannelHeaderSize {
		return dnfproto.ChannelPacket{}, false, nil
	}
	maxPacketBytes := s.options.maxPacketBytes
	if maxPacketBytes <= 0 {
		maxPacketBytes = defaultMaxPacketBytes
	}
	if total > maxPacketBytes {
		s.logChannelPacket(session, "RECV", "channel-mixed-legacy-invalid", header, "msg_id", msgID)
		return dnfproto.ChannelPacket{}, true, dnfproto.ErrPacketLength
	}
	raw := make([]byte, total)
	copy(raw, header)
	if total > dnfproto.ChannelHeaderSize {
		if _, err := io.ReadFull(reader, raw[dnfproto.ChannelHeaderSize:]); err != nil {
			return dnfproto.ChannelPacket{}, true, err
		}
	}
	body := append([]byte(nil), raw[4:]...)
	s.logChannelPacket(session, "RECV", "channel-mixed-legacy", raw,
		"msg_id", msgID,
		"body_len", len(body))
	return dnfproto.ChannelPacket{
		Header: dnfproto.ChannelHeader{
			Classification: dnfproto.DefaultChannelClassification,
			MsgID:          msgID,
			Length:         uint32(total),
			Seq:            mixedLegacySequence(body),
		},
		Body: body,
	}, true, nil
}

func isMixedLegacyChannelMsg(msgID uint16) bool {
	switch dnfenum.ChannelMsg(msgID) {
	case dnfenum.MsgCSConnect, dnfenum.MsgCSAskChannelInfoNew:
		return true
	default:
		return false
	}
}

func mixedLegacySequence(body []byte) uint16 {
	for _, value := range body {
		if value != 0 {
			return uint16(value)
		}
	}
	return 1
}

func (s *Service) logChannelReadError(ctx context.Context, conn net.Conn, err error) {
	if !isQuietChannelReadError(err) && ctx.Err() == nil {
		s.logWarn("dnfbridge read channel packet failed", "remote", conn.RemoteAddr().String(), "error", err)
	}
}

func isQuietChannelReadError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET)
}

func (s *Service) handleChannelPacket(session *channelSession, packet dnfproto.ChannelPacket) error {
	switch dnfenum.ChannelMsg(packet.Header.MsgID) {
	case dnfenum.MsgCSUpdateChannelInfo:
		body, err := s.buildChannelUpdateBody(packet.Body)
		if err != nil {
			return err
		}
		return s.sendChannelClass(session, uint16(dnfenum.MsgSCAskChannelInfo), body, packet.Header.Classification)
	case dnfenum.MsgCSConnect:
		return s.sendChannel(session, uint16(dnfenum.MsgSCConnect), s.buildConnectBody())
	case dnfenum.MsgCSCheckScript:
		body, err := s.buildScriptVersionBody()
		if err != nil {
			return err
		}
		return s.sendChannel(session, uint16(dnfenum.MsgSCCheckScript), body)
	case dnfenum.MsgCSGetScript:
		return s.sendChannelScript(session)
	case dnfenum.MsgCSAskChannelInfoNew:
		if !session.scriptSent {
			if err := s.sendChannelScript(session); err != nil {
				return err
			}
		}
		body, err := s.buildChannelInfoBodyFor(packet.Body, true)
		if err != nil {
			return err
		}
		return s.sendChannel(session, uint16(dnfenum.MsgSCAskChannelInfoNew), body)
	default:
		return nil
	}
}

// sendChannelScript 用最新频道包下发 channel_info.etc，只影响客户端频道静态表加载，不写玩家状态。
func (s *Service) sendChannelScript(session *channelSession) error {
	body, err := s.buildScriptBody()
	if err != nil {
		return err
	}
	if err := s.sendChannel(session, uint16(dnfenum.MsgSCGetScript), body); err != nil {
		return err
	}
	session.scriptSent = true
	return nil
}

func (s *Service) sendChannel(session *channelSession, msgID uint16, body []byte) error {
	return s.sendChannelClass(session, msgID, body, dnfproto.DefaultChannelClassification)
}

func (s *Service) sendChannelClass(session *channelSession, msgID uint16, body []byte, class byte) error {
	packet, err := dnfproto.BuildChannelPacket(msgID, body, session.seq, dnfproto.DefaultChannelClassification)
	if err != nil {
		return err
	}
	if class != 0 && class != dnfproto.DefaultChannelClassification {
		packet, err = dnfproto.BuildChannelPacket(msgID, body, session.seq, class)
		if err != nil {
			return err
		}
	}
	session.seq++
	s.logChannelPacket(session, "SEND", "channel", packet,
		"msg_id", msgID,
		"header_seq", session.seq-1,
		"body_len", len(body))
	_, err = session.conn.Write(packet)
	return err
}

func (s *Service) logChannelPacket(session *channelSession, direction string, kind string, data []byte, fields ...any) {
	if session == nil {
		s.logPacket(direction, kind, data, fields...)
		return
	}
	session.packetSeq++
	args := make([]any, 0, len(fields)+4)
	args = append(args, "conn_id", session.connID, "pkt_seq", session.packetSeq)
	args = append(args, fields...)
	s.logPacket(direction, kind, data, args...)
}

func (s *Service) buildConnectBody() []byte {
	key := s.aesKey()
	body := make([]byte, 36)
	copy(body[4:], []byte(key))
	return body
}

func (s *Service) buildScriptVersionBody() ([]byte, error) {
	raw := make([]byte, 20)
	copy(raw[4:], []byte(s.options.scriptVersion))
	return encryptChannelData(raw, s.aesKey(), false)
}

func (s *Service) buildScriptBody() ([]byte, error) {
	script := s.currentChannelScript()
	if len(script) == 0 {
		raw, err := osReadFile(s.options.channelInfoFile)
		if err != nil {
			return nil, err
		}
		script, err = s.buildChannelScript(raw)
		if err != nil {
			return nil, fmt.Errorf("build 90CN online channel script: %w", err)
		}
	}
	return encryptChannelData(script, s.aesKey(), true)
}

func (s *Service) buildChannelUpdateBody(_ []byte) ([]byte, error) {
	channel, err := s.loginChannel()
	if err != nil {
		return nil, err
	}
	var writer packetWriter
	writer.writeUint32(0)
	writer.buffer.Write(fixedASCII(s.options.serverIP, 16))
	writer.writeInt32(channel.Port)
	writer.writeInt32(channel.MaxUsers)
	writer.writeInt32(channel.CurrentUsers)
	return writer.bytes(), nil
}

func (s *Service) loginChannel() (channelcatalog.Channel, error) {
	catalog := s.currentCatalog()
	if catalog == nil {
		return channelcatalog.Channel{}, channelcatalog.ErrEmpty
	}
	if channel, ok := catalog.Channel(dnfenum.DefaultGameChannelID); ok {
		return channel, nil
	}
	return channelcatalog.Channel{}, fmt.Errorf(
		"%w: resident channel %d is missing",
		channelcatalog.ErrEmpty,
		dnfenum.DefaultGameChannelID,
	)
}

func (s *Service) buildChannelInfoBody(request []byte) ([]byte, error) {
	return s.buildChannelInfoBodyFor(request, false)
}

func (s *Service) buildChannelInfoBodyFor(request []byte, bootstrap bool) ([]byte, error) {
	catalog := s.currentCatalog()
	if catalog == nil {
		return nil, channelcatalog.ErrEmpty
	}
	group := readNullASCII(request, 0, 20)
	channels := s.channelsForRequestMode(group, bootstrap)
	firstChannel := 0
	lastChannel := 0
	if len(channels) > 0 {
		firstChannel = channels[0].ID
		lastChannel = channels[len(channels)-1].ID
	}
	s.logPacketEvent("channel-advertise",
		"requested_group", group,
		"source_server", s.channelServerID(),
		"advertise_server", s.channelAdvertiseID(),
		"body_mode", s.channelInfoBodyMode(),
		"bootstrap", bootstrap,
		"count", len(channels),
		"first_channel", firstChannel,
		"last_channel", lastChannel)
	var writer packetWriter
	s.writeChannelInfoPrefix(&writer, len(channels))
	s.writeChannelInfoEntries(&writer, channels)
	return encryptChannelData(writer.bytes(), s.aesKey(), true)
}

func (s *Service) writeChannelInfoPrefix(writer *packetWriter, count int) {
	// IDA/MCP 复核 sub_1CA2440：最新客户端按 server_count/server_id/channel_count 解析 0x12 明文头。
	// 旧 C# 的 2 字节保留位前缀已确认会把客户端解析游标带偏，不能再作为当前桥接输出。
	writer.writeUint32(1)
	writer.writeUint32(uint32(s.channelAdvertiseID()))
	writer.writeUint32(uint32(count))
}

func (s *Service) writeChannelInfoEntries(writer *packetWriter, channels []channelcatalog.Channel) {
	for _, channel := range channels {
		writer.buffer.Write(fixedASCII("#"+channel.Name, 20))
		writer.writeInt32(channel.MaxUsers)
		writer.writeInt32(channel.CurrentUsers)
		writer.buffer.Write(fixedASCII(s.options.serverIP, 16))
		writer.writeInt32(channel.Port)
	}
}

func (s *Service) channelsForRequest(group string) []channelcatalog.Channel {
	return s.channelsForRequestMode(group, false)
}

func (s *Service) channelsForRequestMode(group string, bootstrap bool) []channelcatalog.Channel {
	catalog := s.currentCatalog()
	if catalog == nil {
		return nil
	}
	var channels []channelcatalog.Channel
	if bootstrap {
		channels = catalog.FilterForBootstrap(group)
	} else {
		channels = catalog.FilterForRequest(group)
	}
	if len(channels) == 0 && isDefaultChannelGroup(group) {
		if channel, ok := catalog.ForPort(dnfenum.GamePortBase + dnfenum.DefaultGameChannelID); ok {
			return []channelcatalog.Channel{channel}
		}
	}
	return channels
}

func isDefaultChannelGroup(group string) bool {
	group = strings.TrimSpace(strings.Trim(group, "`"))
	group = strings.TrimPrefix(group, "[")
	group = strings.TrimSuffix(group, "]")
	group = strings.ToLower(strings.TrimSpace(group))
	return group == "" || group == dnfenum.GroupCain
}

func readNullASCII(data []byte, offset int, size int) string {
	if offset >= len(data) || size <= 0 {
		return ""
	}
	limit := offset + size
	if limit > len(data) {
		limit = len(data)
	}
	end := offset
	for end < limit && data[end] != 0 {
		end++
	}
	return strings.TrimSpace(string(data[offset:end]))
}

func osReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自部署配置，DNF bridge 只读取 channel_info.etc 活配置。
	if err != nil {
		return nil, fmt.Errorf("read channel info script: %w", err)
	}
	return data, nil
}
