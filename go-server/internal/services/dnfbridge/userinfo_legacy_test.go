// 本文件验证 C# USERINFO legacy 表到旧客户端 NOTI2 body 的编码边界。
package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type fakeLegacyUserInfoRepo struct {
	rows map[string][]dnfrepo.LegacyUserInfoRow
}

func (r fakeLegacyUserInfoRepo) Check(context.Context) error {
	return nil
}

func (r fakeLegacyUserInfoRepo) SelectRows(_ context.Context, _ string, tableSuffix string, columns []string, _ []string) ([]dnfrepo.LegacyUserInfoRow, error) {
	src := r.rows[tableSuffix]
	out := make([]dnfrepo.LegacyUserInfoRow, 0, len(src))
	for _, row := range src {
		cloned := make(dnfrepo.LegacyUserInfoRow, len(columns))
		for _, column := range columns {
			cloned[column] = row[column]
		}
		out = append(out, cloned)
	}
	return out, nil
}

func (r fakeLegacyUserInfoRepo) SelectOne(ctx context.Context, characterID string, tableSuffix string, columns []string) (dnfrepo.LegacyUserInfoRow, bool, error) {
	rows, err := r.SelectRows(ctx, characterID, tableSuffix, columns, nil)
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	return rows[0], true, nil
}

func TestBuildCSharpLegacyUserInfoSkipsWhenRepositoryMissing(t *testing.T) {
	service := Service{characterStats: testCharacterStatTable(t)}
	if body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, nil, 1, 0x017c); ok || body != nil {
		t.Fatalf("legacy userinfo without repository = %x/%v, want skipped", body, ok)
	}
}

