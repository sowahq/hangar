package s3

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
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
	trailer    bool
}

func newChunkedReader(src io.Reader, ah *AuthHeader) *chunkedReader {
	scope := ah.Date + "/" + ah.Region + "/" + ah.Service + "/" + scopeTerminator
	return &chunkedReader{
		src:        bufio.NewReader(src),
		prevSig:    ah.Signature,
		signingKey: ah.SigningKey,
		amzDate:    ah.AmzDate,
		scope:      scope,
		verify:     !ah.UnsignedChunks,
		trailer:    ah.Trailer,
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
	if cr.verify && chunkSig == "" {
		return fmt.Errorf("%w: missing chunk signature", ErrChunkedMalformed)
	}

	data := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(cr.src, data); err != nil {
			return fmt.Errorf("%w: read chunk body: %v", ErrChunkedMalformed, err)
		}
	}

	if cr.verify {
		if err := cr.verifyChunkSig(data, chunkSig); err != nil {
			return err
		}
		cr.prevSig = chunkSig
	}

	if size == 0 {
		cr.eof = true
		if cr.trailer {
			return cr.readTrailerSection()
		}
		return cr.readCRLF()
	}

	if err := cr.readCRLF(); err != nil {
		return err
	}

	cr.leftover = data
	return nil
}

func (cr *chunkedReader) readCRLF() error {
	crlf := make([]byte, 2)
	if _, err := io.ReadFull(cr.src, crlf); err != nil {
		return fmt.Errorf("%w: read chunk crlf: %v", ErrChunkedMalformed, err)
	}
	if crlf[0] != '\r' || crlf[1] != '\n' {
		return fmt.Errorf("%w: missing crlf after chunk", ErrChunkedMalformed)
	}
	return nil
}

func (cr *chunkedReader) readTrailerSection() error {
	var trailerSig string
	var headers []string

	for {
		line, err := cr.src.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")

		if err != nil {
			if errors.Is(err, io.EOF) && trimmed == "" {
				break
			}
			return fmt.Errorf("%w: read trailer: %v", ErrChunkedMalformed, err)
		}

		if trimmed == "" {
			break
		}

		if rest, ok := strings.CutPrefix(trimmed, "x-amz-trailer-signature:"); ok {
			trailerSig = strings.TrimSpace(rest)
			continue
		}

		headers = append(headers, trimmed)
	}

	if cr.verify {
		if trailerSig == "" {
			return fmt.Errorf("%w: missing trailer signature", ErrChunkedMalformed)
		}
		return cr.verifyTrailerSig(headers, trailerSig)
	}

	return nil
}

func (cr *chunkedReader) verifyTrailerSig(headers []string, trailerSig string) error {
	canonical := make([]string, 0, len(headers))
	for _, h := range headers {
		name, value, found := strings.Cut(h, ":")
		if !found {
			return fmt.Errorf("%w: bad trailer header %q", ErrChunkedMalformed, h)
		}
		canonical = append(canonical, strings.ToLower(strings.TrimSpace(name))+":"+strings.TrimSpace(value))
	}
	sort.Strings(canonical)

	trailerHash := sha256.Sum256([]byte(strings.Join(canonical, "\n") + "\n"))

	sts := "AWS4-HMAC-SHA256-TRAILER\n" +
		cr.amzDate + "\n" +
		cr.scope + "\n" +
		cr.prevSig + "\n" +
		hex.EncodeToString(trailerHash[:])

	expected := Sign(sts, cr.signingKey)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(trailerSig)) != 1 {
		return ErrChunkedBadSignature
	}

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
