package neartx

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeArgsPreservesCompleteBinary(t *testing.T) {
	args := bytes.Repeat([]byte{0xff, 0x00, 0x81}, 400)
	text := DecodeArgs(args)
	if !strings.HasPrefix(text, "base64:") {
		t.Fatal("二进制参数必须明确标为 base64")
	}
	got, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(text, "base64:"))
	if err != nil || !bytes.Equal(got, args) {
		t.Fatalf("批准中的二进制参数不完整：%v", err)
	}
}
