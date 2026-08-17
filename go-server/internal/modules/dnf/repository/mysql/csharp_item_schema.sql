PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS accounts (
    account_id     INTEGER PRIMARY KEY AUTOINCREMENT,
    m_id           TEXT    NOT NULL UNIQUE,
    password_hash  TEXT    NOT NULL DEFAULT '',
    last_login_ip  TEXT    NOT NULL DEFAULT '',
    last_login_at  TEXT,
    created_at     TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cera           INTEGER NOT NULL DEFAULT 0,
    token_cera     INTEGER NOT NULL DEFAULT 0,
    happy_token_cera INTEGER NOT NULL DEFAULT 0,
    lucky_star     INTEGER NOT NULL DEFAULT 0,
    cube_black     INTEGER NOT NULL DEFAULT 0,
    cube_white     INTEGER NOT NULL DEFAULT 0,
    cube_red       INTEGER NOT NULL DEFAULT 0,
    cube_blue      INTEGER NOT NULL DEFAULT 0,
    cube_clear     INTEGER NOT NULL DEFAULT 0,
    cube_gold      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS characters (
    character_id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    job INTEGER NOT NULL DEFAULT 0,
    grow_type INTEGER NOT NULL DEFAULT 0,
    level INTEGER NOT NULL DEFAULT 1,
    exp INTEGER NOT NULL DEFAULT 0,
    ex_equip_slot_stat INTEGER NOT NULL DEFAULT 0,
    bonus_sp INTEGER NOT NULL DEFAULT 0,
    bonus_tp INTEGER NOT NULL DEFAULT 0,
    pvp_grade INTEGER NOT NULL DEFAULT 0,
    pvp_rating_grade INTEGER NOT NULL DEFAULT 0,
    user_state INTEGER NOT NULL DEFAULT 0,
    gold INTEGER NOT NULL DEFAULT 0,
    coin INTEGER NOT NULL DEFAULT 0,
    town_id INTEGER NOT NULL DEFAULT 0,
    area_id INTEGER NOT NULL DEFAULT 0,
    pos_x INTEGER NOT NULL DEFAULT 0,
    pos_y INTEGER NOT NULL DEFAULT 0,
    direction INTEGER NOT NULL DEFAULT 5,
    area_state INTEGER NOT NULL DEFAULT 3,
    name_bytes BLOB,
    appearance_blob BLOB,
    delete_flag INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_characters_name_unique
    ON characters(name);

CREATE INDEX IF NOT EXISTS idx_characters_account
    ON characters(account_id, delete_flag);

CREATE TABLE IF NOT EXISTS character_container_state (
    character_id INTEGER NOT NULL,
    list_type INTEGER NOT NULL,
    list_param16 INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, list_type),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_items (
    item_uid INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_scope TEXT NOT NULL CHECK (owner_scope IN ('character', 'account')),
    owner_id INTEGER NOT NULL,
    character_id INTEGER,
    list_type INTEGER NOT NULL,
    slot_index INTEGER NOT NULL,
    item_template_id INTEGER NOT NULL,
    item_kind TEXT NOT NULL DEFAULT 'unknown' CHECK (item_kind IN ('unknown', 'stackable', 'equipment', 'avatar', 'pet', 'special')),
    stack_count INTEGER NOT NULL DEFAULT 0,
    instance_value INTEGER NOT NULL DEFAULT 0,
    durability INTEGER NOT NULL DEFAULT 0,
    seal_flag INTEGER NOT NULL DEFAULT 0,
    option_value INTEGER NOT NULL DEFAULT 0,
    expire_time INTEGER NOT NULL DEFAULT 0,
    marker_16 INTEGER NOT NULL DEFAULT 0,
    pet_serial_or_handle INTEGER NOT NULL DEFAULT 0,
    extra_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(owner_scope, owner_id, list_type, slot_index, item_kind),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_character_items_owner_container
    ON character_items(owner_scope, owner_id, list_type, slot_index);

CREATE INDEX IF NOT EXISTS idx_character_items_template
    ON character_items(item_template_id);

CREATE INDEX IF NOT EXISTS idx_character_items_character
    ON character_items(character_id, list_type, slot_index);

CREATE TABLE IF NOT EXISTS account_cargo_state (
    account_id INTEGER PRIMARY KEY,
    selection_key INTEGER NOT NULL DEFAULT 0,
    value32 INTEGER NOT NULL DEFAULT 0,
    item_count INTEGER NOT NULL DEFAULT 0, -- 此前只在 RunMigrations 的 ALTER 里, 空库全新 CREATE 不走迁移 → 选角查询崩(2026-06-11 修)
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS item_audit_log (
    audit_id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_scope TEXT NOT NULL,
    owner_id INTEGER NOT NULL,
    character_id INTEGER,
    action_name TEXT NOT NULL,
    list_type INTEGER,
    slot_index INTEGER,
    item_uid INTEGER,
    item_template_id INTEGER,
    delta_stack_count INTEGER NOT NULL DEFAULT 0,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS character_skills (
    character_id INTEGER NOT NULL,
    page_index INTEGER NOT NULL,
    page_header INTEGER NOT NULL DEFAULT 0,
    slot INTEGER NOT NULL,
    skill_id INTEGER NOT NULL,
    level INTEGER NOT NULL DEFAULT 0,
    extra_values BLOB,
    PRIMARY KEY (character_id, page_index, slot),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_skill_tail (
    character_id INTEGER PRIMARY KEY,
    tail0 INTEGER NOT NULL DEFAULT 0,
    tail1 INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_skill_points (
    character_id INTEGER PRIMARY KEY,
    total_sp INTEGER NOT NULL DEFAULT 0,
    remaining_sp INTEGER NOT NULL DEFAULT 0,
    total_sfp INTEGER NOT NULL DEFAULT 0,
    remaining_sfp INTEGER NOT NULL DEFAULT 0,
    total_tp INTEGER NOT NULL DEFAULT 0,
    remaining_tp INTEGER NOT NULL DEFAULT 0,
    synced_level INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_creatures (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    creature_key INTEGER NOT NULL,
    field04 INTEGER NOT NULL DEFAULT 0,
    mode_flag INTEGER NOT NULL DEFAULT 0,
    progress_value INTEGER NOT NULL DEFAULT 0,
    mode1_field0a INTEGER NOT NULL DEFAULT 0,
    mode1_field0b INTEGER NOT NULL DEFAULT 0,
    field_after_value INTEGER NOT NULL DEFAULT 0,
    creature_text BLOB,
    tail_flag INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_init_flags (
    character_id INTEGER PRIMARY KEY,
    shop_coin_event_flag INTEGER NOT NULL DEFAULT 0,
    level60_ui_state INTEGER NOT NULL DEFAULT 0,
    pc_room_state INTEGER NOT NULL DEFAULT 0,
    expert_job_blob BLOB,
    champion_break_blob BLOB,
    boss_tower_placeholder INTEGER NOT NULL DEFAULT 0,
    mailbox_loaded_count INTEGER NOT NULL DEFAULT 0,
    mailbox_mode INTEGER NOT NULL DEFAULT 0,
    mailbox_not_loaded_count INTEGER NOT NULL DEFAULT 0,
    mailbox_unknown_count_c INTEGER NOT NULL DEFAULT 0,
    event_info_tail_byte INTEGER NOT NULL DEFAULT 0,
    hotkey_key_type INTEGER NOT NULL DEFAULT 0,
    main_game_option_blob BLOB,
    quickchat_bank0 BLOB,
    quickchat_bank1 BLOB,
    charac_invisible_falgs_payload_len INTEGER NOT NULL DEFAULT 0,  -- IDA 正名: CLEAR_QUEST_LIST payload 长度
    racing_dungeon_current_enter_count INTEGER NOT NULL DEFAULT 0,  -- IDA 正名: DAILY_CHALLENGE 当日进入次数
    racing_dungeon_group_flags BLOB,  -- IDA 正名: DAILY_CHALLENGE 组标志
    -- CMD 0x0004 SELECT_CHARACTER ACK 结构化字段
    ack_account_reg_time INTEGER NOT NULL DEFAULT 0,
    ack_premium_blob BLOB,           -- premiumCount(1) + N×(type(1)+endTime(8))
    ack_quest_display_ids BLOB,      -- 4×u32 (sub_A44480 消费的16B)
    ack_char_slot_index INTEGER NOT NULL DEFAULT 0,
    ack_fatigue_battery INTEGER NOT NULL DEFAULT 0,
    ack_fatigue_grownup_buff INTEGER NOT NULL DEFAULT 0,
    ack_trade_punish_flag INTEGER NOT NULL DEFAULT 0,
    ack_extra_field_86jp INTEGER NOT NULL DEFAULT 0,
    ack_reserved_8b BLOB,            -- 8B 客户端不读取但需保留
    ack_tutorial_skipable INTEGER NOT NULL DEFAULT 0,
    ack_post_tutorial_u16 INTEGER NOT NULL DEFAULT 0,
    ack_unread_tail BLOB,            -- 剩余尾部 客户端不读取
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_item_values (
    character_id INTEGER NOT NULL,
    list_kind TEXT NOT NULL,
    sort_order INTEGER NOT NULL,
    item_id INTEGER NOT NULL,
    value INTEGER NOT NULL,
    PRIMARY KEY (character_id, list_kind, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_item_locks (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    type_or_list INTEGER NOT NULL,
    item_key_or_slot INTEGER NOT NULL,
    state INTEGER NOT NULL,
    extra_value INTEGER,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_growth_weapon_stages (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    stage_id INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_show_effects (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    effect_index INTEGER NOT NULL,
    duration_seconds INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_pvp_missions (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    mission_id INTEGER NOT NULL,
    progress_value INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_dungeon_permissions (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    dungeon_id INTEGER NOT NULL,
    clear_state INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_event_info (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    repeat_event_index INTEGER NOT NULL,
    event_data BLOB,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_hotkey_slots (
    character_id INTEGER NOT NULL,
    slot_index INTEGER NOT NULL,
    hotkey_value INTEGER NOT NULL,
    PRIMARY KEY (character_id, slot_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- IDA 正名: 实际协议 NOTI 0x0164 CLEAR_QUEST_LIST(已清除任务 30000-bit bitmap)
-- 原名 character_invisible_falgs 是早期误判，保留表名避免 migration
CREATE TABLE IF NOT EXISTS character_invisible_falgs (
    character_id INTEGER NOT NULL,
    slot_index INTEGER NOT NULL,
    flag_value INTEGER NOT NULL,
    PRIMARY KEY (character_id, slot_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- IDA 正名: character_racing_dungeon_* 实际协议 NOTI 0x0286 DAILY_CHALLENGE(每日挑战)
CREATE TABLE IF NOT EXISTS character_racing_dungeon_groups (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL,
    group_id INTEGER NOT NULL,
    PRIMARY KEY (character_id, group_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_racing_dungeon_entries (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL,
    entry_index INTEGER NOT NULL,
    track_like_id INTEGER NOT NULL,
    value_a INTEGER NOT NULL,
    value_b INTEGER NOT NULL,
    PRIMARY KEY (character_id, group_index, entry_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_racing_dungeon_tail_ids (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    id_value INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_achievement_complete (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    achievement_id INTEGER NOT NULL,
    p1 INTEGER NOT NULL DEFAULT 0,
    p2 INTEGER NOT NULL DEFAULT 0,
    p3 INTEGER NOT NULL DEFAULT 0,
    p4 INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- IDA 正名: 实际协议 NOTI 0x0166 TITLE_BOOK_LIST(称号簿, 非成就)
-- 22B/entry: titleId + flag + 时间戳, PVF titlebook/ 交叉验证
CREATE TABLE IF NOT EXISTS character_achievement_chunks (
    character_id INTEGER NOT NULL,
    chunk_index INTEGER NOT NULL,
    mode_byte INTEGER NOT NULL DEFAULT 0,
    owner_id16 INTEGER NOT NULL DEFAULT 0,
    entries_blob BLOB,
    PRIMARY KEY (character_id, chunk_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- IDA 正名: 实际协议 NOTI 0x02D5 DAILYSCHEDULE_CONTENTS_STATE(每日副本计费状态)
CREATE TABLE IF NOT EXISTS character_unknown725 (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    param_a INTEGER NOT NULL,
    mode_or_state INTEGER NOT NULL,
    content_id INTEGER NOT NULL,
    param_b INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- IDA 正名: 实际协议 NOTI 0x02DA BUY_RESTRICT_ITEM_LIST(限购物品列表)
CREATE TABLE IF NOT EXISTS character_unknown730 (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    entry_id INTEGER NOT NULL,
    sentinel_or_value INTEGER NOT NULL,
    flag INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo_blobs (
    character_id INTEGER NOT NULL,
    blob_kind TEXT NOT NULL,
    subtype INTEGER NOT NULL,
    user_info_type INTEGER NOT NULL DEFAULT 0,
    gate_or_count INTEGER NOT NULL DEFAULT 0,
    user_id INTEGER NOT NULL DEFAULT 0,
    name_bytes BLOB,
    remaining_bytes BLOB,
    PRIMARY KEY (character_id, blob_kind, subtype),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS global_raw_packets (
    noti_type INTEGER PRIMARY KEY,
    packet_body BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS get_userinfo_template (
    id INTEGER PRIMARY KEY DEFAULT 1,
    seed_character_id INTEGER NOT NULL DEFAULT 1000,
    response_blob BLOB,
    pkt0_routing_byte7 INTEGER NOT NULL DEFAULT 0,
    gate_or_count1 INTEGER NOT NULL DEFAULT 32,
    gate_or_count2 INTEGER NOT NULL DEFAULT 32,
    flag_or_manage INTEGER NOT NULL DEFAULT 2,
    key_or_point INTEGER NOT NULL DEFAULT 0,
    unknown16 INTEGER NOT NULL DEFAULT 0,
    unknown32 INTEGER NOT NULL DEFAULT 0,
    pkt2_result_code INTEGER NOT NULL DEFAULT 1,
    pkt2_character_key INTEGER NOT NULL DEFAULT 0,
    pkt2_slot_flag1 INTEGER NOT NULL DEFAULT 0,
    pkt2_slot_flag2 INTEGER NOT NULL DEFAULT 1,
    pkt2_state_flag INTEGER NOT NULL DEFAULT 255,
    pkt2_flag3 INTEGER NOT NULL DEFAULT 1,
    pkt2_reserved INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS account_character_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_index INTEGER NOT NULL,
    slot_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    name_bytes BLOB,
    body_after_name BLOB NOT NULL,
    UNIQUE(entry_index)
);

CREATE TABLE IF NOT EXISTS getuserinfo_extra_packets (
    seq INTEGER PRIMARY KEY,
    command INTEGER NOT NULL,
    noti_type INTEGER NOT NULL,
    body BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS packet_sequence (
    character_id INTEGER NOT NULL,
    seq_index INTEGER NOT NULL,
    command INTEGER NOT NULL,
    noti_type INTEGER NOT NULL,
    kind INTEGER NOT NULL DEFAULT 0,
    item_list_type INTEGER NOT NULL DEFAULT -1,
    occurrence_index INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, seq_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS packet_templates (
    character_id INTEGER NOT NULL,
    command INTEGER NOT NULL,
    noti_type INTEGER NOT NULL,
    occurrence_index INTEGER NOT NULL DEFAULT 0,
    body BLOB NOT NULL,
    body_length INTEGER NOT NULL,
    PRIMARY KEY (character_id, command, noti_type, occurrence_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS equipped_items (
    character_id INTEGER NOT NULL,
    equip_list_blob BLOB NOT NULL,
    PRIMARY KEY (character_id),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS unequipped_entries (
    character_id INTEGER NOT NULL,
    item_template_id INTEGER NOT NULL,
    raw_entry BLOB NOT NULL,
    PRIMARY KEY (character_id, item_template_id),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS global_server_event_phase (
    id INTEGER PRIMARY KEY,
    event_phase_bitmap BLOB NOT NULL,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- USERINFO subtype1 动态化: 结构化字段表(替代 equipped_items.equip_list_blob 整块 blob)

-- 进游戏 init 流的每包独立存储(替代 packet_templates 的混合大表)
-- 种子从 packet_templates 迁移; 新角色按需 INSERT 默认值
CREATE TABLE IF NOT EXISTS character_init_bodies (
    character_id INTEGER NOT NULL,
    noti_type INTEGER NOT NULL,
    occurrence_index INTEGER NOT NULL DEFAULT 0,
    body BLOB NOT NULL,
    PRIMARY KEY (character_id, noti_type, occurrence_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- NOTI 0x0002 subtype 0 (USERINFO Minimum) 104B tail 的结构化字段。
-- 布局: Reverse/INIT_PACKET/0x0002_USERINFO_SUBTYPE0.md (IDA readUserInfoMinimum 0xF55490 逐 PacketPop 验证)
-- 不入表的字段: isAlive(+38,恒1) / 86jp_reserved(+46..+52,客户端 dead store) / isOver14(+70,恒100)
--               progressA/B(+57/+61) 与 skillTreeIndex(+79) 同源 character_subtype1_fields (客户端同 obj 偏移 0x394/0x398)
CREATE TABLE IF NOT EXISTS character_subtype0_fields (
    character_id INTEGER PRIMARY KEY,
    name_tag_item_id INTEGER NOT NULL DEFAULT 0,        -- +0  u32 名称装饰卡 itemId → vfunc+20 (语义已解2026-06-10: 100330501=[name tag]模板"我在恋爱")
    creature_field1 INTEGER NOT NULL DEFAULT 0,         -- +4  u8
    creature_field2 INTEGER NOT NULL DEFAULT 0,         -- +5  u8
    creature_field3 INTEGER NOT NULL DEFAULT 0,         -- +6  u8 (客户端读后未用)
    creature_field4 INTEGER NOT NULL DEFAULT 0,         -- +7  u8 (客户端读后未用)
    creature_buffer BLOB,                               -- +8  8B i64; low32!=0 → 创建宠物实体到 slot 24 (sub_F55120)
    stamina INTEGER NOT NULL DEFAULT 0,                 -- +16 u8  体力 (readEntryByteOffset648)
    fatigue_penalty INTEGER NOT NULL DEFAULT 0,         -- +17 u32 疲劳恢复惩罚 (readEntryDwordOffset672)
    is_event_character INTEGER NOT NULL DEFAULT 0,      -- +21 u8
    pc_room_id INTEGER NOT NULL DEFAULT 65537,          -- +22 u32 (sub_F502B0; 真机无PC房=0x00010001)
    is_private_store INTEGER NOT NULL DEFAULT 0,        -- +26 u8
    is_premium_pc_room INTEGER NOT NULL DEFAULT 0,      -- +27 u8
    server_group_id INTEGER NOT NULL DEFAULT 0,         -- +28 u8 (readEntryByteOffset704)
    black_count INTEGER NOT NULL DEFAULT 0,             -- +29 u32
    guild_level INTEGER NOT NULL DEFAULT 0,             -- +33 u8 (sub_F51710)
    chaos_point INTEGER NOT NULL DEFAULT 0,             -- +34 u32
    disguise_kind INTEGER NOT NULL DEFAULT 0,           -- +39 u8 (sub_F53450)
    is_disguised INTEGER NOT NULL DEFAULT 0,            -- +40 u8
    expert_job_type INTEGER NOT NULL DEFAULT 0,         -- +41 u8  副职业类型 (sub_F51830)
    expert_job_exp INTEGER NOT NULL DEFAULT 0,          -- +42 u32 副职业经验
    extra46 INTEGER NOT NULL DEFAULT 0,                 -- +46 u8  subtype0 tail offset-preserved, semantic pending
    extra47 INTEGER NOT NULL DEFAULT 0,                 -- +47 u32 subtype0 tail offset-preserved, semantic pending
    extra51 INTEGER NOT NULL DEFAULT 0,                 -- +51 u16 subtype0 tail offset-preserved, semantic pending
    is_hardcore_mode INTEGER NOT NULL DEFAULT 0,        -- +53 u8 (readHardcoreMinimum)
    is_hardcore_dead INTEGER NOT NULL DEFAULT 0,        -- +54 u8
    hardcore_death_count INTEGER NOT NULL DEFAULT 0,    -- +55 u16
    user_state_bits INTEGER NOT NULL DEFAULT 3,         -- +65 u8 复合位 (sub_F50340; 3=城镇可见)
    chat_ban_end_time INTEGER NOT NULL DEFAULT 0,       -- +66 u32
    fatigue_update INTEGER NOT NULL DEFAULT 0,          -- +71 u16
    return_user_flag INTEGER NOT NULL DEFAULT 1,        -- +73 u8 (sub_1FAC210; 默认1=旧builder新角色基线)
    channel_display_mode INTEGER NOT NULL DEFAULT 0,    -- +74 u16
    channel_type INTEGER NOT NULL DEFAULT 0,            -- +76 u8
    channel_id INTEGER NOT NULL DEFAULT 2,              -- +77 u16 (<1000=普通频道)
    is_return_user INTEGER NOT NULL DEFAULT 0,          -- +80 u8
    link_slot_enabled INTEGER NOT NULL DEFAULT 0,       -- +81 u8
    link_type_a INTEGER NOT NULL DEFAULT 0,             -- +82 u8 (sub_F50410)
    link_type_b INTEGER NOT NULL DEFAULT 0,             -- +83 u8
    emotion_index INTEGER NOT NULL DEFAULT 0,           -- +84 u16
    action_byte INTEGER NOT NULL DEFAULT 0,             -- +86 u8
    fatigue_display_update INTEGER NOT NULL DEFAULT 0,  -- +87 u16
    costume_flag INTEGER NOT NULL DEFAULT 0,            -- +89 u8 obj[865]
    aura_flag INTEGER NOT NULL DEFAULT 0,               -- +90 u8 obj+868
    pet_display_flag INTEGER NOT NULL DEFAULT 0,        -- +91 u8 obj+872
    title_display_flag INTEGER NOT NULL DEFAULT 0,      -- +92 u8 obj[876]
    pvp_stat_a INTEGER NOT NULL DEFAULT 0,              -- +93 u32 (sub_F50BA0)
    pvp_win_streak INTEGER NOT NULL DEFAULT 0,          -- +97 u8
    pvp_lose_streak INTEGER NOT NULL DEFAULT 0,         -- +98 u8
    pvp_rank_point INTEGER NOT NULL DEFAULT 0,          -- +99 u32
    trailing_byte INTEGER NOT NULL DEFAULT 0,           -- +103 u8
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_subtype1_fields (
    character_id INTEGER PRIMARY KEY,
    stat_hp_max INTEGER NOT NULL DEFAULT 0,
    stat_mp_max INTEGER NOT NULL DEFAULT 0,
    stat_strength INTEGER NOT NULL DEFAULT 0,
    stat_intelligence INTEGER NOT NULL DEFAULT 0,
    stat_vitality INTEGER NOT NULL DEFAULT 0,
    stat_spirit INTEGER NOT NULL DEFAULT 0,
    stat_physical_attack INTEGER NOT NULL DEFAULT 0,
    stat_physical_defense INTEGER NOT NULL DEFAULT 0,
    stat_magical_attack INTEGER NOT NULL DEFAULT 0,
    stat_magical_defense INTEGER NOT NULL DEFAULT 0,
    stat_independent_attack INTEGER NOT NULL DEFAULT 0,
    stat_fire_resistance INTEGER NOT NULL DEFAULT 0,
    stat_water_resistance INTEGER NOT NULL DEFAULT 0,
    stat_dark_resistance INTEGER NOT NULL DEFAULT 0,
    stat_light_resistance INTEGER NOT NULL DEFAULT 0,
    -- u16[17] 状态异常抗性(slow/freeze/poison/stun 等, ACTIVESTATUS_TAG), wire order preserved.
    active_status_resistance_00 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_01 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_02 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_03 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_04 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_05 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_06 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_07 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_08 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_09 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_10 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_11 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_12 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_13 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_14 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_15 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_16 INTEGER NOT NULL DEFAULT 0,
    active_status_resistance_17 INTEGER NOT NULL DEFAULT 0,
    stat_inventory_limit INTEGER NOT NULL DEFAULT 0,
    stat_hp_regen_speed INTEGER NOT NULL DEFAULT 0,
    stat_mp_regen_speed INTEGER NOT NULL DEFAULT 0,
    stat_move_speed INTEGER NOT NULL DEFAULT 0,
    stat_attack_speed INTEGER NOT NULL DEFAULT 0,
    stat_cast_speed INTEGER NOT NULL DEFAULT 0,
    stat_hit_recovery INTEGER NOT NULL DEFAULT 0,
    stat_jump_power INTEGER NOT NULL DEFAULT 0,
    stat_weight INTEGER NOT NULL DEFAULT 0,
    stat_level INTEGER NOT NULL DEFAULT 0,
    name_tag_item_id INTEGER NOT NULL DEFAULT 0,     -- 名称装饰卡 itemId (sub_F546B0 i64 low32 → slot 28; 旧误名 skill_tree_check)
    name_tag_expire_time INTEGER NOT NULL DEFAULT 0, -- 名称装饰卡到期时间 (i64 high32)
    skill_tree_index INTEGER NOT NULL DEFAULT 0,
    equipped_creature_level INTEGER NOT NULL DEFAULT 0,
    equip_list_trailing INTEGER NOT NULL DEFAULT 0,
    manage_level INTEGER NOT NULL DEFAULT 0,
    flag_byte INTEGER NOT NULL DEFAULT 0,
    guild_power_war INTEGER NOT NULL DEFAULT 0,
    server_timestamp INTEGER NOT NULL DEFAULT 0,
    quest_shop_count INTEGER NOT NULL DEFAULT 0,
    progress1 INTEGER NOT NULL DEFAULT 0,
    progress2 INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_equipped_entries (
    character_id INTEGER NOT NULL,
    slot INTEGER NOT NULL,
    item_id INTEGER NOT NULL,
    expire_time INTEGER NOT NULL DEFAULT 0,
    raw_entry BLOB NOT NULL,
    PRIMARY KEY (character_id, slot),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack NOTI2 USERINFO slot families.
-- Evidence: NoPack 0x23/sub_1D4D380 writes 8 categories and groups 0/1/2;
-- 0x47/sub_1CF52B0 updates category state and group 3/4; 0x58/sub_1D7C940
-- and opcode 1026 reuse the same sub_32BC570 slot writer.
CREATE TABLE IF NOT EXISTS character_userinfo_slot_control (
    character_id INTEGER PRIMARY KEY,
    visible_category_count INTEGER NOT NULL DEFAULT 4,
    group0_entry_total INTEGER NOT NULL DEFAULT 0,
    group0_tail_refresh_value INTEGER NOT NULL DEFAULT 0,
    refresh_value_a INTEGER NOT NULL DEFAULT 0,
    refresh_value_b INTEGER NOT NULL DEFAULT 0,
    selected_category_a INTEGER NOT NULL DEFAULT -1,
    selected_category_b INTEGER NOT NULL DEFAULT -1,
    msg72_value_a INTEGER NOT NULL DEFAULT 0,
    msg72_state INTEGER NOT NULL DEFAULT 0,
    render_flag_1949 INTEGER NOT NULL DEFAULT 0,
    render_flag_1950 INTEGER NOT NULL DEFAULT 0,
    special_group_flag_1951 INTEGER NOT NULL DEFAULT 0,
    special_group_mode_1952 INTEGER NOT NULL DEFAULT 0,
    mode_bits INTEGER NOT NULL DEFAULT 0,
    tail_value_a INTEGER NOT NULL DEFAULT 0,
    tail_flag_a INTEGER NOT NULL DEFAULT 0,
    tail_bool_b INTEGER NOT NULL DEFAULT 0,
    tail_value_b INTEGER NOT NULL DEFAULT 0,
    tail_pair_a INTEGER NOT NULL DEFAULT 0,
    tail_pair_b INTEGER NOT NULL DEFAULT 0,
    tail_index6_value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo_slot_category_state (
    character_id INTEGER NOT NULL,
    category INTEGER NOT NULL CHECK (category BETWEEN 0 AND 7),
    state_a INTEGER NOT NULL DEFAULT 0,
    state_b INTEGER NOT NULL DEFAULT 0,
    enable_flag INTEGER NOT NULL DEFAULT 0,
    group12_active_flag INTEGER NOT NULL DEFAULT 0,
    empty_marker INTEGER NOT NULL DEFAULT -1,
    last_selection_tick INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, category),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo_slot_group_entries (
    character_id INTEGER NOT NULL,
    group_id INTEGER NOT NULL CHECK (group_id BETWEEN 0 AND 4),
    category INTEGER NOT NULL CHECK (category BETWEEN 0 AND 7),
    sort_order INTEGER NOT NULL DEFAULT 0,
    key_or_item_id INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    value_width_bits INTEGER NOT NULL DEFAULT 32 CHECK (value_width_bits IN (16, 32)),
    source_noti_type INTEGER NOT NULL DEFAULT 35,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_id, category, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_character_userinfo_slot_group_lookup
    ON character_userinfo_slot_group_entries(character_id, category, group_id);

-- Current NoPack NOTI2 0x23 scalar sinks.
-- Stores exact setter family/index/offset evidence without forcing unstable
-- human labels too early.
CREATE TABLE IF NOT EXISTS character_userinfo23_scalar_values (
    character_id INTEGER NOT NULL,
    family TEXT NOT NULL CHECK (family IN ('direct', 'e90', 'd90', 'db0', 'tail')),
    scalar_index INTEGER NOT NULL,
    source_order INTEGER NOT NULL DEFAULT 0,
    byte_offset INTEGER NOT NULL DEFAULT -1,
    value INTEGER NOT NULL DEFAULT 0,
    width_bits INTEGER NOT NULL DEFAULT 32 CHECK (width_bits IN (8, 16, 32)),
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, family, scalar_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_character_userinfo23_scalar_order
    ON character_userinfo23_scalar_values(character_id, source_order);

-- Current NoPack NOTI2 0x23 variable sections after scalar sinks:
-- fixed4: four u32 values consumed by sub_25E2B40(index,value)
-- pair entries: d90_ext/db0_ext/map sections
-- object rows: u16,u32,u32,u16,u8,u16 rows before slot vectors
CREATE TABLE IF NOT EXISTS character_userinfo23_fixed_values (
    character_id INTEGER NOT NULL,
    section TEXT NOT NULL,
    slot_index INTEGER NOT NULL,
    source_order INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, section, slot_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo23_pair_entries (
    character_id INTEGER NOT NULL,
    section TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    entry_key INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, section, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo23_object_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    object_or_slot_id INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    value_d INTEGER NOT NULL DEFAULT 0,
    value_e INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_character_userinfo23_pair_section
    ON character_userinfo23_pair_entries(character_id, section, sort_order);

-- Current NoPack USERINFO neighbors:
-- 0x5B: u8,u16,count*(u16,u8,u8,u8,u8,u16)
-- 0x5C: u8,u8,8*u32; older two-u32 encoders are not current-version safe.
CREATE TABLE IF NOT EXISTS character_userinfo5b_control (
    character_id INTEGER PRIMARY KEY,
    header_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo5b_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    value_d INTEGER NOT NULL DEFAULT 0,
    value_e INTEGER NOT NULL DEFAULT 0,
    value_f INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo5c_state (
    character_id INTEGER PRIMARY KEY,
    header_a INTEGER NOT NULL DEFAULT 0,
    header_b INTEGER NOT NULL DEFAULT 0,
    state0 INTEGER NOT NULL DEFAULT 0,
    state1 INTEGER NOT NULL DEFAULT 0,
    state2 INTEGER NOT NULL DEFAULT 0,
    state3 INTEGER NOT NULL DEFAULT 0,
    state4 INTEGER NOT NULL DEFAULT 0,
    state5 INTEGER NOT NULL DEFAULT 0,
    state6 INTEGER NOT NULL DEFAULT 0,
    state7 INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack 6-slot display sibling:
-- 0x57: u16 row_count; rows are u16,4*u8,u32, then 6*(u8,u16).
-- 0x58: u16 count; count*(u16,u8,u16).
-- 0x59: u16 object_key,u8 count,count*(u8,u8,u16).
-- 0x5F: u8,u8,u8,u16,u16.
-- 0x154: u32,u16,u32,u16. 0x159: u8,count*(u8,u32).
CREATE TABLE IF NOT EXISTS character_userinfo57_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    object_key INTEGER NOT NULL DEFAULT 0,
    field_a INTEGER NOT NULL DEFAULT 0,
    route_or_index INTEGER NOT NULL DEFAULT 0,
    field_c INTEGER NOT NULL DEFAULT 0,
    state INTEGER NOT NULL DEFAULT 0,
    value32 INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo57_slots (
    character_id INTEGER NOT NULL,
    row_sort_order INTEGER NOT NULL DEFAULT 0,
    slot_index INTEGER NOT NULL CHECK (slot_index BETWEEN 0 AND 5),
    mode INTEGER NOT NULL DEFAULT 255,
    value INTEGER NOT NULL DEFAULT 65535,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, row_sort_order, slot_index),
    FOREIGN KEY (character_id, row_sort_order)
        REFERENCES character_userinfo57_rows(character_id, sort_order) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo58_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    object_key INTEGER NOT NULL DEFAULT 0,
    state INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo59_control (
    character_id INTEGER PRIMARY KEY,
    object_key INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo59_slots (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    category INTEGER NOT NULL CHECK (category BETWEEN 0 AND 5),
    mode INTEGER NOT NULL DEFAULT 255,
    value INTEGER NOT NULL DEFAULT 65535,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo5f_state (
    character_id INTEGER PRIMARY KEY,
    category INTEGER NOT NULL DEFAULT 0 CHECK (category BETWEEN 0 AND 5),
    mode_or_apply_flag INTEGER NOT NULL DEFAULT 0,
    scale_or_visual_flag INTEGER NOT NULL DEFAULT 0,
    delta_value INTEGER NOT NULL DEFAULT 0,
    existing_slot_value INTEGER NOT NULL DEFAULT 65535,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo6a_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    object_key INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo6b_state (
    character_id INTEGER PRIMARY KEY,
    object_key INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo73_state (
    character_id INTEGER PRIMARY KEY,
    mode INTEGER NOT NULL DEFAULT 0,
    state INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 65535,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo7a_control (
    character_id INTEGER PRIMARY KEY,
    mode INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo7a_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    key_a INTEGER NOT NULL DEFAULT 0,
    key_b INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo81_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo83_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo85_state (
    character_id INTEGER PRIMARY KEY,
    object_key INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo86_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    key_value INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo86_children (
    character_id INTEGER NOT NULL,
    row_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    child_key INTEGER NOT NULL DEFAULT 0,
    state INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, row_sort_order, sort_order),
    FOREIGN KEY (character_id, row_sort_order)
        REFERENCES character_userinfo86_rows(character_id, sort_order) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo87_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo88_state (
    character_id INTEGER PRIMARY KEY,
    object_key INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo89_state (
    character_id INTEGER PRIMARY KEY,
    state INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo154_state (
    character_id INTEGER PRIMARY KEY,
    key_a INTEGER NOT NULL DEFAULT 0,
    slot_or_value_a INTEGER NOT NULL DEFAULT 0,
    key_b INTEGER NOT NULL DEFAULT 0,
    delta INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo159_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    slot INTEGER NOT NULL DEFAULT 255 CHECK ((slot BETWEEN 0 AND 5) OR slot = 255),
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack NOTI2 0x80 actor/profile display packet:
-- u32,u8,u8,u8,u16,u16,u8,count*(u8,u8,u16),u8,count*u16.
-- The trailing u16 list is preserved as raw words because the current client
-- consumes it for cursor alignment without a confirmed persistent state sink.
CREATE TABLE IF NOT EXISTS character_userinfo80_control (
    character_id INTEGER PRIMARY KEY,
    actor_key INTEGER NOT NULL DEFAULT 0,
    profile_a INTEGER NOT NULL DEFAULT 0,
    profile_b INTEGER NOT NULL DEFAULT 0,
    route INTEGER NOT NULL DEFAULT 0,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo80_slots (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    category INTEGER NOT NULL CHECK (category BETWEEN 0 AND 7),
    mode INTEGER NOT NULL DEFAULT 255,
    value INTEGER NOT NULL DEFAULT 65535,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_character_userinfo80_slots_category
    ON character_userinfo80_slots(character_id, category, sort_order);

CREATE TABLE IF NOT EXISTS character_userinfo80_extra_words (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack NOTI2 0x8F actor/display list snapshot:
-- u16,u32,u32,u8,countA*(u32,u16,u32,4*u8),u8,countB*(u8,3*u32,u16,u8,2*u16).
CREATE TABLE IF NOT EXISTS character_userinfo8f_control (
    character_id INTEGER PRIMARY KEY,
    context_key INTEGER NOT NULL DEFAULT 0,
    root_value INTEGER NOT NULL DEFAULT 0,
    header_value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo8f_list_a (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    key_value INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    bool_flag INTEGER NOT NULL DEFAULT 0,
    state INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo8f_list_b (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    row_type INTEGER NOT NULL DEFAULT 0,
    key_a INTEGER NOT NULL DEFAULT 0,
    key_b INTEGER NOT NULL DEFAULT 0,
    key_c INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack NOTI2 0x90 text matrix:
-- u8,u32,u32,u8,u32,u8, optional 8*(wstr,u8,u8)+summary,
-- then 5 groups of 8*(wstr,u8,u8)+summary.
CREATE TABLE IF NOT EXISTS character_userinfo90_control (
    character_id INTEGER PRIMARY KEY,
    flag_a INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 1,
    flag_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    include_primary_block INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo90_text_rows (
    character_id INTEGER NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
    group_index INTEGER NOT NULL DEFAULT -1 CHECK (group_index BETWEEN -1 AND 4),
    slot_index INTEGER NOT NULL DEFAULT 0 CHECK (slot_index BETWEEN 0 AND 7),
    text_value TEXT NOT NULL DEFAULT '',
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, is_primary, group_index, slot_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo90_summaries (
    character_id INTEGER NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
    group_index INTEGER NOT NULL DEFAULT -1 CHECK (group_index BETWEEN -1 AND 4),
    summary_word INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, is_primary, group_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack NOTI2 0x91/0x92/0x98/0x9B adjacent state packets.
CREATE TABLE IF NOT EXISTS character_userinfo91_control (
    character_id INTEGER PRIMARY KEY,
    header_value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo91_rows (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL CHECK (group_index BETWEEN 0 AND 3),
    sort_order INTEGER NOT NULL DEFAULT 0,
    key_value INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_index, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo92_state (
    character_id INTEGER PRIMARY KEY,
    mode_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo98_state (
    character_id INTEGER PRIMARY KEY,
    header INTEGER NOT NULL DEFAULT 0,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    state0 INTEGER NOT NULL DEFAULT 0,
    state1 INTEGER NOT NULL DEFAULT 0,
    state2 INTEGER NOT NULL DEFAULT 0,
    state3 INTEGER NOT NULL DEFAULT 0,
    apply_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo9b_state (
    character_id INTEGER PRIMARY KEY,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack NOTI2 0xA0/0xA1/0xA2/0xA3/0xAA fixed adjacent states.
CREATE TABLE IF NOT EXISTS character_userinfoa0_control (
    character_id INTEGER PRIMARY KEY,
    header_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoa0_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    selector INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoa1_state (
    character_id INTEGER PRIMARY KEY,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    value3 INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoa2_state (
    character_id INTEGER PRIMARY KEY,
    mode INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoa3_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoaa_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

-- Current NoPack NOTI2 0xB0/0xB6/0xBC/0xC8/0xC9/0xCF display adjacent states.
CREATE TABLE IF NOT EXISTS character_userinfob0_state (
    character_id INTEGER PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfob6_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order BETWEEN 0 AND 2),
    text_a TEXT NOT NULL DEFAULT '',
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    flag_c INTEGER NOT NULL DEFAULT 0,
    text_b TEXT NOT NULL DEFAULT '',
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfob6_values (
    character_id INTEGER NOT NULL,
    row_sort_order INTEGER NOT NULL DEFAULT 0 CHECK (row_sort_order BETWEEN 0 AND 2),
    value_index INTEGER NOT NULL CHECK (value_index BETWEEN 0 AND 11),
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, row_sort_order, value_index),
    FOREIGN KEY (character_id, row_sort_order)
        REFERENCES character_userinfob6_rows(character_id, sort_order) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfobc_state (
    character_id INTEGER PRIMARY KEY,
    state INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoc8_state (
    character_id INTEGER PRIMARY KEY,
    delta INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoc9_state (
    character_id INTEGER PRIMARY KEY,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    value3 INTEGER NOT NULL DEFAULT 0,
    value4 INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfocf_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo60_pairs (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    key INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo60_wide_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    key INTEGER NOT NULL DEFAULT 0,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    value3 INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo64_state (
    character_id INTEGER PRIMARY KEY,
    object_key INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo67_state (
    character_id INTEGER PRIMARY KEY,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod0_state (
    character_id INTEGER PRIMARY KEY,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    value3 INTEGER NOT NULL DEFAULT 0,
    value4 INTEGER NOT NULL DEFAULT 0,
    value5 INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod1_control (
    character_id INTEGER PRIMARY KEY,
    header_a INTEGER NOT NULL DEFAULT 0,
    header_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod1_rows (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL DEFAULT 0 CHECK (group_index BETWEEN 0 AND 3),
    sort_order INTEGER NOT NULL DEFAULT 0,
    key INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_index, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod2_control (
    character_id INTEGER PRIMARY KEY,
    tail_value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod2_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    row_type INTEGER NOT NULL DEFAULT 0,
    context_key INTEGER NOT NULL DEFAULT 0,
    key INTEGER NOT NULL DEFAULT 0,
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    value_d INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod3_control (
    character_id INTEGER PRIMARY KEY,
    header_a INTEGER NOT NULL DEFAULT 0,
    header_b INTEGER NOT NULL DEFAULT 0,
    header_value INTEGER NOT NULL DEFAULT 0,
    global_flag INTEGER NOT NULL DEFAULT 0,
    mode INTEGER NOT NULL DEFAULT 0,
    extra_value INTEGER NOT NULL DEFAULT 0,
    tail_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod3_primary_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    word0 INTEGER NOT NULL DEFAULT 0,
    byte0 INTEGER NOT NULL DEFAULT 0,
    byte1 INTEGER NOT NULL DEFAULT 0,
    word1 INTEGER NOT NULL DEFAULT 0,
    word2 INTEGER NOT NULL DEFAULT 0,
    word3 INTEGER NOT NULL DEFAULT 0,
    byte2 INTEGER NOT NULL DEFAULT 0,
    word4 INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    byte3 INTEGER NOT NULL DEFAULT 0,
    byte4 INTEGER NOT NULL DEFAULT 0,
    bool_flag INTEGER NOT NULL DEFAULT 0,
    byte5 INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod3_secondary_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    row_type INTEGER NOT NULL DEFAULT 0,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    word0 INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    word1 INTEGER NOT NULL DEFAULT 0,
    word2 INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod5_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod6_state (
    character_id INTEGER PRIMARY KEY,
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod7_state (
    character_id INTEGER PRIMARY KEY,
    flag INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod8_control (
    character_id INTEGER PRIMARY KEY,
    mode INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfod8_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfodc_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfodd_state (
    character_id INTEGER PRIMARY KEY,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfodf_state (
    character_id INTEGER PRIMARY KEY,
    flag INTEGER NOT NULL DEFAULT 0,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    value3 INTEGER NOT NULL DEFAULT 0,
    value4 INTEGER NOT NULL DEFAULT 0,
    value5 INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoe0_control (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoe0_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoe6_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoeb_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfofe_state (
    character_id INTEGER PRIMARY KEY,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfoff_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo109_state (
    character_id INTEGER PRIMARY KEY,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    value3 INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo10c_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo117_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo118_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    key INTEGER NOT NULL DEFAULT 0,
    flag_a INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    value_d INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo11d_state (
    character_id INTEGER PRIMARY KEY,
    mode INTEGER NOT NULL DEFAULT 0,
    byte_a INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    byte_b INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo126_state (
    character_id INTEGER PRIMARY KEY,
    text TEXT NOT NULL DEFAULT '',
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo12a_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo17c_control (
    character_id INTEGER PRIMARY KEY,
    header_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo17c_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    selector INTEGER NOT NULL DEFAULT 0,
    item_or_key INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo182_control (
    character_id INTEGER PRIMARY KEY,
    actor_key INTEGER NOT NULL DEFAULT 0,
    header_flag INTEGER NOT NULL DEFAULT 0,
    outer_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo182_groups (
    character_id INTEGER NOT NULL,
    phase INTEGER NOT NULL DEFAULT 0,
    group_index INTEGER NOT NULL DEFAULT 0,
    group_flag INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, phase, group_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo182_first_rows (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    row_state INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_index, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo182_first_values (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL DEFAULT 0,
    row_sort_order INTEGER NOT NULL DEFAULT 0,
    value_index INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_index, row_sort_order, value_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo182_second_values (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL DEFAULT 0,
    value_index INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_index, value_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo183_control (
    character_id INTEGER PRIMARY KEY,
    header_a INTEGER NOT NULL DEFAULT 0,
    header_b INTEGER NOT NULL DEFAULT 0,
    header_value INTEGER NOT NULL DEFAULT 0,
    global_flag INTEGER NOT NULL DEFAULT 0,
    mode INTEGER NOT NULL DEFAULT 0,
    extra_value INTEGER NOT NULL DEFAULT 0,
    tail_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo183_primary_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    word0 INTEGER NOT NULL DEFAULT 0,
    key_or_value INTEGER NOT NULL DEFAULT 0,
    word1 INTEGER NOT NULL DEFAULT 0,
    word2 INTEGER NOT NULL DEFAULT 0,
    flag0 INTEGER NOT NULL DEFAULT 0,
    flag1 INTEGER NOT NULL DEFAULT 0,
    bool_flag INTEGER NOT NULL DEFAULT 0,
    flag2 INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo183_secondary_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    row_type INTEGER NOT NULL DEFAULT 0,
    value0 INTEGER NOT NULL DEFAULT 0,
    value1 INTEGER NOT NULL DEFAULT 0,
    value2 INTEGER NOT NULL DEFAULT 0,
    word0 INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    word1 INTEGER NOT NULL DEFAULT 0,
    word2 INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo184_control (
    character_id INTEGER PRIMARY KEY,
    header INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo184_first_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo184_second_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo184_third_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo1bf_control (
    character_id INTEGER PRIMARY KEY,
    refresh_flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo1bf_groups (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    context_state INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo1bf_selectors (
    character_id INTEGER NOT NULL,
    group_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    selector INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_sort_order, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo1bf_values (
    character_id INTEGER NOT NULL,
    group_sort_order INTEGER NOT NULL DEFAULT 0,
    selector_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_sort_order, selector_sort_order, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo327_blobs (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    blob_key INTEGER NOT NULL DEFAULT 0,
    payload BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo329_targets (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    target_key INTEGER NOT NULL DEFAULT 0,
    refresh_flag INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo329_groups (
    character_id INTEGER NOT NULL,
    target_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    context_state INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, target_sort_order, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo329_selectors (
    character_id INTEGER NOT NULL,
    target_sort_order INTEGER NOT NULL DEFAULT 0,
    group_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    selector INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, target_sort_order, group_sort_order, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo329_values (
    character_id INTEGER NOT NULL,
    target_sort_order INTEGER NOT NULL DEFAULT 0,
    group_sort_order INTEGER NOT NULL DEFAULT 0,
    selector_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, target_sort_order, group_sort_order, selector_sort_order, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34b_state (
    character_id INTEGER PRIMARY KEY,
    value_a INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    state INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34c_control (
    character_id INTEGER PRIMARY KEY,
    word INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34c_first_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    key INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    flag_a INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    flag_c INTEGER NOT NULL DEFAULT 0,
    flag_d INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34c_second_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    row_type INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    word_a INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    word_c INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34d_control (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34d_rows (
    character_id INTEGER NOT NULL,
    group_index INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_index, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34e_state (
    character_id INTEGER PRIMARY KEY,
    state INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo_fixed_raws (
    character_id INTEGER NOT NULL,
    noti_type INTEGER NOT NULL,
    payload BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, noti_type),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo_byte_count_raw_rows (
    character_id INTEGER NOT NULL,
    noti_type INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    payload BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, noti_type, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo22d_state (
    character_id INTEGER PRIMARY KEY,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    word_c INTEGER NOT NULL DEFAULT 0,
    word_d INTEGER NOT NULL DEFAULT 0,
    mode INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo22e_state (
    character_id INTEGER PRIMARY KEY,
    object_key INTEGER NOT NULL DEFAULT 0,
    mode INTEGER NOT NULL DEFAULT 0,
    byte_a INTEGER NOT NULL DEFAULT 0,
    byte_b INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo22e_pairs (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo237_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo238_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo253_state (
    character_id INTEGER PRIMARY KEY,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    tail_word_a INTEGER NOT NULL DEFAULT 0,
    tail_word_b INTEGER NOT NULL DEFAULT 0,
    tail_flag INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo253_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo254_state (
    character_id INTEGER PRIMARY KEY,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo254_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo255_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo25b_state (
    character_id INTEGER PRIMARY KEY,
    flag_a INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    flag_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo26e_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo274_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo275_state (
    character_id INTEGER PRIMARY KEY,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    word_c INTEGER NOT NULL DEFAULT 0,
    word_d INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo276_state (
    character_id INTEGER PRIMARY KEY,
    byte_a INTEGER NOT NULL DEFAULT 0,
    byte_b INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo287_state (
    character_id INTEGER PRIMARY KEY,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo287_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL DEFAULT '',
    byte_a INTEGER NOT NULL DEFAULT 0,
    byte_b INTEGER NOT NULL DEFAULT 0,
    packed_flag INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT '',
    value_c INTEGER NOT NULL DEFAULT 0,
    value_d INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    byte_c INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo287_extras (
    character_id INTEGER NOT NULL,
    row_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    extra_index INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, row_sort_order, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo28a_state (
    character_id INTEGER PRIMARY KEY,
    tail_value_a INTEGER NOT NULL DEFAULT 0,
    tail_value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo28a_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    row_type INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT '',
    word INTEGER NOT NULL DEFAULT 0,
    byte_a INTEGER NOT NULL DEFAULT 0,
    packed_flag INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo28b_state (
    character_id INTEGER PRIMARY KEY,
    word INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo29f_state (
    character_id INTEGER PRIMARY KEY,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo29f_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    category INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2a9_state (
    character_id INTEGER PRIMARY KEY,
    header_word INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2a9_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    byte_a INTEGER NOT NULL DEFAULT 0,
    word_a INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2a9_values (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2aa_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2b0_state (
    character_id INTEGER PRIMARY KEY,
    key INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2bc_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2c1_state (
    character_id INTEGER PRIMARY KEY,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2d2_state (
    character_id INTEGER PRIMARY KEY,
    flag INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    value_c INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2d2_groups (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    group_key INTEGER NOT NULL DEFAULT 0,
    word_a INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2d2_rows (
    character_id INTEGER NOT NULL,
    group_sort_order INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_sort_order, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2d2_pairs (
    character_id INTEGER NOT NULL,
    group_sort_order INTEGER NOT NULL DEFAULT 0,
    row_sort_order INTEGER NOT NULL DEFAULT 0,
    pair_kind TEXT NOT NULL DEFAULT 'first',
    sort_order INTEGER NOT NULL DEFAULT 0,
    word INTEGER NOT NULL DEFAULT 0,
    flag INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_sort_order, row_sort_order, pair_kind, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2d3_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2d8_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo2ef_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo31d_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo324_state (
    character_id INTEGER PRIMARY KEY,
    byte_a INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT '',
    byte_b INTEGER NOT NULL DEFAULT 0,
    byte_c INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo336_state (
    character_id INTEGER PRIMARY KEY,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo336_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    byte_a INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT '',
    byte_b INTEGER NOT NULL DEFAULT 0,
    byte_c INTEGER NOT NULL DEFAULT 0,
    packed_flag INTEGER NOT NULL DEFAULT 0,
    byte_d INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34c_text_state (
    character_id INTEGER PRIMARY KEY,
    category INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34d_value_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo34e_byte_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo352_state (
    character_id INTEGER PRIMARY KEY,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo354_state (
    character_id INTEGER PRIMARY KEY,
    word0 INTEGER NOT NULL DEFAULT 0,
    word1 INTEGER NOT NULL DEFAULT 0,
    word2 INTEGER NOT NULL DEFAULT 0,
    word3 INTEGER NOT NULL DEFAULT 0,
    word4 INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo355_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo359_state (
    character_id INTEGER PRIMARY KEY,
    byte_a INTEGER NOT NULL DEFAULT 0,
    byte_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo359_groups (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    group_key INTEGER NOT NULL DEFAULT 0,
    raw0 BLOB NOT NULL DEFAULT X'',
    raw1 BLOB NOT NULL DEFAULT X'',
    raw2 BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo36b_state (
    character_id INTEGER PRIMARY KEY,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo36b_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    kind INTEGER NOT NULL DEFAULT 0,
    byte_a INTEGER NOT NULL DEFAULT 0,
    byte_b INTEGER NOT NULL DEFAULT 0,
    byte_c INTEGER NOT NULL DEFAULT 0,
    word_a INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    byte_d INTEGER NOT NULL DEFAULT 0,
    word_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo37b_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    raw124 BLOB NOT NULL DEFAULT X'',
    raw64 BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo393_state (
    character_id INTEGER PRIMARY KEY,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo393_rows (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    value_a INTEGER NOT NULL DEFAULT 0,
    value_b INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo3cd_state (
    character_id INTEGER PRIMARY KEY,
    flag INTEGER NOT NULL DEFAULT 0,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo3e6_state (
    character_id INTEGER PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo374_state (
    character_id INTEGER PRIMARY KEY,
    header BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo374_rows (
    character_id INTEGER NOT NULL,
    group_kind TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    payload BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_kind, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo379_state (
    character_id INTEGER PRIMARY KEY,
    header BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo379_rows (
    character_id INTEGER NOT NULL,
    group_kind TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    payload BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_kind, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo37a_state (
    character_id INTEGER PRIMARY KEY,
    header1 BLOB NOT NULL DEFAULT X'',
    header33 BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_userinfo37a_rows (
    character_id INTEGER NOT NULL,
    group_kind TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    payload BLOB NOT NULL DEFAULT X'',
    note TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, group_kind, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_dimensions (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    dim_key INTEGER NOT NULL,
    val1 INTEGER NOT NULL DEFAULT 0,
    val2 INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_dimension_flags (
    character_id INTEGER PRIMARY KEY,
    flag1 INTEGER NOT NULL DEFAULT 0,
    flag2 INTEGER NOT NULL DEFAULT 0,
    flag3 INTEGER NOT NULL DEFAULT 0,
    flag4 INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_pvp_results (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    value_u32 INTEGER NOT NULL DEFAULT 0,
    value_u16a INTEGER NOT NULL DEFAULT 0,
    value_u16b INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_abuse_values (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    abuse_value INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS character_sort_item_locks (
    character_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    list_type INTEGER NOT NULL,
    slot_index INTEGER NOT NULL,
    state INTEGER NOT NULL,
    PRIMARY KEY (character_id, sort_order),
    UNIQUE(character_id, list_type, slot_index),
    FOREIGN KEY (character_id) REFERENCES characters(character_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS account_settings (
    account_id INTEGER PRIMARY KEY,
    main_game_option BLOB,
    quickchat_bank0 BLOB,
    quickchat_bank1 BLOB,
    hotkey_key_type INTEGER NOT NULL DEFAULT 0,
    hotkey_slots BLOB,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

INSERT OR IGNORE INTO accounts (account_id, m_id, password_hash) VALUES
    (1, '10038', '');

-- character 和 container_state 由 EnsureInitialized 从封包样本动态 seed（不再硬编码）

INSERT OR IGNORE INTO account_cargo_state (account_id, selection_key, value32) VALUES
    (1, 16, 0);
