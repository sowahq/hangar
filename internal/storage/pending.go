package storage

import "sync"

var (
	pendingMu     sync.Mutex
	pendingChunks = make(map[string]int)
)

func MarkChunkPending(hash string) {
	if hash == "" {
		return
	}

	pendingMu.Lock()
	pendingChunks[hash]++
	pendingMu.Unlock()
}

func MarkChunksPending(hashes []string) {
	if len(hashes) == 0 {
		return
	}

	pendingMu.Lock()
	defer pendingMu.Unlock()

	for _, h := range hashes {
		if h == "" {
			continue
		}

		pendingChunks[h]++
	}
}

func UnmarkChunkPending(hash string) {
	if hash == "" {
		return
	}

	pendingMu.Lock()
	defer pendingMu.Unlock()

	c, ok := pendingChunks[hash]
	if !ok {
		return
	}

	if c <= 1 {
		delete(pendingChunks, hash)
		return
	}

	pendingChunks[hash] = c - 1
}

func UnmarkChunksPending(hashes []string) {
	if len(hashes) == 0 {
		return
	}

	pendingMu.Lock()
	defer pendingMu.Unlock()

	for _, h := range hashes {
		if h == "" {
			continue
		}

		c, ok := pendingChunks[h]
		if !ok {
			continue
		}

		if c <= 1 {
			delete(pendingChunks, h)
			continue
		}

		pendingChunks[h] = c - 1
	}
}

func IsChunkPending(hash string) bool {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	return pendingChunks[hash] > 0
}

func PendingChunkCount() int {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	return len(pendingChunks)
}
