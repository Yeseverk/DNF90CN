//go:build windows

package dnfbridge

import (
	"io"
	"net"
	"os"
	"os/exec"
	"testing"
)

func TestLocalTCPConnectionOwnerPIDUsesWindowsTCPTable(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	command := exec.Command(os.Args[0], "-test.run=TestLocalTCPConnectionOwnerPIDHelper")
	command.Env = append(
		os.Environ(),
		"DNF90_TCP_OWNER_HELPER=1",
		"DNF90_TCP_OWNER_ADDRESS="+listener.Addr().String(),
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}()

	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	pid, found := localTCPConnectionOwnerPID(server)
	if !found {
		t.Fatal("Windows TCP owner PID was not found")
	}
	if want := uint32(command.Process.Pid); pid != want {
		t.Fatalf("TCP peer owner PID=%d, want helper process PID=%d", pid, want)
	}
}

func TestLocalTCPConnectionOwnerPIDHelper(t *testing.T) {
	if os.Getenv("DNF90_TCP_OWNER_HELPER") != "1" {
		return
	}
	conn, err := net.Dial("tcp4", os.Getenv("DNF90_TCP_OWNER_ADDRESS"))
	if err != nil {
		os.Exit(2)
	}
	_, _ = io.Copy(io.Discard, conn)
	_ = conn.Close()
	os.Exit(0)
}
