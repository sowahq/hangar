package ioutils

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type closingReader struct {
	r        io.Reader
	closed   bool
	closeErr error
}

func (c *closingReader) Read(p []byte) (int, error) {
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	return c.r.Read(p)
}

func (c *closingReader) Close() error {
	c.closed = true
	return c.closeErr
}

func TestCancelableReaderPassthrough(t *testing.T) {
	want := "hello world"
	src := &closingReader{r: strings.NewReader(want)}
	cr := NewCancelableReader(context.Background(), src)

	got, err := io.ReadAll(cr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != want {
		t.Errorf("got=%q want=%q", string(got), want)
	}
	if err := cr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !src.closed {
		t.Error("underlying reader not closed")
	}
}

func TestCancelableReaderCancelBeforeRead(t *testing.T) {
	src := &closingReader{r: strings.NewReader("never read")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cr := NewCancelableReader(ctx, src)

	buf := make([]byte, 4)
	n, err := cr.Read(buf)
	if n != 0 {
		t.Errorf("n=%d want=0", n)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v want=context.Canceled", err)
	}
	if !src.closed {
		t.Error("underlying reader not closed after ctx cancel")
	}
}

func TestCancelableReaderCancelMidStream(t *testing.T) {
	body := bytes.Repeat([]byte("A"), 1024)
	src := &closingReader{r: bytes.NewReader(body)}
	ctx, cancel := context.WithCancel(context.Background())
	cr := NewCancelableReader(ctx, src)

	buf := make([]byte, 16)
	if _, err := cr.Read(buf); err != nil {
		t.Fatalf("first Read: %v", err)
	}
	cancel()
	_, err := cr.Read(buf)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v want=context.Canceled", err)
	}
}

func TestCancelableReaderCloseIdempotent(t *testing.T) {
	src := &closingReader{r: strings.NewReader("data")}
	cr := NewCancelableReader(context.Background(), src)

	if err := cr.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCancelableReaderReadAfterCloseReturnsEOF(t *testing.T) {
	src := &closingReader{r: strings.NewReader("data")}
	cr := NewCancelableReader(context.Background(), src)
	if err := cr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	buf := make([]byte, 4)
	n, err := cr.Read(buf)
	if n != 0 {
		t.Errorf("n=%d want=0", n)
	}
	if err != io.EOF {
		t.Errorf("err=%v want=io.EOF", err)
	}
}
