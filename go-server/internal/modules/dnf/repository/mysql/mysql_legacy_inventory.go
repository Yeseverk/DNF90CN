// 本文件实现 C# legacy 物品表的 MySQL 只读查询。
package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"

	"longheng.io/server/internal/platform/db"
)

type mysqlLegacyInventoryStore struct {
	mysqlStoreBase
}

const (
	mysqlLegacyItemExtraTable        = "legacy_character_item_extra"
	mysqlLegacyItemAuditPayloadTable = "legacy_item_audit_payload_values"
)

// SelectItems 读取指定角色和 listType 的 legacy 物品行。
func (s *mysqlLegacyInventoryStore) SelectItems(ctx context.Context, characterID string, listType byte) ([]repository.LegacyInventoryItem, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return nil, db.ErrRecordKeyRequired
	}
	table, err := s.router.readTable("legacy_character_items", characterID)
	if err != nil {
		return nil, err
	}
	extraTable, err := s.router.readTable(mysqlLegacyItemExtraTable, characterID)
	if err != nil {
		return nil, err
	}
	query := "SELECT `item_uid`, `list_type`, `slot_index`, `item_template_id`, `stack_count`, `instance_value`, `durability`, `seal_flag`, `option_value`, `marker_16`, `pet_serial_or_handle` FROM " + table + " WHERE `character_id` = ? AND `list_type` = ? ORDER BY `slot_index`"
	rows, err := s.router.db.QueryContext(ctx, query, characterID, int(listType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]repository.LegacyInventoryItem, 0)
	itemIndexes := make(map[int64]int)
	for rows.Next() {
		var (
			itemUID   sql.NullInt64
			listRaw   sql.NullInt64
			slotRaw   sql.NullInt64
			itemRaw   sql.NullInt64
			stackRaw  sql.NullInt64
			instRaw   sql.NullInt64
			durRaw    sql.NullInt64
			sealRaw   sql.NullInt64
			optionRaw sql.NullInt64
			markerRaw sql.NullInt64
			petRaw    sql.NullInt64
		)
		if err := rows.Scan(&itemUID, &listRaw, &slotRaw, &itemRaw, &stackRaw, &instRaw, &durRaw, &sealRaw, &optionRaw, &markerRaw, &petRaw); err != nil {
			return nil, err
		}
		item := repository.LegacyInventoryItem{
			ListType:          byte(nullInt64(listRaw)),
			SlotIndex:         int16(nullInt64(slotRaw)),
			ItemTemplateID:    nullInt64(itemRaw),
			StackCount:        nullInt64(stackRaw),
			InstanceValue:     nullInt64(instRaw),
			Durability:        nullInt64(durRaw),
			SealFlag:          nullInt64(sealRaw),
			OptionValue:       nullInt64(optionRaw),
			Marker16:          nullInt64(markerRaw),
			PetSerialOrHandle: nullInt64(petRaw),
		}
		itemIndexes[nullInt64(itemUID)] = len(out)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	extraQuery := "SELECT item.item_uid, extra.extra_key, extra.extra_value FROM " + extraTable + " extra JOIN " + table + " item ON item.item_uid = extra.item_uid WHERE item.character_id = ? AND item.list_type = ? ORDER BY item.slot_index, extra.extra_key"
	extras, err := s.router.db.QueryContext(ctx, extraQuery, characterID, int(listType))
	if err != nil {
		return nil, err
	}
	defer extras.Close()
	for extras.Next() {
		var itemUID int64
		var key, value string
		if err := extras.Scan(&itemUID, &key, &value); err != nil {
			return nil, err
		}
		index, ok := itemIndexes[itemUID]
		if !ok {
			continue
		}
		if out[index].Extra == nil {
			out[index].Extra = make(map[string]string)
		}
		out[index].Extra[key] = value
	}
	if err := extras.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func nullInt64(value sql.NullInt64) int64 {
	if value.Valid {
		return value.Int64
	}
	return 0
}
