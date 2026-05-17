package cluster

import (
	"math"
	"sort"

	"github.com/zeebo/xxh3"
)

type Node struct {
	ID     string
	Weight float64
}

func RankNodes(key string, nodes []Node) []Node {
	if len(nodes) == 0 {
		return nil
	}

	type scored struct {
		score float64
		node  Node
	}

	pairs := make([]scored, len(nodes))
	for i, n := range nodes {
		pairs[i] = scored{score: hrwScore(key, n), node: n}
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].score != pairs[j].score {
			return pairs[i].score > pairs[j].score
		}
		return pairs[i].node.ID < pairs[j].node.ID
	})

	out := make([]Node, len(pairs))
	for i, p := range pairs {
		out[i] = p.node
	}
	return out
}

func TopN(key string, nodes []Node, n int) []Node {
	ranked := RankNodes(key, nodes)
	if n > len(ranked) {
		n = len(ranked)
	}
	return ranked[:n]
}

func hrwScore(key string, n Node) float64 {
	h := xxh3.HashString(key + "|" + n.ID)

	u := (float64(h>>11) + 1) / float64(uint64(1)<<53)

	w := n.Weight
	if w <= 0 {
		w = 1.0
	}

	return w / -math.Log(u)
}
