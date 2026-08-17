package channelinfo

import (
	"strconv"
	"strings"
	"testing"
)

func TestBuild90CNOnlineScriptExpandsPackedSourceServer(t *testing.T) {
	data := []byte("[dungeon]\n`[sky_catle]` `sky` 11 12\n[/dungeon]\n[server]\n1 1 `tower` 2 `[none]` 0 0 0 6 `trade` 3 `[none]` 0 0 0 10 `forest` 0 `[granfloris]` 5 0 0 19 `crack` 1 `[crack]` 5 0 0 1 0 0 0\n[/server]\n[server]\n2 19 `other` 0 `[sky_catle]` 3 0 0\n[/server]\n")

	script, err := Build90CNOnlineScript(data, 1, 0, 19)
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, want := range []string{
		"[dungeon]\n`[sky_catle]` `sky` 11 12\n[/dungeon]",
		"[server]\n0\n",
		"1 `tower` 11 `[none]` 0 0 0 ``\n",
		"6 `trade` 3 `[none]` 0 0 0 ``\n",
		"10 `forest` 22 `[granfloris]` 5 0 0 ``\n",
		"19 `crack` 22 `[crack]` 5 0 0 1 0 0 0 ``\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("online script is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "2 19 `other`") {
		t.Fatalf("online script contains another server:\n%s", text)
	}

	index, err := Parse(script)
	if err != nil {
		t.Fatal(err)
	}
	channels := index.ChannelsForServer(0)
	if len(channels) != 4 {
		t.Fatalf("online channels = %d, want 4: %+v", len(channels), channels)
	}
	if channels[0].Type != 11 || channels[1].Type != 3 || channels[2].Type != 22 || channels[3].Type != 22 {
		t.Fatalf("online channel types = %d/%d/%d/%d", channels[0].Type, channels[1].Type, channels[2].Type, channels[3].Type)
	}
}

func TestBuild90CNOnlineScriptAcceptsInlineServerID(t *testing.T) {
	data := []byte("[dungeon]\n`[granfloris]`\n<4::channel_info_dname_1>\n3\n[/dungeon]\n[server] 1\n10 `<chn_channel_info_028>` 11 `[granfloris]` 5 0.0 0.0 ``\n19 `<chn_channel_info_028>` 11 `[granfloris]` 5 0 0 ``\n[/server]\n")

	script, err := Build90CNOnlineScript(data, 1, 0, 19)
	if err != nil {
		t.Fatal(err)
	}
	index, err := Parse(script)
	if err != nil {
		t.Fatal(err)
	}
	channels := index.ChannelsForServer(0)
	if len(channels) != 2 || channels[0].ID != 10 || channels[1].ID != 19 {
		t.Fatalf("online inline channels mismatch: %+v", channels)
	}
	if channels[0].NameKey != "chn_channel_info_028" ||
		channels[1].NameKey != "chn_channel_info_028" ||
		channels[0].AreaKey != "granfloris" ||
		channels[1].AreaKey != "granfloris" {
		t.Fatalf("online inline source mapping was not preserved: %+v", channels)
	}
}

func TestBuild90CNOnlineScriptPreservesSourceMappingForChannel38(t *testing.T) {
	data := []byte("[server]\n1\n19 `crack` 1 `[crack]` 5 0 0\n38 `trade` 3 `[trade]` 0 0 0\n[/server]\n")

	script, err := Build90CNOnlineScript(data, 1, 0, 19)
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if !strings.Contains(text, "38 `trade` 3 `[trade]`") {
		t.Fatalf("online script did not preserve channel 38 source mapping:\n%s", text)
	}
}

