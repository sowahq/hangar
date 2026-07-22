package handlers

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"

	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/api/http/validation"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
)

type completeMultipartBody struct {
	Parts []int `json:"parts"`
}

func PostObject(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key, err := validation.ValidateKey(c, "*")
	if err != nil {
		return err
	}
	if _, err := bucket.GetBucket(bucketName); err != nil {
		return response.Error(c, fiber.StatusNotFound, "Bucket not found: "+bucketName)
	}

	if c.Request().URI().QueryArgs().Has("uploads") {
		res, err := object.InitiateMultipart(&object.InitiateMultipartRequest{Bucket: bucketName, Key: key})
		if err != nil {
			return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to initiate multipart", err, "init mpu: "+key)
		}
		log.Debug().Msgf("Multipart initiated: bucket=%s key=%s upload=%s", bucketName, key, res.UploadID)
		return c.Status(fiber.StatusOK).JSON(res)
	}

	uploadID := c.Query("uploadId")
	if uploadID == "" {
		return response.Error(c, fiber.StatusBadRequest, "Missing uploadId or uploads selector")
	}

	body := c.Body()
	var req completeMultipartBody
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
		}
	}
	res, err := object.CompleteMultipart(&object.CompleteMultipartRequest{
		Bucket:   bucketName,
		Key:      key,
		UploadID: uploadID,
		Parts:    req.Parts,
	})
	if err != nil {
		switch {
		case errors.Is(err, object.ErrMultipartNotFound):
			return response.Error(c, fiber.StatusNotFound, "Multipart upload not found")
		case errors.Is(err, object.ErrNoPartsToComplete):
			return response.Error(c, fiber.StatusBadRequest, "No parts to complete")
		case errors.Is(err, object.ErrPartMissing):
			return response.Error(c, fiber.StatusBadRequest, err.Error())
		case errors.Is(err, object.ErrCompleteQuotaFail):
			return response.Error(c, fiber.StatusRequestEntityTooLarge, "Quota exceeded")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to complete multipart", err, "complete mpu: "+key)
	}
	log.Debug().Msgf("Multipart completed: bucket=%s key=%s upload=%s size=%d", bucketName, key, uploadID, res.Size)
	if res.VersionID != "" {
		c.Set("X-Version-Id", res.VersionID)
	}
	return c.Status(fiber.StatusOK).JSON(res)
}

func uploadMultipartPart(c *fiber.Ctx, bucketName, key, uploadID string) error {
	partStr := c.Query("partNumber")
	if partStr == "" {
		return response.Error(c, fiber.StatusBadRequest, "Missing partNumber")
	}
	partNumber, err := strconv.Atoi(partStr)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid partNumber")
	}

	bodyStream := c.Request().BodyStream()
	res, err := object.UploadPart(&object.UploadPartRequest{
		Bucket:     bucketName,
		Key:        key,
		UploadID:   uploadID,
		PartNumber: partNumber,
		Body:       bodyStream,
	})
	if err != nil {
		switch {
		case errors.Is(err, object.ErrInvalidPartNumber):
			return response.Error(c, fiber.StatusBadRequest, "Invalid partNumber")
		case errors.Is(err, object.ErrMultipartNotFound):
			return response.Error(c, fiber.StatusNotFound, "Multipart upload not found")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to upload part", err, "upload part: "+key)
	}
	c.Set("ETag", res.ETag)
	return response.JSON(c, res)
}

func abortMultipart(c *fiber.Ctx, bucketName, key, uploadID string) error {
	err := object.AbortMultipart(&object.AbortMultipartRequest{Bucket: bucketName, Key: key, UploadID: uploadID})
	if err != nil {
		if errors.Is(err, object.ErrMultipartNotFound) {
			return response.Error(c, fiber.StatusNotFound, "Multipart upload not found")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to abort multipart", err, "abort mpu: "+key)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func listMultipartParts(c *fiber.Ctx, bucketName, key, uploadID string) error {
	res, err := object.ListPartsService(bucketName, key, uploadID)
	if err != nil {
		if errors.Is(err, object.ErrMultipartNotFound) {
			return response.Error(c, fiber.StatusNotFound, "Multipart upload not found")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to list parts", err, "list parts: "+key)
	}
	return response.JSON(c, res)
}
