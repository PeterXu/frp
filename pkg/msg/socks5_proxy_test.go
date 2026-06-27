package msg

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewSocks5VisitorConnJSONRoundTrip(t *testing.T) {
	original := &NewSocks5VisitorConn{
		RunID:        "abc-123",
		Group:        "testgroup",
		UserID:       "userA",
		TargetUser:   "",
		DstAddr:      "127.0.0.1",
		DstPort:      8080,
		AuthPassword: "secret",
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got NewSocks5VisitorConn
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != *original {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, original)
	}
}

func TestNewSocks5VisitorConnRespJSONRoundTrip(t *testing.T) {
	for _, errStr := range []string{"", "select frpc failed"} {
		original := &NewSocks5VisitorConnResp{Error: errStr}
		encoded, _ := json.Marshal(original)
		var got NewSocks5VisitorConnResp
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Error != errStr {
			t.Fatalf("got %q want %q", got.Error, errStr)
		}
		// Sanity check that the encoded form is byte-reasonable.
		if !bytes.Contains(encoded, []byte("error")) && errStr != "" {
			t.Fatalf("encoded form missing 'error' key: %s", encoded)
		}
	}
}
