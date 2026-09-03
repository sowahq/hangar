package s3

import (
	"errors"
	"strings"
	"testing"
)

func TestChunkedRejectsOversizeChunk(t *testing.T) {
	ah := &AuthHeader{UnsignedChunks: true}
	cr := newChunkedReader(strings.NewReader("7fffffffffffffff\r\n"), ah)

	_, err := cr.Read(make([]byte, 16))
	if !errors.Is(err, ErrChunkedMalformed) {
		t.Fatalf("want ErrChunkedMalformed for oversize chunk, got %v", err)
	}
}

func TestChunkedReadsUnsignedChunk(t *testing.T) {
	ah := &AuthHeader{UnsignedChunks: true}
	payload := "hello"
	frame := "5\r\n" + payload + "\r\n0\r\n\r\n"
	cr := newChunkedReader(strings.NewReader(frame), ah)

	buf := make([]byte, len(payload))
	n, err := cr.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:n]) != payload {
		t.Fatalf("got %q want %q", buf[:n], payload)
	}
}
