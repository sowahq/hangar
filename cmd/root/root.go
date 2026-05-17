package root

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anhostfr/hangar/cmd/backup"
	"github.com/anhostfr/hangar/cmd/bucket"
	clusterCmd "github.com/anhostfr/hangar/cmd/cluster"
	"github.com/anhostfr/hangar/cmd/s3keys"
	scrubcmd "github.com/anhostfr/hangar/cmd/scrub"
	"github.com/anhostfr/hangar/internal/api/http"
	metricsRouter "github.com/anhostfr/hangar/internal/api/metrics"
	"github.com/anhostfr/hangar/internal/api/s3"
	"github.com/anhostfr/hangar/internal/cluster"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/service/accesslog"
	"github.com/anhostfr/hangar/internal/service/audit"
	gcService "github.com/anhostfr/hangar/internal/service/gc"
	lifecycleService "github.com/anhostfr/hangar/internal/service/lifecycle"
	metricsService "github.com/anhostfr/hangar/internal/service/metrics"
	scrubService "github.com/anhostfr/hangar/internal/service/scrub"
	"github.com/anhostfr/hangar/internal/service/sse"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
	"github.com/urfave/cli/v2"
)

const shutdownTimeout = 30 * time.Second

func Execute() {
	app := &cli.App{
		Name:        "hangar",
		Description: "Object Storage CLI",
		Commands: []*cli.Command{
			{
				Name:        "bucket",
				Usage:       "Manage buckets",
				Subcommands: bucket.Commands(),
			},
			{
				Name:        "s3keys",
				Usage:       "Manage S3 access keys",
				Subcommands: s3keys.Commands(),
			},
			{
				Name:        "backup",
				Usage:       "Create and restore data backups",
				Subcommands: backup.Commands(),
			},
			{
				Name:        "scrub",
				Usage:       "Verify chunk integrity (re-hash, quarantine corrupt)",
				Subcommands: scrubcmd.Commands(),
			},
			{
				Name:        "cluster",
				Usage:       "Inspect and manage cluster state",
				Subcommands: clusterCmd.Commands(),
			},
			{
				Name:  "server",
				Usage: "Start the object storage server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "config",
						Usage:   "Path to the configuration file",
						Aliases: []string{"c"},
						Value:   "config.toml",
					},
				},
				Action: func(c *cli.Context) error {
					configPath := c.String("config")

					log.Info().Msgf("Starting Hangar server with config file: %s", configPath)

					if err := config.LoadServerConfig(configPath); err != nil {
						log.Error().Err(err).Msg("Failed to load configuration.")
						return err
					}

					if err := os.MkdirAll(config.ServerConfig().DataDirectory, 0755); err != nil {
						log.Error().Err(err).Msg("Failed to create data directory.")
					}

					if err := os.MkdirAll(config.ChunksPath(), 0755); err != nil {
						log.Error().Err(err).Msg("Failed to create chunks directory.")
					}

					log.Debug().Msgf("Created data directory: %s", config.ServerConfig().DataDirectory)

					if err := storage.BootstrapChunkRefs(); err != nil {
						log.Error().Err(err).Msg("Failed to bootstrap chunkref index.")
						return err
					}

					if err := sse.Bootstrap(config.MasterKey()); err != nil {
						log.Error().Err(err).Msg("Failed to bootstrap sse keyring.")
						return err
					}

					if config.AuditEnabled() {
						if err := audit.Init(audit.Options{
							Enabled:       true,
							Path:          config.AuditPath(),
							MaxSizeBytes:  config.AuditMaxSizeBytes(),
							MaxBackups:    config.AuditMaxBackups(),
							RetentionDays: config.AuditRetentionDays(),
						}); err != nil {
							log.Error().Err(err).Msg("Failed to init audit log.")
							return err
						}

						audit.Record(audit.Event{
							ActorType: audit.ActorTypeSystem,
							Action:    "server.start",
						})
					}

					httpRouter := http.Router()
					httpErr := make(chan error, 1)
					go func() {
						httpErr <- httpRouter.Listen(config.ServerConfig().API.BindAddr)
					}()

					var s3Router *fiber.App
					s3Err := make(chan error, 1)
					if config.S3Enabled() {
						s3Router = s3.Router()
						go func() {
							s3Err <- s3.Listen(s3Router, config.S3BindAddr())
						}()
					}

					var metricsApp *fiber.App
					metricsErr := make(chan error, 1)
					if config.MetricsEnabled() {
						metricsService.Init()
						metricsApp = metricsRouter.Router()
						go func() {
							metricsErr <- metricsApp.Listen(config.MetricsBindAddr())
						}()
					}

					ctx, cancel := context.WithCancel(context.Background())

					var clusterRuntime *cluster.Runtime
					if config.ClusterEnabled() {
						peers, err := cluster.ParsePeers(config.ClusterPeers())
						if err != nil {
							log.Error().Err(err).Msg("Failed to parse cluster peers.")
							cancel()
							return err
						}

						clusterRuntime, err = cluster.Start(ctx, cluster.Config{
							NodeID:      cluster.NodeID(config.ClusterNodeID()),
							Listen:      config.ClusterListen(),
							Peers:       peers,
							Secret:      config.ClusterSharedSecret(),
							HeartbeatMS: config.HeartbeatMS(),
						})
						if err != nil {
							log.Error().Err(err).Msg("Failed to start cluster runtime.")
							cancel()
							return err
						}

						if err := clusterRuntime.Cluster.LoadLayout(); err != nil {
							log.Warn().Err(err).Msg("Failed to load persisted cluster layout.")
						}

						storage.SetMetadataStore(cluster.NewClusteredMetadataStore(clusterRuntime.Cluster, clusterRuntime.Pool))
						storage.SetChunkStore(cluster.NewClusteredChunkStore(clusterRuntime.Cluster, clusterRuntime.Pool))
						storage.SetRefcountStore(cluster.NewClusteredRefcountStore(clusterRuntime.Cluster, clusterRuntime.Pool))

						cl := clusterRuntime.Cluster
						gcService.SetClusterLeaderCheck(cl.IsGCLeader)
						scrubService.SetClusterLeaderCheck(cl.IsGCLeader)
						lifecycleService.SetClusterLeaderCheck(cl.IsGCLeader)

						if config.MetricsEnabled() {
							ecData := config.ECDataShards()
							ecParity := config.ECParityShards()
							go cl.StartMetricsSampler(ctx, 5*time.Second, func(vv, lv uint64, alive, total int, leader bool) {
								metricsService.ObserveCluster(vv, lv, alive, total, leader, ecData, ecParity)
							})
						}

						go clusterRuntime.StartAntiEntropy(ctx, time.Hour)
						go clusterRuntime.StartCatchupLoop(ctx, 15*time.Second)

						go func() {
							syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
							defer cancel()
							count, err := clusterRuntime.BootstrapPeerSync(syncCtx, 30, time.Second)
							if err != nil {
								log.Warn().Err(err).Msg("Cluster bootstrap sync error.")
								return
							}
							if count > 0 {
								log.Info().Int("entries", count).Msg("Cluster bootstrap synced replicated KV from peer.")
							}
						}()

						log.Info().
							Str("node_id", config.ClusterNodeID()).
							Str("listen", clusterRuntime.Addr()).
							Int("peers", len(peers)).
							Msg("Cluster runtime started")
					}

					accesslog.Start()
					gcDone := make(chan struct{})
					go gcService.StartScheduledGC(ctx, gcDone)

					scrubDone := make(chan struct{})
					go scrubService.StartScheduledScrub(ctx, scrubDone)

					lifecycleDone := make(chan struct{})
					go lifecycleService.StartScheduled(ctx, lifecycleDone)

					diskDone := make(chan struct{})
					if config.MetricsEnabled() {
						go metricsService.StartDiskSampler(ctx, diskDone)
					} else {
						close(diskDone)
					}

					osSignal := make(chan os.Signal, 1)
					signal.Notify(osSignal, os.Interrupt, syscall.SIGTERM)

					select {
					case sig := <-osSignal:
						log.Info().Str("signal", sig.String()).Msg("Shutdown signal received")
					case err := <-httpErr:
						log.Error().Err(err).Msg("HTTP server exited unexpectedly")
					case err := <-s3Err:
						log.Error().Err(err).Msg("S3 server exited unexpectedly")
					case err := <-metricsErr:
						log.Error().Err(err).Msg("Metrics server exited unexpectedly")
					}

					log.Info().Dur("timeout", shutdownTimeout).Msg("Shutting down Hangar...")

					if err := httpRouter.ShutdownWithTimeout(shutdownTimeout); err != nil {
						log.Error().Err(err).Msg("HTTP server shutdown error")
					}
					if s3Router != nil {
						if err := s3Router.ShutdownWithTimeout(shutdownTimeout); err != nil {
							log.Error().Err(err).Msg("S3 server shutdown error")
						}
					}
					if metricsApp != nil {
						if err := metricsApp.ShutdownWithTimeout(shutdownTimeout); err != nil {
							log.Error().Err(err).Msg("Metrics server shutdown error")
						}
					}

					cancel()

					if clusterRuntime != nil {
						clusterRuntime.Stop()
					}

					accesslog.Stop()
					select {
					case <-gcDone:
					case <-time.After(shutdownTimeout):
						log.Warn().Msg("GC goroutine did not exit within timeout")
					}
					select {
					case <-scrubDone:
					case <-time.After(shutdownTimeout):
						log.Warn().Msg("Scrub goroutine did not exit within timeout")
					}
					select {
					case <-lifecycleDone:
					case <-time.After(shutdownTimeout):
						log.Warn().Msg("Lifecycle goroutine did not exit within timeout")
					}
					select {
					case <-diskDone:
					case <-time.After(shutdownTimeout):
						log.Warn().Msg("Disk sampler did not exit within timeout")
					}

					if audit.Enabled() {
						audit.Record(audit.Event{
							ActorType: audit.ActorTypeSystem,
							Action:    "server.stop",
						})

						if err := audit.Close(); err != nil {
							log.Error().Err(err).Msg("Failed to close audit log")
						}
					}

					if err := database.Close(); err != nil {
						log.Error().Err(err).Msg("Failed to close database")
					}

					log.Info().Msg("Hangar stopped")
					return nil
				},
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
		os.Exit(1)
	}
}
