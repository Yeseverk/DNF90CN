package dnfbridge

import "testing"

func TestGameListenAddress(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{name: "compat all interfaces", port: 10019, want: ":10019"},
		{name: "local IPv4", host: "127.0.0.1", port: 10019, want: "127.0.0.1:10019"},
		{name: "IPv6", host: "::1", port: 10019, want: "[::1]:10019"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gameListenAddress(test.host, test.port); got != test.want {
				t.Fatalf("gameListenAddress(%q, %d) = %q, want %q", test.host, test.port, got, test.want)
			}
		})
	}
}
