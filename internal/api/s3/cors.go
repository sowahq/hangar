package s3

import (
	"encoding/xml"
	"errors"
	"strconv"
	"strings"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type CORSRuleXML struct {
	ID             string   `xml:"ID,omitempty"`
	AllowedOrigins []string `xml:"AllowedOrigin"`
	AllowedMethods []string `xml:"AllowedMethod"`
	AllowedHeaders []string `xml:"AllowedHeader,omitempty"`
	ExposeHeaders  []string `xml:"ExposeHeader,omitempty"`
	MaxAgeSeconds  int      `xml:"MaxAgeSeconds,omitempty"`
}

type CORSConfigurationXML struct {
	XMLName xml.Name      `xml:"CORSConfiguration"`
	Xmlns   string        `xml:"xmlns,attr,omitempty"`
	Rules   []CORSRuleXML `xml:"CORSRule"`
}

func toCORSXML(cfg *bucket.CORSConfiguration) CORSConfigurationXML {
	out := CORSConfigurationXML{Xmlns: xmlNamespace}
	for _, r := range cfg.Rules {
		out.Rules = append(out.Rules, CORSRuleXML{
			ID:             r.ID,
			AllowedOrigins: r.AllowedOrigins,
			AllowedMethods: r.AllowedMethods,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  r.MaxAgeSeconds,
		})
	}
	return out
}

func fromCORSXML(in *CORSConfigurationXML) *bucket.CORSConfiguration {
	cfg := &bucket.CORSConfiguration{}
	for _, r := range in.Rules {
		cfg.Rules = append(cfg.Rules, bucket.CORSRule{
			ID:             r.ID,
			AllowedOrigins: r.AllowedOrigins,
			AllowedMethods: r.AllowedMethods,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  r.MaxAgeSeconds,
		})
	}
	return cfg
}

func handleGetBucketCORS(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	cfg, err := bucket.GetCORS(name)
	if err != nil {
		if errors.Is(err, bucket.ErrCORSNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchCORSConfiguration", "The CORS configuration does not exist", "/"+name)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return writeXML(c, fiber.StatusOK, toCORSXML(cfg))
}

func handlePutBucketCORS(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty cors body", "/"+name)
	}

	var in CORSConfigurationXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}
	if len(in.Rules) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "at least one CORSRule required", "/"+name)
	}

	for _, r := range in.Rules {
		if len(r.AllowedOrigins) == 0 || len(r.AllowedMethods) == 0 {
			return writeError(c, fiber.StatusBadRequest, "MalformedXML", "AllowedOrigin and AllowedMethod are required", "/"+name)
		}
	}

	if err := bucket.PutCORS(name, fromCORSXML(&in)); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteBucketCORS(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	if err := bucket.DeleteCORS(name); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func handleCORSPreflight(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if name == "" {
		return c.SendStatus(fiber.StatusForbidden)
	}

	origin := c.Get("Origin")
	method := c.Get("Access-Control-Request-Method")
	if origin == "" || method == "" {
		return c.SendStatus(fiber.StatusBadRequest)
	}

	cfg, err := bucket.GetCORS(name)
	if err != nil {
		return c.SendStatus(fiber.StatusForbidden)
	}

	headers := splitCSV(c.Get("Access-Control-Request-Headers"))

	rule, ok := bucket.MatchCORS(cfg, origin, method, headers)
	if !ok {
		return c.SendStatus(fiber.StatusForbidden)
	}

	applyCORSResponse(c, rule, origin, headers)
	return c.SendStatus(fiber.StatusOK)
}

func corsResponseMiddleware(c *fiber.Ctx) error {
	if err := c.Next(); err != nil {
		return err
	}

	origin := c.Get("Origin")
	name := c.Params("bucket")
	if origin == "" || name == "" {
		return nil
	}

	cfg, err := bucket.GetCORS(name)
	if err != nil {
		return nil
	}

	rule, ok := bucket.MatchCORS(cfg, origin, c.Method(), nil)
	if !ok {
		return nil
	}

	applyCORSResponse(c, rule, origin, nil)
	return nil
}

func applyCORSResponse(c *fiber.Ctx, rule *bucket.CORSRule, origin string, requested []string) {
	c.Set("Access-Control-Allow-Origin", allowOriginFor(rule, origin))
	c.Set("Vary", "Origin")

	if len(rule.AllowedMethods) > 0 {
		c.Set("Access-Control-Allow-Methods", strings.Join(rule.AllowedMethods, ", "))
	}

	if len(requested) > 0 {
		c.Set("Access-Control-Allow-Headers", strings.Join(requested, ", "))
	} else if len(rule.AllowedHeaders) > 0 {
		c.Set("Access-Control-Allow-Headers", strings.Join(rule.AllowedHeaders, ", "))
	}

	if len(rule.ExposeHeaders) > 0 {
		c.Set("Access-Control-Expose-Headers", strings.Join(rule.ExposeHeaders, ", "))
	}

	if rule.MaxAgeSeconds > 0 {
		c.Set("Access-Control-Max-Age", strconv.Itoa(rule.MaxAgeSeconds))
	}
}

func allowOriginFor(rule *bucket.CORSRule, origin string) string {
	for _, p := range rule.AllowedOrigins {
		if p == "*" {
			return "*"
		}
	}
	return origin
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
