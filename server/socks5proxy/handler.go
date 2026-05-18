// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package socks5proxy

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"

	libio "github.com/fatedier/golib/io"

	"github.com/fatedier/frp/pkg/util/xlog"
)

// SOCKS5Handler is a server-level SOCKS5 proxy listener.
type SOCKS5Handler struct {
	listener     net.Listener
	authPassword string
	selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)
}

func NewSOCKS5Handler(listener net.Listener, authPassword string, selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)) *SOCKS5Handler {
	return &SOCKS5Handler{
		listener:     listener,
		authPassword: authPassword,
		selectFrpcFn: selectFrpcFn,
	}
}

func (h *SOCKS5Handler) Run(ctx context.Context) {
	xl := xlog.FromContextSafe(ctx)
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				xl.Warnf("socks5 listener accept error: %v", err)
				continue
			}
		}
		go h.handleConn(ctx, conn)
	}
}

func (h *SOCKS5Handler) Close() error {
	if h.listener != nil {
		return h.listener.Close()
	}
	return nil
}

func (h *SOCKS5Handler) handleConn(ctx context.Context, clientConn net.Conn) {
	xl := xlog.FromContextSafe(ctx)
	defer clientConn.Close()

	// SOCKS5 handshake
	if err := h.socks5Handshake(clientConn); err != nil {
		xl.Debugf("socks5 handshake error: %v", err)
		return
	}

	// SOCKS5 auth (username = group, password = authPassword)
	username, err := h.socks5Auth(clientConn)
	if err != nil {
		xl.Debugf("socks5 auth error: %v", err)
		return
	}

	// SOCKS5 connect request - get target address
	dstAddr, dstPort, err := h.socks5ConnectRequest(clientConn)
	if err != nil {
		xl.Debugf("socks5 connect request error: %v", err)
		return
	}

	// Select frpc and get work connection with target address
	workConn, err := h.selectFrpcFn(username, dstAddr, dstPort)
	if err != nil {
		xl.Warnf("select frpc for group [%s] error: %v", username, err)
		h.sendSOCKS5Reply(clientConn, 0x01) // general SOCKS server failure
		return
	}
	defer workConn.Close()

	// Send success reply to client
	h.sendSOCKS5Reply(clientConn, 0x00)

	// Bridge traffic
	_, _, _ = libio.Join(clientConn, workConn)
}

// socks5Handshake performs the initial SOCKS5 greeting.
func (h *SOCKS5Handler) socks5Handshake(conn net.Conn) error {
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

	// Check if username/password auth (0x02) is offered
	hasUserPassAuth := false
	for _, m := range methods {
		if m == 0x02 {
			hasUserPassAuth = true
			break
		}
	}

	if !hasUserPassAuth {
		// No acceptable methods
		conn.Write([]byte{0x05, 0xFF})
		return fmt.Errorf("client does not support username/password auth")
	}

	// Select username/password auth method
	conn.Write([]byte{0x05, 0x02})
	return nil
}

// socks5Auth performs SOCKS5 username/password authentication.
// Returns the username (which equals the group name).
func (h *SOCKS5Handler) socks5Auth(conn net.Conn) (string, error) {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", fmt.Errorf("read auth version: %w", err)
	}
	// buf[0] = sub-negotiation version (0x01)
	ulen := int(buf[1])
	username := make([]byte, ulen)
	if _, err := io.ReadFull(conn, username); err != nil {
		return "", fmt.Errorf("read username: %w", err)
	}

	buf2 := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf2); err != nil {
		return "", fmt.Errorf("read password length: %w", err)
	}
	plen := int(buf2[0])
	password := make([]byte, plen)
	if _, err := io.ReadFull(conn, password); err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	if subtle.ConstantTimeCompare(password, []byte(h.authPassword)) != 1 {
		conn.Write([]byte{0x01, 0x01}) // auth failure
		return "", fmt.Errorf("auth failed for user [%s]", string(username))
	}

	conn.Write([]byte{0x01, 0x00}) // auth success
	if len(username) == 0 {
		return "", fmt.Errorf("empty username")
	}
	return string(username), nil
}

// socks5ConnectRequest reads the SOCKS5 connect request and returns target address.
func (h *SOCKS5Handler) socks5ConnectRequest(conn net.Conn) (string, uint16, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", 0, fmt.Errorf("read connect request header: %w", err)
	}
	// buf[0] = version (0x05), buf[1] = cmd, buf[2] = reserved
	if buf[1] != 0x01 { // only CONNECT command supported
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
	case 0x03: // Domain name
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

// sendSOCKS5Reply sends a SOCKS5 reply to the client.
func (h *SOCKS5Handler) sendSOCKS5Reply(conn net.Conn, rep byte) {
	// Version(0x05) | Reply | Reserved(0x00) | AddrType(0x01=IPv4) | 0.0.0.0 | Port(0)
	reply := []byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	conn.Write(reply)
}