func TestBuildCSharpLegacyUserInfo23MatchesCSharpLayout(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo23_scalar_values": {
			{"family": "direct", "scalar_index": "401", "source_order": "0", "value": "101"},
			{"family": "e90", "scalar_index": "1", "source_order": "0", "value": "102"},
			{"family": "e90", "scalar_index": "2", "source_order": "0", "value": "103"},
			{"family": "e90", "scalar_index": "3", "source_order": "0", "value": "104"},
			{"family": "direct", "scalar_index": "399", "source_order": "0", "value": "5"},
			{"family": "e90", "scalar_index": "6", "source_order": "0", "value": "106"},
			{"family": "direct", "scalar_index": "397", "source_order": "0", "value": "107"},
			{"family": "e90", "scalar_index": "5", "source_order": "0", "value": "108"},
			{"family": "e90", "scalar_index": "9", "source_order": "0", "value": "109"},
			{"family": "direct", "scalar_index": "398", "source_order": "0", "value": "110"},
			{"family": "e90", "scalar_index": "4", "source_order": "0", "value": "111"},
			{"family": "e90", "scalar_index": "8", "source_order": "0", "value": "112"},
			{"family": "e90", "scalar_index": "10", "source_order": "0", "value": "113"},
			{"family": "e90", "scalar_index": "11", "source_order": "0", "value": "114"},
			{"family": "e90", "scalar_index": "12", "source_order": "0", "value": "115"},
			{"family": "e90", "scalar_index": "13", "source_order": "0", "value": "116"},
			{"family": "e90", "scalar_index": "14", "source_order": "0", "value": "117"},
			{"family": "db0", "scalar_index": "5", "source_order": "0", "value": "118"},
			{"family": "d90", "scalar_index": "2", "source_order": "0", "value": "119"},
			{"family": "d90", "scalar_index": "10", "source_order": "0", "value": "120"},
			{"family": "d90", "scalar_index": "7", "source_order": "0", "value": "200"},
			{"family": "d90", "scalar_index": "4", "source_order": "0", "value": "122"},
			{"family": "d90", "scalar_index": "5", "source_order": "0", "value": "123"},
			{"family": "d90", "scalar_index": "8", "source_order": "0", "value": "124"},
			{"family": "d90", "scalar_index": "9", "source_order": "0", "value": "125"},
			{"family": "d90", "scalar_index": "6", "source_order": "0", "value": "126"},
			{"family": "d90", "scalar_index": "3", "source_order": "0", "value": "127"},
			{"family": "d90", "scalar_index": "0", "source_order": "0", "value": "129"},
			{"family": "d90", "scalar_index": "1", "source_order": "0", "value": "130"},
			{"family": "d90", "scalar_index": "11", "source_order": "0", "value": "211"},
			{"family": "db0", "scalar_index": "11", "source_order": "0", "value": "212"},
			{"family": "e90", "scalar_index": "16", "source_order": "0", "value": "131"},
			{"family": "e90", "scalar_index": "17", "source_order": "0", "value": "132"},
			{"family": "e90", "scalar_index": "18", "source_order": "0", "value": "133"},
			{"family": "e90", "scalar_index": "19", "source_order": "0", "value": "134"},
			{"family": "e90", "scalar_index": "20", "source_order": "0", "value": "135"},
			{"family": "d90", "scalar_index": "13", "source_order": "0", "value": "136"},
			{"family": "e90", "scalar_index": "21", "source_order": "0", "value": "137"},
			{"family": "e90", "scalar_index": "22", "source_order": "0", "value": "138"},
		},
		"legacy_character_userinfo23_fixed_values": {
			{"section": "fixed4", "slot_index": "0", "value": "1"},
			{"section": "fixed4", "slot_index": "1", "value": "2"},
			{"section": "fixed4", "slot_index": "2", "value": "3"},
			{"section": "fixed4", "slot_index": "3", "value": "4"},
		},
		"legacy_character_userinfo23_pair_entries": {
			{"section": "map", "entry_key": "9", "value": "10"},
		},
		"legacy_character_userinfo23_object_rows": {{
			"object_or_slot_id": "4660",
			"value_a":           "16909060",
			"value_b":           "5",
			"value_c":           "6",
			"value_d":           "7",
			"value_e":           "8",
		}},
		"legacy_character_userinfo_slot_control": {{
			"group0_tail_refresh_value": "2864434397",
			"mode_bits":                 "17",
			"tail_value_a":              "3735928559",
			"tail_flag_a":               "34",
			"tail_bool_b":               "51",
			"tail_value_b":              "1146447479",
			"tail_pair_a":               "305419896",
			"tail_pair_b":               "2596069104",
			"tail_index6_value":         "66",
		}},
		"legacy_character_userinfo_slot_group_entries": {
			{"group_id": "0", "category": "0", "key_or_item_id": "1", "value": "2"},
			{"group_id": "1", "category": "7", "key_or_item_id": "3", "value": "4"},
			{"group_id": "2", "category": "2", "key_or_item_id": "5", "value": "6"},
		},
	}}
	service := Service{characterStats: testCharacterStatTable(t)}
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x0023)
	if !ok {
		t.Fatal("legacy userinfo 0x23 skipped")
	}

	var want packetWriter
	for _, value := range []uint32{101, 102, 103, 104} {
		want.writeUint32(value)
	}
	want.writeByte(5)
	for _, value := range []uint32{106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119, 120, 86, 122, 123, 124, 125, 126, 127, 0, 129, 130} {
		want.writeUint32(value)
	}
	want.writeByte(1)
	want.writeUint32(1)
	want.writeUint32(211)
	want.writeByte(1)
	want.writeUint32(1)
	want.writeUint32(212)
	for _, value := range []uint32{131, 132, 133, 134, 135, 136, 137, 138, 1, 2, 3, 4} {
		want.writeUint32(value)
	}
	want.writeUint32(1)
	want.writeUint32(9)
	want.writeUint32(10)
	want.writeByte(1)
	want.writeUint16(4660)
	want.writeUint32(16909060)
	want.writeUint32(5)
	want.writeUint16(6)
	want.writeByte(7)
	want.writeUint16(8)
	want.writeByte(1)
	want.writeUint32(1)
	want.writeUint32(2)
	for i := 1; i < 8; i++ {
		want.writeByte(0)
	}
	want.writeUint32(2864434397)
	for i := 0; i < 7; i++ {
		want.writeByte(0)
	}
	want.writeByte(1)
	want.writeUint32(3)
	want.writeUint32(4)
	for i := 0; i < 2; i++ {
		want.writeByte(0)
	}
	want.writeByte(1)
	want.writeUint32(5)
	want.writeUint32(6)
	for i := 3; i < 8; i++ {
		want.writeByte(0)
	}
	want.writeUint32(17)
	want.writeUint32(3735928559)
	want.writeByte(34)
	want.writeByte(51)
	want.writeUint32(1146447479)
	want.writeUint32(305419896)
	want.writeUint32(2596069104)
	want.writeUint32(66)

	if !bytes.Equal(body, want.bytes()) {
		t.Fatalf("0x23 body = %x, want %x", body, want.bytes())
	}
}

