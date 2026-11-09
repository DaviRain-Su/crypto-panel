package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestJSONRPCUsesDecodedStringValues(t *testing.T) {
	frame := []byte(`{"jsonrpc":"` + string(rune(92)) + `u0032.0","id":"a","result":{}}`)
	msg, err := parseMessage(frame)
	if err != nil || msg.idKey != "s:a" {
		t.Fatalf("valid JSON escapes rejected: %+v %v", msg, err)
	}
}

func TestFrameBoundaryAndMultipleMessages(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	data = append(data, bytes.Repeat([]byte(" "), maxMessageBytes-len(data)-1)...)
	data = append(data, '\n')
	data = append(data, []byte("{\"jsonrpc\":\"2.0\",\"method\":\"_extension\"}\n")...)
	r := bufio.NewReaderSize(bytes.NewReader(data), 7)
	for i := 0; i < 2; i++ {
		frame, err := readFrame(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseMessage(frame); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
	}
	if _, err := readFrame(r); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing EOF: %v", err)
	}
	v := map[string]any{"text": ""}
	overhead, _ := json.Marshal(v)
	v["text"] = strings.Repeat("x", maxMessageBytes-len(overhead)-1)
	if data, err := encodeMessage(v); err != nil || len(data) != maxMessageBytes {
		t.Fatalf("exact outbound limit: len=%d err=%v", len(data), err)
	}
	v["text"] = v["text"].(string) + "x"
	if _, err := encodeMessage(v); !errors.Is(err, ErrProtocol) {
		t.Fatalf("outbound limit not enforced: %v", err)
	}
}
