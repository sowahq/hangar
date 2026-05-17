package cluster

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
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
		{"asym_6_2", 6, 2, 1 << 15},
		{"single_data_1_2", 1, 2, 333},
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

func TestECEncoderExactlyK(t *testing.T) {
	enc, err := NewECEncoder(4, 2)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	payload := bytes.Repeat([]byte("a"), 4096)
	shards, err := enc.Encode(payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	cases := [][]int{
		{0, 1, 2, 3},
		{0, 2, 4, 5},
		{1, 3, 4, 5},
		{2, 3, 4, 5},
	}
	for _, present := range cases {
		recv := make([][]byte, len(shards))
		for _, i := range present {
			recv[i] = shards[i]
		}
		got, err := enc.Decode(recv)
		if err != nil {
			t.Fatalf("decode with %v: %v", present, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("payload mismatch with %v", present)
		}
	}
}

func TestECEncoderShardSizeMismatch(t *testing.T) {
	enc, err := NewECEncoder(4, 2)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	payload := make([]byte, 1024)
	shards, err := enc.Encode(payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	shards[2] = append([]byte{}, shards[2]...)
	shards[2] = shards[2][:len(shards[2])-1]
	if _, err := enc.Decode(shards); err == nil {
		t.Fatalf("expected size-mismatch error")
	}
}

func TestECEncoderWrongShardCount(t *testing.T) {
	enc, err := NewECEncoder(4, 2)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	if _, err := enc.Decode(make([][]byte, 3)); err == nil {
		t.Fatalf("expected error for wrong count")
	}
	if _, err := enc.Decode(make([][]byte, 7)); err == nil {
		t.Fatalf("expected error for wrong count")
	}
	if err := enc.Reconstruct(make([][]byte, 3)); err == nil {
		t.Fatalf("expected error for wrong count")
	}
}

func TestECEncoderBounds(t *testing.T) {
	cases := []struct {
		k, m int
		ok   bool
	}{
		{0, 1, false},
		{1, 0, false},
		{-1, 2, false},
		{2, -1, false},
		{1, 1, true},
		{4, 2, true},
		{8, 8, true},
	}
	for _, tc := range cases {
		_, err := NewECEncoder(tc.k, tc.m)
		gotOK := err == nil
		if gotOK != tc.ok {
			t.Fatalf("NewECEncoder(%d,%d) ok=%v want %v err=%v", tc.k, tc.m, gotOK, tc.ok, err)
		}
	}
}

func TestECEncoderCorruptedLengthHeader(t *testing.T) {
	enc, err := NewECEncoder(4, 2)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	payload := make([]byte, 8192)
	_, _ = rand.Read(payload)
	shards, err := enc.Encode(payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(shards[0]) < ecLengthHeader {
		t.Fatalf("shard too small for header test")
	}
	binary.BigEndian.PutUint64(shards[0][:ecLengthHeader], 1<<40)
	if _, err := enc.Decode(shards); err == nil {
		t.Fatalf("expected declared-length error")
	}
}

func TestECEncoderRoundtripAfterReconstruct(t *testing.T) {
	enc, err := NewECEncoder(4, 2)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	payload := make([]byte, 8000)
	_, _ = rand.Read(payload)
	shards, err := enc.Encode(payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	want := make([][]byte, len(shards))
	for i := range shards {
		want[i] = append([]byte(nil), shards[i]...)
	}

	recv := make([][]byte, len(shards))
	recv[0] = nil
	recv[1] = shards[1]
	recv[2] = nil
	recv[3] = shards[3]
	recv[4] = shards[4]
	recv[5] = shards[5]

	if err := enc.Reconstruct(recv); err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if !bytes.Equal(recv[0], want[0]) {
		t.Fatalf("shard 0 not recovered")
	}
	if !bytes.Equal(recv[2], want[2]) {
		t.Fatalf("shard 2 not recovered")
	}

	got, err := enc.Decode(recv)
	if err != nil {
		t.Fatalf("Decode after Reconstruct: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestECEncoderNilPayload(t *testing.T) {
	enc, err := NewECEncoder(3, 2)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	shards, err := enc.Encode(nil)
	if err != nil {
		t.Fatalf("Encode(nil): %v", err)
	}
	got, err := enc.Decode(shards)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty payload, got %d bytes", len(got))
	}
}

func TestECEncoderTotalDataParity(t *testing.T) {
	enc, err := NewECEncoder(6, 3)
	if err != nil {
		t.Fatalf("NewECEncoder: %v", err)
	}
	if enc.Total() != 9 || enc.Data() != 6 || enc.Parity() != 3 {
		t.Fatalf("counts wrong total=%d data=%d parity=%d", enc.Total(), enc.Data(), enc.Parity())
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
