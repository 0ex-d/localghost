package apparedis

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestEncodeRESPHostileValues: the injection-impossibility claim, checked. A value containing CRLF,
// RESP syntax, or an entire second command must arrive as ONE bulk string whose declared byte length
// covers all of it , the framing leaves nothing to break out of.
func TestEncodeRESPHostileValues(t *testing.T) {
	hostiles := []string{
		"plain",
		"has\r\ncrlf",
		"$5\r\nFAKE!",                       // RESP syntax inside a value
		"x\r\nFLUSHALL\r\n",                 // a smuggled command
		"*2\r\n$3\r\nDEL\r\n$1\r\nk\r\n",    // a whole framed command as a value
		"",                                   // empty value is legal: $0
		strings.Repeat("A", 4096),
	}
	for _, h := range hostiles {
		enc := encodeRESP([]string{"SET", "key", h})
		want := fmt.Sprintf("*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$%d\r\n%s\r\n", len(h), h)
		if string(enc) != want {
			t.Fatalf("framing broke for %q:\n got %q\nwant %q", h, enc, want)
		}
	}
}

// TestEncodeRESPByteLength: length is BYTES, not runes , multibyte UTF-8 must not desync the frame.
func TestEncodeRESPByteLength(t *testing.T) {
	v := "café☕" // 4 runes beyond ASCII, more bytes than runes
	enc := encodeRESP([]string{"SET", "k", v})
	if !bytes.Contains(enc, []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(v), v))) {
		t.Fatalf("byte-length framing wrong: %q", enc)
	}
}

// TestReadReplyRoundTrip: the parser must read exactly the declared lengths back out, including
// values full of protocol syntax , proving both directions treat values as opaque bytes.
func TestReadReplyRoundTrip(t *testing.T) {
	payload := "$9\r\nnot\r\nreal\r\n" // bulk of 9 bytes containing CRLF
	r := bufio.NewReader(strings.NewReader(payload))
	rep, err := readReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Str != "not\r\nreal" {
		t.Fatalf("value mangled: %q", rep.Str)
	}
	// arrays with mixed types
	arr := "*3\r\n:42\r\n$-1\r\n+OK\r\n"
	rep, err = readReply(bufio.NewReader(strings.NewReader(arr)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Array) != 3 || rep.Array[0].Int != 42 || !rep.Array[1].Nil || rep.Array[2].Str != "OK" {
		t.Fatalf("array parse wrong: %+v", rep)
	}
}
