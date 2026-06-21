// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	libio "github.com/fatedier/golib/io"
	libnet "github.com/fatedier/golib/net"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/util/tlsfingerprint"
	"github.com/fatedier/frp/pkg/util/util"
)

const maxConcurrentAcquireTimeout = 30 * time.Second

func init() {
	RegisterProxyFactory(reflect.TypeFor[*v1.Socks5RelayProxyConfig](), NewSocks5RelayProxy)
}

type Socks5RelayProxy struct {
	*BaseProxy
	cfg       *v1.Socks5RelayProxyConfig
	tokenPool chan struct{} // semaphore for concurrent connection limiting
}

func NewSocks5RelayProxy(baseProxy *BaseProxy, cfg v1.ProxyConfigurer) Proxy {
	unwrapped, ok := cfg.(*v1.Socks5RelayProxyConfig)
	if !ok {
		return nil
	}
	pxy := &Socks5RelayProxy{
		BaseProxy: baseProxy,
		cfg:       unwrapped,
	}
	// Initialize token pool if maxConcurrent > 0 (empty channel, acts as semaphore)
	if unwrapped.MaxConcurrent > 0 {
		pxy.tokenPool = make(chan struct{}, unwrapped.MaxConcurrent)
	}
	return pxy
}

func (pxy *Socks5RelayProxy) Run() error {
	return nil
}

// InWorkConn handles work connections from frps.
// Note: deliberately does NOT call BaseProxy.HandleTCPWorkConnection, which applies
// per-proxy encryption/compression. The wire-level AEAD encryption (negotiated in the
// control channel) already protects tunnel traffic. socks5_relay targets are dynamic,
// so per-proxy encryption wrapping is not applicable.
func (pxy *Socks5RelayProxy) InWorkConn(conn net.Conn, m *msg.StartWorkConn) {
	xl := pxy.xl

	// Acquire token from pool with timeout.
	// If maxConcurrent is reached, waits up to maxConcurrentAcquireTimeout before failing.
	if pxy.tokenPool != nil {
		select {
		case pxy.tokenPool <- struct{}{}:
			defer func() { <-pxy.tokenPool }()
		case <-time.After(maxConcurrentAcquireTimeout):
			xl.Errorf("maxConcurrent limit reached, timeout acquiring slot")
			conn.Close()
			return
		}
	}

	if m.DstAddr == "" || m.DstPort == 0 {
		xl.Errorf("missing target address in StartWorkConn message")
		conn.Close()
		return
	}

	targetAddr := net.JoinHostPort(m.DstAddr, strconv.Itoa(int(m.DstPort)))
	xl.Debugf("socks5_relay dialing target [%s]", targetAddr)

	var targetConn net.Conn
	var err error

	// Fallback: RELAY_PROXY environment variable (dedicated for socks5_relay)
	proxyURL := util.FirstNonEmpty(pxy.cfg.OutboundProxy,
		os.Getenv("RELAY_PROXY"), os.Getenv("relay_proxy"))

	if proxyURL != "" {
		targetConn, err = pxy.dialViaProxyURL(targetAddr, proxyURL)
	} else {
		targetConn, err = libnet.Dial(targetAddr, libnet.WithTimeout(10*time.Second))
	}

	if err != nil {
		xl.Errorf("dial target [%s] error: %v", targetAddr, err)
		conn.Close()
		return
	}

	xl.Debugf("socks5_relay connected to target [%s], bridging", targetAddr)
	n1, n2, _ := libio.Join(conn, targetConn)
	xl.Debugf("socks5_relay to target [%s], bytes in=%d out=%d", targetAddr, n1, n2)
}

func (pxy *Socks5RelayProxy) Close() {
	// No cleanup needed - channel garbage collected when proxy stops
}

// dialViaProxyURL dials the target address through the specified proxy URL.
// For HTTPS proxies, uses TLS fingerprint simulation if configured.
func (pxy *Socks5RelayProxy) dialViaProxyURL(targetAddr string, proxyURL string) (net.Conn, error) {
	proxyType, addr, auth, err := libnet.ParseProxyURL(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy URL %s error: %w", redactProxyURL(proxyURL), err)
	}

	// Check if proxy is HTTPS (needs TLS connection to proxy)
	isHTTPSProxy := strings.HasPrefix(proxyURL, "https://")

	if isHTTPSProxy && pxy.cfg.TLSFingerprint != "" {
		// Use custom TLS fingerprint for HTTPS proxy connection
		return pxy.dialViaHTTPSProxyWithFingerprint(targetAddr, addr, auth)
	}

	return libnet.Dial(targetAddr,
		libnet.WithTimeout(10*time.Second),
		libnet.WithProxy(proxyType, addr),
		libnet.WithProxyAuth(auth),
	)
}

