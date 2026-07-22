package s3

import (
	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

func handleGetBucketLocation(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	region := config.S3Region()
	if region == "us-east-1" {
		region = ""
	}

	return writeXML(c, fiber.StatusOK, LocationConstraintXML{
		Xmlns:  xmlNamespace,
		Region: region,
	})
}
