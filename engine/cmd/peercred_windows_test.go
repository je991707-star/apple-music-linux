//go:build windows

package main

import (
	"net"
	"testing"
)

// A client in this very process runs as the engine's own user, so the check must accept it.
func TestPeerSIDMatchAcceptsAClientRunningAsTheSameUser(t *testing.T) {
	self, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()

	got, err := peerSIDMatch(server, self)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("peerSIDMatch = %d, want 1 (same user)", got)
	}
}
