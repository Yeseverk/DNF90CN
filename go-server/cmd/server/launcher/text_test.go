package main

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSummarizeControlErrorUsesBoundedSingleLine(t *testing.T) {
	got := summarizeControlError(
		"first line\r\n"+strings.Repeat("错误", 200),
		errors.New("exit status 1"),
	)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("summary contains a newline: %q", got)
	}
	if utf8.RuneCountInString(got) > 106 {
		t.Fatalf("summary is not bounded: %d runes", utf8.RuneCountInString(got))
	}
}

func TestSummarizeControlErrorFallsBackToCommandError(t *testing.T) {
	got := summarizeControlError("", errors.New("exit status 1"))
	if !strings.Contains(got, "exit status 1") {
		t.Fatalf("summary = %q", got)
	}
}

func TestSummarizeControlErrorUsesFriendlyLoginMessages(t *testing.T) {
	got := summarizeControlError(
		"FAILED: this username is already registered",
		errors.New("exit status 1"),
	)
	if got != "该账号已注册，请直接点击“进入游戏”。" {
		t.Fatalf("summary = %q", got)
	}
}
