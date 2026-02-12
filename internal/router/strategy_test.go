package router

import (
	"testing"
)

func TestHashRouteDistribution(t *testing.T) {
	numShards := 3
	counts := make(map[int]int)

	// Hash 100 keys and verify distribution across shards
	for i := 0; i < 100; i++ {
		key := string(rune('a'+i%26)) + string(rune('0'+i/26))
		shard := HashRoute(key, numShards)
		if shard < 0 || shard >= numShards {
			t.Fatalf("HashRoute returned %d, expected 0-%d", shard, numShards-1)
		}
		counts[shard]++
	}

	// Each shard should get at least some keys
	for i := 0; i < numShards; i++ {
		if counts[i] == 0 {
			t.Errorf("shard %d got zero keys", i)
		}
	}
}

func TestHashRouteDeterministic(t *testing.T) {
	for i := 0; i < 50; i++ {
		key := "test-key"
		a := HashRoute(key, 5)
		b := HashRoute(key, 5)
		if a != b {
			t.Fatalf("HashRoute not deterministic for key %q", key)
		}
	}
}
