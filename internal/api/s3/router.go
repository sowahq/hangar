package s3

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/metrics"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

func adaptRequest(c *fiber.Ctx) *Request {
	h := make(http.Header)
	c.Request().Header.VisitAll(func(k, v []byte) {
		h.Add(string(k), string(v))
	})

	if h.Get("Host") == "" {
		h.Set("Host", string(c.Request().Host()))
	}

	return &Request{
		Method:   string(c.Method()),
		Path:     string(c.Request().URI().Path()),
		RawQuery: string(c.Request().URI().QueryString()),
		Headers:  h,
	}
}

func sigv4Middleware(now func() time.Time) fiber.Handler {
	lookup := func(accessKeyID string) (string, error) {
		k, err := auth.GetS3Key(accessKeyID)
		if err != nil {
			return "", err
		}
		return k.SecretKey, nil
	}

	return func(c *fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		if c.Method() == fiber.MethodPost {
			ct := string(c.Request().Header.ContentType())
			if strings.HasPrefix(ct, "multipart/form-data") {
				return c.Next()
			}
		}

		req := adaptRequest(c)

		ah, err := Verify(req, lookup, now())
		if err != nil {
			return s3AuthError(c, err)
		}

		k, err := auth.GetS3Key(ah.AccessKeyID)
		if err != nil {
			return writeError(c, fiber.StatusForbidden, "InvalidAccessKeyId", "unknown access key", c.Path())
		}

		c.Locals("s3_key", k)
		c.Locals("s3_auth", ah)
		return c.Next()
	}
}

func s3AuthError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrSigV4UnknownKey):
		return writeError(c, fiber.StatusForbidden, "InvalidAccessKeyId", err.Error(), c.Path())
	case errors.Is(err, ErrSigV4BadSignature):
		return writeError(c, fiber.StatusForbidden, "SignatureDoesNotMatch", err.Error(), c.Path())
	case errors.Is(err, ErrSigV4ClockSkew):
		return writeError(c, fiber.StatusForbidden, "RequestTimeTooSkewed", err.Error(), c.Path())
	case errors.Is(err, ErrSigV4Expired):
		return writeError(c, fiber.StatusForbidden, "AccessDenied", err.Error(), c.Path())
	case errors.Is(err, ErrSigV4MissingPayloadHash), errors.Is(err, ErrSigV4MissingDate):
		return writeError(c, fiber.StatusBadRequest, "InvalidRequest", err.Error(), c.Path())
	default:
		return writeError(c, fiber.StatusBadRequest, "AuthorizationHeaderMalformed", err.Error(), c.Path())
	}
}

func Router() *fiber.App {
	return NewRouter(time.Now)
}

func NewRouter(now func() time.Time) *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:                    0,
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
		IdleTimeout:                  3 * time.Minute,
		DisableStartupMessage:        true,
		Network:                      "tcp",
	})

	if config.MetricsEnabled() {
		app.Use(metrics.Middleware("s3"))
	}

	app.Use(sigv4Middleware(now))
	app.Use(corsResponseMiddleware)

	app.Options("/:bucket", handleCORSPreflight)
	app.Options("/:bucket/*", handleCORSPreflight)

	app.Get("/", handleListBuckets)
	app.Put("/:bucket", handleCreateBucket)
	app.Delete("/:bucket", handleDeleteBucket)
	app.Head("/:bucket", handleHeadBucket)
	app.Post("/:bucket", handleBucketPost)
	app.Get("/:bucket", handleListObjectsV2)

	app.Head("/:bucket/*", handleHeadObject)
	app.Get("/:bucket/*", handleObjectGet)
	app.Put("/:bucket/*", handleObjectPut)
	app.Post("/:bucket/*", handleObjectPost)
	app.Delete("/:bucket/*", handleObjectDelete)

	app.Hooks().OnListen(func(data fiber.ListenData) error {
		log.Info().Msgf("Started S3 server on %s:%s", data.Host, data.Port)
		return nil
	})

	return app
}
