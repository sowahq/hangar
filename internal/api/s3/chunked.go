package s3

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var (
	ErrChunkedMalformed     = errors.New("aws-chunked: malformed framing")
	ErrChunkedBadSignature  = errors.New("aws-chunked: chunk signature mismatch")
)

type chunkedReader struct {
	src        *bufio.Reader
	prevSig    string
	signingKey []byte
	amzDate    string
	scope      string
	leftover   []byte
	eof        bool
	verify     bool
}

func newChunkedReader(src io.Reader, ah *AuthHeader) *chunkedReader {
	scope := ah.Date + "/" + ah.Region + "/" + ah.Service + "/" + scopeTerminator
	return &chunkedReader{
		src:        bufio.NewReader(src),
		prevSig:    ah.Signature,
		signingKey: ah.SigningKey,
		amzDate:    ah.AmzDate,
		scope:      scope,
		verify:     true,
	}
}

func (cr *chunkedReader) Read(p []byte) (int, error) {
	if len(cr.leftover) > 0 {
		n := copy(p, cr.leftover)
		cr.leftover = cr.leftover[n:]
		return n, nil
	}

	if cr.eof {
		return 0, io.EOF
	}

	if err := cr.readChunk(); err != nil {
		return 0, err
	}

	if cr.eof && len(cr.leftover) == 0 {
		return 0, io.EOF
	}

	n := copy(p, cr.leftover)
	cr.leftover = cr.leftover[n:]
	return n, nil
}

func (cr *chunkedReader) readChunk() error {
	line, err := cr.src.ReadString('\n')
	if err != nil {
		return fmt.Errorf("%w: read header: %v", ErrChunkedMalformed, err)
	}

	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return fmt.Errorf("%w: empty header", ErrChunkedMalformed)
	}

	sizeStr, sigPart := splitChunkHeader(line)

	size, err := strconv.ParseInt(sizeStr, 16, 64)
	if err != nil || size < 0 {
		return fmt.Errorf("%w: bad chunk size %q", ErrChunkedMalformed, sizeStr)
	}

	chunkSig := parseChunkSig(sigPart)

	data := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(cr.src, data); err != nil {
			return fmt.Errorf("%w: read chunk body: %v", ErrChunkedMalformed, err)
		}
	}

	trailer := make([]byte, 2)
	if _, err := io.ReadFull(cr.src, trailer); err != nil {
		return fmt.Errorf("%w: read chunk crlf: %v", ErrChunkedMalformed, err)
	}
	if trailer[0] != '\r' || trailer[1] != '\n' {
		return fmt.Errorf("%w: missing crlf after chunk", ErrChunkedMalformed)
	}

	if cr.verify && chunkSig != "" {
		if err := cr.verifyChunkSig(data, chunkSig); err != nil {
			return err
		}
		cr.prevSig = chunkSig
	}

	if size == 0 {
		cr.eof = true
		return nil
	}

	cr.leftover = data
	return nil
}

func splitChunkHeader(line string) (sizeStr, sigPart string) {
	semi := strings.IndexByte(line, ';')
	if semi < 0 {
		return line, ""
	}
	return line[:semi], line[semi+1:]
}

func parseChunkSig(sigPart string) string {
	rest, ok := strings.CutPrefix(sigPart, "chunk-signature=")
	if !ok {
		return ""
	}
	return rest
}

func (cr *chunkedReader) verifyChunkSig(data []byte, chunkSig string) error {
	dataHash := sha256.Sum256(data)

	sts := "AWS4-HMAC-SHA256-PAYLOAD\n" +
		cr.amzDate + "\n" +
		cr.scope + "\n" +
		cr.prevSig + "\n" +
		emptyStringSHA256 + "\n" +
		hex.EncodeToString(dataHash[:])

	expected := Sign(sts, cr.signingKey)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(chunkSig)) != 1 {
		return ErrChunkedBadSignature
	}

	return nil
}
