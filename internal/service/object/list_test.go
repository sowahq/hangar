package object

import (
	"bytes"
	"sort"
	"testing"

	"github.com/sowahq/hangar/internal/testutil"
)

func TestListObjectsInBucket(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		seed   map[string][]string
		prefix string
		want   []string
	}{
		{
			name:   "empty bucket",
			bucket: "b",
			seed:   map[string][]string{},
			want:   nil,
		},
		{
			name:   "lists all without prefix",
			bucket: "b",
			seed:   map[string][]string{"b": {"a.txt", "logs/x.log", "logs/y.log"}},
			want:   []string{"a.txt", "logs/x.log", "logs/y.log"},
		},
		{
			name:   "prefix filters",
			bucket: "b",
			seed:   map[string][]string{"b": {"a.txt", "logs/x.log", "logs/y.log"}},
			prefix: "logs/",
			want:   []string{"logs/x.log", "logs/y.log"},
		},
		{
			name:   "isolates bucket",
			bucket: "b",
			seed: map[string][]string{
				"b":     {"keep.txt"},
				"other": {"hidden.txt"},
			},
			want: []string{"keep.txt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupServer(t)
			for bucket, keys := range tc.seed {
				for _, k := range keys {
					body := bytes.Repeat([]byte("z"), 16)
					if _, err := PutObject(&PutObjectRequest{Bucket: bucket, Key: k, Body: bytes.NewReader(body)}); err != nil {
						t.Fatalf("PutObject(%s/%s): %v", bucket, k, err)
					}
				}
			}

			resp, err := ListObjectsInBucket(tc.bucket, tc.prefix)
			if err != nil {
				t.Fatalf("ListObjectsInBucket: %v", err)
			}
			got := make([]string, 0, len(resp.Objects))
			for _, o := range resp.Objects {
				got = append(got, o.Key)
			}
			sort.Strings(got)
			sort.Strings(tc.want)
			if len(got) != len(tc.want) {
				t.Fatalf("count: got=%d (%v) want=%d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got=%s want=%s", i, got[i], tc.want[i])
				}
			}
		})
	}
}
