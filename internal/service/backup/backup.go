package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/sowahq/hangar/internal/database"
)

const ManifestFile = "manifest.json"

const ManifestVersion = 1

type Manifest struct {
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	StoreBytes int64     `json:"store_bytes"`
	ChunkBytes int64     `json:"chunk_bytes"`
	ChunkFiles int64     `json:"chunk_files"`
}

var (
	ErrBackupExists    = errors.New("backup destination already exists")
	ErrRestoreOccupied = errors.New("restore destination is not empty")
	ErrInvalidBackup   = errors.New("invalid backup directory")
)

func Create(dataDir, outDir string) (*Manifest, error) {
	if dataDir == "" || outDir == "" {
		return nil, fmt.Errorf("dataDir and outDir are required")
	}

	if _, err := os.Stat(outDir); err == nil {
		return nil, ErrBackupExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("stat outDir: %w", err)
	}

	storeSrc := filepath.Join(dataDir, "store")
	chunksSrc := filepath.Join(dataDir, "chunks")

	if _, err := os.Stat(storeSrc); err != nil {
		return nil, fmt.Errorf("source store missing: %w", err)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir out: %w", err)
	}

	db, err := database.NewPebbleDB(storeSrc)
	if err != nil {
		return nil, fmt.Errorf("open source store (is the server stopped?): %w", err)
	}

	storeDst := filepath.Join(outDir, "store")
	if err := db.Checkpoint(storeDst); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pebble checkpoint: %w", err)
	}

	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close source store: %w", err)
	}

	chunksDst := filepath.Join(outDir, "chunks")
	chunkFiles, chunkBytes, err := cloneTree(chunksSrc, chunksDst)
	if err != nil {
		return nil, fmt.Errorf("copy chunks: %w", err)
	}

	storeBytes, err := dirSize(storeDst)
	if err != nil {
		return nil, fmt.Errorf("size store: %w", err)
	}

	m := &Manifest{
		Version:    ManifestVersion,
		CreatedAt:  time.Now().UTC(),
		StoreBytes: storeBytes,
		ChunkBytes: chunkBytes,
		ChunkFiles: chunkFiles,
	}

	if err := writeManifest(outDir, m); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	return m, nil
}

func Restore(inDir, dataDir string) (*Manifest, error) {
	if inDir == "" || dataDir == "" {
		return nil, fmt.Errorf("inDir and dataDir are required")
	}

	m, err := readManifest(inDir)
	if err != nil {
		return nil, err
	}

	storeSrc := filepath.Join(inDir, "store")
	chunksSrc := filepath.Join(inDir, "chunks")

	if _, err := os.Stat(storeSrc); err != nil {
		return nil, fmt.Errorf("%w: missing store/", ErrInvalidBackup)
	}

	if err := ensureEmpty(dataDir); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data: %w", err)
	}

	if _, _, err := cloneTree(storeSrc, filepath.Join(dataDir, "store")); err != nil {
		return nil, fmt.Errorf("restore store: %w", err)
	}

	if _, err := os.Stat(chunksSrc); err == nil {
		if _, _, err := cloneTree(chunksSrc, filepath.Join(dataDir, "chunks")); err != nil {
			return nil, fmt.Errorf("restore chunks: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("stat chunks src: %w", err)
	} else {
		if err := os.MkdirAll(filepath.Join(dataDir, "chunks"), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir chunks: %w", err)
		}
	}

	return m, nil
}

func ensureEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat dataDir: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if name == "store" || name == "chunks" {
			return ErrRestoreOccupied
		}
	}
	return nil
}

func writeManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ManifestFile), data, 0o644)
}

func readManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: missing %s", ErrInvalidBackup, ManifestFile)
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBackup, err)
	}

	if m.Version != ManifestVersion {
		return nil, fmt.Errorf("%w: unsupported manifest version %d", ErrInvalidBackup, m.Version)
	}
	return &m, nil
}

func cloneTree(src, dst string) (int64, int64, error) {
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, os.MkdirAll(dst, 0o755)
		}
		return 0, 0, err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, 0, err
	}

	var files, bytes int64

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if err := linkOrCopy(path, target, info.Mode()); err != nil {
			return err
		}

		files++
		bytes += info.Size()
		return nil
	})

	return files, bytes, err
}

func linkOrCopy(src, dst string, mode fs.FileMode) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}

	return out.Close()
}

func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}
