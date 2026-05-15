package config

import (
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
		ChunkSize         int  `toml:"chunk_size" validate:"min=1024"` // Min 1KB
		EnableCompression bool `toml:"enable_compression"`
	} `toml:"storage"`

	GarbageCollection struct {
		Interval int `toml:"interval_hours" validate:"min=1"` // Hours between GC runs
	} `toml:"garbage_collection"`

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
}

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

	return nil
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

func DataPath() string {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.DataDirectory
}
