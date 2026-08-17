// 本文件定义 C# USERINFO legacy 表的只读仓储接口。
package repository

import "context"

// LegacyUserInfoRepository 只按白名单表后缀读取 C# USERINFO legacy 表。
// 它不负责 wire 编码；旧客户端 packet body 仍由 dnfbridge 按协议证据构造。
type LegacyUserInfoRepository interface {
	Check(context.Context) error
	SelectRows(ctx context.Context, characterID string, tableSuffix string, columns []string, orderBy []string) ([]LegacyUserInfoRow, error)
	SelectOne(ctx context.Context, characterID string, tableSuffix string, columns []string) (LegacyUserInfoRow, bool, error)
}

// LegacyUserInfoRow 是 legacy 表一行的字符串化列值。
// 调用方按 C# builder 证据把列值转成 u8/u16/u32 或文本。
type LegacyUserInfoRow map[string]string
