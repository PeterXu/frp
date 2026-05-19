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
	cfg *v1.Socks5RelayProxyConfig
}

func NewSocks5RelayProxy(baseProxy *BaseProxy, cfg v1.ProxyConfigurer) Proxy {
	unwrapped, ok := cfg.(*v1.Socks5RelayProxyConfig)
	if !ok {
		return nil
	}
	return &Socks5RelayProxy{
		BaseProxy: baseProxy,
		cfg:       unwrapped,
	}
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

	if m.DstAddr == "" || m.DstPort == 0 {
		xl.Errorf("missing target address in StartWorkConn message")
		conn.Close()
		return
	}

	targetAddr := net.JoinHostPort(m.DstAddr, strconv.Itoa(int(m.DstPort)))
	xl.Debugf("socks5_relay dialing target [%s]", targetAddr)

	var targetConn net.Conn
	var err error

	if pxy.cfg.OutboundProxy != "" {
		targetConn, err = pxy.dialViaProxy(targetAddr)
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
	// nothing to clean up
}

// dialViaProxy dials the target address through the configured outbound proxy.
// Uses the same libnet API that FRP uses for transport.proxyURL (see client/connector.go).
func (pxy *Socks5RelayProxy) dialViaProxy(targetAddr string) (net.Conn, error) {
	proxyType, addr, auth, err := libnet.ParseProxyURL(pxy.cfg.OutboundProxy)
	if err != nil {
		return nil, fmt.Errorf("parse outbound proxy URL error: %w", err)
	}

	return libnet.Dial(targetAddr,
		libnet.WithTimeout(10*time.Second),
		libnet.WithProxy(proxyType, addr),
		libnet.WithProxyAuth(auth),
	)
}
