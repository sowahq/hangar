package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestRangeRequests(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "rng")
	tok := s.createToken(t, "rng", []string{"read", "write"})

	payload := make([]byte, 5000)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	resp := s.doAuth(t, http.MethodPut, "/rng/big.bin", tok, bytes.NewReader(payload), "application/octet-stream")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	size := int64(len(payload))
	tests := []struct {
		name      string
		hdr       string
		wantStat  int
		wantStart int64
		wantEnd   int64
	}{
		{name: "full prefix", hdr: "bytes=0-99", wantStat: http.StatusPartialContent, wantStart: 0, wantEnd: 99},
		{name: "mid range", hdr: "bytes=1500-1999", wantStat: http.StatusPartialContent, wantStart: 1500, wantEnd: 1999},
		{name: "open end", hdr: "bytes=4990-", wantStat: http.StatusPartialContent, wantStart: 4990, wantEnd: size - 1},
		{name: "suffix", hdr: "bytes=-100", wantStat: http.StatusPartialContent, wantStart: size - 100, wantEnd: size - 1},
		{name: "out of bounds", hdr: "bytes=10000-20000", wantStat: http.StatusRequestedRangeNotSatisfiable},
		{name: "multi range", hdr: "bytes=0-10,20-30", wantStat: http.StatusRequestedRangeNotSatisfiable},
		{name: "invalid", hdr: "bytes=abc-def", wantStat: http.StatusRequestedRangeNotSatisfiable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, s.url+"/rng/big.bin", nil)
			if err != nil {
				t.Fatalf("req: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+tok)
			req.Header.Set("Range", tt.hdr)
			resp, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStat {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, tt.wantStat, string(b))
			}
			if tt.wantStat != http.StatusPartialContent {
				return
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			expected := payload[tt.wantStart : tt.wantEnd+1]
			if !bytes.Equal(body, expected) {
				t.Fatalf("body len=%d want=%d (first mismatch?)", len(body), len(expected))
			}
			cr := resp.Header.Get("Content-Range")
			wantCR := fmt.Sprintf("bytes %d-%d/%d", tt.wantStart, tt.wantEnd, size)
			if cr != wantCR {
				t.Fatalf("Content-Range=%q want=%q", cr, wantCR)
			}
		})
	}
}

func TestFullDownloadHasAcceptRanges(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "arb")
	tok := s.createToken(t, "arb", []string{"read", "write"})

	resp := s.doAuth(t, http.MethodPut, "/arb/f.txt", tok, bytes.NewReader([]byte("hello")), "application/octet-stream")
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodGet, "/arb/f.txt", tok, nil, "")
	defer resp.Body.Close()
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges=%q want=bytes", got)
	}
}
