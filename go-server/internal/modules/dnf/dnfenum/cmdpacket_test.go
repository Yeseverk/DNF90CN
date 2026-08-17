package dnfenum

import "testing"

func TestCmdPacketNameUsesRuntimeTable(t *testing.T) {
	tests := []struct {
		name  string
		code  uint16
		want  string
		known bool
	}{
		{name: "login", code: 1, want: "ENUM_CMDPACKET_LOGIN", known: true},
		{name: "select character", code: 4, want: "ENUM_CMDPACKET_SELECT_CHARACTER", known: true},
		{name: "missing 545", code: 545, want: "ENUM_CMDPACKET_UNKNOWN", known: false},
		{name: "missing 1456", code: 1456, want: "ENUM_CMDPACKET_UNKNOWN", known: false},
		{name: "end", code: 1519, want: "ENUM_CMDPACKET_END", known: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CmdPacketName(tt.code); got != tt.want {
				t.Fatalf("CmdPacketName(%d) = %q, want %q", tt.code, got, tt.want)
			}
			if got := IsKnownCmdPacket(tt.code); got != tt.known {
				t.Fatalf("IsKnownCmdPacket(%d) = %v, want %v", tt.code, got, tt.known)
			}
		})
	}
}
