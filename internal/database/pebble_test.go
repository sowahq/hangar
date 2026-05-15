package database

import (
	"bytes"
	"testing"
)

func TestKeyUpperBound(t *testing.T) {
	tests := []struct {
		name   string
		prefix []byte
		want   []byte
	}{
		{
			name:   "ascii prefix increments last byte",
			prefix: []byte("metadata:"),
			want:   []byte("metadata;"),
		},
		{
			name:   "trailing 0xff carries over",
			prefix: []byte{'a', 0xff},
			want:   []byte{'b'},
		},
		{
			name:   "multiple trailing 0xff carry over",
			prefix: []byte{'a', 0xff, 0xff},
			want:   []byte{'b'},
		},
		{
			name:   "all 0xff yields nil (no upper bound)",
			prefix: []byte{0xff, 0xff},
			want:   nil,
		},
		{
			name:   "empty prefix yields nil",
			prefix: []byte{},
			want:   nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := keyUpperBound(tc.prefix)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestNewIteratorWithPrefixBounded(t *testing.T) {
	db, err := NewPebbleDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewPebbleDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	seed := map[string]string{
		"bucket:alpha":   "1",
		"bucket:beta":    "2",
		"chunkref:aaa":   "3",
		"metadata:b/obj": "4",
	}
	for k, v := range seed {
		if err := db.Put([]byte(k), []byte(v)); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{"bucket prefix stops before chunkref", "bucket:", []string{"bucket:alpha", "bucket:beta"}},
		{"chunkref prefix isolates chunkref keys", "chunkref:", []string{"chunkref:aaa"}},
		{"metadata prefix isolates metadata keys", "metadata:", []string{"metadata:b/obj"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			iter, err := db.NewIteratorWithPrefix([]byte(tc.prefix))
			if err != nil {
				t.Fatalf("NewIteratorWithPrefix: %v", err)
			}
			defer iter.Close()

			var got []string
			for iter.First(); iter.Valid(); iter.Next() {
				got = append(got, string(iter.Key()))
			}
			if len(got) != len(tc.want) {
				t.Fatalf("count: got=%d %v want=%d %v", len(got), got, len(tc.want), tc.want)
			}
			for i, k := range tc.want {
				if got[i] != k {
					t.Errorf("[%d] got=%s want=%s", i, got[i], k)
				}
			}
		})
	}
}
