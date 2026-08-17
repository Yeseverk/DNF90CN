package channelinfo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

var (
	ErrMissing = errors.New("dnf channel info is missing")
	ErrInvalid = errors.New("dnf channel info is invalid")
)

// SourceKind 标记 channel_info.etc 的实际来源，便于启动日志和验收证据区分。
type SourceKind string

const (
	SourcePVF  SourceKind = "pvf"
	SourceFile SourceKind = "file"
)

// Source 记录本次加载命中的来源位置。
type Source struct {
	Kind SourceKind
	Path string
}

// Reader 是 PVF archive 或其他资源包需要实现的最小文本读取接口；未找到路径时应返回 os.ErrNotExist。
type Reader interface {
	ReadText(path string) (string, error)
}

// LoadOptions 配置外部活配置路径；Paths 仅供离线 PVF 导出路径复用。
type LoadOptions struct {
	Paths    []string
	FilePath string
}

// Channel 是单条 DNF 频道到区域的路由配置。
type Channel struct {
	ServerID  int
	ID        int
	NameKey   string
	Type      int
	AreaKey   string
	RawFields []string
}

// Area 是 DNF 区域到副本 ID 的映射。
type Area struct {
	Key        string
	NameKey    string
	DungeonIDs []int
}

// Index 是启动时构建好的只读查询索引，查询路径不再重复解析 channel_info.etc。
type Index struct {
	channels          map[channelKey]Channel
	channelByID       map[int]channelKey
	channelOrder      []channelKey
	areas             map[string]Area
	areasByDungeon    map[int][]string
	channelsByDungeon map[int][]channelKey
	channelsByServer  map[int][]channelKey
}

type channelKey struct {
	serverID  int
	channelID int
}

// DefaultPaths 返回 PVF 中查找 channel_info.etc 的候选路径。
func DefaultPaths() []string {
	return []string{
		"channel_info.etc",
		"channel_info/channel_info.etc",
		"etc/channel_info.etc",
		"Etc/channel_info.etc",
		"script/channel_info.etc",
		"Script/channel_info.etc",
		"game/channel_info/channel_info.etc",
	}
}

// Load 只从外部 channel_info.etc 加载目录；PVF 文本读取请走 ReadPVFText。
func Load(_ Reader, options LoadOptions) (*Index, Source, error) {
	if path := strings.TrimSpace(options.FilePath); path != "" {
		index, err := LoadFile(path)
		if err == nil {
			return index, Source{Kind: SourceFile, Path: path}, nil
		}
		return nil, Source{}, fmt.Errorf("load external channel info %s: %w", path, err)
	}
	return nil, Source{}, ErrMissing
}

// ReadPVFText 从 PVF 候选路径中读取原始 channel_info.etc 文本，供加载和导出共用。
func ReadPVFText(reader Reader, paths []string) (string, Source, error) {
	if len(paths) == 0 {
		paths = DefaultPaths()
	}
	if reader != nil {
		for _, path := range paths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			text, err := reader.ReadText(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return "", Source{}, fmt.Errorf("read pvf channel info %s: %w", path, err)
			}
			return text, Source{Kind: SourcePVF, Path: path}, nil
		}
	}
	return "", Source{}, ErrMissing
}

// LoadFile 从外部文件加载 channel_info.etc；生产侧可把它当作可修改的活配置。
func LoadFile(path string) (*Index, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrMissing
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自启动配置或测试临时目录，调用方负责限定资源根目录。
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrMissing, path)
		}
		return nil, err
	}
	return Parse(data)
}

// Parse 解析 channel_info.etc 文本并一次性构建内存索引。
func Parse(data []byte) (*Index, error) {
	parser := newParser()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		parser.line++
		if err := parser.accept(cleanLine(scanner.Text())); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := parser.close(); err != nil {
		return nil, err
	}
	return buildIndex(parser.areas, parser.channels, parser.channelOrder)
}

// Channel 按频道 ID 查询路由，返回值已 clone，可被调用方安全修改。
func (i *Index) Channel(id int) (Channel, bool) {
	if i == nil {
		return Channel{}, false
	}
	key, ok := i.channelByID[id]
	if !ok {
		return Channel{}, false
	}
	channel := i.channels[key]
	return cloneChannel(channel), true
}

// ServerChannel 按服务器组和频道 ID 查询精确路由。
func (i *Index) ServerChannel(serverID, channelID int) (Channel, bool) {
	if i == nil {
		return Channel{}, false
	}
	channel, ok := i.channels[channelKey{serverID: serverID, channelID: channelID}]
	if !ok {
		return Channel{}, false
	}
	return cloneChannel(channel), true
}

