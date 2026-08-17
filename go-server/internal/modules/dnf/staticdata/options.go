// 本文件负责 DNF 静态数据装配选项的默认值和列表去重。
package staticdata

import (
	"path"
	"strings"

	"longheng.io/server/internal/modules/dnf/drop"
	"longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/equip"
	"longheng.io/server/internal/modules/dnf/monster"
	"longheng.io/server/internal/modules/dnf/reward"
	"longheng.io/server/internal/modules/dnf/skill"
)

// DefaultLists 返回静态数据总装配默认需要解析的 `.lst` 路径。
func DefaultLists() []string {
	return []string{
		equip.DefaultList,
		skill.DefaultList,
		monster.DefaultList,
		dungeon.DefaultList,
		drop.DefaultList,
		reward.DefaultList,
	}
}

func normalizeOptions(options Options) Options {
	if options.Equip.ListPath == "" {
		options.Equip.ListPath = equip.DefaultList
	}
	if options.Skill.ListPath == "" {
		options.Skill.ListPath = skill.DefaultList
	}
	if options.Monster.ListPath == "" {
		options.Monster.ListPath = monster.DefaultList
	}
	if options.Dungeon.ListPath == "" {
		options.Dungeon.ListPath = dungeon.DefaultList
	}
	if options.Drop.ListPath == "" {
		options.Drop.ListPath = drop.DefaultList
	}
	if options.Reward.ListPath == "" {
		options.Reward.ListPath = reward.DefaultList
	}
	options.Build.Lists = mergeLists(options.Build.Lists, []string{
		options.Equip.ListPath,
		options.Skill.ListPath,
		options.Monster.ListPath,
		options.Dungeon.ListPath,
		options.Drop.ListPath,
		options.Reward.ListPath,
	})
	return options
}

func mergeLists(base []string, required []string) []string {
	out := make([]string, 0, len(base)+len(required))
	seen := make(map[string]struct{}, len(base)+len(required))
	for _, value := range append(append([]string(nil), base...), required...) {
		key := listKey(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleanList(value))
	}
	return out
}

func listKey(value string) string {
	return strings.ToLower(cleanList(value))
}

func cleanList(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.HasPrefix(value, "./") || strings.HasPrefix(value, "/") {
		value = strings.TrimPrefix(value, "./")
		value = strings.TrimPrefix(value, "/")
	}
	if value == "" {
		return ""
	}
	clean := strings.TrimSuffix(path.Clean(value), "/")
	if clean == "." {
		return ""
	}
	return clean
}
