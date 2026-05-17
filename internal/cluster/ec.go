package cluster

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/klauspost/reedsolomon"
)

const ecLengthHeader = 8

type ECEncoder struct {
	k   int
	m   int
	enc reedsolomon.Encoder
}

func NewECEncoder(k, m int) (*ECEncoder, error) {
	if k <= 0 || m <= 0 {
		return nil, errors.New("ec: data and parity shard counts must be positive")
	}
	enc, err := reedsolomon.New(k, m)
	if err != nil {
		return nil, fmt.Errorf("ec: build encoder: %w", err)
	}
	return &ECEncoder{k: k, m: m, enc: enc}, nil
}

func (e *ECEncoder) Total() int { return e.k + e.m }

func (e *ECEncoder) Data() int { return e.k }

func (e *ECEncoder) Parity() int { return e.m }

func (e *ECEncoder) Encode(payload []byte) ([][]byte, error) {
	if payload == nil {
		payload = []byte{}
	}
	buf := make([]byte, ecLengthHeader+len(payload))
	binary.BigEndian.PutUint64(buf[:ecLengthHeader], uint64(len(payload)))
	copy(buf[ecLengthHeader:], payload)

	dataShards, err := e.enc.Split(buf)
	if err != nil {
		return nil, fmt.Errorf("ec: split: %w", err)
	}

	shards := make([][]byte, e.k+e.m)
	for i, s := range dataShards {
		shards[i] = s
	}
	shardSize := len(dataShards[0])
	for i := e.k; i < e.k+e.m; i++ {
		shards[i] = make([]byte, shardSize)
	}

	if err := e.enc.Encode(shards); err != nil {
		return nil, fmt.Errorf("ec: encode: %w", err)
	}
	return shards, nil
}

func (e *ECEncoder) Reconstruct(shards [][]byte) error {
	if len(shards) != e.k+e.m {
		return fmt.Errorf("ec: expected %d shards got %d", e.k+e.m, len(shards))
	}
	present := 0
	var shardSize int
	for _, s := range shards {
		if s != nil {
			present++
			if shardSize == 0 {
				shardSize = len(s)
			} else if len(s) != shardSize {
				return fmt.Errorf("ec: shard size mismatch %d vs %d", len(s), shardSize)
			}
		}
	}
	if present < e.k {
		return fmt.Errorf("ec: not enough shards (%d/%d)", present, e.k)
	}
	if err := e.enc.Reconstruct(shards); err != nil {
		return fmt.Errorf("ec: reconstruct: %w", err)
	}
	return nil
}

func (e *ECEncoder) Decode(shards [][]byte) ([]byte, error) {
	if len(shards) != e.k+e.m {
		return nil, fmt.Errorf("ec: expected %d shards got %d", e.k+e.m, len(shards))
	}

	present := 0
	var shardSize int
	for _, s := range shards {
		if s != nil {
			present++
			if shardSize == 0 {
				shardSize = len(s)
			} else if len(s) != shardSize {
				return nil, fmt.Errorf("ec: shard size mismatch %d vs %d", len(s), shardSize)
			}
		}
	}
	if present < e.k {
		return nil, fmt.Errorf("ec: not enough shards (%d/%d)", present, e.k)
	}

	if err := e.enc.ReconstructData(shards); err != nil {
		return nil, fmt.Errorf("ec: reconstruct: %w", err)
	}

	total := shardSize * e.k
	buf := make([]byte, total)
	for i := 0; i < e.k; i++ {
		copy(buf[i*shardSize:(i+1)*shardSize], shards[i])
	}
	if len(buf) < ecLengthHeader {
		return nil, errors.New("ec: decoded buffer smaller than header")
	}
	plen := binary.BigEndian.Uint64(buf[:ecLengthHeader])
	if uint64(len(buf)-ecLengthHeader) < plen {
		return nil, fmt.Errorf("ec: declared length %d exceeds decoded %d", plen, len(buf)-ecLengthHeader)
	}
	return buf[ecLengthHeader : ecLengthHeader+plen], nil
}

func shardKey(hash string, idx int) string {
	return fmt.Sprintf("%s_s%d", hash, idx)
}
