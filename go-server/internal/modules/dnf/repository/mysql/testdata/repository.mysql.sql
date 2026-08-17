-- DNF 仓储 MySQL 表结构示例。
-- 实际库名由 servergroup 区服配置 meta 派生；本文件由仓储 Schema 生成。
-- MySQL 关系表是权威存储；运行时仓储不读写 JSON 列，dnf_legacy_* 表镜像 C# inventory.db schema。
CREATE DATABASE IF NOT EXISTS `dnf_s9999_w01` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_accounts` (
  account_id VARCHAR(128) NOT NULL,
  state VARCHAR(32) NOT NULL DEFAULT '',
  honor_exp BIGINT UNSIGNED NOT NULL DEFAULT 0,
  represent_account_name VARCHAR(64) NULL,
  created_at DATETIME(6) NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (account_id),
  UNIQUE KEY uk_dnf_accounts_represent_account_name (represent_account_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_account_inventories` (
  account_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_characters` (
  character_id VARCHAR(128) NOT NULL,
  account_id VARCHAR(128) NOT NULL,
  slot INT NOT NULL DEFAULT 0,
  name VARCHAR(64) NOT NULL DEFAULT '',
  job VARCHAR(64) NOT NULL DEFAULT '',
  level INT NOT NULL DEFAULT 0,
  `grow_type` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `exp` BIGINT NOT NULL DEFAULT 0,
  `ex_equip_slot_stat` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `bonus_sp` INT NOT NULL DEFAULT 0,
  `bonus_tp` INT NOT NULL DEFAULT 0,
  `pvp_grade` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `pvp_rating_grade` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `user_state` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `tutorial_completed` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `tutorial_reward_progress_38` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `story_digest_last_level` INT UNSIGNED NOT NULL DEFAULT 0,
  `story_digest_migration_version` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `gold` BIGINT NOT NULL DEFAULT 0,
  `cera` BIGINT NOT NULL DEFAULT 0,
  `coin` BIGINT NOT NULL DEFAULT 0,
  `town_id` SMALLINT UNSIGNED NOT NULL DEFAULT 38,
  `area_id` SMALLINT UNSIGNED NOT NULL DEFAULT 1,
  `pos_x` SMALLINT NOT NULL DEFAULT 450,
  `pos_y` SMALLINT NOT NULL DEFAULT 234,
  `direction` TINYINT UNSIGNED NOT NULL DEFAULT 5,
  `area_state` TINYINT UNSIGNED NOT NULL DEFAULT 3,
  `delete_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `is_event_character` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `name_tag_item_id` BIGINT NOT NULL DEFAULT 0,
  `stamina` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `fatigue` SMALLINT UNSIGNED NOT NULL DEFAULT 156,
  `fatigue_limit` SMALLINT UNSIGNED NOT NULL DEFAULT 156,
  `fatigue_penalty` BIGINT NOT NULL DEFAULT 0,
  `pc_room_id` BIGINT NOT NULL DEFAULT 65537,
  `is_private_store` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `is_premium_pc_room` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `server_group_id` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `black_count` BIGINT NOT NULL DEFAULT 0,
  `guild_level` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `chaos_point` BIGINT NOT NULL DEFAULT 0,
  `disguise_kind` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `is_disguised` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `expert_job_type` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `expert_job_exp` BIGINT NOT NULL DEFAULT 0,
  `extra46` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `extra47` BIGINT NOT NULL DEFAULT 0,
  `extra51` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `is_hardcore_mode` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `is_hardcore_dead` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `hardcore_death_count` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `user_state_bits` TINYINT UNSIGNED NOT NULL DEFAULT 3,
  `chat_ban_end_time` BIGINT NOT NULL DEFAULT 0,
  `fatigue_update` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `return_user_flag` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `channel_display_mode` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `channel_type` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `channel_id` SMALLINT UNSIGNED NOT NULL DEFAULT 2,
  `is_return_user` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `link_slot_enabled` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `link_type_a` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `link_type_b` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `emotion_index` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `action_byte` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `fatigue_display_update` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `costume_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `aura_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `pet_display_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `title_display_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `pvp_stat_a` BIGINT NOT NULL DEFAULT 0,
  `pvp_win_streak` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `pvp_lose_streak` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `pvp_rank_point` BIGINT NOT NULL DEFAULT 0,
  `trailing_byte` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_block_marker` BIGINT NOT NULL DEFAULT 83,
  `stat_hp_max` BIGINT NOT NULL DEFAULT 0,
  `stat_mp_max` BIGINT NOT NULL DEFAULT 0,
  `stat_strength` INT NOT NULL DEFAULT 0,
  `stat_intelligence` INT NOT NULL DEFAULT 0,
  `stat_vitality` INT NOT NULL DEFAULT 0,
  `stat_spirit` INT NOT NULL DEFAULT 0,
  `stat_physical_attack` INT NOT NULL DEFAULT 0,
  `stat_physical_defense` INT NOT NULL DEFAULT 0,
  `stat_magical_attack` INT NOT NULL DEFAULT 0,
  `stat_magical_defense` INT NOT NULL DEFAULT 0,
  `stat_independent_attack` INT NOT NULL DEFAULT 0,
  `stat_fire_resistance` INT NOT NULL DEFAULT 0,
  `stat_water_resistance` INT NOT NULL DEFAULT 0,
  `stat_dark_resistance` INT NOT NULL DEFAULT 0,
  `stat_light_resistance` INT NOT NULL DEFAULT 0,
  `active_status_resistance_00` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_01` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_02` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_03` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_04` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_05` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_06` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_07` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_08` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_09` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_10` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_11` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_12` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_13` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_14` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_15` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_16` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `active_status_resistance_17` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_inventory_limit` BIGINT NOT NULL DEFAULT 0,
  `stat_hp_regen_speed` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_mp_regen_speed` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_move_speed` BIGINT NOT NULL DEFAULT 0,
  `stat_attack_speed` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_cast_speed` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_hit_recovery` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_jump_power` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `stat_weight` BIGINT NOT NULL DEFAULT 0,
  `stat_level` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `name_tag_expire_time` BIGINT NOT NULL DEFAULT 0,
  `skill_tree_index` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `equipped_creature_level` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `equip_list_trailing` BIGINT NOT NULL DEFAULT 0,
  `manage_level` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `flag_byte` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `guild_power_war` BIGINT NOT NULL DEFAULT 0,
  `server_timestamp` BIGINT NOT NULL DEFAULT 0,
  `quest_shop_count` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `progress1` BIGINT NOT NULL DEFAULT 0,
  `progress2` BIGINT NOT NULL DEFAULT 0,
  `create_option_len` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_state0` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_time_a` BIGINT NOT NULL DEFAULT 0,
  `roster_time_b` BIGINT NOT NULL DEFAULT 0,
  `roster_value0` BIGINT NOT NULL DEFAULT 0,
  `roster_value1` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_value2` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_reserved_a` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_reserved_b` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_00` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_01` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_02` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_03` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_04` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_05` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_06` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_linked_id_07` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_value3` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_object_id` BIGINT NOT NULL DEFAULT 0,
  `roster_flag0_eq1` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_card_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_value5` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_display_flags` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_00` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_01` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_02` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_03` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_04` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_05` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_06` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_07` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_08` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_09` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_10` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_tail_11` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `roster_flag6_eq1` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_00` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_01` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_02` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_03` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_04` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_05` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_06` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_07` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_08` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_09` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_10` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_11` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_12` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_13` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_14` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_15` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_16` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_17` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_18` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_19` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_20` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_21` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_22` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_23` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_24` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_25` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_26` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_27` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_28` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_29` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_30` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_31` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_32` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_33` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_34` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_35` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_36` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_37` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_38` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_39` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_40` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_41` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_42` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_43` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_44` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_45` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_46` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_47` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_48` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_49` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_50` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_51` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_52` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_53` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_54` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_55` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_56` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_57` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_58` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_59` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_60` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_61` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_62` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `create_option_byte_63` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(6) NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id),
  UNIQUE KEY uk_dnf_characters_account_slot_active (account_id, slot, delete_flag),
  KEY idx_dnf_characters_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_inventories` (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_equipments` (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_pets` (
  character_id VARCHAR(128) NOT NULL,
  equipped_key VARCHAR(128) NOT NULL DEFAULT '',
  town_display TINYINT UNSIGNED NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_quests` (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_skills` (
  character_id VARCHAR(128) NOT NULL,
  total_sp INT NOT NULL DEFAULT 0,
  remaining_sp INT NOT NULL DEFAULT 0,
  total_tp INT NOT NULL DEFAULT 0,
  remaining_tp INT NOT NULL DEFAULT 0,
  synced_level INT NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_dungeon_permissions` (
  character_id VARCHAR(128) NOT NULL,
  dungeon_id BIGINT UNSIGNED NOT NULL,
  clear_state TINYINT UNSIGNED NOT NULL DEFAULT 0,
  sort_order INT NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id, dungeon_id),
  KEY idx_dnf_dungeon_permissions_character_sort (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_mailboxes` (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_packet_templates` (
  template_id VARCHAR(128) NOT NULL,
  name VARCHAR(128) NOT NULL DEFAULT '',
  body LONGBLOB NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (template_id),
  KEY idx_dnf_packet_templates_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_settings` (
  scope VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (scope)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_account_metadata` (
  `account_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  entry_value LONGTEXT NOT NULL,
  PRIMARY KEY (`account_id`, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_account_inventory_items` (
  `account_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  item_id BIGINT NOT NULL DEFAULT 0,
  item_count BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (`account_id`, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_account_inventory_item_extra` (
  `account_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (`account_id`, entry_key, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_character_stats` (
  character_id VARCHAR(128) NOT NULL,
  stat_key VARCHAR(128) NOT NULL,
  stat_value BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, stat_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_character_locations` (
  character_id VARCHAR(128) NOT NULL,
  channel_id INT NOT NULL DEFAULT 0,
  town_id BIGINT NOT NULL DEFAULT 0,
  dungeon_id BIGINT NOT NULL DEFAULT 0,
  room_id VARCHAR(128) NOT NULL DEFAULT '',
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_character_rosters` (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_character_roster_equipment` (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_character_roster_lists` (
  character_id VARCHAR(128) NOT NULL,
  list_name VARCHAR(32) NOT NULL,
  ordinal INT NOT NULL,
  int_value BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, list_name, ordinal)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_inventory_items` (
  `character_id` VARCHAR(128) NOT NULL,
  collection_name VARCHAR(32) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  item_id BIGINT NOT NULL DEFAULT 0,
  item_count BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (`character_id`, collection_name, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_inventory_item_extra` (
  `character_id` VARCHAR(128) NOT NULL,
  collection_name VARCHAR(32) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (`character_id`, collection_name, entry_key, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_equipment_entries` (
  character_id VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  slot_index SMALLINT NOT NULL DEFAULT 0,
  item_id BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (character_id, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_equipment_entry_extra` (
  `character_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (`character_id`, entry_key, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_pet_entries` (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_pet_entry_extra` (
  `character_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (`character_id`, entry_key, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_pet_clear_tokens` (
  character_id VARCHAR(128) NOT NULL,
  pet_key VARCHAR(128) NOT NULL,
  token_order INT NOT NULL,
  token VARCHAR(255) NOT NULL,
  applied TINYINT UNSIGNED NOT NULL DEFAULT 1,
  PRIMARY KEY (character_id, pet_key, token),
  UNIQUE KEY uk_dnf_pet_clear_token_order (character_id, pet_key, token_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_pet_artifacts` (
  `character_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  item_id BIGINT NOT NULL DEFAULT 0,
  item_count BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (`character_id`, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_pet_artifact_extra` (
  `character_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (`character_id`, entry_key, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_quest_states` (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_quest_state_extra` (
  character_id VARCHAR(128) NOT NULL,
  state_group VARCHAR(16) NOT NULL,
  quest_id BIGINT NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (character_id, state_group, quest_id, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_skill_states` (
  character_id VARCHAR(128) NOT NULL,
  skill_id BIGINT NOT NULL,
  skill_level INT NOT NULL DEFAULT 0,
  enabled TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_skill_layouts` (
  character_id VARCHAR(128) NOT NULL,
  tree_id INT NOT NULL,
  slot_index INT NOT NULL,
  skill_id INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, tree_id, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_skill_cooldowns` (
  character_id VARCHAR(128) NOT NULL,
  skill_id BIGINT NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  PRIMARY KEY (character_id, skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_mails` (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_mail_metadata` (
  character_id VARCHAR(128) NOT NULL,
  mail_id VARCHAR(128) NOT NULL,
  metadata_key VARCHAR(128) NOT NULL,
  metadata_value LONGTEXT NOT NULL,
  PRIMARY KEY (character_id, mail_id, metadata_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_mail_attachments` (
  character_id VARCHAR(128) NOT NULL,
  mail_id VARCHAR(128) NOT NULL,
  attachment_index INT NOT NULL,
  item_id BIGINT NOT NULL DEFAULT 0,
  item_count BIGINT NOT NULL DEFAULT 0,
  bind_flag TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(6) NULL,
  raw_entry LONGBLOB NULL,
  PRIMARY KEY (character_id, mail_id, attachment_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_mail_attachment_extra` (
  character_id VARCHAR(128) NOT NULL,
  mail_id VARCHAR(128) NOT NULL,
  attachment_index INT NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (character_id, mail_id, attachment_index, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_packet_template_metadata` (
  `template_id` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  entry_value LONGTEXT NOT NULL,
  PRIMARY KEY (`template_id`, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_setting_values` (
  `scope` VARCHAR(128) NOT NULL,
  entry_key VARCHAR(128) NOT NULL,
  entry_value LONGTEXT NOT NULL,
  PRIMARY KEY (`scope`, entry_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_accounts` (
  `account_id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `m_id` VARCHAR(255) NOT NULL UNIQUE,
  `password_hash` VARCHAR(255) NOT NULL DEFAULT '',
  `last_login_ip` VARCHAR(255) NOT NULL DEFAULT '',
  `last_login_at` VARCHAR(255),
  `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `cera` BIGINT NOT NULL DEFAULT 0,
  `token_cera` BIGINT NOT NULL DEFAULT 0,
  `happy_token_cera` BIGINT NOT NULL DEFAULT 0,
  `lucky_star` BIGINT NOT NULL DEFAULT 0,
  `cube_black` BIGINT NOT NULL DEFAULT 0,
  `cube_white` BIGINT NOT NULL DEFAULT 0,
  `cube_red` BIGINT NOT NULL DEFAULT 0,
  `cube_blue` BIGINT NOT NULL DEFAULT 0,
  `cube_clear` BIGINT NOT NULL DEFAULT 0,
  `cube_gold` BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_characters` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `account_id` BIGINT NOT NULL,
  `name` VARCHAR(255) NOT NULL,
  `job` BIGINT NOT NULL DEFAULT 0,
  `grow_type` BIGINT NOT NULL DEFAULT 0,
  `level` BIGINT NOT NULL DEFAULT 1,
  `exp` BIGINT NOT NULL DEFAULT 0,
  `ex_equip_slot_stat` BIGINT NOT NULL DEFAULT 0,
  `bonus_sp` BIGINT NOT NULL DEFAULT 0,
  `bonus_tp` BIGINT NOT NULL DEFAULT 0,
  `pvp_grade` BIGINT NOT NULL DEFAULT 0,
  `pvp_rating_grade` BIGINT NOT NULL DEFAULT 0,
  `user_state` BIGINT NOT NULL DEFAULT 0,
  `gold` BIGINT NOT NULL DEFAULT 0,
  `coin` BIGINT NOT NULL DEFAULT 0,
  `town_id` BIGINT NOT NULL DEFAULT 0,
  `area_id` BIGINT NOT NULL DEFAULT 0,
  `pos_x` BIGINT NOT NULL DEFAULT 0,
  `pos_y` BIGINT NOT NULL DEFAULT 0,
  `direction` BIGINT NOT NULL DEFAULT 5,
  `area_state` BIGINT NOT NULL DEFAULT 3,
  `name_bytes` LONGBLOB,
  `appearance_blob` LONGBLOB,
  `delete_flag` BIGINT NOT NULL DEFAULT 0,
  `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY `idx_characters_name_unique` (name),
  KEY `idx_characters_account` (account_id, delete_flag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_container_state` (
  `character_id` BIGINT NOT NULL,
  `list_type` BIGINT NOT NULL,
  `list_param16` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, list_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_items` (
  `item_uid` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `owner_scope` VARCHAR(255) NOT NULL,
  `owner_id` BIGINT NOT NULL,
  `character_id` BIGINT,
  `list_type` BIGINT NOT NULL,
  `slot_index` BIGINT NOT NULL,
  `item_template_id` BIGINT NOT NULL,
  `item_kind` VARCHAR(255) NOT NULL DEFAULT 'unknown',
  `stack_count` BIGINT NOT NULL DEFAULT 0,
  `instance_value` BIGINT NOT NULL DEFAULT 0,
  `durability` BIGINT NOT NULL DEFAULT 0,
  `seal_flag` BIGINT NOT NULL DEFAULT 0,
  `option_value` BIGINT NOT NULL DEFAULT 0,
  `expire_time` BIGINT NOT NULL DEFAULT 0,
  `marker_16` BIGINT NOT NULL DEFAULT 0,
  `pet_serial_or_handle` BIGINT NOT NULL DEFAULT 0,
  `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE(owner_scope, owner_id, list_type, slot_index, item_kind),
  KEY `idx_character_items_owner_container` (owner_scope, owner_id, list_type, slot_index),
  KEY `idx_character_items_template` (item_template_id),
  KEY `idx_character_items_character` (character_id, list_type, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_item_extra` (
  item_uid BIGINT NOT NULL,
  extra_key VARCHAR(128) NOT NULL,
  extra_value LONGTEXT NOT NULL,
  PRIMARY KEY (item_uid, extra_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_account_cargo_state` (
  `account_id` BIGINT NOT NULL PRIMARY KEY,
  `selection_key` BIGINT NOT NULL DEFAULT 0,
  `value32` BIGINT NOT NULL DEFAULT 0,
  `item_count` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_item_audit_log` (
  `audit_id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `owner_scope` VARCHAR(255) NOT NULL,
  `owner_id` BIGINT NOT NULL,
  `character_id` BIGINT,
  `action_name` VARCHAR(255) NOT NULL,
  `list_type` BIGINT,
  `slot_index` BIGINT,
  `item_uid` BIGINT,
  `item_template_id` BIGINT,
  `delta_stack_count` BIGINT NOT NULL DEFAULT 0,
  `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_item_audit_payload_values` (
  audit_id BIGINT NOT NULL,
  value_path VARCHAR(512) NOT NULL,
  value_type VARCHAR(16) NOT NULL,
  string_value LONGTEXT NULL,
  number_value VARCHAR(128) NULL,
  bool_value TINYINT UNSIGNED NULL,
  PRIMARY KEY (audit_id, value_path)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_skills` (
  `character_id` BIGINT NOT NULL,
  `page_index` BIGINT NOT NULL,
  `page_header` BIGINT NOT NULL DEFAULT 0,
  `slot` BIGINT NOT NULL,
  `skill_id` BIGINT NOT NULL,
  `level` BIGINT NOT NULL DEFAULT 0,
  `extra_values` LONGBLOB,
  PRIMARY KEY (character_id, page_index, slot)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_skill_tail` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `tail0` BIGINT NOT NULL DEFAULT 0,
  `tail1` BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_skill_points` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `total_sp` BIGINT NOT NULL DEFAULT 0,
  `remaining_sp` BIGINT NOT NULL DEFAULT 0,
  `total_sfp` BIGINT NOT NULL DEFAULT 0,
  `remaining_sfp` BIGINT NOT NULL DEFAULT 0,
  `total_tp` BIGINT NOT NULL DEFAULT 0,
  `remaining_tp` BIGINT NOT NULL DEFAULT 0,
  `synced_level` BIGINT NOT NULL DEFAULT 1,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_creatures` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `creature_key` BIGINT NOT NULL,
  `field04` BIGINT NOT NULL DEFAULT 0,
  `mode_flag` BIGINT NOT NULL DEFAULT 0,
  `progress_value` BIGINT NOT NULL DEFAULT 0,
  `mode1_field0a` BIGINT NOT NULL DEFAULT 0,
  `mode1_field0b` BIGINT NOT NULL DEFAULT 0,
  `field_after_value` BIGINT NOT NULL DEFAULT 0,
  `creature_text` LONGBLOB,
  `tail_flag` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_init_flags` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `shop_coin_event_flag` BIGINT NOT NULL DEFAULT 0,
  `level60_ui_state` BIGINT NOT NULL DEFAULT 0,
  `pc_room_state` BIGINT NOT NULL DEFAULT 0,
  `expert_job_blob` LONGBLOB,
  `champion_break_blob` LONGBLOB,
  `boss_tower_placeholder` BIGINT NOT NULL DEFAULT 0,
  `mailbox_loaded_count` BIGINT NOT NULL DEFAULT 0,
  `mailbox_mode` BIGINT NOT NULL DEFAULT 0,
  `mailbox_not_loaded_count` BIGINT NOT NULL DEFAULT 0,
  `mailbox_unknown_count_c` BIGINT NOT NULL DEFAULT 0,
  `event_info_tail_byte` BIGINT NOT NULL DEFAULT 0,
  `hotkey_key_type` BIGINT NOT NULL DEFAULT 0,
  `main_game_option_blob` LONGBLOB,
  `quickchat_bank0` LONGBLOB,
  `quickchat_bank1` LONGBLOB,
  `charac_invisible_falgs_payload_len` BIGINT NOT NULL DEFAULT 0,
  `racing_dungeon_current_enter_count` BIGINT NOT NULL DEFAULT 0,
  `racing_dungeon_group_flags` LONGBLOB,
  `ack_account_reg_time` BIGINT NOT NULL DEFAULT 0,
  `ack_premium_blob` LONGBLOB,
  `ack_quest_display_ids` LONGBLOB,
  `ack_char_slot_index` BIGINT NOT NULL DEFAULT 0,
  `ack_fatigue_battery` BIGINT NOT NULL DEFAULT 0,
  `ack_fatigue_grownup_buff` BIGINT NOT NULL DEFAULT 0,
  `ack_trade_punish_flag` BIGINT NOT NULL DEFAULT 0,
  `ack_extra_field_86jp` BIGINT NOT NULL DEFAULT 0,
  `ack_reserved_8b` LONGBLOB,
  `ack_tutorial_skipable` BIGINT NOT NULL DEFAULT 0,
  `ack_post_tutorial_u16` BIGINT NOT NULL DEFAULT 0,
  `ack_unread_tail` LONGBLOB
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_item_values` (
  `character_id` BIGINT NOT NULL,
  `list_kind` VARCHAR(255) NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `item_id` BIGINT NOT NULL,
  `value` BIGINT NOT NULL,
  PRIMARY KEY (character_id, list_kind, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_item_locks` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `type_or_list` BIGINT NOT NULL,
  `item_key_or_slot` BIGINT NOT NULL,
  `state` BIGINT NOT NULL,
  `extra_value` BIGINT,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_growth_weapon_stages` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `stage_id` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_show_effects` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `effect_index` BIGINT NOT NULL,
  `duration_seconds` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_pvp_missions` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `mission_id` BIGINT NOT NULL,
  `progress_value` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_dungeon_permissions` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `dungeon_id` BIGINT NOT NULL,
  `clear_state` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_event_info` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `repeat_event_index` BIGINT NOT NULL,
  `event_data` LONGBLOB,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_hotkey_slots` (
  `character_id` BIGINT NOT NULL,
  `slot_index` BIGINT NOT NULL,
  `hotkey_value` BIGINT NOT NULL,
  PRIMARY KEY (character_id, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_invisible_falgs` (
  `character_id` BIGINT NOT NULL,
  `slot_index` BIGINT NOT NULL,
  `flag_value` BIGINT NOT NULL,
  PRIMARY KEY (character_id, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_racing_dungeon_groups` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL,
  `group_id` BIGINT NOT NULL,
  PRIMARY KEY (character_id, group_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_racing_dungeon_entries` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL,
  `entry_index` BIGINT NOT NULL,
  `track_like_id` BIGINT NOT NULL,
  `value_a` BIGINT NOT NULL,
  `value_b` BIGINT NOT NULL,
  PRIMARY KEY (character_id, group_index, entry_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_racing_dungeon_tail_ids` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `id_value` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_achievement_complete` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `achievement_id` BIGINT NOT NULL,
  `p1` BIGINT NOT NULL DEFAULT 0,
  `p2` BIGINT NOT NULL DEFAULT 0,
  `p3` BIGINT NOT NULL DEFAULT 0,
  `p4` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_achievement_chunks` (
  `character_id` BIGINT NOT NULL,
  `chunk_index` BIGINT NOT NULL,
  `mode_byte` BIGINT NOT NULL DEFAULT 0,
  `owner_id16` BIGINT NOT NULL DEFAULT 0,
  `entries_blob` LONGBLOB,
  PRIMARY KEY (character_id, chunk_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_unknown725` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `param_a` BIGINT NOT NULL,
  `mode_or_state` BIGINT NOT NULL,
  `content_id` BIGINT NOT NULL,
  `param_b` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_unknown730` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `entry_id` BIGINT NOT NULL,
  `sentinel_or_value` BIGINT NOT NULL,
  `flag` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo_blobs` (
  `character_id` BIGINT NOT NULL,
  `blob_kind` VARCHAR(255) NOT NULL,
  `subtype` BIGINT NOT NULL,
  `user_info_type` BIGINT NOT NULL DEFAULT 0,
  `gate_or_count` BIGINT NOT NULL DEFAULT 0,
  `user_id` BIGINT NOT NULL DEFAULT 0,
  `name_bytes` LONGBLOB,
  `remaining_bytes` LONGBLOB,
  PRIMARY KEY (character_id, blob_kind, subtype)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_global_raw_packets` (
  `noti_type` BIGINT NOT NULL PRIMARY KEY,
  `packet_body` LONGBLOB NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_get_userinfo_template` (
  `id` BIGINT NOT NULL PRIMARY KEY DEFAULT 1,
  `seed_character_id` BIGINT NOT NULL DEFAULT 1000,
  `response_blob` LONGBLOB,
  `pkt0_routing_byte7` BIGINT NOT NULL DEFAULT 0,
  `gate_or_count1` BIGINT NOT NULL DEFAULT 32,
  `gate_or_count2` BIGINT NOT NULL DEFAULT 32,
  `flag_or_manage` BIGINT NOT NULL DEFAULT 2,
  `key_or_point` BIGINT NOT NULL DEFAULT 0,
  `unknown16` BIGINT NOT NULL DEFAULT 0,
  `unknown32` BIGINT NOT NULL DEFAULT 0,
  `pkt2_result_code` BIGINT NOT NULL DEFAULT 1,
  `pkt2_character_key` BIGINT NOT NULL DEFAULT 0,
  `pkt2_slot_flag1` BIGINT NOT NULL DEFAULT 0,
  `pkt2_slot_flag2` BIGINT NOT NULL DEFAULT 1,
  `pkt2_state_flag` BIGINT NOT NULL DEFAULT 255,
  `pkt2_flag3` BIGINT NOT NULL DEFAULT 1,
  `pkt2_reserved` BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_account_character_entries` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `entry_index` BIGINT NOT NULL,
  `slot_index` BIGINT NOT NULL,
  `name` VARCHAR(255) NOT NULL,
  `name_bytes` LONGBLOB,
  `body_after_name` LONGBLOB NOT NULL,
  UNIQUE(entry_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_getuserinfo_extra_packets` (
  `seq` BIGINT NOT NULL PRIMARY KEY,
  `command` BIGINT NOT NULL,
  `noti_type` BIGINT NOT NULL,
  `body` LONGBLOB NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_packet_sequence` (
  `character_id` BIGINT NOT NULL,
  `seq_index` BIGINT NOT NULL,
  `command` BIGINT NOT NULL,
  `noti_type` BIGINT NOT NULL,
  `kind` BIGINT NOT NULL DEFAULT 0,
  `item_list_type` BIGINT NOT NULL DEFAULT -1,
  `occurrence_index` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, seq_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_packet_templates` (
  `character_id` BIGINT NOT NULL,
  `command` BIGINT NOT NULL,
  `noti_type` BIGINT NOT NULL,
  `occurrence_index` BIGINT NOT NULL DEFAULT 0,
  `body` LONGBLOB NOT NULL,
  `body_length` BIGINT NOT NULL,
  PRIMARY KEY (character_id, command, noti_type, occurrence_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_equipped_items` (
  `character_id` BIGINT NOT NULL,
  `equip_list_blob` LONGBLOB NOT NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_unequipped_entries` (
  `character_id` BIGINT NOT NULL,
  `item_template_id` BIGINT NOT NULL,
  `raw_entry` LONGBLOB NOT NULL,
  PRIMARY KEY (character_id, item_template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_global_server_event_phase` (
  `id` BIGINT NOT NULL PRIMARY KEY,
  `event_phase_bitmap` LONGBLOB NOT NULL,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_init_bodies` (
  `character_id` BIGINT NOT NULL,
  `noti_type` BIGINT NOT NULL,
  `occurrence_index` BIGINT NOT NULL DEFAULT 0,
  `body` LONGBLOB NOT NULL,
  PRIMARY KEY (character_id, noti_type, occurrence_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_subtype0_fields` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `name_tag_item_id` BIGINT NOT NULL DEFAULT 0,
  `creature_field1` BIGINT NOT NULL DEFAULT 0,
  `creature_field2` BIGINT NOT NULL DEFAULT 0,
  `creature_field3` BIGINT NOT NULL DEFAULT 0,
  `creature_field4` BIGINT NOT NULL DEFAULT 0,
  `creature_buffer` LONGBLOB,
  `stamina` BIGINT NOT NULL DEFAULT 0,
  `fatigue_penalty` BIGINT NOT NULL DEFAULT 0,
  `is_event_character` BIGINT NOT NULL DEFAULT 0,
  `pc_room_id` BIGINT NOT NULL DEFAULT 65537,
  `is_private_store` BIGINT NOT NULL DEFAULT 0,
  `is_premium_pc_room` BIGINT NOT NULL DEFAULT 0,
  `server_group_id` BIGINT NOT NULL DEFAULT 0,
  `black_count` BIGINT NOT NULL DEFAULT 0,
  `guild_level` BIGINT NOT NULL DEFAULT 0,
  `chaos_point` BIGINT NOT NULL DEFAULT 0,
  `disguise_kind` BIGINT NOT NULL DEFAULT 0,
  `is_disguised` BIGINT NOT NULL DEFAULT 0,
  `expert_job_type` BIGINT NOT NULL DEFAULT 0,
  `expert_job_exp` BIGINT NOT NULL DEFAULT 0,
  `extra46` BIGINT NOT NULL DEFAULT 0,
  `extra47` BIGINT NOT NULL DEFAULT 0,
  `extra51` BIGINT NOT NULL DEFAULT 0,
  `is_hardcore_mode` BIGINT NOT NULL DEFAULT 0,
  `is_hardcore_dead` BIGINT NOT NULL DEFAULT 0,
  `hardcore_death_count` BIGINT NOT NULL DEFAULT 0,
  `user_state_bits` BIGINT NOT NULL DEFAULT 3,
  `chat_ban_end_time` BIGINT NOT NULL DEFAULT 0,
  `fatigue_update` BIGINT NOT NULL DEFAULT 0,
  `return_user_flag` BIGINT NOT NULL DEFAULT 1,
  `channel_display_mode` BIGINT NOT NULL DEFAULT 0,
  `channel_type` BIGINT NOT NULL DEFAULT 0,
  `channel_id` BIGINT NOT NULL DEFAULT 2,
  `is_return_user` BIGINT NOT NULL DEFAULT 0,
  `link_slot_enabled` BIGINT NOT NULL DEFAULT 0,
  `link_type_a` BIGINT NOT NULL DEFAULT 0,
  `link_type_b` BIGINT NOT NULL DEFAULT 0,
  `emotion_index` BIGINT NOT NULL DEFAULT 0,
  `action_byte` BIGINT NOT NULL DEFAULT 0,
  `fatigue_display_update` BIGINT NOT NULL DEFAULT 0,
  `costume_flag` BIGINT NOT NULL DEFAULT 0,
  `aura_flag` BIGINT NOT NULL DEFAULT 0,
  `pet_display_flag` BIGINT NOT NULL DEFAULT 0,
  `title_display_flag` BIGINT NOT NULL DEFAULT 0,
  `pvp_stat_a` BIGINT NOT NULL DEFAULT 0,
  `pvp_win_streak` BIGINT NOT NULL DEFAULT 0,
  `pvp_lose_streak` BIGINT NOT NULL DEFAULT 0,
  `pvp_rank_point` BIGINT NOT NULL DEFAULT 0,
  `trailing_byte` BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_subtype1_fields` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `stat_hp_max` BIGINT NOT NULL DEFAULT 0,
  `stat_mp_max` BIGINT NOT NULL DEFAULT 0,
  `stat_strength` BIGINT NOT NULL DEFAULT 0,
  `stat_intelligence` BIGINT NOT NULL DEFAULT 0,
  `stat_vitality` BIGINT NOT NULL DEFAULT 0,
  `stat_spirit` BIGINT NOT NULL DEFAULT 0,
  `stat_physical_attack` BIGINT NOT NULL DEFAULT 0,
  `stat_physical_defense` BIGINT NOT NULL DEFAULT 0,
  `stat_magical_attack` BIGINT NOT NULL DEFAULT 0,
  `stat_magical_defense` BIGINT NOT NULL DEFAULT 0,
  `stat_independent_attack` BIGINT NOT NULL DEFAULT 0,
  `stat_fire_resistance` BIGINT NOT NULL DEFAULT 0,
  `stat_water_resistance` BIGINT NOT NULL DEFAULT 0,
  `stat_dark_resistance` BIGINT NOT NULL DEFAULT 0,
  `stat_light_resistance` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_00` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_01` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_02` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_03` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_04` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_05` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_06` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_07` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_08` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_09` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_10` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_11` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_12` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_13` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_14` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_15` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_16` BIGINT NOT NULL DEFAULT 0,
  `active_status_resistance_17` BIGINT NOT NULL DEFAULT 0,
  `stat_inventory_limit` BIGINT NOT NULL DEFAULT 0,
  `stat_hp_regen_speed` BIGINT NOT NULL DEFAULT 0,
  `stat_mp_regen_speed` BIGINT NOT NULL DEFAULT 0,
  `stat_move_speed` BIGINT NOT NULL DEFAULT 0,
  `stat_attack_speed` BIGINT NOT NULL DEFAULT 0,
  `stat_cast_speed` BIGINT NOT NULL DEFAULT 0,
  `stat_hit_recovery` BIGINT NOT NULL DEFAULT 0,
  `stat_jump_power` BIGINT NOT NULL DEFAULT 0,
  `stat_weight` BIGINT NOT NULL DEFAULT 0,
  `stat_level` BIGINT NOT NULL DEFAULT 0,
  `name_tag_item_id` BIGINT NOT NULL DEFAULT 0,
  `name_tag_expire_time` BIGINT NOT NULL DEFAULT 0,
  `skill_tree_index` BIGINT NOT NULL DEFAULT 0,
  `equipped_creature_level` BIGINT NOT NULL DEFAULT 0,
  `equip_list_trailing` BIGINT NOT NULL DEFAULT 0,
  `manage_level` BIGINT NOT NULL DEFAULT 0,
  `flag_byte` BIGINT NOT NULL DEFAULT 0,
  `guild_power_war` BIGINT NOT NULL DEFAULT 0,
  `server_timestamp` BIGINT NOT NULL DEFAULT 0,
  `quest_shop_count` BIGINT NOT NULL DEFAULT 0,
  `progress1` BIGINT NOT NULL DEFAULT 0,
  `progress2` BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_equipped_entries` (
  `character_id` BIGINT NOT NULL,
  `slot` BIGINT NOT NULL,
  `item_id` BIGINT NOT NULL,
  `expire_time` BIGINT NOT NULL DEFAULT 0,
  `raw_entry` LONGBLOB NOT NULL,
  PRIMARY KEY (character_id, slot)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo_slot_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `visible_category_count` BIGINT NOT NULL DEFAULT 4,
  `group0_entry_total` BIGINT NOT NULL DEFAULT 0,
  `group0_tail_refresh_value` BIGINT NOT NULL DEFAULT 0,
  `refresh_value_a` BIGINT NOT NULL DEFAULT 0,
  `refresh_value_b` BIGINT NOT NULL DEFAULT 0,
  `selected_category_a` BIGINT NOT NULL DEFAULT -1,
  `selected_category_b` BIGINT NOT NULL DEFAULT -1,
  `msg72_value_a` BIGINT NOT NULL DEFAULT 0,
  `msg72_state` BIGINT NOT NULL DEFAULT 0,
  `render_flag_1949` BIGINT NOT NULL DEFAULT 0,
  `render_flag_1950` BIGINT NOT NULL DEFAULT 0,
  `special_group_flag_1951` BIGINT NOT NULL DEFAULT 0,
  `special_group_mode_1952` BIGINT NOT NULL DEFAULT 0,
  `mode_bits` BIGINT NOT NULL DEFAULT 0,
  `tail_value_a` BIGINT NOT NULL DEFAULT 0,
  `tail_flag_a` BIGINT NOT NULL DEFAULT 0,
  `tail_bool_b` BIGINT NOT NULL DEFAULT 0,
  `tail_value_b` BIGINT NOT NULL DEFAULT 0,
  `tail_pair_a` BIGINT NOT NULL DEFAULT 0,
  `tail_pair_b` BIGINT NOT NULL DEFAULT 0,
  `tail_index6_value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo_slot_category_state` (
  `character_id` BIGINT NOT NULL,
  `category` BIGINT NOT NULL,
  `state_a` BIGINT NOT NULL DEFAULT 0,
  `state_b` BIGINT NOT NULL DEFAULT 0,
  `enable_flag` BIGINT NOT NULL DEFAULT 0,
  `group12_active_flag` BIGINT NOT NULL DEFAULT 0,
  `empty_marker` BIGINT NOT NULL DEFAULT -1,
  `last_selection_tick` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, category)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo_slot_group_entries` (
  `character_id` BIGINT NOT NULL,
  `group_id` BIGINT NOT NULL,
  `category` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key_or_item_id` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `value_width_bits` BIGINT NOT NULL DEFAULT 32,
  `source_noti_type` BIGINT NOT NULL DEFAULT 35,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_id, category, sort_order),
  KEY `idx_character_userinfo_slot_group_lookup` (character_id, category, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo23_scalar_values` (
  `character_id` BIGINT NOT NULL,
  `family` VARCHAR(255) NOT NULL,
  `scalar_index` BIGINT NOT NULL,
  `source_order` BIGINT NOT NULL DEFAULT 0,
  `byte_offset` BIGINT NOT NULL DEFAULT -1,
  `value` BIGINT NOT NULL DEFAULT 0,
  `width_bits` BIGINT NOT NULL DEFAULT 32,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, family, scalar_index),
  KEY `idx_character_userinfo23_scalar_order` (character_id, source_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo23_fixed_values` (
  `character_id` BIGINT NOT NULL,
  `section` VARCHAR(255) NOT NULL,
  `slot_index` BIGINT NOT NULL,
  `source_order` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, section, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo23_pair_entries` (
  `character_id` BIGINT NOT NULL,
  `section` VARCHAR(255) NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `entry_key` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, section, sort_order),
  KEY `idx_character_userinfo23_pair_section` (character_id, section, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo23_object_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `object_or_slot_id` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `value_d` BIGINT NOT NULL DEFAULT 0,
  `value_e` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo5b_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo5b_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `value_d` BIGINT NOT NULL DEFAULT 0,
  `value_e` BIGINT NOT NULL DEFAULT 0,
  `value_f` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo5c_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_a` BIGINT NOT NULL DEFAULT 0,
  `header_b` BIGINT NOT NULL DEFAULT 0,
  `state0` BIGINT NOT NULL DEFAULT 0,
  `state1` BIGINT NOT NULL DEFAULT 0,
  `state2` BIGINT NOT NULL DEFAULT 0,
  `state3` BIGINT NOT NULL DEFAULT 0,
  `state4` BIGINT NOT NULL DEFAULT 0,
  `state5` BIGINT NOT NULL DEFAULT 0,
  `state6` BIGINT NOT NULL DEFAULT 0,
  `state7` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo57_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `field_a` BIGINT NOT NULL DEFAULT 0,
  `route_or_index` BIGINT NOT NULL DEFAULT 0,
  `field_c` BIGINT NOT NULL DEFAULT 0,
  `state` BIGINT NOT NULL DEFAULT 0,
  `value32` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo57_slots` (
  `character_id` BIGINT NOT NULL,
  `row_sort_order` BIGINT NOT NULL DEFAULT 0,
  `slot_index` BIGINT NOT NULL,
  `mode` BIGINT NOT NULL DEFAULT 255,
  `value` BIGINT NOT NULL DEFAULT 65535,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, row_sort_order, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo58_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `state` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo59_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo59_slots` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `category` BIGINT NOT NULL,
  `mode` BIGINT NOT NULL DEFAULT 255,
  `value` BIGINT NOT NULL DEFAULT 65535,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo5f_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `category` BIGINT NOT NULL DEFAULT 0,
  `mode_or_apply_flag` BIGINT NOT NULL DEFAULT 0,
  `scale_or_visual_flag` BIGINT NOT NULL DEFAULT 0,
  `delta_value` BIGINT NOT NULL DEFAULT 0,
  `existing_slot_value` BIGINT NOT NULL DEFAULT 65535,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo6a_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo6b_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo73_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `state` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 65535,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo7a_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo7a_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key_a` BIGINT NOT NULL DEFAULT 0,
  `key_b` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo81_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo83_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo85_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo86_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key_value` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo86_children` (
  `character_id` BIGINT NOT NULL,
  `row_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `child_key` BIGINT NOT NULL DEFAULT 0,
  `state` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, row_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo87_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo88_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo89_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `state` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo154_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `key_a` BIGINT NOT NULL DEFAULT 0,
  `slot_or_value_a` BIGINT NOT NULL DEFAULT 0,
  `key_b` BIGINT NOT NULL DEFAULT 0,
  `delta` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo159_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `slot` BIGINT NOT NULL DEFAULT 255,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo80_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `actor_key` BIGINT NOT NULL DEFAULT 0,
  `profile_a` BIGINT NOT NULL DEFAULT 0,
  `profile_b` BIGINT NOT NULL DEFAULT 0,
  `route` BIGINT NOT NULL DEFAULT 0,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo80_slots` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `category` BIGINT NOT NULL,
  `mode` BIGINT NOT NULL DEFAULT 255,
  `value` BIGINT NOT NULL DEFAULT 65535,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order),
  KEY `idx_character_userinfo80_slots_category` (character_id, category, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo80_extra_words` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo8f_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `context_key` BIGINT NOT NULL DEFAULT 0,
  `root_value` BIGINT NOT NULL DEFAULT 0,
  `header_value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo8f_list_a` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key_value` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `bool_flag` BIGINT NOT NULL DEFAULT 0,
  `state` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo8f_list_b` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_type` BIGINT NOT NULL DEFAULT 0,
  `key_a` BIGINT NOT NULL DEFAULT 0,
  `key_b` BIGINT NOT NULL DEFAULT 0,
  `key_c` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo90_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 1,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `include_primary_block` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo90_text_rows` (
  `character_id` BIGINT NOT NULL,
  `is_primary` BIGINT NOT NULL DEFAULT 0,
  `group_index` BIGINT NOT NULL DEFAULT -1,
  `slot_index` BIGINT NOT NULL DEFAULT 0,
  `text_value` VARCHAR(255) NOT NULL DEFAULT '',
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, is_primary, group_index, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo90_summaries` (
  `character_id` BIGINT NOT NULL,
  `is_primary` BIGINT NOT NULL DEFAULT 0,
  `group_index` BIGINT NOT NULL DEFAULT -1,
  `summary_word` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, is_primary, group_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo91_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo91_rows` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key_value` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_index, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo92_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `mode_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo98_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header` BIGINT NOT NULL DEFAULT 0,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `state0` BIGINT NOT NULL DEFAULT 0,
  `state1` BIGINT NOT NULL DEFAULT 0,
  `state2` BIGINT NOT NULL DEFAULT 0,
  `state3` BIGINT NOT NULL DEFAULT 0,
  `apply_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo9b_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoa0_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoa0_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `selector` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoa1_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `value3` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoa2_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoa3_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoaa_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfob0_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `enabled` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfob6_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `text_a` VARCHAR(255) NOT NULL DEFAULT '',
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `flag_c` BIGINT NOT NULL DEFAULT 0,
  `text_b` VARCHAR(255) NOT NULL DEFAULT '',
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfob6_values` (
  `character_id` BIGINT NOT NULL,
  `row_sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_index` BIGINT NOT NULL,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, row_sort_order, value_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfobc_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `state` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoc8_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `delta` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoc9_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `value3` BIGINT NOT NULL DEFAULT 0,
  `value4` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfocf_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo60_pairs` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo60_wide_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key` BIGINT NOT NULL DEFAULT 0,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `value3` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo64_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo67_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod0_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `value3` BIGINT NOT NULL DEFAULT 0,
  `value4` BIGINT NOT NULL DEFAULT 0,
  `value5` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod1_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_a` BIGINT NOT NULL DEFAULT 0,
  `header_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod1_rows` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_index, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod2_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `tail_value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod2_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_type` BIGINT NOT NULL DEFAULT 0,
  `context_key` BIGINT NOT NULL DEFAULT 0,
  `key` BIGINT NOT NULL DEFAULT 0,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `value_d` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod3_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_a` BIGINT NOT NULL DEFAULT 0,
  `header_b` BIGINT NOT NULL DEFAULT 0,
  `header_value` BIGINT NOT NULL DEFAULT 0,
  `global_flag` BIGINT NOT NULL DEFAULT 0,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `extra_value` BIGINT NOT NULL DEFAULT 0,
  `tail_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod3_primary_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `word0` BIGINT NOT NULL DEFAULT 0,
  `byte0` BIGINT NOT NULL DEFAULT 0,
  `byte1` BIGINT NOT NULL DEFAULT 0,
  `word1` BIGINT NOT NULL DEFAULT 0,
  `word2` BIGINT NOT NULL DEFAULT 0,
  `word3` BIGINT NOT NULL DEFAULT 0,
  `byte2` BIGINT NOT NULL DEFAULT 0,
  `word4` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `byte3` BIGINT NOT NULL DEFAULT 0,
  `byte4` BIGINT NOT NULL DEFAULT 0,
  `bool_flag` BIGINT NOT NULL DEFAULT 0,
  `byte5` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod3_secondary_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_type` BIGINT NOT NULL DEFAULT 0,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `word0` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `word1` BIGINT NOT NULL DEFAULT 0,
  `word2` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod5_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod6_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod7_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod8_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfod8_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfodc_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfodd_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfodf_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `value3` BIGINT NOT NULL DEFAULT 0,
  `value4` BIGINT NOT NULL DEFAULT 0,
  `value5` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoe0_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoe0_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `text` VARCHAR(255) NOT NULL DEFAULT '',
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoe6_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoeb_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfofe_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfoff_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo109_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `value3` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo10c_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo117_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo118_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key` BIGINT NOT NULL DEFAULT 0,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `value_d` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo11d_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo126_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `text` VARCHAR(255) NOT NULL DEFAULT '',
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo12a_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo17c_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo17c_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `selector` BIGINT NOT NULL DEFAULT 0,
  `item_or_key` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo182_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `actor_key` BIGINT NOT NULL DEFAULT 0,
  `header_flag` BIGINT NOT NULL DEFAULT 0,
  `outer_count` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo182_groups` (
  `character_id` BIGINT NOT NULL,
  `phase` BIGINT NOT NULL DEFAULT 0,
  `group_index` BIGINT NOT NULL DEFAULT 0,
  `group_flag` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, phase, group_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo182_first_rows` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_state` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_index, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo182_first_values` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL DEFAULT 0,
  `row_sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_index` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_index, row_sort_order, value_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo182_second_values` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL DEFAULT 0,
  `value_index` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_index, value_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo183_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_a` BIGINT NOT NULL DEFAULT 0,
  `header_b` BIGINT NOT NULL DEFAULT 0,
  `header_value` BIGINT NOT NULL DEFAULT 0,
  `global_flag` BIGINT NOT NULL DEFAULT 0,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `extra_value` BIGINT NOT NULL DEFAULT 0,
  `tail_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo183_primary_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `word0` BIGINT NOT NULL DEFAULT 0,
  `key_or_value` BIGINT NOT NULL DEFAULT 0,
  `word1` BIGINT NOT NULL DEFAULT 0,
  `word2` BIGINT NOT NULL DEFAULT 0,
  `flag0` BIGINT NOT NULL DEFAULT 0,
  `flag1` BIGINT NOT NULL DEFAULT 0,
  `bool_flag` BIGINT NOT NULL DEFAULT 0,
  `flag2` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo183_secondary_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_type` BIGINT NOT NULL DEFAULT 0,
  `value0` BIGINT NOT NULL DEFAULT 0,
  `value1` BIGINT NOT NULL DEFAULT 0,
  `value2` BIGINT NOT NULL DEFAULT 0,
  `word0` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `word1` BIGINT NOT NULL DEFAULT 0,
  `word2` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo184_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo184_first_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo184_second_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo184_third_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo1bf_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `refresh_flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo1bf_groups` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `context_state` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo1bf_selectors` (
  `character_id` BIGINT NOT NULL,
  `group_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `selector` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo1bf_values` (
  `character_id` BIGINT NOT NULL,
  `group_sort_order` BIGINT NOT NULL DEFAULT 0,
  `selector_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_sort_order, selector_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo327_blobs` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `blob_key` BIGINT NOT NULL DEFAULT 0,
  `payload` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo329_targets` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `target_key` BIGINT NOT NULL DEFAULT 0,
  `refresh_flag` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo329_groups` (
  `character_id` BIGINT NOT NULL,
  `target_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `context_state` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, target_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo329_selectors` (
  `character_id` BIGINT NOT NULL,
  `target_sort_order` BIGINT NOT NULL DEFAULT 0,
  `group_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `selector` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, target_sort_order, group_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo329_values` (
  `character_id` BIGINT NOT NULL,
  `target_sort_order` BIGINT NOT NULL DEFAULT 0,
  `group_sort_order` BIGINT NOT NULL DEFAULT 0,
  `selector_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, target_sort_order, group_sort_order, selector_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34b_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `state` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34c_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `word` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34c_first_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `key` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `flag_c` BIGINT NOT NULL DEFAULT 0,
  `flag_d` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34c_second_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_type` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `word_c` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34d_control` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34d_rows` (
  `character_id` BIGINT NOT NULL,
  `group_index` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_index, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34e_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `state` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo_fixed_raws` (
  `character_id` BIGINT NOT NULL,
  `noti_type` BIGINT NOT NULL,
  `payload` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, noti_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo_byte_count_raw_rows` (
  `character_id` BIGINT NOT NULL,
  `noti_type` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `payload` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, noti_type, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo22d_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `word_c` BIGINT NOT NULL DEFAULT 0,
  `word_d` BIGINT NOT NULL DEFAULT 0,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo22e_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `object_key` BIGINT NOT NULL DEFAULT 0,
  `mode` BIGINT NOT NULL DEFAULT 0,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo22e_pairs` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo237_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo238_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo253_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `tail_word_a` BIGINT NOT NULL DEFAULT 0,
  `tail_word_b` BIGINT NOT NULL DEFAULT 0,
  `tail_flag` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo253_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo254_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo254_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo255_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo25b_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag_a` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `flag_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo26e_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo274_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo275_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `word_c` BIGINT NOT NULL DEFAULT 0,
  `word_d` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo276_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo287_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo287_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `name` VARCHAR(255) NOT NULL DEFAULT '',
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `packed_flag` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `text` VARCHAR(255) NOT NULL DEFAULT '',
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `value_d` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `byte_c` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo287_extras` (
  `character_id` BIGINT NOT NULL,
  `row_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `extra_index` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, row_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo28a_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `tail_value_a` BIGINT NOT NULL DEFAULT 0,
  `tail_value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo28a_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_type` BIGINT NOT NULL DEFAULT 0,
  `text` VARCHAR(255) NOT NULL DEFAULT '',
  `word` BIGINT NOT NULL DEFAULT 0,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `packed_flag` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo28b_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `word` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo29f_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo29f_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `category` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2a9_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header_word` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2a9_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2a9_values` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2aa_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2b0_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `key` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2bc_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2c1_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2d2_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `value_c` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2d2_groups` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `group_key` BIGINT NOT NULL DEFAULT 0,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2d2_rows` (
  `character_id` BIGINT NOT NULL,
  `group_sort_order` BIGINT NOT NULL DEFAULT 0,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2d2_pairs` (
  `character_id` BIGINT NOT NULL,
  `group_sort_order` BIGINT NOT NULL DEFAULT 0,
  `row_sort_order` BIGINT NOT NULL DEFAULT 0,
  `pair_kind` VARCHAR(255) NOT NULL DEFAULT 'first',
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `word` BIGINT NOT NULL DEFAULT 0,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_sort_order, row_sort_order, pair_kind, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2d3_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2d8_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo2ef_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo31d_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo324_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `text` VARCHAR(255) NOT NULL DEFAULT '',
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `byte_c` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo336_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo336_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `text` VARCHAR(255) NOT NULL DEFAULT '',
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `byte_c` BIGINT NOT NULL DEFAULT 0,
  `packed_flag` BIGINT NOT NULL DEFAULT 0,
  `byte_d` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34c_text_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `category` BIGINT NOT NULL DEFAULT 0,
  `text` VARCHAR(255) NOT NULL DEFAULT '',
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34d_value_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo34e_byte_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo352_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo354_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `word0` BIGINT NOT NULL DEFAULT 0,
  `word1` BIGINT NOT NULL DEFAULT 0,
  `word2` BIGINT NOT NULL DEFAULT 0,
  `word3` BIGINT NOT NULL DEFAULT 0,
  `word4` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo355_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo359_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo359_groups` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `group_key` BIGINT NOT NULL DEFAULT 0,
  `raw0` LONGBLOB NOT NULL,
  `raw1` LONGBLOB NOT NULL,
  `raw2` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo36b_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo36b_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `kind` BIGINT NOT NULL DEFAULT 0,
  `byte_a` BIGINT NOT NULL DEFAULT 0,
  `byte_b` BIGINT NOT NULL DEFAULT 0,
  `byte_c` BIGINT NOT NULL DEFAULT 0,
  `word_a` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `byte_d` BIGINT NOT NULL DEFAULT 0,
  `word_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo37b_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `raw124` LONGBLOB NOT NULL,
  `raw64` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo393_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo393_rows` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `value_a` BIGINT NOT NULL DEFAULT 0,
  `value_b` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo3cd_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag` BIGINT NOT NULL DEFAULT 0,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo3e6_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `value` BIGINT NOT NULL DEFAULT 0,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo374_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo374_rows` (
  `character_id` BIGINT NOT NULL,
  `group_kind` VARCHAR(255) NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `payload` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_kind, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo379_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo379_rows` (
  `character_id` BIGINT NOT NULL,
  `group_kind` VARCHAR(255) NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `payload` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_kind, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo37a_state` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `header1` LONGBLOB NOT NULL,
  `header33` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_userinfo37a_rows` (
  `character_id` BIGINT NOT NULL,
  `group_kind` VARCHAR(255) NOT NULL,
  `sort_order` BIGINT NOT NULL DEFAULT 0,
  `payload` LONGBLOB NOT NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (character_id, group_kind, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_dimensions` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `dim_key` BIGINT NOT NULL,
  `val1` BIGINT NOT NULL DEFAULT 0,
  `val2` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_dimension_flags` (
  `character_id` BIGINT NOT NULL PRIMARY KEY,
  `flag1` BIGINT NOT NULL DEFAULT 0,
  `flag2` BIGINT NOT NULL DEFAULT 0,
  `flag3` BIGINT NOT NULL DEFAULT 0,
  `flag4` BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_pvp_results` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `value_u32` BIGINT NOT NULL DEFAULT 0,
  `value_u16a` BIGINT NOT NULL DEFAULT 0,
  `value_u16b` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_abuse_values` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `abuse_value` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_character_sort_item_locks` (
  `character_id` BIGINT NOT NULL,
  `sort_order` BIGINT NOT NULL,
  `list_type` BIGINT NOT NULL,
  `slot_index` BIGINT NOT NULL,
  `state` BIGINT NOT NULL,
  PRIMARY KEY (character_id, sort_order),
  UNIQUE(character_id, list_type, slot_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_account_settings` (
  `account_id` BIGINT NOT NULL PRIMARY KEY,
  `main_game_option` LONGBLOB,
  `quickchat_bank0` LONGBLOB,
  `quickchat_bank1` LONGBLOB,
  `hotkey_key_type` BIGINT NOT NULL DEFAULT 0,
  `hotkey_slots` LONGBLOB
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO `dnf_s9999_w01`.`dnf_legacy_accounts` (account_id, m_id, password_hash) VALUES
    (1, '10038', '');

INSERT IGNORE INTO `dnf_s9999_w01`.`dnf_legacy_account_cargo_state` (account_id, selection_key, value32) VALUES
    (1, 16, 0);