// dialViaHTTPSProxyWithFingerprint connects to HTTPS proxy with TLS fingerprint simulation.
func (pxy *Socks5RelayProxy) dialViaHTTPSProxyWithFingerprint(targetAddr, proxyAddr string, auth *libnet.ProxyAuth) (net.Conn, error) {
	xl := pxy.xl

	// Step 1: TCP connect to proxy
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("connect to HTTPS proxy %s: %w", proxyAddr, err)
	}

	// Step 2: TLS handshake with fingerprint.
	// Bound the handshake so a slow/hung proxy cannot stall this goroutine (which
	// holds a maxConcurrent token slot) indefinitely.
	profile := tlsfingerprint.GetProfile(pxy.cfg.TLSFingerprint)
	if profile == nil {
		profile = tlsfingerprint.GetProfile("chrome") // fallback to chrome
	}

	handshakeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tlsConn, err := tlsfingerprint.PerformTLSHandshake(handshakeCtx, conn, profile, proxyAddr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("TLS handshake with proxy %s: %w", proxyAddr, err)
	}
	xl.Debugf("socks5_relay connected to HTTPS proxy [%s] with fingerprint [%s]", proxyAddr, pxy.cfg.TLSFingerprint)

	// Step 3: Send CONNECT request (HTTP/1.1 requires CRLF line terminators).
	var connectReq strings.Builder
	fmt.Fprintf(&connectReq, "CONNECT %s HTTP/1.1\r\n", targetAddr)
	fmt.Fprintf(&connectReq, "Host: %s\r\n", targetAddr)
	if auth != nil && auth.Username != "" {
		credentials := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Passwd))
		fmt.Fprintf(&connectReq, "Proxy-Authorization: Basic %s\r\n", credentials)
	}
	connectReq.WriteString("\r\n")

	if _, err := tlsConn.Write([]byte(connectReq.String())); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("write CONNECT request: %w", err)
	}

	// Step 4: Read the CONNECT response. Use a bufio.Reader so we consume exactly
	// the response headers even when they arrive across multiple TLS records, and
	// so any bytes buffered past the headers (belonging to the tunnel) are preserved.
	reader := bufio.NewReader(tlsConn)
	code, err := readConnectResponse(reader)
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	if code < 200 || code >= 300 {
		tlsConn.Close()
		return nil, fmt.Errorf("proxy CONNECT failed with status %d", code)
	}

	// A well-behaved proxy sends only headers then waits, so there is normally
	// nothing buffered. Guard anyway: tunnel bytes already received must be
	// served before reading fresh data from the connection.
	if n := reader.Buffered(); n > 0 {
		peeked, err := reader.Peek(n)
		if err != nil {
			tlsConn.Close()
			return nil, fmt.Errorf("read CONNECT response leftover: %w", err)
		}
		return &prefixedConn{
			Reader: io.MultiReader(bytes.NewReader(append([]byte(nil), peeked...)), tlsConn),
			Conn:   tlsConn,
		}, nil
	}

	xl.Debugf("socks5_relay HTTPS proxy tunnel established to [%s]", targetAddr)
	return tlsConn, nil
}

// readConnectResponse reads the HTTP CONNECT response from r up to the end of the
// header block (a blank line) and returns the parsed status code. It handles
// responses delivered across multiple reads and rejects oversized/ malformed ones.
func readConnectResponse(r *bufio.Reader) (int, error) {
	const maxHeaderBytes = 1 << 16 // 64 KiB safety bound
	var statusLine string
	var total int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("read CONNECT response: %w", err)
		}
		if statusLine == "" {
			statusLine = line
		}
		total += len(line)
		if total > maxHeaderBytes {
			return 0, fmt.Errorf("CONNECT response headers too large")
		}
		// Blank line (CRLF or LF) marks the end of the header block.
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}
	fields := strings.Fields(statusLine)
	if len(fields) < 2 {
		return 0, fmt.Errorf("malformed CONNECT status line: %q", strings.TrimSpace(statusLine))
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("malformed CONNECT status code %q: %w", fields[1], err)
	}
	return code, nil
}

// prefixedConn serves buffered bytes (the CONNECT response leftover) before
// delegating reads to the underlying connection.
type prefixedConn struct {
	io.Reader
	net.Conn
}

func (c *prefixedConn) Read(p []byte) (int, error) { return c.Reader.Read(p) }

// redactProxyURL masks any embedded password so proxy URLs can be safely included
// in error messages and logs. Falls back to the raw string if it can't be parsed.
func redactProxyURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	return u.Redacted()
}
