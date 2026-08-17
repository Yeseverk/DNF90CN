// 本文件解析角色选择链 C2S 请求体。
// 字段顺序参考 C# CharacterSelectHandler；当前只用于 fallback 日志和后续 owner 化计划。
package character

import (
	"encoding/binary"
	"fmt"
)

// SelectCharacterRequest 描述选择角色请求。
type SelectCharacterRequest struct {
	SlotOrCharacterID uint16
}

// CreateCharacterRequest 描述创建角色请求。
type CreateCharacterRequest struct {
	Job  byte
	Name string
}

// DeleteCharacterRequest 描述删除角色请求。
type DeleteCharacterRequest struct {
	Slot byte
	Name string
}

// CheckNameRequest 描述角色名查重请求。
type CheckNameRequest struct {
	Name string
}

// DecodeSelectCharacterRequest 解析 C# 选角请求的前 2 字节槽位。
func DecodeSelectCharacterRequest(body []byte) (SelectCharacterRequest, error) {
	if len(body) < 2 {
		return SelectCharacterRequest{}, fmt.Errorf("body too short: got %d want >= 2", len(body))
	}
	return SelectCharacterRequest{SlotOrCharacterID: binary.LittleEndian.Uint16(body)}, nil
}

// DecodeCreateCharacterRequest 解析创建角色请求：u8 job + i32 nameLen + name bytes。
func DecodeCreateCharacterRequest(body []byte) (CreateCharacterRequest, error) {
	if len(body) < 6 {
		return CreateCharacterRequest{}, fmt.Errorf("body too short: got %d want >= 6", len(body))
	}
	nameLen := int(int32(binary.LittleEndian.Uint32(body[1:])))
	if nameLen < 0 || 5+nameLen+1 > len(body) {
		return CreateCharacterRequest{}, fmt.Errorf("name length %d invalid for body %d", nameLen, len(body))
	}
	return CreateCharacterRequest{
		Job:  body[0],
		Name: string(body[5 : 5+nameLen]),
	}, nil
}

// DecodeDeleteCharacterRequest 解析删除角色请求：u8 slot + i32 nameLen + name bytes。
func DecodeDeleteCharacterRequest(body []byte) (DeleteCharacterRequest, error) {
	if len(body) < 6 {
		return DeleteCharacterRequest{}, fmt.Errorf("body too short: got %d want >= 6", len(body))
	}
	nameLen := int(int32(binary.LittleEndian.Uint32(body[1:])))
	if nameLen <= 0 || 5+nameLen > len(body) {
		return DeleteCharacterRequest{}, fmt.Errorf("name length %d invalid for body %d", nameLen, len(body))
	}
	return DeleteCharacterRequest{
		Slot: body[0],
		Name: string(body[5 : 5+nameLen]),
	}, nil
}

// DecodeCheckNameRequest 解析角色名查重请求：i32 nameLen + name bytes。
func DecodeCheckNameRequest(body []byte) (CheckNameRequest, error) {
	if len(body) < 5 {
		return CheckNameRequest{}, fmt.Errorf("body too short: got %d want >= 5", len(body))
	}
	nameLen := int(int32(binary.LittleEndian.Uint32(body)))
	if nameLen <= 0 || 4+nameLen > len(body) {
		return CheckNameRequest{}, fmt.Errorf("name length %d invalid for body %d", nameLen, len(body))
	}
	return CheckNameRequest{Name: string(body[4 : 4+nameLen])}, nil
}

func (r SelectCharacterRequest) String() string {
	return fmt.Sprintf("slotOrChar=%d", r.SlotOrCharacterID)
}

func (r CreateCharacterRequest) String() string {
	return fmt.Sprintf("job=%d name=%q", r.Job, r.Name)
}

func (r DeleteCharacterRequest) String() string {
	return fmt.Sprintf("slot=%d name=%q", r.Slot, r.Name)
}

func (r CheckNameRequest) String() string {
	return fmt.Sprintf("name=%q", r.Name)
}
