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
// client, requires username/password auth (0x02) to be offered, and replies with
// the server's method selection (0x02). Returns error if the client does not
// offer 0x02, or if the read fails.
func Handshake(conn net.Conn) error {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("read version/method selection: %w", err)
	}
	if buf[0] != 0x05 {
		return fmt.Errorf("not SOCKS5 protocol")
	}
	nMethods := int(buf[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("read methods: %w", err)
	}

	hasUserPassAuth := false
	for _, m := range methods {
		if m == 0x02 {
			hasUserPassAuth = true
			break
		}
	}

	if !hasUserPassAuth {
		conn.Write([]byte{0x05, 0xFF})
		conn.Close()
		return fmt.Errorf("client does not support username/password auth")
	}

	conn.Write([]byte{0x05, 0x02})
	return nil
}

// Authenticate performs SOCKS5 username/password authentication. Username format
// follows ParseGroupUserID. expectedPassword is compared in constant time.
// Returns parsed group, userID, targetUser. Sends the appropriate auth reply
// (success 0x01 0x00 or failure 0x01 0x01) to the client.
func Authenticate(conn net.Conn, expectedPassword string) (group, userID, targetUser string, err error) {
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

	if subtle.ConstantTimeCompare(password, []byte(expectedPassword)) != 1 {
		conn.Write([]byte{0x01, 0x01})
		return "", "", "", fmt.Errorf("auth failed for user [%s]", string(username))
	}

	conn.Write([]byte{0x01, 0x00})

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
func SendReply(conn net.Conn, rep byte) {
	reply := []byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	conn.Write(reply)
}
