package storage

import (
	"testing"
)

func TestChunkBufPool(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		wantLen  int
		wantMinC int
	}{
		{"small", 1024, 1024, 1024},
		{"chunk size", 4 * 1024 * 1024, 4 * 1024 * 1024, 4 * 1024 * 1024},
		{"odd", 12345, 12345, 12345},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bp := getChunkBuf(tc.size)
			if got := len(*bp); got != tc.wantLen {
				t.Errorf("len: got=%d want=%d", got, tc.wantLen)
			}
			if got := cap(*bp); got < tc.wantMinC {
				t.Errorf("cap: got=%d want>=%d", got, tc.wantMinC)
			}
			putChunkBuf(bp)
		})
	}
}

func TestChunkBufPoolReuse(t *testing.T) {
	bp1 := getChunkBuf(8192)
	addr1 := &(*bp1)[0]
	putChunkBuf(bp1)

	bp2 := getChunkBuf(8192)
	defer putChunkBuf(bp2)

	if len(*bp2) != 8192 {
		t.Fatalf("reused buf len: got=%d want=8192", len(*bp2))
	}
	addr2 := &(*bp2)[0]
	if addr1 != addr2 {
		t.Logf("note: pool did not reuse the same backing array (GC may have evicted)")
	}
}

func BenchmarkChunkBufPool(b *testing.B) {
	const size = 4 * 1024 * 1024
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bp := getChunkBuf(size)
		_ = (*bp)[0]
		putChunkBuf(bp)
	}
}

func BenchmarkChunkBufNoPool(b *testing.B) {
	const size = 4 * 1024 * 1024
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := make([]byte, size)
		_ = buf[0]
	}
}

func TestChunkBufPoolGrows(t *testing.T) {
	bp := getChunkBuf(1024)
	putChunkBuf(bp)

	bigger := getChunkBuf(64 * 1024)
	defer putChunkBuf(bigger)

	if len(*bigger) != 64*1024 {
		t.Errorf("len: got=%d want=%d", len(*bigger), 64*1024)
	}
	if cap(*bigger) < 64*1024 {
		t.Errorf("cap: got=%d want>=%d", cap(*bigger), 64*1024)
	}
}
