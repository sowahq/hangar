package gc

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
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

func RunGarbageCollection(dryRun bool) (*GCStats, error) {
	log.Info().Bool("dry_run", dryRun).Msg("Starting garbage collection")
	
	stats := &GCStats{}

	log.Debug().Msg("Collecting referenced chunks from database")
	referencedChunks, err := collectReferencedChunks()
	if err != nil {
		log.Error().Err(err).Msg("Failed to collect referenced chunks")
		return nil, fmt.Errorf("failed to collect referenced chunks: %w", err)
	}

	log.Debug().Int("referenced_chunks", len(referencedChunks)).Msg("Found referenced chunks")

	chunksPath := config.ChunksPath()
	log.Debug().Str("chunks_path", chunksPath).Msg("Walking chunks directory")
	
	err = filepath.WalkDir(chunksPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		chunkHash := dbutils.GetChunkHashFromPath(path, chunksPath)
		if chunkHash == "" {
			return nil
		}

		stats.TotalChunks++

		if _, exists := referencedChunks[chunkHash]; !exists {
			stats.OrphanChunks++

			if !dryRun {
				if info, err := os.Stat(path); err == nil {
					stats.FreedSpace += info.Size()
				}
				log.Debug().Str("chunk", chunkHash).Str("path", path).Msg("Removing orphan chunk")
				if err := os.Remove(path); err == nil {
					stats.DeletedChunks++
				} else {
					log.Warn().Err(err).Str("path", path).Msg("Failed to remove chunk")
				}
			} else {
				if info, err := os.Stat(path); err == nil {
					stats.FreedSpace += info.Size()
				}
			}
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

// StartScheduledGC starts the garbage collection scheduler
func StartScheduledGC(ctx context.Context) {
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

func collectReferencedChunks() (map[string]bool, error) {
	referenced := make(map[string]bool)

	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	iter, err := db.NewIteratorWithPrefix([]byte("metadata:"))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}

	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		filename := dbutils.ExtractFilenameFromKey(string(iter.Key()))
		metadata, err := storage.GetMetadata(filename)
		if err != nil {
			continue
		}

		for _, chunkHash := range metadata.ChunkHashes {
			referenced[chunkHash] = true
		}
	}

	return referenced, nil
}
