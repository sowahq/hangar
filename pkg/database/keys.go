package database

import (
	"path/filepath"
)

func ExtractFilenameFromKey(key string) string {
	const prefix = "metadata:"
	if len(key) <= len(prefix) {
		return ""
	}
	return key[len(prefix):]
}

func ExtractKeyFromFilename(filename string) string {
	return "metadata:" + filename
}

func GetChunkHashFromPath(fullPath, chunksPath string) string {
	if _, err := filepath.Rel(chunksPath, fullPath); err != nil {
		return ""
	}
	return filepath.Base(fullPath)
}
