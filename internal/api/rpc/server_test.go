package rpc

import (
	"context"
	"testing"

	"storj.io/drpc/drpcerr"
)

func TestServerImplementsInterface(t *testing.T) {
	var s DRPCClusterServer = NewServer(nil)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestUnimplementedHandshake(t *testing.T) {
	s := NewServer(nil)
	_, err := s.Handshake(context.Background(), &Hello{NodeId: "n1"})
	if err == nil {
		t.Fatal("Handshake: want error, got nil")
	}
	if drpcerr.Code(err) != drpcerr.Unimplemented {
		t.Fatalf("Handshake: want drpcerr.Unimplemented, got code=%d err=%v", drpcerr.Code(err), err)
	}
}

func TestUnimplementedGetMetadata(t *testing.T) {
	s := NewServer(nil)
	_, err := s.GetMetadata(context.Background(), &MetadataKey{Bucket: "b", Key: "k"})
	if err == nil {
		t.Fatal("GetMetadata: want error, got nil")
	}
	if drpcerr.Code(err) != drpcerr.Unimplemented {
		t.Fatalf("GetMetadata: want drpcerr.Unimplemented, got code=%d", drpcerr.Code(err))
	}
}

func TestUnimplementedAck(t *testing.T) {
	s := NewServer(nil)
	_, err := s.IncRefs(context.Background(), &RefDelta{Hashes: []string{"abc"}})
	if err == nil {
		t.Fatal("IncRefs: want error, got nil")
	}
	if drpcerr.Code(err) != drpcerr.Unimplemented {
		t.Fatalf("IncRefs: want drpcerr.Unimplemented, got code=%d", drpcerr.Code(err))
	}
}
