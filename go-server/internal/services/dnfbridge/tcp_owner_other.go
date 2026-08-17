//go:build !windows

package dnfbridge

import "net"

func localTCPConnectionOwnerPID(net.Conn) (uint32, bool) {
	return 0, false
}
