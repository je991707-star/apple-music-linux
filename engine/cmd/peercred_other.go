//go:build !windows

package main

import "net"

// guardListenerWindows is only reached on Windows; elsewhere the listener is unchanged.
func guardListenerWindows(l net.Listener) net.Listener { return l }
