package bucket

import (
	"testing"

	"github.com/sowahq/hangar/internal/testutil"
)

func TestBucketLoggingCRUD(t *testing.T) {
	testutil.SetupServer(t)
	if _, err := CreateBucket(&CreateBucketRequest{Name: "logsrc"}); err != nil {
		t.Fatalf("src: %v", err)
	}
	if _, err := CreateBucket(&CreateBucketRequest{Name: "logtgt"}); err != nil {
		t.Fatalf("tgt: %v", err)
	}

	if _, err := GetLogging("logsrc"); err == nil {
		t.Fatalf("expected not found before put")
	}

	if err := PutLogging("logsrc", &LoggingConfig{TargetBucket: "logtgt", TargetPrefix: "logs/"}); err != nil {
		t.Fatalf("put: %v", err)
	}

	cfg, err := GetLogging("logsrc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cfg.TargetBucket != "logtgt" || cfg.TargetPrefix != "logs/" {
		t.Fatalf("got %+v", cfg)
	}

	if err := PutLogging("logsrc", &LoggingConfig{TargetBucket: "nonexistent"}); err == nil {
		t.Fatalf("expected error on missing target")
	}

	if err := DeleteLogging("logsrc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetLogging("logsrc"); err == nil {
		t.Fatalf("expected not found after delete")
	}
}
