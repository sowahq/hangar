package s3

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/sowahq/hangar/internal/service/accesslog"
	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/gofiber/fiber/v2"
)

func accessLogMiddleware(c *fiber.Ctx) error {
	start := time.Now()
	var reqIDBytes [8]byte
	_, _ = rand.Read(reqIDBytes[:])
	reqID := hex.EncodeToString(reqIDBytes[:])
	c.Locals("s3_req_id", reqID)

	err := c.Next()

	bucketName := c.Params("bucket")
	if bucketName == "" {
		return err
	}

	var accessKey string
	if k, ok := c.Locals("s3_key").(*auth.S3Key); ok && k != nil {
		accessKey = k.AccessKeyID
	}

	accesslog.Enqueue(accesslog.Record{
		When:         start,
		SourceBucket: bucketName,
		RemoteIP:     c.IP(),
		AccessKey:    accessKey,
		RequestID:    reqID,
		Method:       c.Method(),
		Path:         string(c.Request().URI().Path()),
		Key:          c.Params("*"),
		Status:       c.Response().StatusCode(),
		BytesSent:    int64(len(c.Response().Body())),
		TotalMillis:  time.Since(start).Milliseconds(),
		UserAgent:    string(c.Request().Header.UserAgent()),
		Referer:      string(c.Request().Header.Referer()),
	})

	return err
}
