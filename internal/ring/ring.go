package ring

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"

	"github.com/brown/duckdb-cluster/internal/config"
)

// Ring represents a consistent hash ring for distributed node discovery
type Ring struct {
	cfg       *config.Config
	nodes     map[string]*Node // node ID -> node
	hashRing  []uint32         // sorted hash values
	ringMap   map[uint32]string // hash -> node ID
	vnodes    int              // virtual nodes per instance
	mu        sync.RWMutex
}

// Node represents a cluster member
type Node struct {
	ID       string
	Addr     string
	IsLocal  bool
}

// NewRing creates a new consistent hash ring
func NewRing(cfg *config.Config) *Ring {
	return &Ring{
		cfg:      cfg,
		nodes:    make(map[string]*Node),
		ringMap:  make(map[uint32]string),
		vnodes:   128, // 128 virtual nodes per instance (Loki default)
	}
}

// Init initializes the ring
func (r *Ring) Init() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// In single-node mode, add only the local node
	if r.cfg.IsSingleNode() {
		localNode := &Node{
			ID:      r.cfg.Ring.InstanceID,
			Addr:    r.cfg.Ring.InstanceAddr,
			IsLocal: true,
		}
		r.addNode(localNode)
	} else {
		// TODO: In distributed mode, use memberlist for discovery
		return fmt.Errorf("distributed mode not yet implemented")
	}
	
	return nil
}

// addNode adds a node to the ring (must hold lock)
func (r *Ring) addNode(node *Node) {
	r.nodes[node.ID] = node
	
	// Add virtual nodes to the ring
	for i := 0; i < r.vnodes; i++ {
		hash := r.hash(fmt.Sprintf("%s-%d", node.ID, i))
		r.ringMap[hash] = node.ID
		r.hashRing = append(r.hashRing, hash)
	}
	
	// Sort the ring
	sort.Slice(r.hashRing, func(i, j int) bool {
		return r.hashRing[i] < r.hashRing[j]
	})
}

// GetNode returns the node responsible for the given key
func (r *Ring) GetNode(key string) (*Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if len(r.nodes) == 0 {
		return nil, fmt.Errorf("ring is empty")
	}
	
	hash := r.hash(key)
	
	// Find the first node with hash >= key hash
	idx := sort.Search(len(r.hashRing), func(i int) bool {
		return r.hashRing[i] >= hash
	})
	
	// Wrap around if we went past the end
	if idx >= len(r.hashRing) {
		idx = 0
	}
	
	nodeID := r.ringMap[r.hashRing[idx]]
	node, exists := r.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found in ring", nodeID)
	}
	
	return node, nil
}

// GetAllNodes returns all nodes in the ring
func (r *Ring) GetAllNodes() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	nodes := make([]*Node, 0, len(r.nodes))
	for _, node := range r.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetLocalNode returns the local node
func (r *Ring) GetLocalNode() (*Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	for _, node := range r.nodes {
		if node.IsLocal {
			return node, nil
		}
	}
	
	return nil, fmt.Errorf("local node not found")
}

// hash computes a hash for a key
func (r *Ring) hash(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// IsSingleNode returns true if this is a single-node ring
func (r *Ring) IsSingleNode() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes) == 1
}

// TODO: Future enhancements for distributed mode
// - AddNode(node) - add a new node to the ring
// - RemoveNode(nodeID) - remove a node from the ring
// - Integration with memberlist for gossip-based discovery
