package cluster

import "sync/atomic"

var current atomic.Pointer[Cluster]

func SetGlobal(c *Cluster) { current.Store(c) }

func Global() *Cluster { return current.Load() }
