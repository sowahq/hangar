package cluster

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestECEncoderRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		k, m int
		size int
	}{
		{"small_4_2", 4, 2, 1024},
		{"med_4_2", 4, 2, 1 << 20},
		{"odd_3_2", 3, 2, 12345},
		{"zero_byte_4_2", 4, 2, 0},
		{"one_byte_4_2", 4, 2, 1},
		{"big_8_3", 8, 3, 5 << 20},
		{"asym_2_4", 2, 4, 4096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := NewECEncoder(tc.k, tc.m)
			if err != nil {
				t.Fatalf("NewECEncoder: %v", err)
			}
			payload := make([]byte, tc.size)
			_, _ = rand.Read(payload)

			shards, err := enc.Encode(payload)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if len(shards) != tc.k+tc.m {
				t.Fatalf("shard count %d want %d", len(shards), tc.k+tc.m)
			}

			recv := make([][]byte, len(shards))
			for i := 0; i < tc.k; i++ {
				recv[i] = shards[i]
			}
			for i := tc.k; i < tc.k+tc.m; i++ {
				recv[i] = nil
			}
			got, err := enc.Decode(recv)
			if err != nil {
				t.Fatalf("Decode from data only: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("body mismatch from data shards")
			}

			recv = make([][]byte, len(shards))
			for i := tc.m; i < tc.k+tc.m; i++ {
				recv[i] = shards[i]
			}
			got, err = enc.Decode(recv)
			if err != nil {
				t.Fatalf("Decode from tail %d shards: %v", tc.k, err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("body mismatch from tail shards")
			}
		})
	}
}

func TestECEncoderTooFewShards(t *testing.T) {
	enc, err := NewECEncoder(4, 2)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	payload := make([]byte, 1024)
	_, _ = rand.Read(payload)
	shards, err := enc.Encode(payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	recv := make([][]byte, len(shards))
	for i := 0; i < 3; i++ {
		recv[i] = shards[i]
	}
	if _, err := enc.Decode(recv); err == nil {
		t.Fatalf("expected error when fewer than k shards present")
	}
}

func TestShardKey(t *testing.T) {
	if got := shardKey("abc", 0); got != "abc_s0" {
		t.Fatalf("shardKey: %q", got)
	}
	if got := shardKey("abc", 10); got != "abc_s10" {
		t.Fatalf("shardKey idx10: %q", got)
	}
}
