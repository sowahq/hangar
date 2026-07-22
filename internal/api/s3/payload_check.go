package s3

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"
)

var (
	ErrContentSHA256Mismatch = errors.New("payload sha256 does not match x-amz-content-sha256")
	ErrBadDigest             = errors.New("payload md5 does not match Content-MD5")
)

type validatingReader struct {
	r          io.Reader
	sha        hash.Hash
	md5h       hash.Hash
	wantSHAHex string
	wantMD5B64 string
	done       bool
}

func (v *validatingReader) Read(p []byte) (int, error) {
	n, err := v.r.Read(p)
	if n > 0 {
		if v.sha != nil {
			v.sha.Write(p[:n])
		}
		if v.md5h != nil {
			v.md5h.Write(p[:n])
		}
	}

	if errors.Is(err, io.EOF) && !v.done {
		v.done = true
		if verr := v.verify(); verr != nil {
			return n, verr
		}
	}

	return n, err
}

func (v *validatingReader) verify() error {
	if v.sha != nil {
		got := hex.EncodeToString(v.sha.Sum(nil))
		if !strings.EqualFold(got, v.wantSHAHex) {
			return ErrContentSHA256Mismatch
		}
	}

	if v.md5h != nil {
		got := base64.StdEncoding.EncodeToString(v.md5h.Sum(nil))
		if got != v.wantMD5B64 {
			return ErrBadDigest
		}
	}

	return nil
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func maybeValidatingBody(c *fiber.Ctx, r io.Reader, streaming bool) io.Reader {
	if streaming {
		return r
	}

	vr := &validatingReader{r: r}

	if h := c.Get(headerContentSHA256); isHexSHA256(h) {
		vr.sha = sha256.New()
		vr.wantSHAHex = h
	}

	if m := strings.TrimSpace(c.Get("Content-MD5")); m != "" {
		vr.md5h = md5.New()
		vr.wantMD5B64 = m
	}

	if vr.sha == nil && vr.md5h == nil {
		return r
	}

	return vr
}
