package rpc

type Server struct {
	DRPCClusterUnimplementedServer
}

func NewServer() *Server {
	return &Server{}
}

var _ DRPCClusterServer = (*Server)(nil)
