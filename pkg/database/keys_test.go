package database

import (
	"path/filepath"
	"testing"
)

func TestExtractFilenameFromKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"normal key", "metadata:obj", "obj"},
		{"nested key", "metadata:bucket/path/obj", "bucket/path/obj"},
		{"prefix only", "metadata:", ""},
		{"empty", "", ""},
		{"shorter than prefix", "meta", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractFilenameFromKey(tc.key); got != tc.want {
				t.Errorf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestGetChunkHashFromPath(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	tests := []struct {
		name       string
		chunksPath string
		fullPath   string
		want       string
	}{
		{
			name:       "standard nested layout",
			chunksPath: "data/chunks",
			fullPath:   filepath.Join("data", "chunks", hash[:2], hash[2:4], hash),
			want:       hash,
		},
		{
			name:       "absolute paths",
			chunksPath: "/var/lib/hangar/chunks",
			fullPath:   filepath.Join("/var/lib/hangar/chunks", hash[:2], hash[2:4], hash),
			want:       hash,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GetChunkHashFromPath(tc.fullPath, tc.chunksPath); got != tc.want {
				t.Errorf("got=%q want=%q", got, tc.want)
			}
		})
	}
}
