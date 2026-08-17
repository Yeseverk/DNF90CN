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
	const maxCharacters = 280
	if utf8.RuneCountInString(output) > maxCharacters {
		runes := []rune(output)
		output = string(runes[len(runes)-maxCharacters:])
	}
	output = strings.ReplaceAll(output, "\r", " ")
	output = strings.ReplaceAll(output, "\n", " ")
	output = strings.Join(strings.Fields(output), " ")
	return fmt.Sprintf("操作失败：%s", output)
}
