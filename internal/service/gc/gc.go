package gc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
	dbutils "github.com/anhostfr/hangar/internal/utils/database"
)

type GCStats struct {
	TotalChunks   int
	OrphanChunks  int
	DeletedChunks int
	FreedSpace    int64
}

func RunGarbageCollection(dryRun bool) (*GCStats, error) {
	stats := &GCStats{}

	referencedChunks, err := collectReferencedChunks()
	if err != nil {
		return nil, fmt.Errorf("failed to collect referenced chunks: %w", err)
	}

	chunksPath := config.ChunksPath()
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
				if err := os.Remove(path); err == nil {
					stats.DeletedChunks++
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk chunks directory: %w", err)
	}

	return stats, nil
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
