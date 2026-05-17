package cluster

import "sync/atomic"

var (
	current        atomic.Pointer[Cluster]
	currentRuntime atomic.Pointer[Runtime]
)

func SetGlobal(c *Cluster) { current.Store(c) }

func Global() *Cluster { return current.Load() }

func SetGlobalRuntime(r *Runtime) { currentRuntime.Store(r) }

func GlobalRuntime() *Runtime { return currentRuntime.Load() }
