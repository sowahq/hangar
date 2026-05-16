package lifecycle

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/phuslu/log"
)

type Stats struct {
	BucketsScanned   int
	ObjectsExpired   int
	MultipartsAborted int
}

var (
	mu       sync.Mutex
	lastTick time.Time
)

func now() time.Time { return time.Now() }

func Run(ctx context.Context) (*Stats, error) {
	stats := &Stats{}

	res, err := bucket.ListBuckets()
	if err != nil {
		return stats, err
	}

	for _, b := range res.Buckets {
		if ctx != nil && ctx.Err() != nil {
			return stats, ctx.Err()
		}

		cfg, err := bucket.GetLifecycle(b.Name)
		if err != nil {
			continue
		}

		stats.BucketsScanned++

		if err := expireObjects(ctx, b.Name, cfg, stats); err != nil {
			log.Error().Err(err).Str("bucket", b.Name).Msg("lifecycle expire failed")
		}

		if err := abortStaleMultiparts(b.Name, cfg, stats); err != nil {
			log.Error().Err(err).Str("bucket", b.Name).Msg("lifecycle abort mpu failed")
		}
	}

	mu.Lock()
	lastTick = now()
	mu.Unlock()

	return stats, nil
}

func expireObjects(ctx context.Context, bucketName string, cfg *bucket.LifecycleConfiguration, stats *Stats) error {
	db := database.LocalStore()
	if db == nil {
		return nil
	}

	prefix := []byte("metadata:" + bucketName + "/")
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return err
	}

	type victim struct {
		key string
	}
	var victims []victim

	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		k := iter.Key()
		if !strings.HasPrefix(string(k), string(prefix)) {
			break
		}

		var m storage.Metadatas
		if err := json.Unmarshal(iter.Value(), &m); err != nil {
			continue
		}
		if m.IsDeleteMarker {
			continue
		}

		rule := bucket.MatchLifecycleRule(cfg, m.Key)
		if rule == nil {
			continue
		}

		age := now().Sub(time.UnixMilli(m.CreatedAt))
		if age < time.Duration(rule.ExpirationDays)*24*time.Hour {
			continue
		}

		victims = append(victims, victim{key: m.Key})
	}
	iter.Close()

	for _, v := range victims {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		_, err := object.DeleteObject(&object.DeleteObjectRequest{Bucket: bucketName, Key: v.key})
		if err != nil {
			log.Warn().Err(err).Str("bucket", bucketName).Str("key", v.key).Msg("lifecycle delete failed")
			continue
		}
		stats.ObjectsExpired++
	}

	return nil
}

func abortStaleMultiparts(bucketName string, cfg *bucket.LifecycleConfiguration, stats *Stats) error {
	maxDays := 0
	for _, r := range cfg.Rules {
		if r.Enabled && r.AbortMultipartAfterDays > maxDays {
			maxDays = r.AbortMultipartAfterDays
		}
	}
	if maxDays <= 0 {
		return nil
	}

	cutoff := now().Add(-time.Duration(maxDays) * 24 * time.Hour).UnixMilli()

	headers, err := storage.ScanBucketMultiparts(bucketName)
	if err != nil {
		return err
	}

	for _, h := range headers {
		if h.CreatedAt >= cutoff {
			continue
		}
		if err := object.AbortMultipart(&object.AbortMultipartRequest{Bucket: h.Bucket, Key: h.Key, UploadID: h.UploadID}); err != nil {
			log.Warn().Err(err).Str("bucket", h.Bucket).Str("upload_id", h.UploadID).Msg("lifecycle abort mpu failed")
			continue
		}
		stats.MultipartsAborted++
	}

	return nil
}

func StartScheduled(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	if !config.LifecycleEnabled() {
		log.Info().Msg("Lifecycle scheduler disabled")
		return
	}

	hours := config.LifecycleIntervalHours()
	if hours <= 0 {
		hours = 24
	}
	interval := time.Duration(hours) * time.Hour
	log.Info().Dur("interval", interval).Msg("Starting lifecycle scheduler")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping lifecycle scheduler")
			return
		case <-ticker.C:
			stats, err := Run(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Lifecycle run failed")
				continue
			}
			log.Info().Int("expired", stats.ObjectsExpired).Int("aborted_mpu", stats.MultipartsAborted).Msg("Lifecycle run completed")
		}
	}
}

func LastTick() time.Time {
	mu.Lock()
	defer mu.Unlock()
	return lastTick
}
