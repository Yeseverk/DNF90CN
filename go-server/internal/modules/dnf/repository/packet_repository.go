// 本文件定义 DNF 旧协议 packet 模板仓储接口和记录。
package repository

import (
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// PacketTemplateRepository 保存旧协议 packet 模板。
type PacketTemplateRepository interface {
	db.Store[PacketTemplateRecord]
}

// PacketTemplateRecord 是旧客户端 packet 模板仓储记录。
type PacketTemplateRecord struct {
	TemplateID string            `json:"template_id"`
	Name       string            `json:"name,omitempty"`
	Body       []byte            `json:"body,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at,omitempty"`
}

// ClonePacketTemplate 拷贝 packet 模板记录，避免 body 和 Metadata 被调用方污染。
func ClonePacketTemplate(record PacketTemplateRecord) PacketTemplateRecord {
	record.Body = append([]byte(nil), record.Body...)
	record.Metadata = cloneStringMap(record.Metadata)
	return record
}

func PacketTemplateKey(record PacketTemplateRecord) string {
	return strings.TrimSpace(record.TemplateID)
}
