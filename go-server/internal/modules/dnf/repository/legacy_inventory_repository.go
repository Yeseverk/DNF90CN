// 本文件定义 C# legacy 物品表的只读查询接口，供当前 EXE 初始化包桥接使用。
package repository

import "context"

// LegacyInventoryRepository 只读取导入自 C# inventory.db 的 legacy_character_items。
// 它不能替代新的 InventoryRepository，只用于当前协议尚未完成迁移时的初始化兜底。
type LegacyInventoryRepository interface {
	SelectItems(ctx context.Context, characterID string, listType byte) ([]LegacyInventoryItem, error)
}

// LegacyInventoryItem 是 legacy_character_items 中可映射到当前 0x166 row 的稳定字段。
type LegacyInventoryItem struct {
	ListType          byte
	SlotIndex         int16
	ItemTemplateID    int64
	StackCount        int64
	InstanceValue     int64
	Durability        int64
	SealFlag          int64
	OptionValue       int64
	Marker16          int64
	PetSerialOrHandle int64
	Extra             map[string]string
}
