//go:build windows

package main

import "testing"

func TestWindowsCredentialRoundTrip(t *testing.T) {
	target := credentialTarget(t.TempDir())
	t.Cleanup(func() {
		_ = deleteCredential(target)
	})

	const (
		username = "credential-test-user"
		password = "credential-test-password"
	)
	if err := writeCredential(target, username, password); err != nil {
		t.Fatal(err)
	}
	gotUsername, gotPassword, found, err := readCredential(target)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("written Windows credential was not found")
	}
	if gotUsername != username || gotPassword != password {
		t.Fatalf(
			"credential = (%q, %q), want (%q, %q)",
			gotUsername,
			gotPassword,
			username,
			password,
		)
	}
	if err := deleteCredential(target); err != nil {
		t.Fatal(err)
	}
	_, _, found, err = readCredential(target)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("deleted Windows credential is still present")
	}
}
