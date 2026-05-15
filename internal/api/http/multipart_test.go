package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
)

func TestMultipartHTTPRoundtrip(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "mbucket")
	tok := s.createToken(t, "mbucket", []string{"read", "write", "delete"})

	resp := s.doAuth(t, http.MethodPost, "/mbucket/big.bin?uploads", tok, nil, "")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("initiate status=%d body=%s", resp.StatusCode, string(b))
	}
	var initOut struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(readBody(t, resp), &initOut); err != nil {
		t.Fatalf("init unmarshal: %v", err)
	}
	if initOut.UploadID == "" {
		t.Fatal("empty upload id")
	}

	parts := [][]byte{
		bytes.Repeat([]byte("X"), 1500),
		bytes.Repeat([]byte("Y"), 2000),
	}
	for i, body := range parts {
		path := "/mbucket/big.bin?uploadId=" + initOut.UploadID + "&partNumber=" + strconv.Itoa(i+1)
		resp := s.doAuth(t, http.MethodPut, path, tok, bytes.NewReader(body), "application/octet-stream")
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("upload part %d status=%d body=%s", i+1, resp.StatusCode, string(b))
		}
		resp.Body.Close()
	}

	completeBody, _ := json.Marshal(map[string]any{"parts": []int{1, 2}})
	resp = s.doAuth(t, http.MethodPost, "/mbucket/big.bin?uploadId="+initOut.UploadID, tok, bytes.NewReader(completeBody), "application/json")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("complete status=%d body=%s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodGet, "/mbucket/big.bin", tok, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d", resp.StatusCode)
	}
	got := readBody(t, resp)
	var want []byte
	for _, p := range parts {
		want = append(want, p...)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %d bytes want %d", len(got), len(want))
	}
}

func TestMultipartHTTPAbort(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "mbucket")
	tok := s.createToken(t, "mbucket", []string{"read", "write", "delete"})

	resp := s.doAuth(t, http.MethodPost, "/mbucket/k?uploads", tok, nil, "")
	var initOut struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(readBody(t, resp), &initOut); err != nil {
		t.Fatalf("init unmarshal: %v", err)
	}

	path := "/mbucket/k?uploadId=" + initOut.UploadID + "&partNumber=1"
	resp = s.doAuth(t, http.MethodPut, path, tok, bytes.NewReader([]byte("payload")), "application/octet-stream")
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodDelete, "/mbucket/k?uploadId="+initOut.UploadID, tok, nil, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("abort status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodPut, path, tok, bytes.NewReader([]byte("x")), "application/octet-stream")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("upload after abort status=%d want=404", resp.StatusCode)
	}
	resp.Body.Close()
}
