//go:build windows

package dnfbridge

import (
	"encoding/binary"
	"math/bits"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	tcpTableOwnerPIDAll = 5
)

var (
	iphlpapiDLL             = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCPTable = iphlpapiDLL.NewProc("GetExtendedTcpTable")
)

func localTCPConnectionOwnerPID(conn net.Conn) (uint32, bool) {
	if conn == nil {
		return 0, false
	}
	local, localOK := conn.LocalAddr().(*net.TCPAddr)
	remote, remoteOK := conn.RemoteAddr().(*net.TCPAddr)
	if !localOK || !remoteOK || local.IP.To4() == nil || remote.IP.To4() == nil {
		return 0, false
	}
	// The accepted socket's local->remote row belongs to DNF90Server.exe.
	// Reverse the port tuple to select the peer row, whose owning PID is the
	// local DNF.exe process authenticated by the launcher.
	return lookupTCP4ConnectionOwnerPID(uint16(remote.Port), uint16(local.Port))
}

func lookupTCP4ConnectionOwnerPID(localPort, remotePort uint16) (uint32, bool) {
	var size uint32
	result, _, _ := procGetExtendedTCPTable.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(syscall.AF_INET),
		tcpTableOwnerPIDAll,
		0,
	)
	if result != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size < 4 {
		return 0, false
	}
	table := make([]byte, size)
	result, _, _ = procGetExtendedTCPTable.Call(
		uintptr(unsafe.Pointer(&table[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(syscall.AF_INET),
		tcpTableOwnerPIDAll,
		0,
	)
	if result != 0 || size < 4 {
		return 0, false
	}
	count := int(binary.LittleEndian.Uint32(table[:4]))
	const rowSize = 24
	for index := 0; index < count; index++ {
		offset := 4 + index*rowSize
		if offset+rowSize > int(size) || offset+rowSize > len(table) {
			break
		}
		row := table[offset : offset+rowSize]
		rowLocalPort := bits.ReverseBytes16(uint16(binary.LittleEndian.Uint32(row[8:12])))
		rowRemotePort := bits.ReverseBytes16(uint16(binary.LittleEndian.Uint32(row[16:20])))
		if rowLocalPort == localPort && rowRemotePort == remotePort {
			pid := binary.LittleEndian.Uint32(row[20:24])
			return pid, pid != 0
		}
	}
	return 0, false
}
