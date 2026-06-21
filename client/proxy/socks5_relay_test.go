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
	"io"
	"strings"
	"testing"
)

func TestReadConnectResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name:  "200 connection established",
			input: "HTTP/1.1 200 Connection established\r\n\r\n",
			want:  200,
		},
		{
			name: "200 with extra headers",
			input: "HTTP/1.1 200 OK\r\n" +
				"Proxy-Agent: test/1.0\r\n" +
				"Connection: keep-alive\r\n\r\n",
			want: 200,
		},
		{
			name:  "200 LF-only line endings",
			input: "HTTP/1.1 200 OK\n\n",
			want:  200,
		},
		{
			name:  "407 proxy auth required",
			input: "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic\r\n\r\n",
			want:  407,
		},
		{
			name:  "502 bad gateway",
			input: "HTTP/1.1 502 Bad Gateway\r\n\r\n",
			want:  502,
		},
		{
			name:    "malformed status line",
			input:   "garbage\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "non-numeric status code",
			input:   "HTTP/1.1 ABC Bogus\r\n\r\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tt.input))
			code, err := readConnectResponse(r)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got code=%d", code)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != tt.want {
				t.Fatalf("status code = %d, want %d", code, tt.want)
			}
		})
	}
}

// TestReadConnectResponseByteByByte ensures the parser works when the response
// arrives one byte at a time (the multi-record fragmentation case the original
// single-Read implementation got wrong).
func TestReadConnectResponseByteByByte(t *testing.T) {
	input := "HTTP/1.1 200 Connection established\r\nProxy-Agent: x/1\r\n\r\n"
	r := bufio.NewReader(&oneByteReader{data: []byte(input)})
	code, err := readConnectResponse(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 200 {
		t.Fatalf("status code = %d, want 200", code)
	}
}

type oneByteReader struct {
	data []byte
	pos  int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}
