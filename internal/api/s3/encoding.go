package s3

import "github.com/gofiber/fiber/v2"

func listEncoding(c *fiber.Ctx) (string, bool) {
	et := c.Query("encoding-type")
	if et == "" {
		return "", true
	}
	if et == "url" {
		return "url", true
	}
	return "", false
}

func encodeListValue(encodingType, v string) string {
	if encodingType == "url" {
		return uriEncode(v, false)
	}
	return v
}
