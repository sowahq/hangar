//go:build tools

package tools

import (
	_ "github.com/klauspost/reedsolomon"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
	_ "storj.io/drpc"
	_ "storj.io/drpc/cmd/protoc-gen-go-drpc"
	_ "storj.io/drpc/drpcconn"
	_ "storj.io/drpc/drpcmux"
	_ "storj.io/drpc/drpcserver"
)
