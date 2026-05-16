package config

import (
	"encoding/base64"
	"log"
	"os"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/go-playground/validator/v10"

	"github.com/anhostfr/hangar/internal/database"
)

type serverConfig struct {
	DataDirectory string `toml:"data_directory" validate:"required"`

	Pprof bool `toml:"pprof"`

	API struct {
		BindAddr string `toml:"bind_addr" validate:"required"`
	} `toml:"api"`

	Storage struct {
		ChunkSize         int  `toml:"chunk_size" validate:"min=1024"`
		EnableCompression bool `toml:"enable_compression"`

		MinFreeBytes int64 `toml:"min_free_bytes" validate:"min=0"`
		MinFreePct   int   `toml:"min_free_pct" validate:"min=0,max=100"`
		NodeMaxBytes int64 `toml:"node_max_bytes" validate:"min=0"`
	} `toml:"storage"`

	GarbageCollection struct {
		Interval int `toml:"interval_hours" validate:"min=1"` // Hours between GC runs
	} `toml:"garbage_collection"`

	Scrub struct {
		IntervalHours   int   `toml:"interval_hours" validate:"min=0"`
		RateBytesPerSec int64 `toml:"rate_bytes_per_sec" validate:"min=0"`
	} `toml:"scrub"`

	RateLimit struct {
		Enabled   bool `toml:"enabled"`
		Max       int  `toml:"max"`
		WindowSec int  `toml:"window_sec"`
	} `toml:"rate_limit"`

	S3 struct {
		Enabled  bool   `toml:"enabled"`
		BindAddr string `toml:"bind_addr"`
		Region   string `toml:"region"`
	} `toml:"s3"`

	Security struct {
		MasterKeyB64 string `toml:"master_key_b64"`
	} `toml:"security"`
}

var masterKey []byte

var (
	c  *serverConfig
	mu sync.RWMutex
)

func DefaultServerConfig() *serverConfig {
	config := new(serverConfig)

	config.DataDirectory = "data"
	config.API.BindAddr = ":8080"

	// Storage defaults
	config.Storage.ChunkSize = 4194304 // 4MB
	config.Storage.EnableCompression = true

	// GC defaults
	config.GarbageCollection.Interval = 24 // 24 hours

	config.Scrub.IntervalHours = 0
	config.Scrub.RateBytesPerSec = 0

	config.RateLimit.Enabled = false
	config.RateLimit.Max = 100
	config.RateLimit.WindowSec = 60

	config.S3.Enabled = false
	config.S3.BindAddr = ":9000"
	config.S3.Region = "us-east-1"

	return config
}

func S3Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	if c == nil {
		c = DefaultServerConfig()
	}
	return c.S3.Enabled
}

func S3BindAddr() string {
	mu.RLock()
	defer mu.RUnlock()
	if c == nil {
		c = DefaultServerConfig()
	}
	return c.S3.BindAddr
}

func S3Region() string {
	mu.RLock()
	defer mu.RUnlock()
	if c == nil {
		c = DefaultServerConfig()
	}
	return c.S3.Region
}

func RateLimitEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	if c == nil {
		c = DefaultServerConfig()
	}
	return c.RateLimit.Enabled
}

func RateLimitMax() int {
	mu.RLock()
	defer mu.RUnlock()
	if c == nil {
		c = DefaultServerConfig()
	}
	return c.RateLimit.Max
}

func RateLimitWindowSec() int {
	mu.RLock()
	defer mu.RUnlock()
	if c == nil {
		c = DefaultServerConfig()
	}
	return c.RateLimit.WindowSec
}

func SetRateLimitForTest(enabled bool, max, windowSec int) {
	mu.Lock()
	defer mu.Unlock()
	if c == nil {
		c = DefaultServerConfig()
	}
	c.RateLimit.Enabled = enabled
	c.RateLimit.Max = max
	c.RateLimit.WindowSec = windowSec
}

func LoadServerConfig(path string) error {
	mu.Lock()
	defer mu.Unlock()

	if _, err := toml.DecodeFile(path, &c); err != nil {
		c = DefaultServerConfig()
		f, createErr := os.Create(path)
		if createErr != nil {
			return createErr
		}
		defer f.Close()
		if encodeErr := toml.NewEncoder(f).Encode(c); encodeErr != nil {
			return encodeErr
		}
	}

	validate := validator.New()
	if err := validate.Struct(c); err != nil {
		return err
	}

	if err := database.Init(c.DataDirectory); err != nil {
		return err
	}

	loadMasterKey()

	return nil
}

func loadMasterKey() {
	raw := c.Security.MasterKeyB64
	if env := os.Getenv("HANGAR_MASTER_KEY"); env != "" {
		raw = env
	}

	if raw == "" {
		masterKey = nil
		log.Printf("SSE-S3 disabled: no master key configured ([security] master_key_b64 or HANGAR_MASTER_KEY)")
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		masterKey = nil
		log.Printf("SSE-S3 disabled: master key not valid base64: %v", err)
		return
	}

	if len(decoded) != 32 {
		masterKey = nil
		log.Printf("SSE-S3 disabled: master key must decode to 32 bytes (got %d)", len(decoded))
		return
	}

	masterKey = decoded
	log.Printf("SSE-S3 enabled: master key loaded")
}

func MasterKey() []byte {
	mu.RLock()
	defer mu.RUnlock()
	return masterKey
}

func SetMasterKeyForTest(k []byte) {
	mu.Lock()
	defer mu.Unlock()
	masterKey = k
}

func ChunksPath() string {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.DataDirectory + "/chunks"
}

func ChunkHashToPath(hash string) string {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.DataDirectory + "/chunks/" + hash[:2] + "/" + hash[2:4] + "/" + hash
}

func ServerConfig() *serverConfig {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c
}

func ChunkSize() int {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.Storage.ChunkSize
}

func MinFreeBytes() int64 {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.Storage.MinFreeBytes
}

func MinFreePct() int {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.Storage.MinFreePct
}

func NodeMaxBytes() int64 {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.Storage.NodeMaxBytes
}

func SetDiskSafeguardForTest(minFreeBytes int64, minFreePct int, nodeMaxBytes int64) {
	mu.Lock()
	defer mu.Unlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	c.Storage.MinFreeBytes = minFreeBytes
	c.Storage.MinFreePct = minFreePct
	c.Storage.NodeMaxBytes = nodeMaxBytes
}

func CompressionEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.Storage.EnableCompression
}

func GCInterval() int {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.GarbageCollection.Interval
}

func ScrubIntervalHours() int {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.Scrub.IntervalHours
}

func ScrubRateBytesPerSec() int64 {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.Scrub.RateBytesPerSec
}

func SetScrubForTest(intervalHours int, rateBytesPerSec int64) {
	mu.Lock()
	defer mu.Unlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	c.Scrub.IntervalHours = intervalHours
	c.Scrub.RateBytesPerSec = rateBytesPerSec
}

func DataPath() string {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.DataDirectory
}
