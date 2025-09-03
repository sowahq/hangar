package ioutils

import (
	"context"
	"io"
	"sync"
)

type CancelableReader struct {
	reader io.ReadCloser
	ctx    context.Context
	closed bool
	mu     sync.Mutex
}

func NewCancelableReader(ctx context.Context, reader io.ReadCloser) *CancelableReader {
	return &CancelableReader{
		reader: reader,
		ctx:    ctx,
		closed: false,
	}
}

func (cr *CancelableReader) Read(p []byte) (n int, err error) {
	select {
	case <-cr.ctx.Done():
		cr.Close()
		return 0, cr.ctx.Err()
	default:
	}

	cr.mu.Lock()
	if cr.closed {
		cr.mu.Unlock()
		return 0, io.EOF
	}
	cr.mu.Unlock()

	return cr.reader.Read(p)
}

func (cr *CancelableReader) Close() error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if !cr.closed {
		cr.closed = true
		return cr.reader.Close()
	}
	return nil
}
