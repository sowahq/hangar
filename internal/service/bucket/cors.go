package bucket

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

var ErrCORSNotFound = errors.New("cors configuration not found")

type CORSRule struct {
	ID             string   `json:"id,omitempty"`
	AllowedOrigins []string `json:"allowed_origins"`
	AllowedMethods []string `json:"allowed_methods"`
	AllowedHeaders []string `json:"allowed_headers,omitempty"`
	ExposeHeaders  []string `json:"expose_headers,omitempty"`
	MaxAgeSeconds  int      `json:"max_age_seconds,omitempty"`
}

type CORSConfiguration struct {
	Rules []CORSRule `json:"rules"`
}

func corsKey(bucket string) []byte {
	return []byte(fmt.Sprintf("cors:%s", bucket))
}

func PutCORS(bucket string, cfg *CORSConfiguration) error {
	if _, err := GetBucket(bucket); err != nil {
		return err
	}

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal cors: %w", err)
	}

	return db.Put(corsKey(bucket), data)
}

func GetCORS(bucket string) (*CORSConfiguration, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	data, err := db.Get(corsKey(bucket))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrCORSNotFound
		}
		return nil, fmt.Errorf("get cors: %w", err)
	}

	var cfg CORSConfiguration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal cors: %w", err)
	}

	return &cfg, nil
}

func DeleteCORS(bucket string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	return db.Delete(corsKey(bucket))
}

func MatchCORS(cfg *CORSConfiguration, origin, method string, requestHeaders []string) (*CORSRule, bool) {
	if cfg == nil || origin == "" {
		return nil, false
	}

	method = strings.ToUpper(method)

	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if !originMatches(r.AllowedOrigins, origin) {
			continue
		}
		if !methodMatches(r.AllowedMethods, method) {
			continue
		}
		if !headersMatch(r.AllowedHeaders, requestHeaders) {
			continue
		}

		return r, true
	}

	return nil, false
}

func originMatches(patterns []string, origin string) bool {
	for _, p := range patterns {
		if p == "*" || strings.EqualFold(p, origin) {
			return true
		}
		if star := strings.IndexByte(p, '*'); star >= 0 {
			prefix := p[:star]
			suffix := p[star+1:]
			if len(origin) >= len(prefix)+len(suffix) &&
				strings.EqualFold(origin[:len(prefix)], prefix) &&
				strings.EqualFold(origin[len(origin)-len(suffix):], suffix) {
				return true
			}
		}
	}
	return false
}

func methodMatches(methods []string, m string) bool {
	for _, am := range methods {
		if strings.EqualFold(am, m) {
			return true
		}
	}
	return false
}

func headersMatch(allowed []string, requested []string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, h := range requested {
		if !headerAllowed(allowed, h) {
			return false
		}
	}
	return true
}

func headerAllowed(allowed []string, h string) bool {
	for _, a := range allowed {
		if a == "*" || strings.EqualFold(a, h) {
			return true
		}
		if star := strings.IndexByte(a, '*'); star >= 0 {
			prefix := a[:star]
			suffix := a[star+1:]
			if len(h) >= len(prefix)+len(suffix) &&
				strings.EqualFold(h[:len(prefix)], prefix) &&
				strings.EqualFold(h[len(h)-len(suffix):], suffix) {
				return true
			}
		}
	}
	return false
}
