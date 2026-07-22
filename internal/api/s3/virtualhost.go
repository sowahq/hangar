package s3

import (
	"strings"

	"github.com/valyala/fasthttp"
)

const vhOrigPathKey = "vh_orig_path"

func virtualHostBucket(host, base string) string {
	if base == "" {
		return ""
	}

	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	suffix := "." + strings.TrimPrefix(base, ".")
	if !strings.HasSuffix(host, suffix) {
		return ""
	}
	bucket := strings.TrimSuffix(host, suffix)
	if bucket == "" || strings.Contains(bucket, "/") {
		return ""
	}
	return bucket
}

func virtualHostWrap(base string, next fasthttp.RequestHandler) fasthttp.RequestHandler {
	if base == "" {
		return next
	}

	return func(ctx *fasthttp.RequestCtx) {
		host := string(ctx.Host())
		bucket := virtualHostBucket(host, base)
		if bucket == "" {
			next(ctx)
			return
		}

		origPath := string(ctx.URI().Path())
		ctx.SetUserValue(vhOrigPathKey, origPath)

		var newPath string
		if origPath == "" || origPath == "/" {
			newPath = "/" + bucket
		} else {
			newPath = "/" + bucket + origPath
		}
		ctx.URI().SetPath(newPath)
		next(ctx)
	}
}
