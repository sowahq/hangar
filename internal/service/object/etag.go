package object

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
)

func newMD5ETagReader(r io.Reader) (io.Reader, func() string) {
	h := md5.New()

	return io.TeeReader(r, h), func() string {
		return fmt.Sprintf("%q", hex.EncodeToString(h.Sum(nil)))
	}
}

func combineMD5PartETags(partETags []string) (string, error) {
	h := md5.New()

	for _, e := range partETags {
		raw, err := hex.DecodeString(stripETagQuotes(e))
		if err != nil {
			return "", fmt.Errorf("invalid part etag %q: %w", e, err)
		}
		h.Write(raw)
	}

	return fmt.Sprintf("\"%s-%d\"", hex.EncodeToString(h.Sum(nil)), len(partETags)), nil
}
