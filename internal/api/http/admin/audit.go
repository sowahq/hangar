package admin

import (
	"github.com/gofiber/fiber/v2"
	"github.com/sowahq/hangar/internal/service/audit"
)

func recordAdmin(c *fiber.Ctx, action, targetType, target string, err error) {
	ev := audit.Event{
		Actor:      "admin",
		ActorType:  audit.ActorTypeAdmin,
		Action:     action,
		TargetType: targetType,
		Target:     target,
		IP:         c.IP(),
		UserAgent:  string(c.Request().Header.UserAgent()),
		RequestID:  c.Get(fiber.HeaderXRequestID),
	}

	if err != nil {
		ev.Result = audit.ResultError
		ev.Error = err.Error()
	} else {
		ev.Result = audit.ResultSuccess
	}

	audit.Record(ev)
}
