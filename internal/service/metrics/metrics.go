package metrics

import (
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry *prometheus.Registry

	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	inflightGauge   *prometheus.GaugeVec

	gcLastTick      prometheus.Gauge
	gcDeleted       prometheus.Counter
	gcFreedBytes    prometheus.Counter
	gcOrphan        prometheus.Gauge
	gcTotalChunks   prometheus.Gauge

	scrubLastTick     prometheus.Gauge
	scrubCorrupted    prometheus.Counter
	scrubQuarantined  prometheus.Counter
	scrubMissingFiles prometheus.Gauge
	scrubDanglingRefs prometheus.Gauge
	scrubBytesScanned prometheus.Counter

	diskFreeBytes     prometheus.Gauge
	diskTotalBytes    prometheus.Gauge
	nodeUsedBytes     prometheus.Gauge
	nodeMaxBytesGauge prometheus.Gauge

	multipartInflight prometheus.Gauge

	clusterViewVersion   prometheus.Gauge
	clusterLayoutVersion prometheus.Gauge
	clusterAlivePeers    prometheus.Gauge
	clusterTotalPeers    prometheus.Gauge
	clusterGCLeader      prometheus.Gauge
	clusterECDataShards  prometheus.Gauge
	clusterECParityShards prometheus.Gauge

	initOnce sync.Once
)

func Init() {
	initOnce.Do(func() {
		registry = prometheus.NewRegistry()

		requestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "hangar",
				Name:      "requests_total",
				Help:      "Total HTTP requests handled, labeled by api, method and status.",
			},
			[]string{"api", "method", "status"},
		)

		requestDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "hangar",
				Name:      "request_duration_seconds",
				Help:      "Request latency in seconds.",
				Buckets: []float64{
					0.001, 0.005, 0.01, 0.025, 0.05, 0.1,
					0.25, 0.5, 1, 2.5, 5, 10, 30,
				},
			},
			[]string{"api", "method", "status"},
		)

		inflightGauge = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "hangar",
				Name:      "requests_inflight",
				Help:      "In-flight HTTP requests.",
			},
			[]string{"api"},
		)

		gcLastTick = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "gc",
			Name: "last_tick_seconds", Help: "Unix timestamp of last GC tick (0 if never).",
		})
		gcDeleted = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "hangar", Subsystem: "gc",
			Name: "deleted_chunks_total", Help: "Cumulative chunks deleted by GC.",
		})
		gcFreedBytes = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "hangar", Subsystem: "gc",
			Name: "freed_bytes_total", Help: "Cumulative bytes freed by GC.",
		})
		gcOrphan = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "gc",
			Name: "orphan_chunks", Help: "Orphan chunks observed at last GC run.",
		})
		gcTotalChunks = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "gc",
			Name: "total_chunks", Help: "Total chunks observed at last GC run.",
		})

		scrubLastTick = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "scrub",
			Name: "last_tick_seconds", Help: "Unix timestamp of last scrub tick (0 if never).",
		})
		scrubCorrupted = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "hangar", Subsystem: "scrub",
			Name: "corrupted_total", Help: "Cumulative corrupted chunks detected.",
		})
		scrubQuarantined = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "hangar", Subsystem: "scrub",
			Name: "quarantined_total", Help: "Cumulative chunks moved to quarantine.",
		})
		scrubMissingFiles = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "scrub",
			Name: "missing_files", Help: "Chunkrefs pointing to missing files at last scrub.",
		})
		scrubDanglingRefs = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "scrub",
			Name: "dangling_refs", Help: "Zero-valued chunkrefs at last scrub.",
		})
		scrubBytesScanned = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "hangar", Subsystem: "scrub",
			Name: "bytes_scanned_total", Help: "Cumulative bytes scanned during scrubs.",
		})

		diskFreeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "disk",
			Name: "free_bytes", Help: "Free bytes on data filesystem.",
		})
		diskTotalBytes = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "disk",
			Name: "total_bytes", Help: "Total bytes on data filesystem.",
		})
		nodeUsedBytes = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "disk",
			Name: "node_used_bytes", Help: "Bytes used by hangar data directory (cached).",
		})
		nodeMaxBytesGauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "disk",
			Name: "node_max_bytes", Help: "Configured node max bytes (0 = unlimited).",
		})

		multipartInflight = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar",
			Name:      "multipart_inflight",
			Help:      "Multipart uploads currently in flight.",
		})

		clusterViewVersion = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "cluster", Name: "view_version",
			Help: "Cluster membership view version (monotonic).",
		})
		clusterLayoutVersion = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "cluster", Name: "layout_version",
			Help: "Cluster layout version currently applied.",
		})
		clusterAlivePeers = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "cluster", Name: "alive_peers",
			Help: "Number of cluster peers currently Active.",
		})
		clusterTotalPeers = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "cluster", Name: "total_peers",
			Help: "Number of cluster peers configured (self included).",
		})
		clusterGCLeader = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "cluster", Name: "gc_leader",
			Help: "1 if this node is the cluster GC leader (lowest-id alive), 0 otherwise.",
		})
		clusterECDataShards = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "cluster", Name: "ec_data_shards",
			Help: "Configured erasure-coding data shards (k).",
		})
		clusterECParityShards = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hangar", Subsystem: "cluster", Name: "ec_parity_shards",
			Help: "Configured erasure-coding parity shards (m).",
		})

		registry.MustRegister(
			requestsTotal,
			requestDuration,
			inflightGauge,
			gcLastTick, gcDeleted, gcFreedBytes, gcOrphan, gcTotalChunks,
			scrubLastTick, scrubCorrupted, scrubQuarantined,
			scrubMissingFiles, scrubDanglingRefs, scrubBytesScanned,
			diskFreeBytes, diskTotalBytes, nodeUsedBytes, nodeMaxBytesGauge,
			multipartInflight,
			clusterViewVersion, clusterLayoutVersion, clusterAlivePeers, clusterTotalPeers,
			clusterGCLeader, clusterECDataShards, clusterECParityShards,
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
	})
}

