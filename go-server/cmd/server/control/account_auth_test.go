package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestNormalizeLocalUsername(t *testing.T) {
	tests := map[string]string{
		" Alice ": "alice",
		"测试账号":    "测试账号",
		"user-1":  "user-1",
	}
	for input, want := range tests {
		got, err := normalizeLocalUsername(input)
		if err != nil {
			t.Fatalf("normalizeLocalUsername(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeLocalUsername(%q) = %q, want %q", input, got, want)
		}
	}
	for _, invalid := range []string{"ab", "bad name", "bad/name", strings.Repeat("a", 33)} {
		if _, err := normalizeLocalUsername(invalid); err == nil {
			t.Fatalf("normalizeLocalUsername(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestLocalPasswordUsesBcryptAndRejectsUnsafeLengths(t *testing.T) {
	password := "correct-password"
	if err := validateLocalPassword(password); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong-password")); err == nil {
		t.Fatal("wrong password matched bcrypt hash")
	}
	for _, invalid := range []string{"short", strings.Repeat("x", 73), "bad\npassword"} {
		if err := validateLocalPassword(invalid); err == nil {
			t.Fatalf("validateLocalPassword(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestWriteActiveLocalAccountIDPreservesOtherInstanceFields(t *testing.T) {
	root := t.TempDir()
	paths := newProjectPaths(root)
	if err := os.MkdirAll(filepath.Dir(paths.instance), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := validTestInstance()
	cfg.Server.AdvertiseIP = autoDetectAdvertiseIP
	data, err := marshalInstance(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.instance, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeActiveLocalAccountID(paths, "dnf:account-two"); err != nil {
		t.Fatal(err)
	}
	updated, err := decodeInstance(paths.instance)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Server.AccountID != "dnf:account-two" {
		t.Fatalf("active account = %q", updated.Server.AccountID)
	}
	if updated.Server.AdvertiseIP != autoDetectAdvertiseIP {
		t.Fatalf("advertise ip changed to %q", updated.Server.AdvertiseIP)
	}
	if updated.Database != cfg.Database || updated.Protocol != cfg.Protocol {
		t.Fatal("active account update changed unrelated instance configuration")
	}
}