func TestBuildCSharpLegacyUserInfoB6UsesTwelveValues(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfob6_rows": {{
			"sort_order": "0",
			"text_a":     "A",
			"flag_a":     "1",
			"flag_b":     "2",
			"flag_c":     "3",
			"text_b":     "B",
			"value":      "4",
		}},
		"legacy_character_userinfob6_values": {
			{"row_sort_order": "0", "value_index": "0", "value": "10"},
			{"row_sort_order": "0", "value_index": "11", "value": "21"},
			{"row_sort_order": "0", "value_index": "12", "value": "999"},
		},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x00b6)
	if !ok {
		t.Fatal("legacy userinfo 0xb6 skipped")
	}

	var want packetWriter
	want.writeByte(1)
	writeWideNullTerminatedString(&want, "A")
	want.writeByte(1)
	want.writeByte(2)
	want.writeByte(3)
	writeWideNullTerminatedString(&want, "B")
	want.writeUint32(4)
	for idx := 0; idx < 12; idx++ {
		switch idx {
		case 0:
			want.writeUint32(10)
		case 11:
			want.writeUint32(21)
		default:
			want.writeUint32(0)
		}
	}
	if !bytes.Equal(body, want.bytes()) {
		t.Fatalf("0xb6 body = %x, want %x", body, want.bytes())
	}
}

func TestBuildCSharpLegacyUserInfo22EUsesCSharpModeBranches(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo22e_state": {{
			"object_key": "4660",
			"mode":       "1",
			"value_a":    "16909060",
			"byte_a":     "5",
			"byte_b":     "6",
		}},
		"legacy_character_userinfo22e_pairs": {
			{"sort_order": "0", "value_a": "7", "value_b": "8"},
			{"sort_order": "1", "value_a": "9", "value_b": "10"},
		},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x022e)
	if !ok {
		t.Fatal("legacy userinfo 0x22e skipped")
	}

	var want packetWriter
	want.writeUint16(4660)
	want.writeByte(1)
	want.writeUint32(16909060)
	want.writeByte(5)
	want.writeByte(6)
	want.writeByte(2)
	want.writeByte(7)
	want.writeByte(8)
	want.writeByte(9)
	want.writeByte(10)
	if !bytes.Equal(body, want.bytes()) {
		t.Fatalf("0x22e body = %x, want %x", body, want.bytes())
	}

	repo.rows["legacy_character_userinfo22e_state"][0]["mode"] = "99"
	body, ok = service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x022e)
	if ok || body != nil {
		t.Fatalf("invalid 0x22e mode emitted body %x/%v", body, ok)
	}
}

func TestBuildCSharpLegacyUserInfoNewDisplayPacketsSkipWithoutState(t *testing.T) {
	var service Service
	for _, msgID := range []uint16{
		0x022d, 0x022e, 0x0237, 0x0238, 0x0253, 0x0254, 0x0255, 0x025b,
		0x026e, 0x0274, 0x0275, 0x0276, 0x0287, 0x028a, 0x028b, 0x029f,
	} {
		body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, fakeLegacyUserInfoRepo{}, 1, msgID)
		if ok || body != nil {
			t.Fatalf("0x%04x without state emitted body %x/%v", msgID, body, ok)
		}
	}
}

