package object

import (
	"bytes"
	"testing"

	bucketSvc "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/sowahq/hangar/internal/testutil"
)

func enableVersioning(t *testing.T, name string) {
	t.Helper()
	if _, err := bucketSvc.CreateBucket(&bucketSvc.CreateBucketRequest{Name: name}); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if _, err := bucketSvc.UpdateVersioning(name, true); err != nil {
		t.Fatalf("UpdateVersioning: %v", err)
	}
}

func TestVersioningPutCreatesVersion(t *testing.T) {
	testutil.SetupServer(t)
	enableVersioning(t, "vbucket")

	body1 := []byte("v1-payload")
	resp1, err := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader(body1)})
	if err != nil {
		t.Fatalf("PutObject v1: %v", err)
	}
	if resp1.VersionID == "" {
		t.Fatal("v1 version id empty")
	}

	body2 := []byte("v2-payload-longer")
	resp2, err := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader(body2)})
	if err != nil {
		t.Fatalf("PutObject v2: %v", err)
	}
	if resp2.VersionID == "" || resp2.VersionID == resp1.VersionID {
		t.Fatalf("v2 version id invalid: %q (v1=%q)", resp2.VersionID, resp1.VersionID)
	}

	versions, err := storage.ListObjectVersions("vbucket", "k")
	if err != nil {
		t.Fatalf("ListObjectVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("want 2 versions, got %d", len(versions))
	}

	cur, err := storage.GetMetadataFromBucket("vbucket", "k")
	if err != nil {
		t.Fatalf("GetMetadataFromBucket: %v", err)
	}
	if cur.VersionID != resp2.VersionID {
		t.Errorf("current vid=%q want=%q", cur.VersionID, resp2.VersionID)
	}
}

func TestVersioningGetByVersionID(t *testing.T) {
	testutil.SetupServer(t)
	enableVersioning(t, "vbucket")

	resp1, _ := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader([]byte("alpha"))})
	resp2, _ := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader([]byte("beta"))})

	tests := []struct {
		name      string
		versionID string
		want      string
	}{
		{"v1 by id", resp1.VersionID, "alpha"},
		{"v2 by id", resp2.VersionID, "beta"},
		{"current", "", "beta"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := GetObject(&GetObjectRequest{Bucket: "vbucket", Key: "k", VersionID: tt.versionID})
			if err != nil {
				t.Fatalf("GetObject: %v", err)
			}
			got := make([]byte, res.Size)
			if _, err := res.Reader.Read(got); err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got=%q want=%q", string(got), tt.want)
			}
		})
	}
}

func TestVersioningDeleteMarker(t *testing.T) {
	testutil.SetupServer(t)
	enableVersioning(t, "vbucket")

	resp1, _ := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader([]byte("hi"))})

	delRes, err := DeleteObject(&DeleteObjectRequest{Bucket: "vbucket", Key: "k"})
	if err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if !delRes.IsDeleteMarker {
		t.Fatal("expected delete marker")
	}

	_, err = GetObject(&GetObjectRequest{Bucket: "vbucket", Key: "k"})
	if err == nil {
		t.Fatal("expected error fetching after delete marker, got nil")
	}

	res, err := GetObject(&GetObjectRequest{Bucket: "vbucket", Key: "k", VersionID: resp1.VersionID})
	if err != nil {
		t.Fatalf("get v1 after marker: %v", err)
	}
	got := make([]byte, res.Size)
	if _, err := res.Reader.Read(got); err != nil {
		t.Fatalf("read v1: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("got=%q want=hi", string(got))
	}
}

func TestVersioningDeleteSpecificVersion(t *testing.T) {
	testutil.SetupServer(t)
	enableVersioning(t, "vbucket")

	resp1, _ := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader([]byte("v1-data"))})
	resp2, _ := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader([]byte("v2-data"))})

	v1Meta, _ := storage.GetObjectVersion("vbucket", "k", resp1.VersionID)
	if v1Meta == nil || len(v1Meta.ChunkHashes) == 0 {
		t.Fatal("v1 meta missing")
	}

	if _, err := DeleteObject(&DeleteObjectRequest{Bucket: "vbucket", Key: "k", VersionID: resp1.VersionID}); err != nil {
		t.Fatalf("DeleteObject v1: %v", err)
	}

	if _, err := storage.GetObjectVersion("vbucket", "k", resp1.VersionID); err == nil {
		t.Error("v1 still present after deletion")
	}

	cur, err := storage.GetMetadataFromBucket("vbucket", "k")
	if err != nil {
		t.Fatalf("GetMetadataFromBucket: %v", err)
	}
	if cur.VersionID != resp2.VersionID {
		t.Errorf("current vid changed: got=%q want=%q", cur.VersionID, resp2.VersionID)
	}

	for _, h := range v1Meta.ChunkHashes {
		shared := false
		for _, h2 := range cur.ChunkHashes {
			if h == h2 {
				shared = true
				break
			}
		}
		ref, _ := storage.IsChunkReferenced(h)
		if !shared && ref {
			t.Errorf("v1 chunk %s still referenced after v1 deletion", h)
		}
	}
}

func TestVersioningDeleteCurrentVersionDropsPointer(t *testing.T) {
	testutil.SetupServer(t)
	enableVersioning(t, "vbucket")

	resp1, _ := PutObject(&PutObjectRequest{Bucket: "vbucket", Key: "k", Body: bytes.NewReader([]byte("only"))})

	if _, err := DeleteObject(&DeleteObjectRequest{Bucket: "vbucket", Key: "k", VersionID: resp1.VersionID}); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if _, err := storage.GetMetadataFromBucket("vbucket", "k"); err == nil {
		t.Error("current pointer should be gone")
	}
}
