package channelinfo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const routeFixture = `
[dungeon]
` + "`[granfloris]`" + `
` + "`<4::channel_info_dname_1>`" + `
100
101
[/dungeon]

[server]
1
10 ` + "`<4::chn_channel_info_003>`" + ` 1 ` + "`[granfloris]`" + ` 0 0
6 ` + "`<4::chn_channel_info_002>`" + ` 3 ` + "`[trade]`" + ` 0
[/server]
`

func TestParseBuildsRouteIndex(t *testing.T) {
	index, err := Parse([]byte(routeFixture))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	channel, ok := index.Channel(10)
	if !ok {
		t.Fatalf("channel 10 should exist")
	}
	if channel.NameKey != "chn_channel_info_003" || channel.AreaKey != "granfloris" || channel.Type != 1 {
		t.Fatalf("channel 10 mismatch: %+v", channel)
	}

	area, ok := index.Area("`[granfloris]`")
	if !ok {
		t.Fatalf("area granfloris should exist")
	}
	if area.NameKey != "channel_info_dname_1" || len(area.DungeonIDs) != 2 {
		t.Fatalf("area granfloris mismatch: %+v", area)
	}

	areas := index.AreasForDungeon(101)
	if len(areas) != 1 || areas[0] != "granfloris" {
		t.Fatalf("dungeon 101 areas mismatch: %+v", areas)
	}
	channels := index.ChannelsForDungeon(100)
	if len(channels) != 1 || channels[0].ID != 10 {
		t.Fatalf("dungeon 100 channels mismatch: %+v", channels)
	}
	if got := index.ChannelsForDungeon(999); len(got) != 0 {
		t.Fatalf("unknown dungeon should return no channel, got %+v", got)
	}
	serverChannels := index.ChannelsForServer(1)
	if len(serverChannels) != 2 || serverChannels[0].ID != 10 || serverChannels[1].ID != 6 {
		t.Fatalf("server channel order mismatch: %+v", serverChannels)
	}
}

func TestParseProtectsIndexFromCallerMutation(t *testing.T) {
	index, err := Parse([]byte(routeFixture))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	channel, ok := index.Channel(10)
	if !ok {
		t.Fatalf("channel 10 should exist")
	}
	channel.RawFields[0] = "mutated"
	again, _ := index.Channel(10)
	if again.RawFields[0] == "mutated" {
		t.Fatalf("channel raw fields should be cloned")
	}

	area, ok := index.Area("granfloris")
	if !ok {
		t.Fatalf("area granfloris should exist")
	}
	area.DungeonIDs[0] = 999
	againArea, _ := index.Area("granfloris")
	if againArea.DungeonIDs[0] == 999 {
		t.Fatalf("area dungeon ids should be cloned")
	}
}

func TestParseAcceptsDuplicateServerBlock(t *testing.T) {
	const text = `
[dungeon]
[skycastle]
<4::channel_info_dname_2>
200 201
[/dungeon]
[server]
1
11 ` + "`<4::channel_info_cname_11>`" + ` 0 ` + "`[skycastle]`" + ` 5
[/server]
[server]
1
11 ` + "`<4::channel_info_cname_11>`" + ` 0 ` + "`[skycastle]`" + ` 5
[/server]
`
	index, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	channels := index.ChannelsForDungeon(201)
	if len(channels) != 1 || channels[0].ID != 11 {
		t.Fatalf("dungeon 201 channels mismatch: %+v", channels)
	}
}

func TestParseAcceptsInlineServerID(t *testing.T) {
	const text = `
[dungeon]
` + "`[granfloris]`" + `
<4::channel_info_dname_1>
3
5
[/dungeon]
[server] 1
10 ` + "`<chn_channel_info_028>`" + ` 11 ` + "`[granfloris]`" + ` 5 0.0 0.0 ` + "``" + `
19 ` + "`<chn_channel_info_028>`" + ` 11 ` + "`[granfloris]`" + ` 5 0 0 ` + "``" + `
[/server]
`
	index, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	channels := index.ChannelsForServer(1)
	if len(channels) != 2 || channels[0].ID != 10 || channels[1].ID != 19 {
		t.Fatalf("inline server channels mismatch: %+v", channels)
	}
	if channels[0].NameKey != "chn_channel_info_028" ||
		channels[0].AreaKey != "granfloris" ||
		channels[0].Type != 11 {
		t.Fatalf("inline server channel mismatch: %+v", channels[0])
	}
}

func TestParseRejectsInvalidInlineServerID(t *testing.T) {
	_, err := Parse([]byte("[server] invalid\n[/server]\n"))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse() error = %v, want ErrInvalid", err)
	}
}

