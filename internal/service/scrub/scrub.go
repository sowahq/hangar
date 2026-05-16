package scrub

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/service/metrics"
	"github.com/anhostfr/hangar/internal/storage"
	dbutils "github.com/anhostfr/hangar/pkg/database"
	"github.com/phuslu/log"
	"github.com/zeebo/blake3"
	"encoding/hex"
)

const (
	quarantineDirName = ".corrupted"
	chunkRefPrefix    = "chunkref:"
)

type Stats struct {
	TotalChunks   int
	BytesScanned  int64
	Corrupted     int
	Quarantined   int
	MissingFiles  int
	DanglingRefs  int
	StartedAt     time.Time
	Duration      time.Duration
}

type Opts struct {
	DryRun          bool
	RateBytesPerSec int64
	Context         context.Context
}

var (
	lastTickMu sync.RWMutex
	lastTick   time.Time
)

func LastTick() time.Time {
	lastTickMu.RLock()
	defer lastTickMu.RUnlock()
	return lastTick
}

func setLastTick(t time.Time) {
	lastTickMu.Lock()
	lastTick = t
	lastTickMu.Unlock()
}

func Run(opts Opts) (*Stats, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	stats := &Stats{StartedAt: time.Now()}
	chunksPath := config.ChunksPath()
	quarantinePath := filepath.Join(chunksPath, quarantineDirName)

	log.Info().
		Bool("dry_run", opts.DryRun).
		Int64("rate_bytes_per_sec", opts.RateBytesPerSec).
		Str("chunks_path", chunksPath).
		Msg("Starting integrity scrub")

	existing := make(map[string]struct{}, 1024)

	walkErr := filepath.WalkDir(chunksPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path != chunksPath && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}

		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		chunkHash := dbutils.GetChunkHashFromPath(path, chunksPath)
		if chunkHash == "" || len(chunkHash) != 64 {
			return nil
		}

		if storage.IsChunkPending(chunkHash) {
			return nil
		}

		stats.TotalChunks++

		info, statErr := d.Info()
		if statErr != nil {
			log.Warn().Err(statErr).Str("path", path).Msg("Stat failed during scrub")
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			log.Warn().Err(readErr).Str("path", path).Msg("Read failed during scrub")
			return nil
		}

		stats.BytesScanned += info.Size()

		if verifyChunk(data, chunkHash) {
			existing[chunkHash] = struct{}{}
		} else {
			stats.Corrupted++
			log.Warn().Str("chunk", chunkHash).Str("path", path).Msg("Corrupted chunk detected")
			if !opts.DryRun {
				if quarantineErr := quarantine(path, chunkHash, quarantinePath); quarantineErr != nil {
					log.Error().Err(quarantineErr).Str("chunk", chunkHash).Msg("Quarantine failed")
				} else {
					stats.Quarantined++
				}
			}
		}

		throttle(info.Size(), opts.RateBytesPerSec)
		return nil
	})

	if walkErr != nil {
		stats.Duration = time.Since(stats.StartedAt)
		return stats, fmt.Errorf("walk chunks: %w", walkErr)
	}

	if refErr := checkChunkRefs(existing, stats); refErr != nil {
		stats.Duration = time.Since(stats.StartedAt)
		return stats, fmt.Errorf("check chunkrefs: %w", refErr)
	}

	stats.Duration = time.Since(stats.StartedAt)

	log.Info().
		Int("total_chunks", stats.TotalChunks).
		Int64("bytes_scanned", stats.BytesScanned).
		Int("corrupted", stats.Corrupted).
		Int("quarantined", stats.Quarantined).
		Int("missing_files", stats.MissingFiles).
		Int("dangling_refs", stats.DanglingRefs).
		Dur("duration", stats.Duration).
		Msg("Integrity scrub completed")

	metrics.ObserveScrub(stats.Corrupted, stats.Quarantined, stats.BytesScanned, stats.MissingFiles, stats.DanglingRefs, time.Now())

	return stats, nil
}

func verifyChunk(data []byte, expectedHash string) bool {
	rawSum := blake3.Sum256(data)
	rawHash := hex.EncodeToString(rawSum[:])
	if rawHash == expectedHash {
		return true
	}

	if !config.CompressionEnabled() {
		return false
	}

	decoder := storage.GetZstdDecoder()
	plain, err := decoder.DecodeAll(data, nil)
	storage.PutZstdDecoder(decoder)
	if err != nil {
		return false
	}

	plainSum := blake3.Sum256(plain)
	return hex.EncodeToString(plainSum[:]) == expectedHash
}

func quarantine(srcPath, hash, quarantineDir string) error {
	if err := os.MkdirAll(quarantineDir, 0755); err != nil {
		return fmt.Errorf("mkdir quarantine: %w", err)
	}

	dst := filepath.Join(quarantineDir, hash)
	if err := os.Rename(srcPath, dst); err != nil {
		return fmt.Errorf("rename to quarantine: %w", err)
	}

	return nil
}

func checkChunkRefs(existing map[string]struct{}, stats *Stats) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	iter, err := db.NewIteratorWithPrefix([]byte(chunkRefPrefix))
	if err != nil {
		return fmt.Errorf("chunkref iterator: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		if !strings.HasPrefix(key, chunkRefPrefix) {
			continue
		}

		hash := key[len(chunkRefPrefix):]
		if len(hash) != 64 {
			continue
		}

		val := iter.Value()
		if len(val) != 8 || binary.BigEndian.Uint64(val) == 0 {
			stats.DanglingRefs++
			continue
		}

		if _, ok := existing[hash]; ok {
			continue
		}

		if storage.IsChunkPending(hash) {
			continue
		}

		stats.MissingFiles++
		log.Warn().Str("chunk", hash).Msg("Chunkref points to missing chunk file")
	}

	return nil
}

func throttle(bytes, rate int64) {
	if rate <= 0 || bytes <= 0 {
		return
	}
	d := time.Duration(float64(bytes) / float64(rate) * float64(time.Second))
	if d > 0 {
		time.Sleep(d)
	}
}

func StartScheduledScrub(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	hours := config.ScrubIntervalHours()
	if hours <= 0 {
		log.Info().Msg("Scheduled scrub disabled (interval_hours=0)")
		return
	}

	interval := time.Duration(hours) * time.Hour
	log.Info().Dur("interval", interval).Msg("Starting scheduled scrub")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping scheduled scrub")
			return
		case <-ticker.C:
			setLastTick(time.Now())
			log.Info().Msg("Running scheduled scrub")
			stats, err := Run(Opts{
				DryRun:          false,
				RateBytesPerSec: config.ScrubRateBytesPerSec(),
				Context:         ctx,
			})
			if err != nil {
				log.Error().Err(err).Msg("Scheduled scrub failed")
				continue
			}
			log.Info().
				Int("corrupted", stats.Corrupted).
				Int("missing_files", stats.MissingFiles).
				Msg("Scheduled scrub completed")
		}
	}
}
