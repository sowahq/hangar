package database

import (
	"path/filepath"
	"strings"
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
	base := filepath.Base(fullPath)
	if i := strings.LastIndex(base, "_s"); i > 0 {
		suffix := base[i+2:]
		if suffix != "" && allDigits(suffix) {
			return base[:i]
		}
	}
	return base
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