func TestParseAcceptsPackedPVFChannelInfo(t *testing.T) {
	const text = `
[dungeon]
` + "`[elven_guard]` `艾尔文防线` 1 2" + `
[/dungeon]
[server]
1 1 ` + "`洛兰`" + ` 2 ` + "`[elven_guard]`" + ` 10 0 0 6 ` + "`交易 - 拍卖行`" + ` 3 ` + "`[none]`" + ` 0 0
[/server]
[server]
98 1 ` + "`内部测试`" + ` 2 ` + "`[elven_guard]`" + ` 9 0
[/server]
`
	index, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if channels := index.Channels(); len(channels) != 3 {
		t.Fatalf("Channels() len = %d, want 3: %+v", len(channels), channels)
	}
	channel, ok := index.ServerChannel(1, 1)
	if !ok {
		t.Fatalf("server 1 channel 1 should exist")
	}
	if channel.NameKey != "洛兰" || channel.AreaKey != "elven_guard" {
		t.Fatalf("server channel mismatch: %+v", channel)
	}
	if channel, ok := index.ServerChannel(98, 1); !ok || channel.NameKey != "内部测试" {
		t.Fatalf("server 98 internal channel should be parsed, got %+v ok=%v", channel, ok)
	}
	if got := index.ChannelsForServer(98); len(got) != 1 || got[0].ServerID != 98 {
		t.Fatalf("server 98 channels mismatch: %+v", got)
	}
	routed := index.ChannelsForDungeon(1)
	if len(routed) != 2 {
		t.Fatalf("dungeon 1 routed channels len = %d, want 2: %+v", len(routed), routed)
	}
	if routed[0].ServerID != 1 || routed[0].ID != 1 {
		t.Fatalf("dungeon route should keep server 1 first: %+v", routed)
	}
}

func TestParseRejectsConflictingDuplicateChannel(t *testing.T) {
	const text = `
[server]
1
11 ` + "`<4::channel_info_cname_11>`" + ` 0 ` + "`[skycastle]`" + `
11 ` + "`<4::channel_info_cname_11>`" + ` 0 ` + "`[granfloris]`" + `
[/server]
`
	_, err := Parse([]byte(text))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse() error = %v, want ErrInvalid", err)
	}
}

func TestLoadUsesExternalFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "channel_info.etc")
	if err := os.WriteFile(filePath, []byte(routeFixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reader := textReader{
		"Etc/channel_info.etc": routeFixture,
	}

	index, source, err := Load(reader, LoadOptions{
		Paths:    []string{"missing/channel_info.etc", "Etc/channel_info.etc"},
		FilePath: filePath,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if source.Kind != SourceFile || source.Path != filePath {
		t.Fatalf("source mismatch: %+v", source)
	}
	if _, ok := index.Channel(10); !ok {
		t.Fatalf("channel 10 should exist")
	}
}

func TestLoadDoesNotReadPVFWhenExternalFileIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "channel_info.etc")
	reader := textReader{"Etc/channel_info.etc": routeFixture}

	_, _, err := Load(reader, LoadOptions{
		Paths:    []string{"missing/channel_info.etc", "Etc/channel_info.etc"},
		FilePath: filePath,
	})
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("Load() error = %v, want ErrMissing", err)
	}
}

func TestLoadRejectsInvalidExternalFileWithoutPVFFallback(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "channel_info.etc")
	if err := os.WriteFile(filePath, []byte("[server]\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reader := textReader{"Etc/channel_info.etc": routeFixture}

	_, _, err := Load(reader, LoadOptions{
		Paths:    []string{"Etc/channel_info.etc"},
		FilePath: filePath,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Load() error = %v, want ErrInvalid", err)
	}
}

func TestParseRejectsEmptyInput(t *testing.T) {
	_, err := Parse([]byte("  \n // empty"))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse() error = %v, want ErrInvalid", err)
	}
}

func TestLoadRealChannelInfoFileWhenConfigured(t *testing.T) {
	path := os.Getenv("DNF_TEST_CHANNEL_INFO_PATH")
	if path == "" {
		t.Skip("DNF_TEST_CHANNEL_INFO_PATH not set")
	}
	index, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", path, err)
	}
	if len(index.Areas()) == 0 || len(index.Channels()) == 0 {
		t.Fatalf("real channel_info.etc parsed empty index")
	}
}

type textReader map[string]string

func (r textReader) ReadText(path string) (string, error) {
	text, ok := r[path]
	if !ok {
		return "", os.ErrNotExist
	}
	return text, nil
}