func TestBuild90CNOnlineScriptPreservesOriginPvpChannels(t *testing.T) {
	data := []byte("[dungeon]\n`[pvp]`\n<4::chn_channel_info_044>\n[/dungeon]\n[server] 1\n" +
		"19 `<chn_channel_info_028>` 11 `[granfloris]` 5 0 0 ``\n" +
		"501 `<chn_channel_info_039>` 24 `[pvp]` 0 0 0 ``\n" +
		"502 `<chn_channel_info_040>` 24 `[pvp]` 0 0 0 ``\n" +
		"503 `<chn_channel_info_041>` 24 `[pvp]` 0 0 0 ``\n" +
		"504 `<chn_channel_info_042>` 24 `[pvp]` 0 0 0 ``\n" +
		"505 `<chn_channel_info_043>` 24 `[pvp]` 0 0 0 ``\n" +
		"506 `<chn_channel_info_044>` 24 `[pvp]` 0 0 0 ``\n" +
		"507 `<chn_channel_info_045>` 24 `[pvp]` 0 0 0 ``\n" +
		"[/server]\n")

	script, err := Build90CNOnlineScript(data, 1, 0, 19)
	if err != nil {
		t.Fatal(err)
	}
	index, err := Parse(script)
	if err != nil {
		t.Fatal(err)
	}
	channels := index.ChannelsForServer(0)
	if len(channels) != 8 {
		t.Fatalf("online channels = %d, want 8: %+v", len(channels), channels)
	}
	for i, channel := range channels[1:] {
		wantID := 501 + i
		wantNameKey := "chn_channel_info_0" + strconv.Itoa(39+i)
		if channel.ID != wantID || channel.Type != 24 ||
			channel.AreaKey != "pvp" || channel.NameKey != wantNameKey {
			t.Fatalf("pvp channel %d mismatch: %+v", wantID, channel)
		}
	}
}

func TestBuild90CNOnlineScriptNormalizesExistingRecordTerminators(t *testing.T) {
	data := []byte("[server]\r\n1\r\n10 `forest` 1 `[granfloris]` 5 0 0 ``\r\n19 `crack` 1 `[crack]` 5 0 0 ``\r\n[/server]\r\n")

	script, err := Build90CNOnlineScript(data, 1, 0, 19)
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if strings.Contains(text, " `` ``\n") {
		t.Fatalf("online script contains duplicate record terminators:\n%s", text)
	}
	if got := strings.Count(text, " ``\n"); got != 2 {
		t.Fatalf("online script terminators = %d, want 2:\n%s", got, text)
	}
	index, err := Parse(script)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(index.ChannelsForServer(0)); got != 2 {
		t.Fatalf("online channels = %d, want 2", got)
	}
}

func TestOnlineTypeFor90CNSeparatesDirectoryTypeFromRawType(t *testing.T) {
	tests := []struct {
		channelID   int
		channelType int
		want        int
	}{
		{channelID: 10, channelType: 0, want: 22},
		{channelID: 31, channelType: 1, want: 22},
		{channelID: 1, channelType: 2, want: 11},
		{channelID: 6, channelType: 3, want: 3},
		{channelID: 51, channelType: 4, want: 4},
	}
	for _, test := range tests {
		if got := OnlineTypeFor90CN(test.channelID, test.channelType); got != test.want {
			t.Errorf("OnlineTypeFor90CN(%d, %d) = %d, want %d", test.channelID, test.channelType, got, test.want)
		}
	}
}

func TestBuild90CNOnlineScriptRequiresBootstrapChannel(t *testing.T) {
	_, err := Build90CNOnlineScript([]byte("[server]\n1 10 `forest` 1 `[granfloris]` 0 0\n[/server]\n"), 1, 1, 19)
	if err == nil || !strings.Contains(err.Error(), "missing bootstrap channel 19") {
		t.Fatalf("error = %v, want missing bootstrap channel", err)
	}
}

func TestBuild90CNOnlineScriptRejectsUnquotedChannelFields(t *testing.T) {
	_, err := Build90CNOnlineScript(
		[]byte("[server]\n1\n19 crack 1 [crack] 5 0 0\n[/server]\n"),
		1,
		0,
		19,
	)
	if err == nil || !strings.Contains(err.Error(), "invalid packed channel row") {
		t.Fatalf("error = %v, want invalid packed channel row", err)
	}
}
