package protocol

import (
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// 示例游戏协议用于在通用蓝图中保留示例消息 ID 和响应流程，
// 避免继承旧项目生成的 protobuf 类型。真实游戏应替换 configs/protocol/game.json 并重新生成。

//go:generate go run ../../cmd/protogen -schema ../../configs/protocol/game.json -out game_schema_gen.go

// 示例 packet id 保留最小登录、请求响应、心跳和聊天流程。
const (
	PacketIDContent  = int32(0)
	PacketIDReqResp  = int32(1)
	PacketIDPingPong = int32(2)
	PacketIDChat     = int32(5)
)

// OnConnectionRequest 是示例 on_connection 请求的轻量解码结果。
type OnConnectionRequest struct {
	RawBody       []byte
	PassthroughID string
}

// OnConnectionResponse 是示例 on_connection 响应。
type OnConnectionResponse struct {
	PassthroughID string
	HasRole       bool
	ShardID       int32
	ServerTime    int64
	CurBundleVer  string
}

// DecodeOnConnectionRequest 从示例请求体提取 passthrough id。
func DecodeOnConnectionRequest(body []byte) OnConnectionRequest {
	passthroughID := extractPassID(body)
	return OnConnectionRequest{
		RawBody:       append([]byte(nil), body...),
		PassthroughID: passthroughID,
	}
}

// EncodeOnConnectionResponse 编码示例 on_connection 响应。
func EncodeOnConnectionResponse(resp OnConnectionResponse) []byte {
	respBody := make([]byte, 0, 64)
	respBody = appendStringField(respBody, 1, resp.PassthroughID)
	respBody = appendVarintField(respBody, 3, 0)
	respBody = appendVarintField(respBody, 7, 0)
	respBody = appendInt64Varint(respBody, 12, resp.ServerTime)

	body := make([]byte, 0, 96)
	body = appendBytesField(body, 1, respBody)
	body = appendBoolField(body, 2, resp.HasRole)
	body = appendI32VarintField(body, 3, resp.ShardID)
	body = appendStringField(body, 4, resp.CurBundleVer)
	return body
}

func appendTag(dst []byte, fieldNumber int, typ protowire.Type) []byte {
	if fieldNumber <= 0 {
		return dst
	}
	return protowire.AppendTag(dst, protowire.Number(fieldNumber), typ) //nolint:gosec // G115：协议字段号由 schema/generator 固定为正数。
}

func appendVarintField(dst []byte, fieldNumber int, value uint64) []byte {
	dst = appendTag(dst, fieldNumber, protowire.VarintType)
	return protowire.AppendVarint(dst, value)
}

func appendIntVarintField(dst []byte, fieldNumber int, value int) []byte {
	return appendInt64Varint(dst, fieldNumber, int64(value))
}

func appendI32VarintField(dst []byte, fieldNumber int, value int32) []byte {
	return appendInt64Varint(dst, fieldNumber, int64(value))
}

func appendInt64Varint(dst []byte, fieldNumber int, value int64) []byte {
	// protobuf varint 对 signed int 兼容旧协议的补码写法，不能改成 zigzag。
	return appendVarintField(dst, fieldNumber, uint64(value)) //nolint:gosec // G115：协议线格式要求保留 int64 的补码位模式。
}

func intFromVarint(value uint64) int {
	signed := int64FromVarint(value)
	if signed > int64(math.MaxInt) {
		return math.MaxInt
	}
	if signed < int64(math.MinInt) {
		return math.MinInt
	}
	return int(signed) //nolint:gosec // G115：前面已按当前平台 int 范围钳制。
}

func int32FromVarint(value uint64) int32 {
	signed := int64FromVarint(value)
	if signed > math.MaxInt32 {
		return math.MaxInt32
	}
	if signed < math.MinInt32 {
		return math.MinInt32
	}
	return int32(signed) //nolint:gosec // G115：前面已按 int32 范围钳制。
}

func int64FromVarint(value uint64) int64 {
	return int64(value) //nolint:gosec // G115：协议线格式要求按补码恢复 int64。
}

func appendBoolField(dst []byte, fieldNumber int, value bool) []byte {
	if value {
		return appendVarintField(dst, fieldNumber, 1)
	}
	return appendVarintField(dst, fieldNumber, 0)
}

func appendStringField(dst []byte, fieldNumber int, value string) []byte {
	dst = appendTag(dst, fieldNumber, protowire.BytesType)
	return protowire.AppendString(dst, value)
}

func appendBytesField(dst []byte, fieldNumber int, value []byte) []byte {
	dst = appendTag(dst, fieldNumber, protowire.BytesType)
	return protowire.AppendBytes(dst, value)
}

func extractPassID(body []byte) string {
	for len(body) > 0 {
		fieldNumber, typ, consumed := protowire.ConsumeTag(body)
		if consumed < 0 {
			return ""
		}
		body = body[consumed:]

		switch {
		case fieldNumber == 1 && typ == protowire.BytesType:
			reqBytes, n := protowire.ConsumeBytes(body)
			if n < 0 {
				return ""
			}
			return extractPassthroughID(reqBytes)
		default:
			n := protowire.ConsumeFieldValue(fieldNumber, typ, body)
			if n < 0 {
				return ""
			}
			body = body[n:]
		}
	}
	return ""
}

func extractPassthroughID(body []byte) string {
	for len(body) > 0 {
		fieldNumber, typ, consumed := protowire.ConsumeTag(body)
		if consumed < 0 {
			return ""
		}
		body = body[consumed:]

		switch {
		case fieldNumber == 1 && typ == protowire.BytesType:
			value, n := protowire.ConsumeString(body)
			if n < 0 {
				return ""
			}
			return value
		default:
			n := protowire.ConsumeFieldValue(fieldNumber, typ, body)
			if n < 0 {
				return ""
			}
			body = body[n:]
		}
	}
	return ""
}
