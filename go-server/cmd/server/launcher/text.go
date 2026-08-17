package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func summarizeControlError(output string, commandErr error) string {
	output = strings.TrimSpace(output)
	if output == "" {
		output = commandErr.Error()
	}
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "this username is already registered"):
		return "该账号已注册，请直接点击“进入游戏”。"
	case strings.Contains(lower, "username or password is incorrect"):
		return "账号或密码错误。"
	case strings.Contains(lower, "client file is missing"):
		return "客户端文件不完整，请确认五个客户端文件已经替换。"
	case strings.Contains(lower, "client file is incompatible"):
		return "客户端文件版本不匹配，请重新替换对应版本文件。"
	case strings.Contains(lower, "client directory is not configured"),
		strings.Contains(lower, "尚未选择游戏客户端"):
		return "尚未选择有效客户端，请点击“选择客户端”。"
	}
	const maxCharacters = 96
	if utf8.RuneCountInString(output) > maxCharacters {
		runes := []rune(output)
		output = string(runes[len(runes)-maxCharacters:])
	}
	output = strings.ReplaceAll(output, "\r", " ")
	output = strings.ReplaceAll(output, "\n", " ")
	output = strings.Join(strings.Fields(output), " ")
	return fmt.Sprintf("操作失败：%s", output)
}
