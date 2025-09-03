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

	// TODO: S3 API
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

	return config
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

func DataPath() string {
	mu.RLock()
	defer mu.RUnlock()

	if c == nil {
		c = DefaultServerConfig()
	}

	return c.DataDirectory
}
