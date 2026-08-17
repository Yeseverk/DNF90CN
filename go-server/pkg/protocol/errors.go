package protocol

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// ErrorCode 是协议层向客户端暴露的稳定错误码。
type ErrorCode int32

// 协议错误码分组保留 HTTP 语义，并扩展框架内部错误。
const (
	CodeOK             ErrorCode = 0
	CodeBadRequest     ErrorCode = 400
	CodeUnauthorized   ErrorCode = 401
	CodeNotFound       ErrorCode = 404
	CodeConflict       ErrorCode = 409
	CodeInternal       ErrorCode = 500
	CodeUnavailable    ErrorCode = 503
	CodeNotImplemented ErrorCode = 1001
)

// AppError 是服务内部携带错误码、公开消息和调试明细的错误。
type AppError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Detail  string    `json:"detail,omitempty"`
}

// ErrorResponse 是发送给客户端的错误响应体。
type ErrorResponse struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	Detail     string    `json:"detail,omitempty"`
	ServerTime int64     `json:"server_time"`
}

// NewError 构造带协议错误码的应用错误。
func NewError(code ErrorCode, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Errorf 使用格式化消息构造应用错误。
func Errorf(code ErrorCode, format string, args ...any) *AppError {
	return NewError(code, fmt.Sprintf(format, args...))
}

// WrapError 把底层错误明细保存在应用错误里。
func WrapError(code ErrorCode, message string, err error) *AppError {
	if err == nil {
		return NewError(code, message)
	}
	return &AppError{Code: code, Message: message, Detail: err.Error()}
}

// ErrorFrom 把任意 error 归一成 AppError。
func ErrorFrom(err error) AppError {
	if err == nil {
		return AppError{Code: CodeOK}
	}
	var appErr *AppError
	if errors.As(err, &appErr) && appErr != nil {
		return *appErr
	}
	return AppError{
		Code:    CodeInternal,
		Message: "internal server error",
		Detail:  err.Error(),
	}
}

// PublicErrorResponse 生成可安全返回给客户端的错误响应。
func PublicErrorResponse(err AppError, serverTime int64) ErrorResponse {
	resp := ErrorResponse{
		Code:       err.Code,
		Message:    err.Message,
		Detail:     err.Detail,
		ServerTime: serverTime,
	}
	if resp.Message == "" {
		resp.Message = err.Code.String()
	}
	switch err.Code {
	case CodeInternal:
		resp.Message = "internal server error"
	case CodeUnavailable:
		resp.Message = "service unavailable"
	case CodeUnauthorized:
		resp.Message = "unauthorized"
	}
	switch err.Code {
	case CodeBadRequest:
	default:
		resp.Detail = ""
	}
	return resp
}

// Error 返回应用错误的日志友好文本。
func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Message, e.Detail)
}

// String 返回错误码的协议文本。
func (c ErrorCode) String() string {
	switch c {
	case CodeOK:
		return "OK"
	case CodeBadRequest:
		return "BAD_REQUEST"
	case CodeUnauthorized:
		return "UNAUTHORIZED"
	case CodeNotFound:
		return "NOT_FOUND"
	case CodeConflict:
		return "CONFLICT"
	case CodeInternal:
		return "INTERNAL"
	case CodeUnavailable:
		return "UNAVAILABLE"
	case CodeNotImplemented:
		return "NOT_IMPLEMENTED"
	default:
		return fmt.Sprintf("ERROR_%d", c)
	}
}

// EncodeErrorResponse 把错误响应编码为轻量 protobuf wire body。
func EncodeErrorResponse(resp ErrorResponse) []byte {
	body := make([]byte, 0, 96)
	body = appendI32VarintField(body, 1, int32(resp.Code))
	body = appendStringField(body, 2, resp.Message)
	body = appendStringField(body, 3, resp.Detail)
	body = appendInt64Varint(body, 4, resp.ServerTime)
	return body
}

// DecodeErrorResponse 从 protobuf wire body 解码错误响应。
func DecodeErrorResponse(body []byte) ErrorResponse {
	var resp ErrorResponse
	for len(body) > 0 {
		fieldNumber, typ, consumed := protowire.ConsumeTag(body)
		if consumed < 0 {
			return resp
		}
		body = body[consumed:]

		switch {
		case fieldNumber == 1 && typ == protowire.VarintType:
			value, n := protowire.ConsumeVarint(body)
			if n < 0 {
				return resp
			}
			resp.Code = ErrorCode(int32FromVarint(value))
			body = body[n:]
		case fieldNumber == 2 && typ == protowire.BytesType:
			value, n := protowire.ConsumeString(body)
			if n < 0 {
				return resp
			}
			resp.Message = value
			body = body[n:]
		case fieldNumber == 3 && typ == protowire.BytesType:
			value, n := protowire.ConsumeString(body)
			if n < 0 {
				return resp
			}
			resp.Detail = value
			body = body[n:]
		case fieldNumber == 4 && typ == protowire.VarintType:
			value, n := protowire.ConsumeVarint(body)
			if n < 0 {
				return resp
			}
			resp.ServerTime = int64FromVarint(value)
			body = body[n:]
		default:
			n := protowire.ConsumeFieldValue(fieldNumber, typ, body)
			if n < 0 {
				return resp
			}
			body = body[n:]
		}
	}
	return resp
}
