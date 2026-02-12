package router

import "hash/fnv"

func HashRoute(partitionKey string, numShards int) int {
	h := fnv.New32a()
	h.Write([]byte(partitionKey))
	return int(h.Sum32()) % numShards
}
