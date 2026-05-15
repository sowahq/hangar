package response

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

var ErrResponseSent = errors.New("response already written")

func Error(c *fiber.Ctx, code int, message string) error {
	_ = c.Status(code).JSON(ErrorResponse{Error: message})
	return ErrResponseSent
}

func ErrorWithLog(c *fiber.Ctx, code int, message string, err error, logMsg string) error {
	log.Error().Err(err).Msg(logMsg)
	return Error(c, code, message)
}

func JSON(c *fiber.Ctx, data any) error {
	return c.JSON(data)
}

func Success(c *fiber.Ctx, data ...any) error {
	if len(data) > 0 {
		return c.JSON(data[0])
	}
	return c.JSON(fiber.Map{"status": "success"})
}

func ErrorHandler(c *fiber.Ctx, err error) error {
	if errors.Is(err, ErrResponseSent) {
		return nil
	}
	code := fiber.StatusInternalServerError
	var fe *fiber.Error
	if errors.As(err, &fe) {
		code = fe.Code
	}
	return c.Status(code).JSON(ErrorResponse{Error: err.Error()})
}
