package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ActorTypeAdmin  = "admin"
	ActorTypeS3Key  = "s3key"
	ActorTypeCLI    = "cli"
	ActorTypeSystem = "system"

	ResultSuccess = "success"
	ResultError   = "error"
)

type Event struct {
	TS         int64  `json:"ts"`
	Actor      string `json:"actor,omitempty"`
	ActorType  string `json:"actor_type"`
	Action     string `json:"action"`
	TargetType string `json:"target_type,omitempty"`
	Target     string `json:"target,omitempty"`
	Result     string `json:"result"`
	Error      string `json:"error,omitempty"`
	IP         string `json:"ip,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Extra      any    `json:"extra,omitempty"`
}

type Options struct {
	Enabled       bool
	Path          string
	MaxSizeBytes  int64
	MaxBackups    int
	RetentionDays int
}

var (
	mu      sync.Mutex
	f       *os.File
	opts    Options
	curSize int64
	nowFn   = func() time.Time { return time.Now() }
)

var ErrDisabled = errors.New("audit log disabled")

func Init(o Options) error {
	mu.Lock()
	defer mu.Unlock()

	if f != nil {
		_ = f.Close()
		f = nil
		curSize = 0
	}

	opts = o
	if !o.Enabled {
		return nil
	}

	if o.Path == "" {
		return fmt.Errorf("audit: empty path")
	}
	if o.MaxSizeBytes <= 0 {
		opts.MaxSizeBytes = 100 * 1024 * 1024
	}

	if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
		return fmt.Errorf("audit mkdir: %w", err)
	}

	file, err := os.OpenFile(o.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit open: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("audit stat: %w", err)
	}

	f = file
	curSize = info.Size()

	pruneBackups()
	pruneRetention()

	return nil
}

func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if f == nil {
		return nil
	}
	err := f.Close()
	f = nil
	curSize = 0
	return err
}

func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return opts.Enabled && f != nil
}

func Record(ev Event) {
	mu.Lock()
	defer mu.Unlock()

	if !opts.Enabled || f == nil {
		return
	}

	if ev.TS == 0 {
		ev.TS = nowFn().UnixMilli()
	}
	if ev.Result == "" {
		ev.Result = ResultSuccess
	}

	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	line = append(line, '\n')

	if curSize+int64(len(line)) > opts.MaxSizeBytes {
		if err := rotateLocked(); err != nil {
			return
		}
	}

	n, err := f.Write(line)
	if err != nil {
		return
	}
	curSize += int64(n)

	_ = f.Sync()
}

func rotateLocked() error {
	if f == nil {
		return nil
	}

	if err := f.Close(); err != nil {
		return err
	}
	f = nil

	ts := nowFn().UnixNano()
	rotated := fmt.Sprintf("%s.%d", opts.Path, ts)

	if err := os.Rename(opts.Path, rotated); err != nil {
		reopened, oerr := os.OpenFile(opts.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if oerr == nil {
			f = reopened
			if info, serr := reopened.Stat(); serr == nil {
				curSize = info.Size()
			}
		}
		return err
	}

	file, err := os.OpenFile(opts.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	f = file
	curSize = 0

	pruneBackups()
	pruneRetention()

	return nil
}

func pruneBackups() {
	if opts.MaxBackups <= 0 {
		return
	}

	backups, err := listBackups()
	if err != nil {
		return
	}

	if len(backups) <= opts.MaxBackups {
		return
	}

	for _, b := range backups[opts.MaxBackups:] {
		_ = os.Remove(b)
	}
}

func pruneRetention() {
	if opts.RetentionDays <= 0 {
		return
	}

	cutoff := nowFn().AddDate(0, 0, -opts.RetentionDays)

	backups, err := listBackups()
	if err != nil {
		return
	}

	for _, b := range backups {
		info, err := os.Stat(b)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(b)
		}
	}
}

func listBackups() ([]string, error) {
	dir := filepath.Dir(opts.Path)
	base := filepath.Base(opts.Path) + "."

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), base) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}

	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

func Tail(limit int) ([]Event, error) {
	mu.Lock()
	path := opts.Path
	enabled := opts.Enabled
	mu.Unlock()

	if !enabled {
		return nil, ErrDisabled
	}
	if limit <= 0 {
		limit = 100
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Event{}, nil
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []Event{}, nil
	}

	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}

	out := make([]Event, 0, len(lines)-start)
	for _, ln := range lines[start:] {
		if ln == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(ln), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

func SetClockForTest(fn func() time.Time) {
	mu.Lock()
	defer mu.Unlock()
	if fn == nil {
		nowFn = func() time.Time { return time.Now() }
		return
	}
	nowFn = fn
}
