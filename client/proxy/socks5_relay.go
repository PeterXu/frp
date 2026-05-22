// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package proxy

import (
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"time"

	libio "github.com/fatedier/golib/io"
	libnet "github.com/fatedier/golib/net"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
)

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

	// Acquire token from pool - blocks if channel is full (limit reached)
	// Requests will wait until a previous request completes and releases its token
	if pxy.tokenPool != nil {
		pxy.tokenPool <- struct{}{} // acquire: send blocks if channel full
		defer func() { <-pxy.tokenPool }() // release: receive frees a slot
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

	proxyURL := pxy.cfg.OutboundProxy
	if proxyURL == "" {
		// Fallback: RELAY_PROXY environment variable (dedicated for socks5_relay)
		proxyURL = os.Getenv("RELAY_PROXY")
		if proxyURL == "" {
			proxyURL = os.Getenv("relay_proxy")
		}
	}

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
	_, _, _ = libio.Join(conn, targetConn)
}

func (pxy *Socks5RelayProxy) Close() {
	// No cleanup needed - channel garbage collected when proxy stops
}

// dialViaProxyURL dials the target address through the specified proxy URL.
func (pxy *Socks5RelayProxy) dialViaProxyURL(targetAddr string, proxyURL string) (net.Conn, error) {
	proxyType, addr, auth, err := libnet.ParseProxyURL(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy URL %q error: %w", proxyURL, err)
	}

	return libnet.Dial(targetAddr,
		libnet.WithTimeout(10*time.Second),
		libnet.WithProxy(proxyType, addr),
		libnet.WithProxyAuth(auth),
	)
}
