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
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"

	libio "github.com/fatedier/golib/io"

	"github.com/fatedier/frp/pkg/util/xlog"
)

// HTTPConnectHandler is a server-level HTTP CONNECT proxy listener.
type HTTPConnectHandler struct {
	listener     net.Listener
	authPassword string
	selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)
}

func NewHTTPConnectHandler(listener net.Listener, authPassword string, selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)) *HTTPConnectHandler {
	return &HTTPConnectHandler{
		listener:     listener,
		authPassword: authPassword,
		selectFrpcFn: selectFrpcFn,
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
	var port uint16
	fmt.Sscanf(portStr, "%d", &port)

	// Select frpc and get work connection
	workConn, err := h.selectFrpcFn(username, host, port)
	if err != nil {
		xl.Warnf("select frpc for group [%s] error: %v", username, err)
		http.Error(newRespWriter(conn), "bad gateway", http.StatusBadGateway)
		return
	}
	defer workConn.Close()

	// Send 200 Connection Established
	resp := &http.Response{
		StatusCode: http.StatusOK,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Body:       http.NoBody,
	}
	resp.Write(conn)

	// Bridge traffic
	_, _, _ = libio.Join(conn, workConn)
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

	if parts[1] != h.authPassword {
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

func (w *respWriter) Header() http.Header        { return w.header }
func (w *respWriter) Write(b []byte) (int, error) { return w.conn.Write(b) }
func (w *respWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	statusText := http.StatusText(code)
	fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n", code, statusText)
}