package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

// ParseUIDList parses a comma-separated list of string uids into a slice of uint32.
func ParseUIDList(s string) ([]uint32, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	uids := make([]uint32, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		u, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid UID %q: %w", p, err)
		}
		uids = append(uids, uint32(u))
	}
	return uids, nil
}

// IsUIDAllowed checks if the given peerUID is permitted by the allowlist.
// If allowlist is empty, only root (UID 0) is permitted.
func IsUIDAllowed(peerUID uint32, allowedUIDs []uint32) bool {
	if len(allowedUIDs) == 0 {
		return peerUID == 0
	}
	for _, u := range allowedUIDs {
		if u == peerUID {
			return true
		}
	}
	return false
}

// CheckPeerUID verifies whether the peer on a Unix domain socket connection is authorized.
func CheckPeerUID(conn net.Conn, allowedUIDs []uint32) (uint32, bool, error) {
	peerUID, err := getPeerUID(conn)
	if err != nil {
		return 0, false, err
	}
	allowed := IsUIDAllowed(peerUID, allowedUIDs)
	return peerUID, allowed, nil
}

// SafeLineSummary computes safe, non-leaking telemetry about an unparseable payload line.
// It returns (byte length, sha256 prefix).
func SafeLineSummary(line []byte) (int, string) {
	h := sha256.Sum256(line)
	hexDigest := hex.EncodeToString(h[:])
	prefix := hexDigest
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return len(line), prefix
}

// ListenUDSProtected creates a Unix domain socket listener.
// If fallbackToFile is true or addr starts without '@', it uses a filesystem socket
// with dir 0700 and socket 0600 permissions.
func ListenUDSProtected(addr string) (net.Listener, error) {
	if strings.HasPrefix(addr, "@") {
		// Linux abstract namespace socket
		return net.Listen("unix", addr)
	}

	// Filesystem-based unix socket path
	dir := "/tmp"
	if idx := strings.LastIndex(addr, "/"); idx != -1 {
		dir = addr[:idx]
	}
	if dir != "" {
		_ = os.MkdirAll(dir, 0700)
		_ = os.Chmod(dir, 0700)
	}
	_ = os.Remove(addr)

	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0600); err != nil {
		log.Printf("[WX-Daemon] WARN: failed to chmod UDS socket %s to 0600: %v", addr, err)
	}
	return l, nil
}
