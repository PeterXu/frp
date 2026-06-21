package socks5

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
)

func TestParseGroupUserID(t *testing.T) {
	cases := []struct {
		name       string
		username   string
		wantGroup  string
		wantUserID string
		wantTarget string
		wantErr    bool
	}{
		{name: "round-robin", username: "testgroup", wantGroup: "testgroup"},
		{name: "session affinity", username: "grp@userA", wantGroup: "grp", wantUserID: "userA"},
		{name: "direct targeting", username: "grp=host-b", wantGroup: "grp", wantTarget: "host-b"},
		{name: "both delimiters", username: "grp@u=h", wantErr: true},
		{name: "empty user after at", username: "grp@", wantErr: true},
		{name: "empty target after equal", username: "grp=", wantErr: true},
		{name: "empty username", username: "", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, u, tgt, err := ParseGroupUserID(c.username)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if g != c.wantGroup || u != c.wantUserID || tgt != c.wantTarget {
				t.Fatalf("got (g=%q u=%q tgt=%q) want (g=%q u=%q tgt=%q)",
					g, u, tgt, c.wantGroup, c.wantUserID, c.wantTarget)
			}
		})
	}
}

// pipeConns returns a connected pair of net.Conns for round-trip tests.
func pipeConns() (net.Conn, net.Conn) {
	c1, c2 := net.Pipe()
	return c1, c2
}

func TestHandshakeRoundTrip(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		// Client sends greeting offering 0x02 (user/pass auth).
		client.Write([]byte{0x05, 0x01, 0x02})
		buf := make([]byte, 2)
		_, err := io.ReadFull(client, buf)
		errCh <- err
		if err == nil {
			if buf[0] != 0x05 || buf[1] != 0x02 {
				errCh <- fmt.Errorf("unexpected server method selection: %v", buf)
			}
		}
	}()

	if _, err := Handshake(server, false); err != nil {
		t.Fatalf("Handshake err: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("client read err: %v", err)
	}
}

func TestHandshakeRejectsNoUserPassAuth(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	go func() {
		client.Write([]byte{0x05, 0x01, 0x00}) // only "no auth"
		// Read the server's rejection response
		buf := make([]byte, 2)
		io.ReadFull(client, buf)
	}()

	_, err := Handshake(server, false)
	if err == nil {
		t.Fatal("expected Handshake to fail when 0x02 not offered")
	}
}

func TestAuthenticateRoundTrip(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	const password = "shared-secret"
	username := "grp@userA"

	errCh := make(chan error, 1)
	go func() {
		// Client sends username/password sub-negotiation.
		client.Write([]byte{0x01, byte(len(username))})
		client.Write([]byte(username))
		client.Write([]byte{byte(len(password))})
		client.Write([]byte(password))
		buf := make([]byte, 2)
		_, err := io.ReadFull(client, buf)
		errCh <- err
		if err == nil && (buf[0] != 0x01 || buf[1] != 0x00) {
			errCh <- fmt.Errorf("expected auth success 01 00, got %v", buf)
		}
	}()

	g, u, _, err := Authenticate(server, 0x02, password)
	if err != nil {
		t.Fatalf("Authenticate err: %v", err)
	}
	if g != "grp" || u != "userA" {
		t.Fatalf("got (g=%q u=%q)", g, u)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("client read err: %v", err)
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	username := "grp"
	password := "wrong"
	go func() {
		client.Write([]byte{0x01, byte(len(username))})
		client.Write([]byte(username))
		client.Write([]byte{byte(len(password))})
		client.Write([]byte(password))
		// drain server's failure reply so pipe doesn't block
		io.ReadAll(client)
	}()

	_, _, _, err := Authenticate(server, 0x02, "shared-secret")
	if err == nil {
		t.Fatal("expected auth failure")
	}
}

// Empty username with valid password must succeed and return empty group,
// so callers (e.g. the visitor) can apply a static fallback.
func TestAuthenticateEmptyUsernameOK(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	password := "shared-secret"
	errCh := make(chan error, 1)
	go func() {
		client.Write([]byte{0x01, 0x00}) // empty username
		client.Write([]byte{byte(len(password))})
		client.Write([]byte(password))
		buf := make([]byte, 2)
		_, err := io.ReadFull(client, buf)
		errCh <- err
		if err == nil && (buf[0] != 0x01 || buf[1] != 0x00) {
			errCh <- fmt.Errorf("expected auth success 01 00, got %v", buf)
		}
	}()

	g, u, tgt, err := Authenticate(server, 0x02, password)
	if err != nil {
		t.Fatalf("Authenticate err: %v", err)
	}
	if g != "" || u != "" || tgt != "" {
		t.Fatalf("expected all-empty routing, got g=%q u=%q tgt=%q", g, u, tgt)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("client read err: %v", err)
	}
}

func TestReadConnectRequestIPv4(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	go func() {
		// ver=5, cmd=1, rsv=0, atyp=1 (IPv4), 127.0.0.1, port=8080
		client.Write([]byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0x1f, 0x90})
	}()

	addr, port, err := ReadConnectRequest(server)
	if err != nil {
		t.Fatalf("ReadConnectRequest err: %v", err)
	}
	if addr != "127.0.0.1" || port != 8080 {
		t.Fatalf("got addr=%q port=%d", addr, port)
	}
}

func TestReadConnectRequestDomain(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	domain := "example.com"
	go func() {
		client.Write([]byte{0x05, 0x01, 0x00, 0x03, byte(len(domain))})
		client.Write([]byte(domain))
		client.Write([]byte{0x00, 0x50}) // port 80
	}()

	addr, port, err := ReadConnectRequest(server)
	if err != nil {
		t.Fatalf("ReadConnectRequest err: %v", err)
	}
	if addr != domain || port != 80 {
		t.Fatalf("got addr=%q port=%d", addr, port)
	}
}

func TestReadConnectRequestUnsupportedCmd(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	go func() {
		// cmd=2 (BIND), not supported
		client.Write([]byte{0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0x1f, 0x90})
	}()

	_, _, err := ReadConnectRequest(server)
	if err == nil {
		t.Fatal("expected error for unsupported command")
	}
}

func TestSendReply(t *testing.T) {
	client, server := pipeConns()
	defer client.Close()
	defer server.Close()

	go SendReply(server, 0x00)

	buf := make([]byte, 10)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("read reply err: %v", err)
	}
	expected := []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(buf, expected) {
		t.Fatalf("got %v want %v", buf, expected)
	}
}
