package database

import (
	"path/filepath"
	"strings"
)

// ExtractFilenameFromKey extracts filename from metadata key
func ExtractFilenameFromKey(key string) string {
	const prefix = "metadata:"
	if len(key) <= len(prefix) {
		return ""
	}
	return key[len(prefix):]
}

// ExtractKeyFromFilename creates a metadata key from filename
func ExtractKeyFromFilename(filename string) string {
	return "metadata:" + filename
}

// GetChunkHashFromPath extracts chunk hash from file path
func GetChunkHashFromPath(fullPath, chunksPath string) string {
	relPath, err := filepath.Rel(chunksPath, fullPath)
	if err != nil {
		return ""
	}

	// Remove directory separators to get hash
	return strings.ReplaceAll(relPath, string(filepath.Separator), "")
}
