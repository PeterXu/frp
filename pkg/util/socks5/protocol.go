// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Package socks5 contains shared SOCKS5 protocol helpers used by both the
// frps-side listener (server/socks5proxy) and the frpc-side socks5 visitor
// (client/visitor). Keeping one source of truth prevents protocol drift.
package socks5

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"strings"
)

// ParseGroupUserID parses a SOCKS5 username into group, userID, and targetUser.
// Format: "group" (round-robin) | "group@userID" (session affinity) | "group=targetUser" (direct targeting).
// Returns error if username contains both '@' and '=' (ambiguous), or if any
// non-empty delimiter is followed by an empty value, or if group ends up empty.
func ParseGroupUserID(username string) (group, userID, targetUser string, err error) {
	hasAt := strings.Index(username, "@") >= 0
	hasEqual := strings.Index(username, "=") >= 0

	if hasAt && hasEqual {
		return "", "", "", fmt.Errorf("username [%s] contains both '@' and '=' - ambiguous format", username)
	}

	if hasEqual {
		idx := strings.Index(username, "=")
		group = username[:idx]
		targetUser = username[idx+1:]
		if targetUser == "" {
			return "", "", "", fmt.Errorf("empty targetUser after '=' in username [%s]", username)
		}
	} else if hasAt {
		idx := strings.Index(username, "@")
		group = username[:idx]
		userID = username[idx+1:]
		if userID == "" {
			return "", "", "", fmt.Errorf("empty userID after '@' in username [%s]", username)
		}
	} else {
		group = username
	}

	if group == "" {
		return "", "", "", fmt.Errorf("empty group in username [%s]", username)
	}
	return group, userID, targetUser, nil
}

// Handshake performs the SOCKS5 greeting: reads version + methods list from the
// client, and selects an authentication method.
// If allowNoAuth is true and client offers method 0x00 (no auth), it is selected.
// Otherwise, method 0x02 (username/password) is required.
// Returns the selected method (0x00 or 0x02), or error if no acceptable method.
func Handshake(conn net.Conn, allowNoAuth bool) (method byte, err error) {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return 0, fmt.Errorf("read version/method selection: %w", err)
	}
	if buf[0] != 0x05 {
		return 0, fmt.Errorf("not SOCKS5 protocol")
	}
	nMethods := int(buf[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return 0, fmt.Errorf("read methods: %w", err)
	}

	hasNoAuth := false
	hasUserPassAuth := false
	for _, m := range methods {
		if m == 0x00 {
			hasNoAuth = true
		}
		if m == 0x02 {
			hasUserPassAuth = true
		}
	}

	// Prefer method 0x02 (username/password) to obtain routing info (group).
	// Only fall back to no-auth (0x00) when client does not support 0x02 and
	// allowNoAuth is true (for clients without username like "curl -x socks5h://host:port").
	if hasUserPassAuth {
		if _, err := conn.Write([]byte{0x05, 0x02}); err != nil {
			return 0, fmt.Errorf("send method selection: %w", err)
		}
		return 0x02, nil
	}

	if allowNoAuth && hasNoAuth {
		if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
			return 0, fmt.Errorf("send method selection: %w", err)
		}
		return 0x00, nil
	}

	if _, err := conn.Write([]byte{0x05, 0xFF}); err != nil {
		conn.Close()
		return 0, fmt.Errorf("send method rejection: %w", err)
	}
	conn.Close()
	return 0, fmt.Errorf("client does not support username/password auth")
}

