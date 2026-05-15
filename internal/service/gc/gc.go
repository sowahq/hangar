package gc

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/storage"
	dbutils "github.com/anhostfr/hangar/pkg/database"
	"github.com/phuslu/log"
)

type GCStats struct {
	TotalChunks   int
	OrphanChunks  int
	DeletedChunks int
	FreedSpace    int64
}

const gcGrace = time.Hour

func RunGarbageCollection(dryRun bool) (*GCStats, error) {
	log.Info().Bool("dry_run", dryRun).Msg("Starting garbage collection")

	stats := &GCStats{}
	chunksPath := config.ChunksPath()
	log.Debug().Str("chunks_path", chunksPath).Msg("Walking chunks directory")

	cutoff := time.Now().Add(-gcGrace)

	err := filepath.WalkDir(chunksPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		name := d.Name()
		if len(name) > 0 && name[0] == '.' {
			info, statErr := d.Info()
			if statErr != nil || info.ModTime().After(cutoff) {
				return nil
			}
			if !dryRun {
				_ = os.Remove(path)
			}
			return nil
		}

		chunkHash := dbutils.GetChunkHashFromPath(path, chunksPath)
		if chunkHash == "" {
			return nil
		}

		stats.TotalChunks++

		referenced, refErr := storage.IsChunkReferenced(chunkHash)
		if refErr != nil {
			log.Warn().Err(refErr).Str("chunk", chunkHash).Msg("Failed to check chunkref; skipping")
			return nil
		}
		if referenced {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			log.Warn().Err(statErr).Str("path", path).Msg("Failed to stat chunk")
			return nil
		}

		if info.ModTime().After(cutoff) {
			log.Debug().Str("chunk", chunkHash).Msg("Skipping young orphan (within grace period)")
			return nil
		}

		stats.OrphanChunks++
		stats.FreedSpace += info.Size()

		if dryRun {
			return nil
		}

		log.Debug().Str("chunk", chunkHash).Str("path", path).Msg("Removing orphan chunk")
		if err := os.Remove(path); err == nil {
			stats.DeletedChunks++
		} else {
			log.Warn().Err(err).Str("path", path).Msg("Failed to remove chunk")
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Msg("Failed to walk chunks directory")
		return nil, fmt.Errorf("failed to walk chunks directory: %w", err)
	}

	log.Info().
		Int("total_chunks", stats.TotalChunks).
		Int("orphan_chunks", stats.OrphanChunks).
		Int("deleted_chunks", stats.DeletedChunks).
		Int64("freed_space", stats.FreedSpace).
		Msg("Garbage collection completed")

	return stats, nil
}

func StartScheduledGC(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	interval := time.Duration(config.GCInterval()) * time.Hour

	log.Info().Dur("interval", interval).Msg("Starting scheduled garbage collection")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping scheduled garbage collection")
			return
		case <-ticker.C:
			log.Info().Msg("Running scheduled garbage collection")
			stats, err := RunGarbageCollection(false)
			if err != nil {
				log.Error().Err(err).Msg("Scheduled garbage collection failed")
				continue
			}
			log.Info().
				Int("deleted_chunks", stats.DeletedChunks).
				Int64("freed_space", stats.FreedSpace).
				Msg("Scheduled garbage collection completed")
		}
	}
}
