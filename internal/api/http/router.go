package http

import (
	"time"

	"github.com/anhostfr/hangar/internal/api/http/admin"
	"github.com/anhostfr/hangar/internal/api/http/handlers"
	"github.com/anhostfr/hangar/internal/api/http/middleware"
	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/metrics"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/phuslu/log"
)

func Router() *fiber.App {
	router := fiber.New(fiber.Config{
		BodyLimit:                    0,
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
		IdleTimeout:                  3 * time.Minute,
		DisableStartupMessage:        true,
		Network:                      "tcp",
		ErrorHandler:                 response.ErrorHandler,
	})

	if config.MetricsEnabled() {
		router.Use(metrics.Middleware("http"))
	}

	if config.RateLimitEnabled() {
		router.Use(limiter.New(limiter.Config{
			Max:        config.RateLimitMax(),
			Expiration: time.Duration(config.RateLimitWindowSec()) * time.Second,
			KeyGenerator: func(c *fiber.Ctx) string {
				if tok, ok := c.Locals("auth_token").(*auth.Token); ok && tok != nil {
					return "tok:" + tok.ID
				}
				return c.IP()
			},
			LimitReached: func(c *fiber.Ctx) error {
				return response.Error(c, fiber.StatusTooManyRequests, "Rate limit exceeded")
			},
		}))
	}

	router.Get("/status", handlers.Status)

	adminGroup := router.Group("/admin")
	adminGroup.Get("/buckets", admin.ListBuckets)
	adminGroup.Put("/buckets/:bucket", admin.CreateBucket)
	adminGroup.Get("/buckets/:bucket", admin.GetBucket)
	adminGroup.Delete("/buckets/:bucket", admin.DeleteBucket)
	adminGroup.Put("/buckets/:bucket/quota", admin.UpdateQuota)
	adminGroup.Put("/buckets/:bucket/versioning", admin.UpdateVersioning)
	adminGroup.Post("/buckets/:bucket/tokens", admin.CreateToken)
	adminGroup.Get("/buckets/:bucket/tokens", admin.ListTokens)
	adminGroup.Delete("/buckets/:bucket/tokens/:id", admin.DeleteToken)
	adminGroup.Post("/s3keys", admin.CreateS3Key)
	adminGroup.Get("/s3keys", admin.ListS3Keys)
	adminGroup.Delete("/s3keys/:id", admin.DeleteS3Key)
	adminGroup.Get("/audit", admin.TailAudit)
	adminGroup.Get("/sse/keys", admin.ListSSEKeys)
	adminGroup.Post("/sse/keys/rotate", admin.RotateSSEKey)
	adminGroup.Put("/sse/keys/:id/activate", admin.ActivateSSEKey)
	adminGroup.Post("/lifecycle/run", admin.RunLifecycle)
	adminGroup.Get("/cluster/status", admin.ClusterStatus)
	adminGroup.Get("/cluster/layout", admin.ClusterLayoutGet)
	adminGroup.Put("/cluster/layout", admin.ClusterLayoutApply)
	adminGroup.Delete("/cluster/node/:id", admin.ClusterNodeRemove)
	adminGroup.Post("/cluster/node/:id/drain", admin.ClusterNodeDrain)
	adminGroup.Put("/buckets/:bucket/encryption", admin.PutBucketEncryption)
	adminGroup.Get("/buckets/:bucket/encryption", admin.GetBucketEncryption)
	adminGroup.Delete("/buckets/:bucket/encryption", admin.DeleteBucketEncryption)
	adminGroup.Put("/buckets/:bucket/object-lock", admin.PutBucketObjectLock)
	adminGroup.Get("/buckets/:bucket/object-lock", admin.GetBucketObjectLock)
	adminGroup.Put("/buckets/:bucket/website", admin.PutBucketWebsite)
	adminGroup.Get("/buckets/:bucket/website", admin.GetBucketWebsite)
	adminGroup.Delete("/buckets/:bucket/website", admin.DeleteBucketWebsite)
	adminGroup.Put("/buckets/:bucket/logging", admin.PutBucketLogging)
	adminGroup.Get("/buckets/:bucket/logging", admin.GetBucketLogging)
	adminGroup.Delete("/buckets/:bucket/logging", admin.DeleteBucketLogging)
	adminGroup.Put("/buckets/:bucket/tagging", admin.PutBucketTagging)
	adminGroup.Get("/buckets/:bucket/tagging", admin.GetBucketTagging)
	adminGroup.Delete("/buckets/:bucket/tagging", admin.DeleteBucketTagging)
	adminGroup.Put("/buckets/:bucket/cors", admin.PutBucketCORS)
	adminGroup.Get("/buckets/:bucket/cors", admin.GetBucketCORS)
	adminGroup.Delete("/buckets/:bucket/cors", admin.DeleteBucketCORS)
	adminGroup.Put("/buckets/:bucket/lifecycle", admin.PutBucketLifecycleAdmin)
	adminGroup.Get("/buckets/:bucket/lifecycle", admin.GetBucketLifecycleAdmin)
	adminGroup.Delete("/buckets/:bucket/lifecycle", admin.DeleteBucketLifecycleAdmin)

	router.Get("/:bucket", middleware.RequireAuth(auth.PermRead), handlers.ListObjects)
	router.Get("/:bucket/*", middleware.RequireAuth(auth.PermRead), handlers.Download)
	router.Put("/:bucket/*", middleware.RequireAuth(auth.PermWrite), handlers.Upload)
	router.Post("/:bucket/*", middleware.RequireAuth(auth.PermWrite), handlers.PostObject)
	router.Delete("/:bucket/*", middleware.RequireAuth(auth.PermDelete), handlers.Delete)

	router.Hooks().OnListen(func(data fiber.ListenData) error {
		log.Info().Msgf("Started web server on %s:%s", data.Host, data.Port)
		return nil
	})

	return router
}
