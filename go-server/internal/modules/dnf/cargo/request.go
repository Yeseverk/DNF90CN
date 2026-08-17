// 本文件解析账号仓库类 C2S 请求体。
// 解析结果只用于生成后续 owner 命令计划，不直接修改金币或仓库状态。
package cargo

import (
	"encoding/binary"
	"fmt"
)

// GoldRequest 描述仓库存取金币请求。
type GoldRequest struct {
	Amount int32
}

// DecodeGoldRequest 解析存入/取出金币请求，C# 证据为 body[0:4] little-endian int32。
func DecodeGoldRequest(body []byte) (GoldRequest, error) {
	if len(body) < 4 {
		return GoldRequest{}, fmt.Errorf("body too short: got %d want >= 4", len(body))
	}
	req := GoldRequest{Amount: int32(binary.LittleEndian.Uint32(body))}
	if req.Amount <= 0 {
		return GoldRequest{}, fmt.Errorf("amount %d invalid", req.Amount)
	}
	return req, nil
}

func (r GoldRequest) String() string {
	return fmt.Sprintf("amount=%d", r.Amount)
}