func TestBuildCSharpLegacyUserInfo287And29FRequireStateRows(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo287_rows": {{
			"sort_order":  "3",
			"name":        "A",
			"byte_a":      "1",
			"byte_b":      "2",
			"packed_flag": "3",
			"value_a":     "4",
			"value_b":     "5",
			"text":        "B",
			"value_c":     "6",
			"value_d":     "7",
			"word":        "8",
			"byte_c":      "9",
		}},
		"legacy_character_userinfo287_extras": {{
			"row_sort_order": "3",
			"sort_order":     "0",
			"extra_index":    "10",
			"value":          "11",
		}},
		"legacy_character_userinfo29f_rows": {{
			"sort_order": "0",
			"word":       "12",
			"category":   "13",
			"value":      "14",
		}},
	}}
	var service Service
	if body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x0287); ok || body != nil {
		t.Fatalf("0x287 without state emitted body %x/%v", body, ok)
	}
	if body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x029f); ok || body != nil {
		t.Fatalf("0x29f without state emitted body %x/%v", body, ok)
	}

	repo.rows["legacy_character_userinfo287_state"] = []dnfrepo.LegacyUserInfoRow{{"note": "enabled"}}
	repo.rows["legacy_character_userinfo29f_state"] = []dnfrepo.LegacyUserInfoRow{{"note": "enabled"}}
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x0287)
	if !ok {
		t.Fatal("legacy userinfo 0x287 skipped")
	}
	var want287 packetWriter
	want287.writeByte(1)
	writeWideNullTerminatedStringMax(&want287, "A", 31)
	want287.writeByte(1)
	want287.writeByte(2)
	want287.writeByte(3)
	want287.writeUint32(4)
	want287.writeUint32(5)
	writeWideNullTerminatedStringMax(&want287, "B", 30)
	want287.writeUint32(6)
	want287.writeUint32(7)
	want287.writeUint16(8)
	want287.writeByte(9)
	want287.writeByte(1)
	want287.writeByte(10)
	want287.writeUint32(11)
	if !bytes.Equal(body, want287.bytes()) {
		t.Fatalf("0x287 body = %x, want %x", body, want287.bytes())
	}

	body, ok = service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x029f)
	if !ok {
		t.Fatal("legacy userinfo 0x29f skipped")
	}
	var want29F packetWriter
	want29F.writeByte(1)
	want29F.writeUint16(12)
	want29F.writeByte(13)
	want29F.writeUint32(14)
	if !bytes.Equal(body, want29F.bytes()) {
		t.Fatalf("0x29f body = %x, want %x", body, want29F.bytes())
	}
}

func TestBuildCSharpLegacyUserInfoLatestRegistryAdditions(t *testing.T) {
	raw124 := bytes.Repeat([]byte{0x12}, 124)
	raw64 := bytes.Repeat([]byte{0x34}, 64)
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo2a9_state": {{
			"header_word": "4660",
		}},
		"legacy_character_userinfo2a9_rows": {{
			"sort_order": "0",
			"byte_a":     "1",
			"word_a":     "515",
			"value":      "67438087",
			"word_b":     "2057",
		}},
		"legacy_character_userinfo2a9_values": {
			{"sort_order": "0", "value": "170"},
			{"sort_order": "1", "value": "187"},
		},
		"legacy_character_userinfo34c_text_state": {{
			"category": "4",
			"text":     "A",
		}},
		"legacy_character_userinfo34c_control": {{
			"word":    "65535",
			"value_a": "1",
			"value_b": "2",
		}},
		"legacy_character_userinfo37b_state": {{
			"value":  "287454020",
			"raw124": string(raw124),
			"raw64":  string(raw64),
		}},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x02a9)
	if !ok {
		t.Fatal("legacy userinfo 0x2a9 skipped")
	}
	var want2A9 packetWriter
	want2A9.writeUint16(4660)
	want2A9.writeByte(1)
	want2A9.writeByte(1)
	want2A9.writeUint16(515)
	want2A9.writeUint32(67438087)
	want2A9.writeUint16(2057)
	want2A9.writeByte(2)
	want2A9.writeByte(170)
	want2A9.writeByte(187)
	if !bytes.Equal(body, want2A9.bytes()) {
		t.Fatalf("0x2a9 body = %x, want %x", body, want2A9.bytes())
	}

	body, ok = service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x034c)
	if !ok {
		t.Fatal("legacy userinfo 0x34c skipped")
	}
	var want34C packetWriter
	want34C.writeByte(4)
	writeWideNullTerminatedStringMax(&want34C, "A", 64)
	if !bytes.Equal(body, want34C.bytes()) {
		t.Fatalf("0x34c body = %x, want current text-state body %x", body, want34C.bytes())
	}

	body, ok = service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x037b)
	if !ok {
		t.Fatal("legacy userinfo 0x37b skipped")
	}
	var want37B packetWriter
	want37B.writeUint32(287454020)
	want37B.writeBytes(raw124)
	want37B.writeBytes(raw64)
	if !bytes.Equal(body, want37B.bytes()) {
		t.Fatalf("0x37b body = %x, want %x", body, want37B.bytes())
	}
}

