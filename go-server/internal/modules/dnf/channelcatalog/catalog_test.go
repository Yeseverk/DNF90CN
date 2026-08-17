// catalog_test.go 验证 DNF channel_info.etc 频道目录和首包频道名派生规则。
package channelcatalog

import (
	"errors"
	"reflect"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelinfo"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const latestChannelInfoFixture = `
[dungeon]
` + "`[elven_guard]` `艾尔文防线` 1 2" + `
[/dungeon]
[dungeon]
` + "`[raid]` `团队频道` 200 201" + `
[/dungeon]
[server]
1 1 ` + "`洛兰`" + ` 2 ` + "`[elven_guard]`" + ` 10 0 0 6 ` + "`交易 - 拍卖行`" + ` 3 ` + "`[none]`" + ` 0 0 200 ` + "`团队频道`" + ` 23 ` + "`[raid]`" + ` 0 0 201 ` + "`隐藏团队频道`" + ` 32 ` + "`[raid]`" + ` 0 0
[/server]
[server]
98 1 ` + "`内部测试`" + ` 2 ` + "`[elven_guard]`" + ` 9 0
[/server]
`

func TestNewBuildsGameChannelCatalog(t *testing.T) {
	index := parseFixture(t)
	catalog, err := New(index, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	channels := catalog.Channels()
	if len(channels) != 4 {
		t.Fatalf("channels len = %d, want 4: %+v", len(channels), channels)
	}
	first := channels[0]
	if first.ID != 1 || first.Type != 2 || first.Group != "elven_guard" || first.Name != dnfenum.ChannelNamePrefix+"1" || first.Port != dnfenum.GamePortBase+1 || first.MaxUsers != dnfenum.DefaultChannelMaxUsers {
		t.Fatalf("first channel mismatch: %+v", first)
	}
	if !reflect.DeepEqual(first.AreaDungeonIDs, []int{1, 2}) {
		t.Fatalf("first channel area dungeon ids = %+v, want [1 2]", first.AreaDungeonIDs)
	}
	channels[0].AreaDungeonIDs[0] = 999
	again, ok := catalog.Channel(1)
	if !ok || !reflect.DeepEqual(again.AreaDungeonIDs, []int{1, 2}) {
		t.Fatalf("channel area dungeon ids should be cloned, got %+v ok=%v", again.AreaDungeonIDs, ok)
	}
	if _, ok := catalog.Channel(98); ok {
		t.Fatalf("server 98 channel should not enter catalog")
	}
	if got := catalog.GamePorts(); !reflect.DeepEqual(got, []int{dnfenum.GamePortBase + 1, dnfenum.GamePortBase + 6, dnfenum.GamePortBase + 200, dnfenum.GamePortBase + 201}) {
		t.Fatalf("GamePorts() = %+v", got)
	}
	channel, ok := catalog.ForPort(dnfenum.GamePortBase + 6)
	if !ok || channel.ID != 6 || channel.Type != 3 {
		t.Fatalf("ForPort(10006) = %+v, %v", channel, ok)
	}
}

func TestNewKeepsNoticeNameOnID(t *testing.T) {
	const text = "\n[dungeon]\n`[crack]` `crack` 1\n[/dungeon]\n[server]\n1 19 `<4::chn_channel_info_021>` 1 `[crack]` 0 0 6 `<4::chn_channel_info_002>` 3 `[crack]` 0 0\n[/server]\n"
	index, err := channelinfo.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	catalog, err := New(index, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	channel19, ok := catalog.Channel(19)
	if !ok {
		t.Fatalf("channel 19 missing")
	}
	if channel19.Name != dnfenum.ChannelNamePrefix+"19" || channel19.NoticeName != dnfenum.ChannelNamePrefix+"19" || channel19.Port != dnfenum.GamePortBase+19 {
		t.Fatalf("channel 19 mismatch: %+v", channel19)
	}
	channel6, ok := catalog.Channel(6)
	if !ok {
		t.Fatalf("channel 6 missing")
	}
	if channel6.Name != dnfenum.ChannelNamePrefix+"6" || channel6.NoticeName != dnfenum.ChannelNamePrefix+"6" || channel6.Port != dnfenum.GamePortBase+6 {
		t.Fatalf("channel 6 mismatch: %+v", channel6)
	}
}

func TestResidentForKeepsNormalChannelAndReplacesCrackBootstrap(t *testing.T) {
	const text = `
[dungeon]
` + "`[crack]` `crack`" + `
[/dungeon]
[dungeon]
` + "`[granfloris]` `granfloris` 1 2" + `
[/dungeon]
[server]
1 19 ` + "`crack`" + ` 1 ` + "`[crack]`" + ` 0 0 10 ` + "`granfloris`" + ` 1 ` + "`[granfloris]`" + ` 0 0 11 ` + "`sky`" + ` 1 ` + "`[granfloris]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	catalog, err := New(index, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	crack, _ := catalog.Channel(19)
	resident, ok := catalog.ResidentFor(crack)
	if !ok || resident.ID != 10 || resident.Group != "granfloris" {
		t.Fatalf("resident for crack = %+v, %v", resident, ok)
	}
	normal, _ := catalog.Channel(11)
	resident, ok = catalog.ResidentFor(normal)
	if !ok || resident.ID != 11 {
		t.Fatalf("resident for normal channel = %+v, %v", resident, ok)
	}
}

func TestFilterForRequest(t *testing.T) {
	catalog, err := New(parseFixture(t), Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := catalog.FilterForRequest("cain"); len(got) != 3 {
		t.Fatalf("cain channels len = %d, want 3: %+v", len(got), got)
	}
	raid := catalog.FilterForRequest("raid")
	if len(raid) != 2 || raid[0].ID != 200 || raid[1].ID != 201 {
		t.Fatalf("raid channels mismatch: %+v", raid)
	}
	elven := catalog.FilterForRequest("`[elven_guard]`")
	if len(elven) != 1 || elven[0].ID != 1 {
		t.Fatalf("elven channels mismatch: %+v", elven)
	}
	unknown := catalog.FilterForRequest("unknown")
	if len(unknown) != 2 || unknown[0].ID != 1 || unknown[1].ID != 6 {
		t.Fatalf("unknown group fallback mismatch: %+v", unknown)
	}
}

func TestFilterForBootstrapOmitsNativeEntrancesOnly(t *testing.T) {
	const text = `
[dungeon]
` + "`[deathtower]` `tower` 1" + `
[/dungeon]
[server]
1 1 ` + "`tower`" + ` 2 ` + "`[deathtower]`" + ` 0 0 6 ` + "`trade`" + ` 3 ` + "`[trade]`" + ` 0 0 19 ` + "`crack`" + ` 1 ` + "`[crack]`" + ` 0 0 31 ` + "`town`" + ` 1 ` + "`[granfloris]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	catalog, err := New(index, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	bootstrap := catalog.FilterForBootstrap(dnfenum.GroupCain)
	if len(bootstrap) != 2 || bootstrap[0].ID != 19 || bootstrap[1].ID != 31 {
		t.Fatalf("bootstrap channels = %+v, want 19/31", bootstrap)
	}
	full := catalog.FilterForRequest(dnfenum.GroupCain)
	if len(full) != 4 {
		t.Fatalf("full refresh channels = %d, want 4: %+v", len(full), full)
	}
}

func TestFilterUsesSourceTypeInsteadOfLegacyRaidIDs(t *testing.T) {
	const text = `
[dungeon]
` + "`[metro]` `metro` 1" + `
[/dungeon]
[dungeon]
` + "`[luke_raid]` `luke` 2" + `
[/dungeon]
[server] 1
201 ` + "`<chn_channel_info_024>`" + ` 11 ` + "`[metro]`" + ` 0 0
241 ` + "`<chn_channel_info_025>`" + ` 32 ` + "`[luke_raid]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	catalog, err := New(index, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ordinary := catalog.FilterForRequest(dnfenum.GroupCain)
	if len(ordinary) != 1 || ordinary[0].ID != 201 {
		t.Fatalf("ordinary channels = %+v, want source type-11 channel 201", ordinary)
	}
	raid := catalog.FilterForRequest(dnfenum.GroupRaid)
	if len(raid) != 1 || raid[0].ID != 241 {
		t.Fatalf("raid channels = %+v, want source type-32 channel 241", raid)
	}
}

func TestNewPreservesChannelInfoOrder(t *testing.T) {
	const text = `
[dungeon]
` + "`[trade]` `交易频道` 1" + `
[/dungeon]
[server]
1 1 ` + "`洛兰`" + ` 2 ` + "`[trade]`" + ` 0 0 38 ` + "`自动频道`" + ` 3 ` + "`[trade]`" + ` 0 0 10 ` + "`洛兰深处`" + ` 1 ` + "`[trade]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	catalog, err := New(index, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	channels := catalog.FilterForRequest("cain")
	if len(channels) != 3 || channels[0].ID != 1 || channels[1].ID != 38 || channels[2].ID != 10 {
		t.Fatalf("channel order mismatch: %+v", channels)
	}
}

func TestNewCanSelectConfiguredServer(t *testing.T) {
	catalog, err := New(parseFixture(t), Options{ServerID: 98})
	if err != nil {
		t.Fatalf("New(server 98) error = %v", err)
	}
	channels := catalog.Channels()
	if len(channels) != 1 || channels[0].ServerID != 98 || channels[0].ID != 1 {
		t.Fatalf("server 98 channels mismatch: %+v", channels)
	}
	if got := catalog.GamePorts(); !reflect.DeepEqual(got, []int{dnfenum.GamePortBase + 1}) {
		t.Fatalf("server 98 ports = %+v", got)
	}
}

func TestNewRejectsEmptyIndex(t *testing.T) {
	_, err := New(nil, Options{})
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("New(nil) error = %v, want ErrEmpty", err)
	}
}

func parseFixture(t *testing.T) *channelinfo.Index {
	t.Helper()
	index, err := channelinfo.Parse([]byte(latestChannelInfoFixture))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return index
}
