package accesslog

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/phuslu/log"
)

type Record struct {
	When         time.Time
	SourceBucket string
	RemoteIP     string
	AccessKey    string
	RequestID    string
	Method       string
	Path         string
	Key          string
	Status       int
	BytesSent    int64
	ObjectSize   int64
	TotalMillis  int64
	UserAgent    string
	Referer      string
}

type writer struct {
	mu      sync.Mutex
	buf     map[string][]Record
	flushCh chan struct{}
	stop    chan struct{}
	wg      sync.WaitGroup
}

var w = newWriter()

func newWriter() *writer {
	return &writer{
		buf:     map[string][]Record{},
		flushCh: make(chan struct{}, 1),
		stop:    make(chan struct{}),
	}
}

func Start() {
	w.wg.Add(1)
	go w.loop()
}

func Stop() {
	close(w.stop)
	w.wg.Wait()
}

func Enqueue(r Record) {
	cfg, err := bucket.GetLogging(r.SourceBucket)
	if err != nil || cfg == nil {
		return
	}
	if cfg.TargetBucket == r.SourceBucket {
		return
	}

	w.mu.Lock()
	w.buf[r.SourceBucket] = append(w.buf[r.SourceBucket], r)
	w.mu.Unlock()

	select {
	case w.flushCh <- struct{}{}:
	default:
	}
}

func (w *writer) loop() {
	defer w.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			w.flush()
			return
		case <-ticker.C:
			w.flush()
		case <-w.flushCh:
		}
	}
}

func (w *writer) flush() {
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.mu.Unlock()
		return
	}
	pending := w.buf
	w.buf = map[string][]Record{}
	w.mu.Unlock()

	for src, recs := range pending {
		cfg, err := bucket.GetLogging(src)
		if err != nil || cfg == nil {
			continue
		}
		w.flushBatch(cfg.TargetBucket, cfg.TargetPrefix, recs)
	}
}

func (w *writer) flushBatch(target, prefix string, recs []Record) {
	if len(recs) == 0 {
		return
	}

	var buf bytes.Buffer
	for _, r := range recs {
		buf.WriteString(formatRecord(&r))
		buf.WriteByte('\n')
	}

	now := time.Now().UTC()
	uniq := make([]byte, 8)
	_, _ = rand.Read(uniq)
	key := fmt.Sprintf("%s%s-%s", prefix, now.Format("2006-01-02-15-04-05"), hex.EncodeToString(uniq))

	body := buf.Bytes()
	if _, err := object.PutObject(&object.PutObjectRequest{
		Bucket:        target,
		Key:           key,
		Body:          bytes.NewReader(body),
		ContentLength: int64(len(body)),
		ContentType:   "text/plain",
	}); err != nil {
		log.Warn().Err(err).Str("bucket", target).Str("key", key).Msg("accesslog: failed to write batch")
	}
}

func formatRecord(r *Record) string {
	ts := r.When.UTC().Format("[02/Jan/2006:15:04:05 +0000]")
	q := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	return fmt.Sprintf("- %s %s %s %s %s %s %s %q %d - %d %d %d - %q %q -",
		q(r.SourceBucket), ts, q(r.RemoteIP), q(r.AccessKey), q(r.RequestID),
		q(r.Method), q(r.Key),
		fmt.Sprintf("%s %s HTTP/1.1", r.Method, r.Path),
		r.Status, r.BytesSent, r.ObjectSize, r.TotalMillis,
		r.UserAgent, r.Referer,
	)
}