func TestBuildCSharpLegacyUserInfoRawFallbacks(t *testing.T) {
	fixed := []byte{1, 2, 3, 4}
	variable := []byte{5, 6, 7, 8, 9}
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo_fixed_raws": {
			{"noti_type": "967", "payload": string(fixed)},
			{"noti_type": "1034", "payload": string(variable)},
		},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x03c7)
	if !ok {
		t.Fatal("legacy userinfo 0x3c7 skipped")
	}
	if !bytes.Equal(body, fixed) {
		t.Fatalf("0x3c7 body = %x, want %x", body, fixed)
	}

	body, ok = service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x040a)
	if !ok {
		t.Fatal("legacy userinfo 0x40a skipped")
	}
	if !bytes.Equal(body, variable) {
		t.Fatalf("0x40a body = %x, want %x", body, variable)
	}
}

func TestCSharpLegacyUserInfoDoesNotPushSlotUnlockOrContextRefresh(t *testing.T) {
	forbidden := map[uint16]string{
		0x015d: "slot unlock",
		0x0161: "context sensitive",
		0x01bf: "context sensitive",
		0x01d4: "latest extracted passive state",
		0x01d5: "latest extracted passive state",
		0x01d6: "latest extracted passive state",
		0x01d7: "latest extracted passive state",
		0x01d8: "latest extracted passive state",
		0x01d9: "latest extracted passive state",
		0x0327: "context sensitive",
		0x0329: "context sensitive",
		0x0343: "latest extracted passive state",
		0x0344: "latest extracted passive state",
		0x0373: "latest extracted passive state",
		0x0374: "context refresh",
		0x0375: "latest extracted passive state",
		0x0376: "latest extracted passive state",
		0x0377: "latest extracted passive state",
		0x0378: "latest extracted passive state",
		0x0379: "context refresh",
		0x037a: "context refresh",
	}
	for _, packet := range csharpLegacyUserInfoInitPackets() {
		if reason, ok := forbidden[packet.msgID]; ok {
			t.Fatalf("legacy userinfo init should not actively push 0x%04x (%s)", packet.msgID, reason)
		}
	}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, fakeLegacyUserInfoRepo{}, 1, 0x015d)
	if ok || body != nil {
		t.Fatalf("0x015d body = %x/%v, want skipped unless client triggers unlock flow", body, ok)
	}
}

func TestBuildCSharpSelectedUserInfoSubtype0UsesCharacterAndLegacyTail(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_subtype0_fields": {{
			"name_tag_item_id":   "287454020",
			"creature_field1":    "1",
			"creature_field2":    "2",
			"creature_field3":    "3",
			"creature_field4":    "4",
			"creature_buffer":    "ABCDEFGH",
			"stamina":            "5",
			"fatigue_penalty":    "1432778632",
			"is_event_character": "1",
			"pc_room_id":         "65537",
			"expert_job_type":    "6",
			"expert_job_exp":     "16909060",
			"extra46":            "7",
			"extra47":            "84281096",
			"extra51":            "4386",
			"user_state_bits":    "3",
			"return_user_flag":   "1",
			"channel_id":         "2",
			"link_slot_enabled":  "1",
			"aura_flag":          "0",
			"pvp_rank_point":     "287454020",
			"trailing_byte":      "9",
		}},
		"legacy_character_subtype1_fields": {{
			"progress1":        "305419896",
			"progress2":        "2596069104",
			"skill_tree_index": "12",
		}},
	}}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Name:        "hero",
		Job:         "14",
		Level:       56,
		Stats: map[string]int64{
			"grow_type":        2,
			"pvp_grade":        3,
			"pvp_rating_grade": 4,
			"user_state":       5,
			"aura_flag":        1,
		},
	}
	var service Service
	body := service.buildCSharpSelectedUserInfoBody(context.Background(), nil, repo, 0, character, true, 77, character.Name)

	if len(body) != 124 {
		t.Fatalf("subtype0 body len = %d, want 124", len(body))
	}
	if body[0] != 0 || binary.LittleEndian.Uint16(body[1:3]) != 1 || binary.LittleEndian.Uint16(body[3:5]) != 77 {
		t.Fatalf("subtype0 header = %x", body[:5])
	}
	if got := string(body[9:13]); got != "hero" {
		t.Fatalf("subtype0 name bytes = %q", got)
	}
	if got := []byte{body[13], body[14], body[15], body[16], body[17], body[18], body[19]}; !bytes.Equal(got, []byte{14, 2, 56, 3, 4, 5, 0}) {
		t.Fatalf("subtype0 character bytes = %v", got)
	}
	tail := body[20:]
	if binary.LittleEndian.Uint32(tail[0:4]) != 287454020 ||
		tail[46] != 7 ||
		binary.LittleEndian.Uint32(tail[47:51]) != 84281096 ||
		binary.LittleEndian.Uint16(tail[51:53]) != 4386 ||
		binary.LittleEndian.Uint32(tail[57:61]) != 305419896 ||
		binary.LittleEndian.Uint32(tail[61:65]) != 2596069104 ||
		tail[79] != 12 ||
		tail[90] != 1 ||
		tail[103] != 9 {
		t.Fatalf("subtype0 tail key fields mismatch: %x", tail)
	}
}