// Channels 返回全部频道路由，顺序与外部 channel_info.etc 保持一致。
func (i *Index) Channels() []Channel {
	if i == nil {
		return nil
	}
	out := make([]Channel, 0, len(i.channelOrder))
	for _, key := range i.channelOrder {
		out = append(out, cloneChannel(i.channels[key]))
	}
	return out
}

// ChannelsForServer 返回指定服务器组的全部频道，顺序与外部 channel_info.etc 保持一致。
func (i *Index) ChannelsForServer(serverID int) []Channel {
	if i == nil {
		return nil
	}
	keys := cloneChannelKeys(i.channelsByServer[serverID])
	out := make([]Channel, 0, len(keys))
	for _, key := range keys {
		if channel, ok := i.channels[key]; ok {
			out = append(out, cloneChannel(channel))
		}
	}
	return out
}

// Area 按区域 key 查询副本映射，返回值已 clone。
func (i *Index) Area(key string) (Area, bool) {
	if i == nil {
		return Area{}, false
	}
	area, ok := i.areas[areaKey(key)]
	if !ok {
		return Area{}, false
	}
	return cloneArea(area), true
}

// Areas 返回全部区域映射，按区域 key 升序排列。
func (i *Index) Areas() []Area {
	if i == nil {
		return nil
	}
	out := make([]Area, 0, len(i.areas))
	for _, area := range i.areas {
		out = append(out, cloneArea(area))
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Key < out[b].Key })
	return out
}

// AreasForDungeon 查询副本 ID 所属区域；同一副本出现在多区域时全部返回。
func (i *Index) AreasForDungeon(id int) []string {
	if i == nil {
		return nil
	}
	return cloneStrings(i.areasByDungeon[id])
}

// ChannelsForDungeon 查询副本 ID 可进入的频道列表。
func (i *Index) ChannelsForDungeon(id int) []Channel {
	if i == nil {
		return nil
	}
	channelIDs := i.channelsByDungeon[id]
	out := make([]Channel, 0, len(channelIDs))
	for _, key := range channelIDs {
		if channel, ok := i.channels[key]; ok {
			out = append(out, cloneChannel(channel))
		}
	}
	return out
}

type parser struct {
	areas        map[string]Area
	channels     map[channelKey]Channel
	channelOrder []channelKey
	section      string
	area         Area
	serverID     int
	stage        int
	line         int
}

func newParser() *parser {
	return &parser{
		areas:    make(map[string]Area),
		channels: make(map[channelKey]Channel),
		serverID: dnfenum.LoginChannelServerIndex,
	}
}

func (p *parser) accept(line string) error {
	if line == "" {
		return nil
	}
	if serverID, ok, err := parseServerSectionHeader(line); ok {
		if err != nil {
			return p.fail(err.Error())
		}
		p.section = "server"
		p.serverID = serverID
		return nil
	}
	switch strings.ToLower(line) {
	case "[dungeon]":
		p.section = "dungeon"
		p.area = Area{}
		p.stage = 0
		return nil
	case "[/dungeon]":
		return p.endDungeon()
	case "[/server]":
		p.section = ""
		return nil
	}
	switch p.section {
	case "dungeon":
		return p.readDungeon(line)
	case "server":
		return p.readServer(line)
	default:
		return nil
	}
}

func (p *parser) readDungeon(line string) error {
	fields := splitFields(line)
	if len(fields) == 0 {
		return nil
	}
	if p.stage == 0 {
		key := areaKey(fields[0])
		if key == "" {
			return p.fail("dungeon area key is required")
		}
		p.area.Key = key
		if len(fields) == 1 {
			p.stage = 1
			return nil
		}
		p.area.NameKey = textKey(fields[1])
		if err := p.appendDungeonIDs(fields[2:]); err != nil {
			return err
		}
		p.stage = 2
		return nil
	}
	if p.stage == 1 {
		if isInt(fields[0]) {
			p.stage = 2
			return p.appendDungeonIDs(fields)
		}
		p.area.NameKey = textKey(fields[0])
		if err := p.appendDungeonIDs(fields[1:]); err != nil {
			return err
		}
		p.stage = 2
		return nil
	}
	return p.appendDungeonIDs(fields)
}

func (p *parser) appendDungeonIDs(fields []string) error {
	for _, field := range fields {
		id, err := strconv.Atoi(field)
		if err != nil {
			return p.fail("dungeon id must be integer")
		}
		if id < 0 {
			return p.fail("dungeon id must not be negative")
		}
		p.area.DungeonIDs = appendInt(p.area.DungeonIDs, id)
	}
	return nil
}