// Authenticate performs SOCKS5 authentication based on the selected method.
// For method 0x00 (no auth), it returns empty values immediately.
// For method 0x02 (username/password), it reads credentials and validates the password.
// When expectedPassword is empty, accepts empty password only (client must also send empty password).
// Username format follows ParseGroupUserID. Non-empty expectedPassword is compared in constant time.
// Returns parsed group, userID, targetUser. For method 0x02, sends the appropriate
// auth reply (success 0x01 0x00 or failure 0x01 0x01) to the client.
func Authenticate(conn net.Conn, method byte, expectedPassword string) (group, userID, targetUser string, err error) {
	// No authentication - skip auth phase entirely
	if method == 0x00 {
		return "", "", "", nil
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", "", "", fmt.Errorf("read auth version: %w", err)
	}
	ulen := int(buf[1])
	username := make([]byte, ulen)
	if _, err := io.ReadFull(conn, username); err != nil {
		return "", "", "", fmt.Errorf("read username: %w", err)
	}

	buf2 := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf2); err != nil {
		return "", "", "", fmt.Errorf("read password length: %w", err)
	}
	plen := int(buf2[0])
	password := make([]byte, plen)
	if _, err := io.ReadFull(conn, password); err != nil {
		return "", "", "", fmt.Errorf("read password: %w", err)
	}

	// Validate password
	if expectedPassword == "" {
		// When expectedPassword is empty, accept empty password only
		if len(password) != 0 {
			authErr := fmt.Errorf("auth failed: expected empty password but got non-empty for user [%s]", string(username))
			return "", "", "", authFailureWithReply(conn, authErr)
		}
	} else {
		if subtle.ConstantTimeCompare(password, []byte(expectedPassword)) != 1 {
			authErr := fmt.Errorf("auth failed for user [%s]", string(username))
			return "", "", "", authFailureWithReply(conn, authErr)
		}
	}

	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return "", "", "", fmt.Errorf("send auth success: %w", err)
	}

	// Empty username is allowed: the caller may use a static fallback (e.g.
	// visitor's ServerName) when no routing info was supplied by the client.
	if len(username) == 0 {
		return "", "", "", nil
	}

	group, userID, targetUser, err = ParseGroupUserID(string(username))
	if err != nil {
		return "", "", "", err
	}
	return group, userID, targetUser, nil
}

// authFailureWithReply best-effort sends the SOCKS5 auth-failure reply (0x01 0x01)
// and returns the primary auth error. If the reply write fails, it is folded into
// the returned error so it is never lost — but the auth-failure reason stays
// primary, so callers logging it still see which identity failed authentication.
func authFailureWithReply(conn net.Conn, authErr error) error {
	if _, werr := conn.Write([]byte{0x01, 0x01}); werr != nil {
		return fmt.Errorf("%w (also failed to send failure reply: %v)", authErr, werr)
	}
	return authErr
}

// ReadConnectRequest reads a SOCKS5 CONNECT request and returns the target
// address (IPv4, IPv6, or domain as a string) and port. Only CONNECT (cmd=1)
// is supported; other commands (BIND, UDP ASSOCIATE) return an error.
func ReadConnectRequest(conn net.Conn) (string, uint16, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", 0, fmt.Errorf("read connect request header: %w", err)
	}
	if buf[1] != 0x01 {
		return "", 0, fmt.Errorf("unsupported SOCKS5 command: %d", buf[1])
	}

	addrType := buf[3]
	var dstAddr string

	switch addrType {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", 0, fmt.Errorf("read IPv4 address: %w", err)
		}
		dstAddr = net.IP(ip).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", 0, fmt.Errorf("read domain length: %w", err)
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", 0, fmt.Errorf("read domain name: %w", err)
		}
		dstAddr = string(domain)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", 0, fmt.Errorf("read IPv6 address: %w", err)
		}
		dstAddr = net.IP(ip).String()
	default:
		return "", 0, fmt.Errorf("unsupported address type: %d", addrType)
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", 0, fmt.Errorf("read port: %w", err)
	}
	dstPort := uint16(portBuf[0])<<8 | uint16(portBuf[1])

	return dstAddr, dstPort, nil
}

// SendReply sends a SOCKS5 reply with the given reply code (rep byte).
// Uses a fixed BND.ADDR=0.0.0.0 / BND.PORT=0 — matches existing frps behavior.
// Returns the write error so callers can react — e.g. skip relaying when the
// success reply never reached the client, avoiding a desynced tunnel.
func SendReply(conn net.Conn, rep byte) error {
	reply := []byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	_, err := conn.Write(reply)
	return err
}
