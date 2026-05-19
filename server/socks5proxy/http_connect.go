// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package socks5proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	libio "github.com/fatedier/golib/io"

	netpkg "github.com/fatedier/frp/pkg/util/net"
	"github.com/fatedier/frp/pkg/util/xlog"
)

// HTTPConnectHandler is a server-level HTTP CONNECT proxy listener.
type HTTPConnectHandler struct {
	listener     net.Listener
	authPassword string
	selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)
	getMetaFn    func(username string) (proxyName, runID string, err error)
	connTracker  *RelayConnTracker
}

func NewHTTPConnectHandler(
	listener net.Listener,
	authPassword string,
	selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error),
	getMetaFn func(username string) (proxyName, runID string, err error),
	connTracker *RelayConnTracker,
) *HTTPConnectHandler {
	return &HTTPConnectHandler{
		listener:     listener,
		authPassword: authPassword,
		selectFrpcFn: selectFrpcFn,
		getMetaFn:    getMetaFn,
		connTracker:  connTracker,
	}
}

func (h *HTTPConnectHandler) Run(ctx context.Context) {
	xl := xlog.FromContextSafe(ctx)
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				xl.Warnf("http connect listener accept error: %v", err)
				continue
			}
		}
		go h.handleConn(ctx, conn)
	}
}

func (h *HTTPConnectHandler) Close() error {
	if h.listener != nil {
		return h.listener.Close()
	}
	return nil
}

func (h *HTTPConnectHandler) handleConn(ctx context.Context, conn net.Conn) {
	xl := xlog.FromContextSafe(ctx)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		xl.Debugf("read http request error: %v", err)
		return
	}

	if req.Method != http.MethodConnect {
		http.Error(newRespWriter(conn), "only CONNECT method supported", http.StatusMethodNotAllowed)
		return
	}

	// Extract username from Proxy-Authorization header
	username, err := h.extractUsername(req)
	if err != nil {
		resp := &http.Response{
			StatusCode: http.StatusProxyAuthRequired,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Proxy-Authenticate": {"Basic"}},
		}
		resp.Write(conn)
		return
	}

	// Parse target host:port
	host, portStr, err := net.SplitHostPort(req.Host)
	if err != nil {
		http.Error(newRespWriter(conn), "invalid host", http.StatusBadRequest)
		return
	}
	parsedPort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		http.Error(newRespWriter(conn), "invalid port", http.StatusBadRequest)
		return
	}
	port := uint16(parsedPort)

	// Select frpc and get work connection
	workConn, err := h.selectFrpcFn(username, host, port)
	if err != nil {
		xl.Warnf("select frpc for group [%s] error: %v", username, err)
		http.Error(newRespWriter(conn), "bad gateway", http.StatusBadGateway)
		return
	}
	defer workConn.Close()

	// Get connection metadata
	proxyName, runID, err := h.getMetaFn(username)
	if err != nil {
		xl.Warnf("get conn meta for group [%s] error: %v", username, err)
		http.Error(newRespWriter(conn), "bad gateway", http.StatusBadGateway)
		return
	}

	// Register connection in tracker
	connID := h.connTracker.Track(RelayConnInfo{
		SourceIP:  conn.RemoteAddr().String(),
		Protocol:  "http_connect",
		Group:     username,
		DstAddr:   host,
		DstPort:   port,
		ProxyName: proxyName,
		RunID:     runID,
		StartTime: time.Now(),
		BytesIn:   0,
		BytesOut:  0,
	})
	defer h.connTracker.Remove(connID)

	// Send 200 Connection Established
	resp := &http.Response{
		StatusCode: http.StatusOK,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Body:       http.NoBody,
	}
	resp.Write(conn)

	// Drain any bytes the client pipelined past the CONNECT request header.
	var clientConn io.ReadWriteCloser = conn
	if n := reader.Buffered(); n > 0 {
		peeked, err := reader.Peek(n)
		if err != nil {
			xl.Warnf("peek buffered data error: %v", err)
			return
		}
		clientConn = &bufferedConn{
			Reader: io.MultiReader(bytes.NewReader(peeked), conn),
			Conn:   conn,
		}
	}

	// Wrap connections for byte tracking
	clientConn = netpkg.WrapStatsConn(conn, func(read, write int64) {
		h.connTracker.UpdateBytes(connID, read, write)
	})
	workConn = netpkg.WrapStatsConn(workConn, func(read, write int64) {
		h.connTracker.UpdateBytes(connID, write, read) // reversed for workConn
	})

	// Bridge traffic
	_, _, _ = libio.Join(clientConn, workConn)
}

func (h *HTTPConnectHandler) extractUsername(req *http.Request) (string, error) {
	authHeader := req.Header.Get("Proxy-Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("no proxy authorization header")
	}

	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", fmt.Errorf("invalid auth scheme")
	}

	decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("decode auth error: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid auth format")
	}

	if parts[0] == "" {
		return "", fmt.Errorf("empty username")
	}

	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(h.authPassword)) != 1 {
		return "", fmt.Errorf("auth failed")
	}

	return parts[0], nil
}

// respWriter wraps a net.Conn to implement http.ResponseWriter for error responses.
type respWriter struct {
	conn   net.Conn
	header http.Header
	wrote  bool
}

func newRespWriter(conn net.Conn) *respWriter {
	return &respWriter{conn: conn, header: make(http.Header)}
}

func (w *respWriter) Header() http.Header         { return w.header }
func (w *respWriter) Write(b []byte) (int, error) { return w.conn.Write(b) }
func (w *respWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	statusText := http.StatusText(code)
	fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n", code, statusText)
}

// bufferedConn wraps a net.Conn but replaces its Read with a custom io.Reader
// (typically an io.MultiReader of peeked bufio bytes + the original conn).
type bufferedConn struct {
	io.Reader
	net.Conn
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.Reader.Read(p) }
func (b *bufferedConn) Close() error               { return b.Conn.Close() }