func TestBuildCSharpSelectedUserInfoSubtype0ReloadsDurableAuraFlag(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "account-1",
		Name:        "hero",
		Job:         "14",
		Level:       56,
		Stats:       map[string]int64{"aura_flag": 1},
	}); err != nil {
		t.Fatal(err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	rosterCharacter := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Name:        "hero",
		Job:         "14",
		Level:       56,
	}
	body := service.buildCSharpSelectedUserInfoBody(
		ctx,
		nil,
		fakeLegacyUserInfoRepo{},
		0,
		rosterCharacter,
		true,
		77,
		rosterCharacter.Name,
	)
	if len(body) < 14 || body[len(body)-14] != 1 {
		t.Fatalf("repository-backed aura flag was not restored in subtype0: len=%d tail=%x", len(body), body)
	}
}

func TestBuildCSharpSelectedUserInfoSubtype0UsesSessionChannelContext(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Name:        "hero",
		Job:         "14",
		Level:       1,
		Stats:       map[string]int64{"channel_id": 2},
	}
	service := Service{}
	session := &gameSession{channel: channelcatalog.Channel{ID: 19, Type: 3, Name: "ch.19", Port: 10019}}

	body := service.buildCSharpSelectedUserInfoBody(context.Background(), session, fakeLegacyUserInfoRepo{}, 0, character, true, 77, character.Name)
	tail := body[20:]
	if got := binary.LittleEndian.Uint16(tail[74:76]); got != 19 {
		t.Fatalf("subtype0 channel display mode = %d, want session channel 19", got)
	}
	if got := tail[76]; got != 3 {
		t.Fatalf("subtype0 channel type = %d, want session type 3", got)
	}
	if got := binary.LittleEndian.Uint16(tail[77:79]); got != 19 {
		t.Fatalf("subtype0 channel id = %d, want session channel 19", got)
	}
}

