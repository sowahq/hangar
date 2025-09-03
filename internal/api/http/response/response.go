package response

import (
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

// Error creates a standardized error response
func Error(c *fiber.Ctx, code int, message string) error {
	return c.Status(code).JSON(ErrorResponse{Error: message})
}

// ErrorWithLog creates an error response and logs the error
func ErrorWithLog(c *fiber.Ctx, code int, message string, err error, logMsg string) error {
	log.Error().Err(err).Msg(logMsg)
	return Error(c, code, message)
}

// JSON creates a successful JSON response
func JSON(c *fiber.Ctx, data any) error {
	return c.JSON(data)
}

// Success creates a success response with optional data
func Success(c *fiber.Ctx, data ...any) error {
	if len(data) > 0 {
		return c.JSON(data[0])
	}
	return c.JSON(fiber.Map{"status": "success"})
}