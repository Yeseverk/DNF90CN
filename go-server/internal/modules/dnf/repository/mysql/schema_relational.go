package mysql

import "fmt"

const (
	mysqlAccountMetadataTable       = "account_metadata"
	mysqlAccountInventoryItemsTable = "account_inventory_items"
	mysqlAccountInventoryExtraTable = "account_inventory_item_extra"
	mysqlCharacterStatsTable        = "character_stats"
	mysqlCharacterLocationTable     = "character_locations"
	mysqlCharacterRosterTable       = "character_rosters"
	mysqlCharacterRosterEquipTable  = "character_roster_equipment"
	mysqlCharacterRosterListTable   = "character_roster_lists"
	mysqlInventoryItemsTable        = "inventory_items"
	mysqlInventoryExtraTable        = "inventory_item_extra"
	mysqlEquipmentEntriesTable      = "equipment_entries"
	mysqlEquipmentExtraTable        = "equipment_entry_extra"
	mysqlPetEntriesTable            = "pet_entries"
	mysqlPetExtraTable              = "pet_entry_extra"
	mysqlPetTokensTable             = "pet_clear_tokens"
	mysqlPetArtifactsTable          = "pet_artifacts"
	mysqlPetArtifactExtraTable      = "pet_artifact_extra"
	mysqlQuestStatesTable           = "quest_states"
	mysqlQuestExtraTable            = "quest_state_extra"
	mysqlSkillStatesTable           = "skill_states"
	mysqlSkillLayoutsTable          = "skill_layouts"
	mysqlSkillCooldownsTable        = "skill_cooldowns"
	mysqlMailsTable                 = "mails"
	mysqlMailMetadataTable          = "mail_metadata"
	mysqlMailAttachmentsTable       = "mail_attachments"
	mysqlMailAttachmentExtraTable   = "mail_attachment_extra"
	mysqlPacketMetadataTable        = "packet_template_metadata"
	mysqlSettingValuesTable         = "setting_values"
)

