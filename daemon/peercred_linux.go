//go:build linux || android

package main

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// getPeerUID extracts the peer process UID from a Unix domain socket connection using SO_PEERCRED.
func getPeerUID(conn net.Conn) (uint32, error) {
	uconn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("connection is not a *net.UnixConn (%T)", conn)
	}

	raw, err := uconn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("failed to get SyscallConn: %w", err)
	}

	var uid uint32
	var getErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		ucred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			getErr = err
			return
		}
		uid = ucred.Uid
	})
	if ctrlErr != nil {
		return 0, fmt.Errorf("raw control error: %w", ctrlErr)
	}
	if getErr != nil {
		return 0, fmt.Errorf("SO_PEERCRED error: %w", getErr)
	}
	return uid, nil
}