func (p *parser) readServer(line string) error {
	fields := splitFields(line)
	if len(fields) == 1 && isInt(fields[0]) {
		serverID, err := strconv.Atoi(fields[0])
		if err != nil || serverID < 0 {
			return p.fail("server id must be non-negative integer")
		}
		p.serverID = serverID
		return nil
	}
	if len(fields) >= 5 && isInt(fields[0]) && isChannelStart(fields, 1) {
		serverID, err := strconv.Atoi(fields[0])
		if err != nil || serverID < 0 {
			return p.fail("server id must be non-negative integer")
		}
		p.serverID = serverID
		return p.readPackedServer(fields[1:])
	}
	return p.readServerRow(p.serverID, fields)
}

func (p *parser) readPackedServer(fields []string) error {
	for start := 0; start < len(fields); {
		if !isChannelStart(fields, start) {
			return p.fail("packed server row has invalid channel header")
		}
		end := len(fields)
		for next := start + 4; next < len(fields); next++ {
			if isChannelStart(fields, next) {
				end = next
				break
			}
		}
		if err := p.readServerRow(p.serverID, fields[start:end]); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func (p *parser) readServerRow(serverID int, fields []string) error {
	if len(fields) < 4 {
		return p.fail("server row requires channel id, name, type and area")
	}
	id, err := strconv.Atoi(fields[0])
	if err != nil || id < 0 {
		return p.fail("channel id must be non-negative integer")
	}
	channelType, err := strconv.Atoi(fields[2])
	if err != nil {
		return p.fail("channel type must be integer")
	}
	channel := Channel{
		ServerID:  serverID,
		ID:        id,
		NameKey:   textKey(fields[1]),
		Type:      channelType,
		AreaKey:   areaKey(fields[3]),
		RawFields: cloneStrings(fields[4:]),
	}
	if channel.NameKey == "" || channel.AreaKey == "" {
		return p.fail("channel name and area are required")
	}
	return p.addChannel(channel)
}

func (p *parser) endDungeon() error {
	if p.section != "dungeon" || p.area.Key == "" {
		return p.fail("dungeon block is incomplete")
	}
	p.section = ""
	p.stage = 0
	return p.addArea(p.area)
}

func (p *parser) close() error {
	if p.section != "" {
		return p.fail("section is not closed")
	}
	if len(p.areas) == 0 && len(p.channels) == 0 {
		return fmt.Errorf("%w: no dungeon or server block", ErrInvalid)
	}
	return nil
}

func (p *parser) addArea(area Area) error {
	if area.Key == "" {
		return p.fail("dungeon area key is required")
	}
	if existing, ok := p.areas[area.Key]; ok {
		if existing.NameKey != "" && area.NameKey != "" && existing.NameKey != area.NameKey {
			return p.fail("duplicate dungeon area has different name")
		}
		if existing.NameKey == "" {
			existing.NameKey = area.NameKey
		}
		for _, id := range area.DungeonIDs {
			existing.DungeonIDs = appendInt(existing.DungeonIDs, id)
		}
		p.areas[area.Key] = existing
		return nil
	}
	area.DungeonIDs = cloneInts(area.DungeonIDs)
	p.areas[area.Key] = area
	return nil
}

func (p *parser) addChannel(channel Channel) error {
	key := channelKey{serverID: channel.ServerID, channelID: channel.ID}
	if existing, ok := p.channels[key]; ok {
		if sameChannel(existing, channel) {
			return nil
		}
		return p.fail("duplicate server channel has different route")
	}
	p.channels[key] = cloneChannel(channel)
	p.channelOrder = append(p.channelOrder, key)
	return nil
}

func (p *parser) fail(message string) error {
	return fmt.Errorf("%w: line %d: %s", ErrInvalid, p.line, message)
}

func buildIndex(areas map[string]Area, channels map[channelKey]Channel, order []channelKey) (*Index, error) {
	if len(areas) == 0 && len(channels) == 0 {
		return nil, ErrMissing
	}
	order = normalizeChannelOrder(order, channels)
	index := &Index{
		channels:          make(map[channelKey]Channel, len(channels)),
		channelByID:       make(map[int]channelKey, len(channels)),
		channelOrder:      cloneChannelKeys(order),
		areas:             make(map[string]Area, len(areas)),
		areasByDungeon:    make(map[int][]string),
		channelsByDungeon: make(map[int][]channelKey),
		channelsByServer:  make(map[int][]channelKey),
	}
	for key, area := range areas {
		area = cloneArea(area)
		index.areas[key] = area
		for _, dungeonID := range area.DungeonIDs {
			index.areasByDungeon[dungeonID] = appendString(index.areasByDungeon[dungeonID], key)
		}
	}
	areaChannels := make(map[string][]channelKey)
	for _, key := range order {
		channel := channels[key]
		channel = cloneChannel(channel)
		index.channels[key] = channel
		if _, ok := index.channelByID[channel.ID]; !ok {
			index.channelByID[channel.ID] = key
		}
		index.channelsByServer[channel.ServerID] = appendChannelKey(index.channelsByServer[channel.ServerID], key)
		areaChannels[channel.AreaKey] = appendChannelKey(areaChannels[channel.AreaKey], key)
	}
	for dungeonID, areaKeys := range index.areasByDungeon {
		for _, areaKey := range areaKeys {
			for _, key := range areaChannels[areaKey] {
				index.channelsByDungeon[dungeonID] = appendChannelKey(index.channelsByDungeon[dungeonID], key)
			}
		}
	}
	return index, nil
}

func normalizeChannelOrder(order []channelKey, channels map[channelKey]Channel) []channelKey {
	out := make([]channelKey, 0, len(channels))
	seen := make(map[channelKey]struct{}, len(channels))
	for _, key := range order {
		if _, ok := channels[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == len(channels) {
		return out
	}
	for _, key := range sortedChannelKeys(channels) {
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, key)
	}
	return out
}

func cleanLine(line string) string {
	line = strings.TrimPrefix(line, "\ufeff")
	for _, mark := range []string{"//", "#", ";"} {
		if index := strings.Index(line, mark); index >= 0 {
			line = line[:index]
		}
	}
	return strings.TrimSpace(line)
}

func splitFields(line string) []string {
	fields := make([]string, 0, 8)
	var builder strings.Builder
	inBacktick := false
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		fields = append(fields, builder.String())
		builder.Reset()
	}
	for _, char := range line {
		if char == '`' {
			inBacktick = !inBacktick
			builder.WriteRune(char)
			continue
		}
		if unicode.IsSpace(char) && !inBacktick {
			flush()
			continue
		}
		builder.WriteRune(char)
	}
	flush()
	return fields
}

func parseServerSectionHeader(line string) (int, bool, error) {
	fields := splitFields(line)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "[server]") {
		return 0, false, nil
	}
	if len(fields) == 1 {
		return dnfenum.LoginChannelServerIndex, true, nil
	}
	if len(fields) != 2 {
		return 0, true, errors.New("server section header accepts at most one server id")
	}
	serverID, err := strconv.Atoi(fields[1])
	if err != nil || serverID < 0 {
		return 0, true, errors.New("server id must be non-negative integer")
	}
	return serverID, true, nil
}

func textKey(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "`")
	value = strings.TrimPrefix(value, "<")
	value = strings.TrimSuffix(value, ">")
	value = strings.TrimPrefix(value, "4::")
	return strings.TrimSpace(value)
}