func mysqlRelationalTableSchema(database, prefix string) []string {
	table := func(suffix string) string { return mysqlTable(database, prefix, suffix) }
	return []string{
		stringMapTableDDL(table(mysqlAccountMetadataTable), "account_id"),
		itemStackTableDDL(table(mysqlAccountInventoryItemsTable), "account_id", false),
		extraTableDDL(table(mysqlAccountInventoryExtraTable), "account_id", false),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  stat_key VARCHAR(128) NOT NULL,
  stat_value BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, stat_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlCharacterStatsTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  channel_id INT NOT NULL DEFAULT 0,
  town_id BIGINT NOT NULL DEFAULT 0,
  dungeon_id BIGINT NOT NULL DEFAULT 0,
  room_id VARCHAR(128) NOT NULL DEFAULT '',
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlCharacterLocationTable)),
		characterRosterTableDDL(table(mysqlCharacterRosterTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  ordinal INT NOT NULL,
  slot BIGINT NOT NULL DEFAULT 0,
  item_id_or_icon BIGINT NOT NULL DEFAULT 0,
  raw_entry LONGBLOB NULL,
  packed_flags BIGINT NOT NULL DEFAULT 0,
  optional_id_or_expire BIGINT NOT NULL DEFAULT 0,
  aux_value BIGINT NOT NULL DEFAULT 0,
  aux_flag BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, ordinal)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlCharacterRosterEquipTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  list_name VARCHAR(32) NOT NULL,
  ordinal INT NOT NULL,
  int_value BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, list_name, ordinal)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlCharacterRosterListTable)),
		itemStackTableDDL(table(mysqlInventoryItemsTable), "character_id", true),
		extraTableDDL(table(mysqlInventoryExtraTable), "character_id", true),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  slot_index SMALLINT NOT NULL DEFAULT 0,
  item_id BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (character_id, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlEquipmentEntriesTable)),
		extraTableDDL(table(mysqlEquipmentExtraTable), "character_id", false),
		petEntryTableDDL(table(mysqlPetEntriesTable)),
		extraTableDDL(table(mysqlPetExtraTable), "character_id", false),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  pet_key VARCHAR(128) NOT NULL,
  token_order INT NOT NULL,
  token VARCHAR(255) NOT NULL,
  applied TINYINT UNSIGNED NOT NULL DEFAULT 1,
  PRIMARY KEY (character_id, pet_key, token),
  UNIQUE KEY uk_dnf_pet_clear_token_order (character_id, pet_key, token_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlPetTokensTable)),
		itemStackTableDDL(table(mysqlPetArtifactsTable), "character_id", false),
		extraTableDDL(table(mysqlPetArtifactExtraTable), "character_id", false),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  state_group VARCHAR(16) NOT NULL,
  quest_id BIGINT NOT NULL,
  status VARCHAR(64) NOT NULL DEFAULT '',
  trigger_type TINYINT UNSIGNED NOT NULL DEFAULT 0,
  progress_value BIGINT NOT NULL DEFAULT 0,
  reward_select_index BIGINT NOT NULL DEFAULT 0,
  multiplier BIGINT NOT NULL DEFAULT 0,
  state_updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id, state_group, quest_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlQuestStatesTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  state_group VARCHAR(16) NOT NULL,
  quest_id BIGINT NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (character_id, state_group, quest_id, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlQuestExtraTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  skill_id BIGINT NOT NULL,
  skill_level INT NOT NULL DEFAULT 0,
  enabled TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlSkillStatesTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  tree_id INT NOT NULL,
  slot_index INT NOT NULL,
  skill_id INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, tree_id, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlSkillLayoutsTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  skill_id BIGINT NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  PRIMARY KEY (character_id, skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlSkillCooldownsTable)),
		mailTableDDL(table(mysqlMailsTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  mail_id VARCHAR(128) NOT NULL,
  metadata_key VARCHAR(128) NOT NULL,
  metadata_value LONGTEXT NOT NULL,
  PRIMARY KEY (character_id, mail_id, metadata_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlMailMetadataTable)),
		mailAttachmentTableDDL(table(mysqlMailAttachmentsTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  mail_id VARCHAR(128) NOT NULL,
  attachment_index INT NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (character_id, mail_id, attachment_index, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table(mysqlMailAttachmentExtraTable)),
		stringMapTableDDL(table(mysqlPacketMetadataTable), "template_id"),
		stringMapTableDDL(table(mysqlSettingValuesTable), "scope"),
	}
}

func stringMapTableDDL(table, ownerColumn string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  %s VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  entry_value LONGTEXT NOT NULL,
  PRIMARY KEY (%s, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table, quoteSQLIdentifier(ownerColumn), quoteSQLIdentifier(ownerColumn))
}

func itemStackTableDDL(table, ownerColumn string, includeCollection bool) string {
	collectionColumn := ""
	collectionKey := ""
	if includeCollection {
		collectionColumn = "\n  collection_name VARCHAR(32) NOT NULL,"
		collectionKey = "collection_name, "
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  %s VARCHAR(128) NOT NULL,%s
  entry_key VARCHAR(128) NOT NULL,
  item_id BIGINT NOT NULL DEFAULT 0,
  item_count BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (%s, %sentry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		table, quoteSQLIdentifier(ownerColumn), collectionColumn, quoteSQLIdentifier(ownerColumn), collectionKey)
}

func extraTableDDL(table, ownerColumn string, includeCollection bool) string {
	collectionColumn := ""
	collectionKey := ""
	if includeCollection {
		collectionColumn = "\n  collection_name VARCHAR(32) NOT NULL,"
		collectionKey = "collection_name, "
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  %s VARCHAR(128) NOT NULL,%s
  entry_key VARCHAR(128) NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (%s, %sentry_key, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		table, quoteSQLIdentifier(ownerColumn), collectionColumn, quoteSQLIdentifier(ownerColumn), collectionKey)
}

func petEntryTableDDL(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  pet_key VARCHAR(128) NOT NULL,
  creature_key BIGINT UNSIGNED NOT NULL DEFAULT 0,
  item_id BIGINT NOT NULL DEFAULT 0,
  source_list_type TINYINT UNSIGNED NOT NULL DEFAULT 0,
  source_slot_index SMALLINT NOT NULL DEFAULT 0,
  pet_name VARCHAR(128) NOT NULL DEFAULT '',
  name_raw LONGBLOB NULL,
  satiety TINYINT UNSIGNED NOT NULL DEFAULT 0,
  satiety_micros BIGINT NOT NULL DEFAULT 0,
  mode_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  mode1_field_0a TINYINT UNSIGNED NOT NULL DEFAULT 0,
  mode1_field_0b TINYINT UNSIGNED NOT NULL DEFAULT 0,
  pet_level BIGINT NOT NULL DEFAULT 0,
  pet_exp BIGINT NOT NULL DEFAULT 0,
  tail_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (character_id, pet_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}

func mailTableDDL(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  mail_id VARCHAR(128) NOT NULL,
  sender_character_id VARCHAR(128) NOT NULL DEFAULT '',
  sender_name VARCHAR(128) NOT NULL DEFAULT '',
  recipient_character_id VARCHAR(128) NOT NULL DEFAULT '',
  recipient_name VARCHAR(128) NOT NULL DEFAULT '',
  title VARCHAR(255) NOT NULL DEFAULT '',
  body LONGTEXT NOT NULL,
  gold BIGINT NOT NULL DEFAULT 0,
  read_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  claimed_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  deleted_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(6) NULL,
  expire_at DATETIME(6) NULL,
  PRIMARY KEY (character_id, mail_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}

func mailAttachmentTableDDL(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  mail_id VARCHAR(128) NOT NULL,
  attachment_index INT NOT NULL,
  item_id BIGINT NOT NULL DEFAULT 0,
  item_count BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (character_id, mail_id, attachment_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}

func characterRosterTableDDL(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  header_unk_a BIGINT NOT NULL DEFAULT 0,
  header_unk_b BIGINT NOT NULL DEFAULT 0,
  header_total_or_slot_limit BIGINT NOT NULL DEFAULT 0,
  header_used_or_remain BIGINT NOT NULL DEFAULT 0,
  header_selected_or_page BIGINT NOT NULL DEFAULT 0,
  header_roster_state BIGINT NOT NULL DEFAULT 0,
  header_page_count BIGINT NOT NULL DEFAULT 0,
  header_roster_flag BIGINT NOT NULL DEFAULT 0,
  header_roster_value_a BIGINT NOT NULL DEFAULT 0,
  header_roster_value_b BIGINT NOT NULL DEFAULT 0,
  entry_byte_a BIGINT NOT NULL DEFAULT 0,
  entry_packed_job_grow BIGINT NOT NULL DEFAULT 0,
  entry_byte_c BIGINT NOT NULL DEFAULT 0,
  entry_field_2cc BIGINT NOT NULL DEFAULT 0,
  entry_state0 BIGINT NOT NULL DEFAULT 0,
  entry_time_a BIGINT NOT NULL DEFAULT 0,
  entry_time_b BIGINT NOT NULL DEFAULT 0,
  entry_value0 BIGINT NOT NULL DEFAULT 0,
  entry_value1 BIGINT NOT NULL DEFAULT 0,
  entry_value2 BIGINT NOT NULL DEFAULT 0,
  entry_reserved_a BIGINT NOT NULL DEFAULT 0,
  entry_reserved_b BIGINT NOT NULL DEFAULT 0,
  entry_value3 BIGINT NOT NULL DEFAULT 0,
  entry_object_id BIGINT NOT NULL DEFAULT 0,
  entry_flag0_eq1 BIGINT NOT NULL DEFAULT 0,
  entry_special_status_flag BIGINT NOT NULL DEFAULT 0,
  entry_value5 BIGINT NOT NULL DEFAULT 0,
  entry_display_flags BIGINT NOT NULL DEFAULT 0,
  entry_reserved_c BIGINT NOT NULL DEFAULT 0,
  entry_reserved_d BIGINT NOT NULL DEFAULT 0,
  entry_value6 BIGINT NOT NULL DEFAULT 0,
  entry_flag1_nonzero BIGINT NOT NULL DEFAULT 0,
  entry_bool_a_eq1 BIGINT NOT NULL DEFAULT 0,
  entry_bool_b_eq1 BIGINT NOT NULL DEFAULT 0,
  entry_bool_c_eq1 BIGINT NOT NULL DEFAULT 0,
  entry_flag2_nonzero BIGINT NOT NULL DEFAULT 0,
  entry_flag3_nonzero BIGINT NOT NULL DEFAULT 0,
  entry_flag4_nonzero BIGINT NOT NULL DEFAULT 0,
  entry_flag5_nonzero BIGINT NOT NULL DEFAULT 0,
  entry_value7 BIGINT NOT NULL DEFAULT 0,
  entry_flag6_eq1 BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}
