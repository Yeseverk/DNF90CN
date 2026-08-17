// 本文件解析装备锁 C2S 请求体。
// 请求体来自 C# TryParseEquipmentItemLockRequest：u8 listType + i16 slotIndex。
package itemlock

import (
	"encoding/binary"
	"fmt"
)

// Request 描述装备锁定、解锁和取消解锁共用请求。
type Request struct {
	ListType  byte
	SlotIndex int16
}

// DecodeRequest 解析装备锁请求。
func DecodeRequest(body []byte) (Request, error) {
	if len(body) < 3 {
		return Request{}, fmt.Errorf("body too short: got %d want >= 3", len(body))
	}
	return Request{
		ListType:  body[0],
		SlotIndex: int16(binary.LittleEndian.Uint16(body[1:])),
	}, nil
}

func (r Request) String() string {
	return fmt.Sprintf("list=%d slot=%d", r.ListType, r.SlotIndex)
}
