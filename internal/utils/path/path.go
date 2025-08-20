package path

import (
	"path/filepath"
)

func ExtractFilename(key string) string {
	return filepath.Base(key)
}

func GetPrefix(key string) string {
	dir := filepath.Dir(key)
	if dir == "." {
		return ""
	}
	return dir + "/"
}
