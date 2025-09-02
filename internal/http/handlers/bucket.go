package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/anhostfr/hangar/internal/http/response"
	"github.com/anhostfr/hangar/internal/http/validation"
	"github.com/anhostfr/hangar/internal/service/bucket"
)

func CreateBucket(c *fiber.Ctx) error {
	var req bucket.CreateBucketRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if req.Name == "" {
		return response.Error(c, fiber.StatusBadRequest, "Bucket name is required")
	}

	result, err := bucket.CreateBucket(&req)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusConflict, err.Error(), err, "Failed to create bucket: "+req.Name)
	}

	return response.JSON(c, result)
}

func ListBuckets(c *fiber.Ctx) error {
	result, err := bucket.ListBuckets()
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, err.Error(), err, "Failed to list buckets")
	}

	return response.JSON(c, result)
}

func GetBucket(c *fiber.Ctx) error {
	name, err := validation.ValidateKey(c, "name")
	if err != nil {
		return err
	}

	result, err := bucket.GetBucket(name)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusNotFound, "Bucket not found", err, "Failed to get bucket: "+name)
	}

	return response.JSON(c, result)
}

func DeleteBucket(c *fiber.Ctx) error {
	name, err := validation.ValidateKey(c, "name")
	if err != nil {
		return err
	}

	var req bucket.DeleteBucketRequest
	req.Name = name
	req.Force = c.QueryBool("force", false)

	err = bucket.DeleteBucket(&req)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusConflict, err.Error(), err, "Failed to delete bucket: "+name)
	}

	return response.JSON(c, fiber.Map{
		"name":    name,
		"message": "Bucket deleted successfully",
	})
}