func TestBuildCSharpSelectedUserInfoSubtype1UsesCSharpFieldOrder(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_subtype1_fields": {{
			"stat_hp_max":                 "1000",
			"stat_mp_max":                 "2000",
			"stat_physical_attack":        "30",
			"active_status_resistance_16": "160",
			"stat_inventory_limit":        "24",
			"stat_level":                  "56",
			"name_tag_item_id":            "287454020",
			"name_tag_expire_time":        "1432778632",
			"skill_tree_index":            "9",
			"equipped_creature_level":     "11",
			"equip_list_trailing":         "305419896",
			"manage_level":                "12",
			"flag_byte":                   "13",
			"guild_power_war":             "16909060",
			"server_timestamp":            "84281096",
			"quest_shop_count":            "4660",
			"progress1":                   "305419896",
			"progress2":                   "2596069104",
		}},
	}}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Stats: map[string]int64{
			"exp":                12345,
			"ex_equip_slot_stat": 6,
		},
	}
	var service Service
	body := service.buildCSharpSelectedUserInfoBody(context.Background(), nil, repo, 1, character, true, 77, "hero")

	if body[0] != 1 || binary.LittleEndian.Uint16(body[1:3]) != 1 || binary.LittleEndian.Uint16(body[3:5]) != 77 {
		t.Fatalf("subtype1 header = %x", body[:5])
	}
	if got := binary.LittleEndian.Uint32(body[5:9]); got != 12345 {
		t.Fatalf("subtype1 exp = %d", got)
	}
	if got := binary.LittleEndian.Uint32(body[9:13]); got != 83 {
		t.Fatalf("subtype1 stat block marker = %d", got)
	}
	if got := binary.LittleEndian.Uint16(body[69:71]); got != 160 {
		t.Fatalf("subtype1 active_status_resistance_16 = %d", got)
	}
	if got := body[96]; got != 6 {
		t.Fatalf("subtype1 ex_equip_slot_stat = %d", got)
	}
	if got := binary.LittleEndian.Uint32(body[98:102]); got != 305419896 {
		t.Fatalf("subtype1 equip_list_trailing = %d", got)
	}
	if got := body[110]; got != 9 {
		t.Fatalf("subtype1 skill_tree_index = %d", got)
	}
	if got := binary.LittleEndian.Uint32(body[len(body)-8 : len(body)-4]); got != 305419896 {
		t.Fatalf("subtype1 progress1 = %d", got)
	}
	if got := binary.LittleEndian.Uint32(body[len(body)-4:]); got != 2596069104 {
		t.Fatalf("subtype1 progress2 = %d", got)
	}
}

func TestBuildCSharpSelectedUserInfoUsesCreatedCharacterSeedWithoutLegacyRows(t *testing.T) {
	req := createCharacterRequest{
		job:     15,
		nameRaw: []byte("hero"),
		options: []byte{0xaa, 0xbb, 0xcc},
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Name:        "hero",
		Job:         "15",
		Level:       1,
		Stats:       defaultCreatedCharacterStatsFromRequest(req),
	}
	service := Service{characterStats: testCharacterStatTable(t)}

	subtype0 := service.buildCSharpSelectedUserInfoBody(context.Background(), nil, fakeLegacyUserInfoRepo{}, 0, character, true, 77, character.Name)
	if len(subtype0) != 124 {
		t.Fatalf("subtype0 body len = %d, want 124", len(subtype0))
	}
	tail := subtype0[20:]
	if got := binary.LittleEndian.Uint32(tail[22:26]); got != 0x00010001 {
		t.Fatalf("subtype0 pc_room_id = %#x, want 0x00010001", got)
	}
	if tail[65] != 3 || tail[73] != 1 || binary.LittleEndian.Uint16(tail[77:79]) != 2 {
		t.Fatalf("subtype0 default tail mismatch: %x", tail)
	}

	subtype1 := service.buildCSharpSelectedUserInfoBody(context.Background(), nil, fakeLegacyUserInfoRepo{}, 1, character, true, 77, character.Name)
	if got := binary.LittleEndian.Uint32(subtype1[9:13]); got != 83 {
		t.Fatalf("subtype1 marker = %d, want 83", got)
	}
	if got := binary.LittleEndian.Uint32(subtype1[13:17]); got != 11000 {
		t.Fatalf("subtype1 hp max = %d, want 11000", got)
	}
	if got := binary.LittleEndian.Uint32(subtype1[17:21]); got != 11800 {
		t.Fatalf("subtype1 mp max = %d, want 11800", got)
	}
	if got := bodyInt16(subtype1[21:23]); got != 7 {
		t.Fatalf("subtype1 physical attack = %d, want 7", got)
	}
	if got := bodyInt16(subtype1[25:27]); got != 6 {
		t.Fatalf("subtype1 magical attack = %d, want 6", got)
	}
	if got := binary.LittleEndian.Uint32(subtype1[71:75]); got != 480000 {
		t.Fatalf("subtype1 inventory limit = %d, want 480000", got)
	}
	if got := subtype1[95]; got != csharpSubtype1ProtocolStatLevel {
		t.Fatalf("subtype1 protocol stat level = %d, want %d", got, csharpSubtype1ProtocolStatLevel)
	}
	if character.Stats["create_option_len"] != 3 ||
		character.Stats["create_option_byte_00"] != 0xaa ||
		character.Stats["create_option_byte_01"] != 0xbb ||
		character.Stats["create_option_byte_02"] != 0xcc {
		t.Fatalf("created option bytes not persisted in stats: %+v", character.Stats)
	}
}

