//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows the TCP table (GetExtendedTcpTable) maps a connection to its owning pid;
// the pid's process token gives the user SID. Connections from any other user are refused,
// mirroring the /proc/net/tcp uid check on Linux.

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	afInet                    = 2
	tcpTableOwnerPidConnected = 4 // MIB_TCPTABLE_OWNER_PID, connections only
)

// mibTCPRowOwnerPID mirrors MIB_TCPROW_OWNER_PID; ports hold a network-order value in the low 16 bits.
type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPid  uint32
}

func guardListenerWindows(l net.Listener) net.Listener {
	self, err := currentUserSID()
	if err != nil {
		slog.Warn("cannot determine the engine's user; the engine API is open to every local user", "err", err)
		return l
	}
	return &peerUIDListener{
		Listener: l,
		lookup:   func(c net.Conn) (uint32, error) { return peerSIDMatch(c, self) },
		allow:    func(ok uint32) bool { return ok == 1 },
	}
}

func currentUserSID() (*windows.SID, error) {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}

// peerSIDMatch returns 1 when the process on the other end of the loopback connection runs as
// the same user as the engine, 0 when it runs as someone else (or in another logon session).
func peerSIDMatch(c net.Conn, self *windows.SID) (uint32, error) {
	client, ok1 := c.RemoteAddr().(*net.TCPAddr)
	server, ok2 := c.LocalAddr().(*net.TCPAddr)
	if !ok1 || !ok2 || client.IP.To4() == nil || server.IP.To4() == nil {
		return 0, errors.New("not an IPv4 TCP connection")
	}
	pid, err := ownerPid(client, server)
	if err != nil {
		return 0, err
	}
	// A token we can read gives the exact user. Chromium's sandboxed helper processes (the
	// network service and renderers) run with restricted tokens that deny TOKEN_QUERY even to
	// the same user, so when the token is unreadable fall back to the logon session: another
	// user's processes live in a different session.
	if sid, ok := processUserSID(pid); ok {
		if windows.EqualSid(sid, self) {
			return 1, nil
		}
		slog.Warn("peer runs as a different user", "pid", pid, "peerSID", sid.String(), "ourSID", self.String())
		return 0, nil
	}
	var peerSession, ourSession uint32
	if err := windows.ProcessIdToSessionId(pid, &peerSession); err != nil {
		slog.Warn("peer token unreadable and its session unknown", "pid", pid, "err", err)
		return 0, nil
	}
	if err := windows.ProcessIdToSessionId(uint32(windows.GetCurrentProcessId()), &ourSession); err != nil {
		return 0, err
	}
	if peerSession == ourSession {
		return 1, nil
	}
	slog.Warn("peer token unreadable and it runs in another session", "pid", pid, "peerSession", peerSession, "ourSession", ourSession)
	return 0, nil
}

// processUserSID returns the user SID of a process, or ok=false when its token cannot be read.
func processUserSID(pid uint32) (*windows.SID, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return nil, false
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, false
	}
	return u.User.Sid, true
}

// ownerPid finds the pid owning the client end (local = client, remote = server) of a connection.
func ownerPid(client, server *net.TCPAddr) (uint32, error) {
	var size uint32
	for attempt := 0; attempt < 3; attempt++ {
		var buf []byte
		var p unsafe.Pointer
		if size > 0 {
			buf = make([]byte, size)
			p = unsafe.Pointer(&buf[0])
		}
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(p), uintptr(unsafe.Pointer(&size)), 0,
			afInet, tcpTableOwnerPidConnected, 0)
		if r == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			size += 4096 // the table can grow between the sizing call and the real one
			continue
		}
		if r != 0 {
			return 0, fmt.Errorf("GetExtendedTcpTable: %w", windows.Errno(r))
		}
		n := binary.LittleEndian.Uint32(buf[:4])
		rows := unsafe.Slice((*mibTCPRowOwnerPID)(unsafe.Pointer(&buf[4])), n)
		wantL, wantR := ip4ToUint32(client.IP), ip4ToUint32(server.IP)
		for _, row := range rows {
			if row.LocalAddr == wantL && row.RemoteAddr == wantR &&
				ntohs(row.LocalPort) == uint16(client.Port) && ntohs(row.RemotePort) == uint16(server.Port) {
				return row.OwningPid, nil
			}
		}
		return 0, fmt.Errorf("no TCP table entry for %v -> %v", client, server)
	}
	return 0, errors.New("TCP table kept growing")
}

func ip4ToUint32(ip net.IP) uint32 { return binary.LittleEndian.Uint32(ip.To4()) }

func ntohs(p uint32) uint16 { v := uint16(p); return v<<8 | v>>8 }
