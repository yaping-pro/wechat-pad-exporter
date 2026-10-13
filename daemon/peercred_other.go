//go:build !linux && !android && !darwin

package main

import (
	"fmt"
	"net"
)

// getPeerUID fallback for unsupported platforms.
func getPeerUID(conn net.Conn) (uint32, error) {
	return 0, fmt.Errorf("SO_PEERCRED not supported on this platform")
}