func bodyInt16(data []byte) int {
	return int(int16(binary.LittleEndian.Uint16(data)))
}

func TestBuildCSharpLegacyUserInfo17CUsesCSharpLayout(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo17c_control": {{
			"header_flag": "7",
		}},
		"legacy_character_userinfo17c_rows": {{
			"selector":    "2",
			"item_or_key": "1000",
			"value":       "55",
		}},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x017c)
	if !ok {
		t.Fatal("legacy userinfo 0x17c skipped")
	}
	want := []byte{7, 1, 0, 2, 0, 0xe8, 0x03, 0, 0, 55, 0, 0, 0}
	if !bytes.Equal(body, want) {
		t.Fatalf("0x17c body = %x, want %x", body, want)
	}
}

func TestBuildCSharpLegacyUserInfo327PreservesBlobPayload(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo327_blobs": {{
			"blob_key": "9",
			"payload":  string([]byte{0, 1, 2, 0xff}),
		}},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x0327)
	if !ok {
		t.Fatal("legacy userinfo 0x327 skipped")
	}
	want := []byte{1, 9, 4, 0, 0, 0, 0, 1, 2, 0xff}
	if !bytes.Equal(body, want) {
		t.Fatalf("0x327 body = %x, want %x", body, want)
	}
}

func TestBuildCSharpLegacyUserInfoRawFixedUsesNotiType(t *testing.T) {
	payload := []byte{
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
	}
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo_fixed_raws": {
			{"noti_type": "835", "payload": string([]byte{0xee})},
			{"noti_type": "836", "payload": string(payload)},
		},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x0344)
	if !ok {
		t.Fatal("legacy userinfo 0x344 skipped")
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("0x344 body = %x, want %x", body, payload)
	}
}

func TestBuildCSharpLegacyUserInfoRawByteCountList(t *testing.T) {
	rowA := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	rowB := []byte{9, 10, 11, 12, 13, 14, 15, 16}
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo_byte_count_raw_rows": {
			{"noti_type": "468", "sort_order": "1", "payload": string(rowA)},
			{"noti_type": "468", "sort_order": "2", "payload": string(rowB)},
			{"noti_type": "471", "sort_order": "1", "payload": string([]byte{0xee})},
		},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x01d4)
	if !ok {
		t.Fatal("legacy userinfo 0x1d4 skipped")
	}
	want := append([]byte{2}, append(rowA, rowB...)...)
	if !bytes.Equal(body, want) {
		t.Fatalf("0x1d4 body = %x, want %x", body, want)
	}
}

func TestBuildCSharpLegacyUserInfo374UsesRawGroups(t *testing.T) {
	header := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	raw12 := []byte{20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	raw13 := []byte{32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44}
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo374_state": {{
			"header": string(header),
		}},
		"legacy_character_userinfo374_rows": {
			{"group_kind": "raw12", "sort_order": "1", "payload": string(raw12)},
			{"group_kind": "raw13", "sort_order": "1", "payload": string(raw13)},
		},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x0374)
	if !ok {
		t.Fatal("legacy userinfo 0x374 skipped")
	}
	want := append([]byte{}, header...)
	want = append(want, 1, 0, 0, 0)
	want = append(want, raw12...)
	want = append(want, 1, 0, 0, 0)
	want = append(want, raw13...)
	if !bytes.Equal(body, want) {
		t.Fatalf("0x374 body = %x, want %x", body, want)
	}
}

func TestBuildCSharpLegacyUserInfo37ARequiresValidHeaders(t *testing.T) {
	repo := fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_userinfo37a_state": {{
			"header1":  string([]byte{1}),
			"header33": string(bytes.Repeat([]byte{2}, 33)),
		}},
	}}
	var service Service
	body, ok := service.buildCSharpLegacyUserInfoBody(context.Background(), nil, repo, 1, 0x037a)
	if !ok {
		t.Fatal("legacy userinfo 0x37a skipped")
	}
	want := append([]byte{1}, bytes.Repeat([]byte{2}, 33)...)
	for i := 0; i < 7; i++ {
		want = append(want, 0, 0, 0, 0)
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("0x37a body = %x, want %x", body, want)
	}
}
