package admin

import (
	"encoding/json"
	"errors"

	"github.com/sowahq/hangar/internal/api/http/response"
	bucketService "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type encryptionRequest struct {
	Algorithm string `json:"algorithm"`
	KMSKeyID  string `json:"kms_key_id,omitempty"`
}

func PutBucketEncryption(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}

	var req encryptionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	if req.Algorithm != "AES256" {
		err := errors.New("algorithm must be AES256")
		recordAdmin(c, "encryption.set", "bucket", bucketName, err)
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	cfg := &bucketService.EncryptionConfig{Algorithm: req.Algorithm, KMSKeyID: req.KMSKeyID}
	err := bucketService.PutEncryption(bucketName, cfg)
	recordAdmin(c, "encryption.set", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, cfg)
}

func GetBucketEncryption(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	cfg, err := bucketService.GetEncryption(bucketName)
	if err != nil {
		if errors.Is(err, bucketService.ErrEncryptionNotFound) {
			return response.Error(c, fiber.StatusNotFound, err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, cfg)
}

func DeleteBucketEncryption(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	err := bucketService.DeleteEncryption(bucketName)
	recordAdmin(c, "encryption.delete", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"deleted": true})
}