func areaKey(value string) string {
	value = textKey(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	return strings.ToLower(strings.TrimSpace(value))
}

func isInt(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}

func isChannelStart(fields []string, index int) bool {
	return index >= 0 && index+3 < len(fields) &&
		isInt(fields[index]) && isQuoted(fields[index+1]) &&
		isInt(fields[index+2]) && isQuoted(fields[index+3])
}

func isQuoted(value string) bool {
	return len(value) >= 2 && value[0] == '`' && value[len(value)-1] == '`'
}

func sameChannel(left, right Channel) bool {
	return left.ServerID == right.ServerID &&
		left.ID == right.ID &&
		left.NameKey == right.NameKey &&
		left.Type == right.Type &&
		left.AreaKey == right.AreaKey &&
		sameStrings(left.RawFields, right.RawFields)
}

func sortedChannelKeys(channels map[channelKey]Channel) []channelKey {
	keys := make([]channelKey, 0, len(channels))
	for key := range channels {
		keys = append(keys, key)
	}
	sortChannelKeys(keys)
	return keys
}

func sortChannelKeys(keys []channelKey) {
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].serverID != keys[right].serverID {
			return keys[left].serverID < keys[right].serverID
		}
		return keys[left].channelID < keys[right].channelID
	})
}

func cloneChannelKeys(keys []channelKey) []channelKey {
	return append([]channelKey(nil), keys...)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func cloneChannel(channel Channel) Channel {
	channel.RawFields = cloneStrings(channel.RawFields)
	return channel
}

func cloneArea(area Area) Area {
	area.DungeonIDs = cloneInts(area.DungeonIDs)
	return area
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneInts(values []int) []int {
	return append([]int(nil), values...)
}

func appendString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func appendInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendChannelKey(values []channelKey, value channelKey) []channelKey {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
