// catalog.go 负责把 DNF channel_info.etc 转成频道广告和 game 监听共用的只读目录。
package channelcatalog

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/channelinfo"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

var ErrEmpty = errors.New("dnf channel catalog is empty")

// Channel 是协议广告和游戏端口监听共用的频道视图。
type Channel struct {
	ServerID       int
	ID             int
	Type           uint8
	Group          string
	AreaDungeonIDs []int
	Name           string
	// NoticeName 只用于 game class0/op1 首包；必须和 channel_info.etc 的频道 ID 对齐。
	NoticeName   string
	MaxUsers     int
	CurrentUsers int
	Port         int
}

// Options 控制频道目录生成规则；零值使用 DNF 默认端口和人数。
type Options struct {
	ServerID     int
	GamePortBase int
	MaxUsers     int
	CurrentUsers int
}

// Catalog 是从 channel_info.etc 派生的只读频道目录。
type Catalog struct {
	channels []Channel
	byID     map[int]Channel
	byPort   map[int]Channel
}

// New 从解析好的 channel_info.etc 索引构建游戏频道目录。
func New(index *channelinfo.Index, options Options) (*Catalog, error) {
	options = normalizeOptions(options)
	source := index.ChannelsForServer(options.ServerID)
	if len(source) == 0 {
		return nil, ErrEmpty
	}
	channels := make([]Channel, 0, len(source))
	seen := make(map[int]struct{}, len(source))
	for _, item := range source {
		if item.ID <= 0 {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		area, _ := index.Area(item.AreaKey)
		channels = append(channels, Channel{
			ServerID:       item.ServerID,
			ID:             item.ID,
			Type:           clampByte(item.Type),
			Group:          normalizeGroup(item.AreaKey),
			AreaDungeonIDs: cloneInts(area.DungeonIDs),
			Name:           dnfenum.ChannelNamePrefix + strconv.Itoa(item.ID),
			NoticeName:     channelNoticeName(item),
			MaxUsers:       options.MaxUsers,
			CurrentUsers:   options.CurrentUsers,
			Port:           options.GamePortBase + item.ID,
		})
	}
	if len(channels) == 0 {
		return nil, ErrEmpty
	}
	catalog := &Catalog{
		channels: cloneChannels(channels),
		byID:     make(map[int]Channel, len(channels)),
		byPort:   make(map[int]Channel, len(channels)),
	}
	for _, channel := range channels {
		catalog.byID[channel.ID] = channel
		catalog.byPort[channel.Port] = channel
	}
	return catalog, nil
}

// Channels 返回全部可广告频道，顺序与外部 channel_info.etc 保持一致。
func (c *Catalog) Channels() []Channel {
	if c == nil {
		return nil
	}
	return cloneChannels(c.channels)
}

// FilterForRequest 按客户端请求的频道组过滤广告列表。
func (c *Catalog) FilterForRequest(group string) []Channel {
	if c == nil {
		return nil
	}
	normalized := normalizeGroup(group)
	if normalized == "" || normalized == dnfenum.GroupCain {
		return filterChannels(c.channels, func(channel Channel) bool {
			return !isHiddenRaid(channel)
		})
	}
	if normalized == dnfenum.GroupRaid || normalized == dnfenum.GroupAttackRaid {
		return filterChannels(c.channels, isRaid)
	}
	matched := filterChannels(c.channels, func(channel Channel) bool {
		return normalizeGroup(channel.Group) == normalized
	})
	if len(matched) > 0 {
		return matched
	}
	return filterChannels(c.channels, func(channel Channel) bool {
		return !isRaid(channel)
	})
}

// FilterForBootstrap returns the directory used immediately after the client
// downloads channel_info.etc. Native tower and trade entrances cannot become
// the resident town channel during that bootstrap and are left for a later
// full directory refresh.
func (c *Catalog) FilterForBootstrap(group string) []Channel {
	return filterChannels(c.FilterForRequest(group), func(channel Channel) bool {
		return !isBootstrapEntrance(channel)
	})
}

// Channel 按频道 ID 查询目录项。
func (c *Catalog) Channel(id int) (Channel, bool) {
	if c == nil {
		return Channel{}, false
	}
	channel, ok := c.byID[id]
	return cloneChannel(channel), ok
}

// ForPort 按本地游戏端口反查频道。
func (c *Catalog) ForPort(port int) (Channel, bool) {
	if c == nil {
		return Channel{}, false
	}
	channel, ok := c.byPort[port]
	return cloneChannel(channel), ok
}

// ResidentFor returns the ordinary town channel that the game client should
// commit after using a special bootstrap channel such as [crack]. The result
// is derived from channel_info.etc order; no channel id is fabricated here.
func (c *Catalog) ResidentFor(connected Channel) (Channel, bool) {
	if c == nil {
		return Channel{}, false
	}
	if candidate, ok := c.byID[connected.ID]; ok &&
		candidate.ServerID == connected.ServerID && isResidentChannel(candidate) {
		return cloneChannel(candidate), true
	}
	for _, channel := range c.channels {
		if isResidentChannel(channel) {
			return cloneChannel(channel), true
		}
	}
	return Channel{}, false
}

// GamePorts 返回需要监听的游戏端口，按端口升序排列。
func (c *Catalog) GamePorts() []int {
	if c == nil {
		return nil
	}
	ports := make([]int, 0, len(c.byPort))
	for port := range c.byPort {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func normalizeOptions(options Options) Options {
	if options.ServerID <= 0 {
		options.ServerID = dnfenum.LoginChannelServerIndex
	}
	if options.GamePortBase <= 0 {
		options.GamePortBase = dnfenum.GamePortBase
	}
	if options.MaxUsers <= 0 {
		options.MaxUsers = dnfenum.DefaultChannelMaxUsers
	}
	if options.CurrentUsers < 0 {
		options.CurrentUsers = dnfenum.DefaultChannelCurrentUsers
	}
	return options
}

func filterChannels(channels []Channel, accept func(Channel) bool) []Channel {
	out := make([]Channel, 0, len(channels))
	for _, channel := range channels {
		if accept(channel) {
			out = append(out, cloneChannel(channel))
		}
	}
	return out
}

func isRaid(channel Channel) bool {
	return channel.Type == dnfenum.RaidChannelType ||
		channel.Type == dnfenum.HiddenRaidType ||
		normalizeGroup(channel.Group) == dnfenum.GroupRaid
}

func isHiddenRaid(channel Channel) bool {
	return channel.Type == dnfenum.HiddenRaidType
}

func isBootstrapEntrance(channel Channel) bool {
	return channel.Type == 2 || channel.Type == 3
}

func isResidentChannel(channel Channel) bool {
	group := normalizeGroup(channel.Group)
	if group == "" || group == "none" || group == dnfenum.GroupCrack ||
		group == dnfenum.GroupDeathTower || group == dnfenum.GroupTrade ||
		group == dnfenum.GroupRaid || group == dnfenum.GroupAttackRaid {
		return false
	}
	return channel.Type != dnfenum.DeathTowerChannelType &&
		channel.Type != dnfenum.TradeChannelType &&
		channel.Type != dnfenum.RaidChannelType &&
		channel.Type != dnfenum.HiddenRaidType
}

func normalizeGroup(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "`")
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	return strings.ToLower(strings.TrimSpace(value))
}

func clampByte(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func channelNoticeName(item channelinfo.Channel) string {
	// NameKey 是显示资源名，不能拿后缀拼出 etc 里不存在的频道。
	return dnfenum.ChannelNamePrefix + strconv.Itoa(item.ID)
}

func cloneChannels(channels []Channel) []Channel {
	out := make([]Channel, len(channels))
	for idx, channel := range channels {
		out[idx] = cloneChannel(channel)
	}
	return out
}

func cloneChannel(channel Channel) Channel {
	channel.AreaDungeonIDs = cloneInts(channel.AreaDungeonIDs)
	return channel
}

func cloneInts(values []int) []int {
	return append([]int(nil), values...)
}