func Registry() *prometheus.Registry {
	Init()
	return registry
}

func Middleware(api string) fiber.Handler {
	Init()

	return func(c *fiber.Ctx) error {
		inflightGauge.WithLabelValues(api).Inc()
		start := time.Now()

		err := c.Next()

		code := c.Response().StatusCode()
		if err != nil {
			if fe, ok := err.(*fiber.Error); ok {
				code = fe.Code
			} else if code < 400 {
				code = fiber.StatusInternalServerError
			}
		}

		status := strconv.Itoa(code)
		method := c.Method()
		dur := time.Since(start).Seconds()

		requestsTotal.WithLabelValues(api, method, status).Inc()
		requestDuration.WithLabelValues(api, method, status).Observe(dur)
		inflightGauge.WithLabelValues(api).Dec()

		return err
	}
}

func Handler() fiber.Handler {
	Init()
	return adaptor.HTTPHandler(promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry}))
}

func ObserveGC(total, orphan, deleted int, freedBytesDelta int64, tick time.Time) {
	Init()
	gcTotalChunks.Set(float64(total))
	gcOrphan.Set(float64(orphan))
	if deleted > 0 {
		gcDeleted.Add(float64(deleted))
	}
	if freedBytesDelta > 0 {
		gcFreedBytes.Add(float64(freedBytesDelta))
	}
	if !tick.IsZero() {
		gcLastTick.Set(float64(tick.Unix()))
	}
}

func ObserveScrub(corruptedDelta, quarantinedDelta int, bytesScannedDelta int64, missing, dangling int, tick time.Time) {
	Init()
	if corruptedDelta > 0 {
		scrubCorrupted.Add(float64(corruptedDelta))
	}
	if quarantinedDelta > 0 {
		scrubQuarantined.Add(float64(quarantinedDelta))
	}
	if bytesScannedDelta > 0 {
		scrubBytesScanned.Add(float64(bytesScannedDelta))
	}
	scrubMissingFiles.Set(float64(missing))
	scrubDanglingRefs.Set(float64(dangling))
	if !tick.IsZero() {
		scrubLastTick.Set(float64(tick.Unix()))
	}
}

func ObserveDisk(free, total, nodeUsed, nodeMax int64) {
	Init()
	if free >= 0 {
		diskFreeBytes.Set(float64(free))
	}
	if total >= 0 {
		diskTotalBytes.Set(float64(total))
	}
	if nodeUsed >= 0 {
		nodeUsedBytes.Set(float64(nodeUsed))
	}
	nodeMaxBytesGauge.Set(float64(nodeMax))
}

func MultipartInflightInc() {
	Init()
	multipartInflight.Inc()
}

func MultipartInflightDec() {
	Init()
	multipartInflight.Dec()
}

func ObserveCluster(viewVersion, layoutVersion uint64, alivePeers, totalPeers int, gcLeader bool, ecData, ecParity int) {
	Init()
	clusterViewVersion.Set(float64(viewVersion))
	clusterLayoutVersion.Set(float64(layoutVersion))
	clusterAlivePeers.Set(float64(alivePeers))
	clusterTotalPeers.Set(float64(totalPeers))
	if gcLeader {
		clusterGCLeader.Set(1)
	} else {
		clusterGCLeader.Set(0)
	}
	clusterECDataShards.Set(float64(ecData))
	clusterECParityShards.Set(float64(ecParity))
}

func ResetForTest() {
	initOnce = sync.Once{}
	registry = nil
}
