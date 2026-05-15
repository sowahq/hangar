package s3

import (
	"errors"

	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
)

const (
	hdrSSEAlgorithm     = "x-amz-server-side-encryption"
	hdrSSECAlgorithm    = "x-amz-server-side-encryption-customer-algorithm"
	hdrSSECKey          = "x-amz-server-side-encryption-customer-key"
	hdrSSECKeyMD5       = "x-amz-server-side-encryption-customer-key-md5"
	hdrCopySrcCAlgo     = "x-amz-copy-source-server-side-encryption-customer-algorithm"
	hdrCopySrcCKey      = "x-amz-copy-source-server-side-encryption-customer-key"
	hdrCopySrcCKeyMD5   = "x-amz-copy-source-server-side-encryption-customer-key-md5"
)

func parseSSERequest(c *fiber.Ctx) (*object.SSERequest, error) {
	if a := c.Get(hdrSSEAlgorithm); a != "" {
		if a != "AES256" {
			return nil, object.ErrSSEAlgorithmInvalid
		}
		return &object.SSERequest{Algorithm: object.SSEAlgoS3}, nil
	}

	if c.Get(hdrSSECAlgorithm) != "" || c.Get(hdrSSECKey) != "" || c.Get(hdrSSECKeyMD5) != "" {
		if c.Get(hdrSSECAlgorithm) != "AES256" {
			return nil, object.ErrSSEAlgorithmInvalid
		}

		key, md5, err := object.ParseCustomerKey(c.Get(hdrSSECKey), c.Get(hdrSSECKeyMD5))
		if err != nil {
			return nil, err
		}

		return &object.SSERequest{Algorithm: object.SSEAlgoC, CustomerKey: key, CustomerKeyMD5: md5}, nil
	}

	return nil, nil
}

func parseCopySourceSSERequest(c *fiber.Ctx) (*object.SSERequest, error) {
	if c.Get(hdrCopySrcCAlgo) == "" && c.Get(hdrCopySrcCKey) == "" && c.Get(hdrCopySrcCKeyMD5) == "" {
		return nil, nil
	}

	if c.Get(hdrCopySrcCAlgo) != "AES256" {
		return nil, object.ErrSSEAlgorithmInvalid
	}

	key, md5, err := object.ParseCustomerKey(c.Get(hdrCopySrcCKey), c.Get(hdrCopySrcCKeyMD5))
	if err != nil {
		return nil, err
	}

	return &object.SSERequest{Algorithm: object.SSEAlgoC, CustomerKey: key, CustomerKeyMD5: md5}, nil
}

func echoSSEResponse(c *fiber.Ctx, algo, customerMD5 string) {
	switch algo {
	case object.SSEAlgoS3:
		c.Set(hdrSSEAlgorithm, "AES256")
	case object.SSEAlgoC:
		c.Set(hdrSSECAlgorithm, "AES256")
		c.Set(hdrSSECKeyMD5, customerMD5)
	}
}

func writeSSEHeaders(c *fiber.Ctx, m *storage.Metadatas) {
	echoSSEResponse(c, m.SSEAlgorithm, m.SSECustomerKeyMD5)
}

func sseErrorResponse(c *fiber.Ctx, err error, resource string) (bool, error) {
	switch {
	case errors.Is(err, object.ErrSSEMasterKeyMissing):
		return true, writeError(c, fiber.StatusServiceUnavailable, "ServerSideEncryptionConfigurationNotFoundError", err.Error(), resource)
	case errors.Is(err, object.ErrSSECustomerKeyRequired),
		errors.Is(err, object.ErrSSECustomerOnUnencrypted),
		errors.Is(err, object.ErrSSECustomerForS3Object):
		return true, writeError(c, fiber.StatusBadRequest, "InvalidRequest", err.Error(), resource)
	case errors.Is(err, object.ErrSSECustomerKeyInvalid),
		errors.Is(err, object.ErrSSECustomerKeyMD5Mismatch),
		errors.Is(err, object.ErrSSEAlgorithmInvalid):
		return true, writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), resource)
	}
	return false, nil
}
