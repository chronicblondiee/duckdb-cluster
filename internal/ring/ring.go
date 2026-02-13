package ring

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"sort"
	"sync"

	"github.com/brown/duckdb-cluster/internal/config"
	"github.com/hashicorp/memberlist"
)

// Ring represents a consistent hash ring for distributed node discovery
type Ring struct {
	cfg           *config.Config
	nodes         map[string]*Node  // node ID -> node
	hashRing      []uint32          // sorted hash values
	ringMap       map[uint32]string // hash -> node ID
	vnodes        int               // virtual nodes per instance
	mu            sync.RWMutex
	memberlist    *memberlist.Memberlist // gossip-based cluster membership
	healthTracker *HealthTracker         // node health tracking
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
		cfg:           cfg,
		nodes:         make(map[string]*Node),
		ringMap:       make(map[uint32]string),
		vnodes:        128, // 128 virtual nodes per instance (Loki default)
		healthTracker: NewHealthTracker(),
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
		return nil
	}
	
	// Distributed mode: use memberlist for cluster membership
	return r.initMemberlist()
}

// initMemberlist initializes memberlist for distributed mode (must hold lock)
func (r *Ring) initMemberlist() error {
	slog.Info("initializing memberlist", 
		"bind_addr", r.cfg.Ring.Memberlist.BindAddr,
		"bind_port", r.cfg.Ring.Memberlist.BindPort)
	
	// Create memberlist config
	mlConfig := memberlist.DefaultLANConfig()
	mlConfig.Name = r.cfg.Ring.InstanceID
	mlConfig.BindAddr = r.cfg.Ring.Memberlist.BindAddr
	mlConfig.BindPort = r.cfg.Ring.Memberlist.BindPort
	mlConfig.Events = &eventDelegate{ring: r}
	
	// Disable logging to stdout (use our own logger)
	mlConfig.LogOutput = nil
	
	// Create memberlist
	ml, err := memberlist.Create(mlConfig)
	if err != nil {
		return fmt.Errorf("create memberlist: %w", err)
	}
	r.memberlist = ml
	
	// Add local node to ring
	localNode := &Node{
		ID:      r.cfg.Ring.InstanceID,
		Addr:    r.cfg.Ring.InstanceAddr,
		IsLocal: true,
	}
	r.addNode(localNode)
	
	// Join existing cluster if peers are specified
	if len(r.cfg.Ring.Memberlist.JoinPeers) > 0 {
		slog.Info("joining cluster", "peers", r.cfg.Ring.Memberlist.JoinPeers)
		n, err := ml.Join(r.cfg.Ring.Memberlist.JoinPeers)
		if err != nil {
			return fmt.Errorf("join cluster: %w", err)
		}
		slog.Info("joined cluster", "contacted_nodes", n)
	}
	
	return nil
}

// addNode adds a node to the ring (must hold lock)
func (r *Ring) addNode(node *Node) {
	r.nodes[node.ID] = node
	
	// Add to health tracker
	r.healthTracker.AddNode(node.ID, node.Addr)
	
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

// GetHealthTracker returns the health tracker for this ring
func (r *Ring) GetHealthTracker() *HealthTracker {
	return r.healthTracker
}

// GetHealthyNodes returns all nodes that are in healthy state
func (r *Ring) GetHealthyNodes() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	healthy := make([]*Node, 0)
	for _, node := range r.nodes {
		if r.healthTracker.IsHealthy(node.ID) {
			healthy = append(healthy, node)
		}
	}
	return healthy
}

// AddNode adds a node to the ring (thread-safe)
func (r *Ring) AddNode(node *Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addNode(node)
}

// RemoveNode removes a node from the ring (thread-safe)
func (r *Ring) RemoveNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeNode(nodeID)
}

// removeNode removes a node from the ring (must hold lock)
func (r *Ring) removeNode(nodeID string) {
	node, exists := r.nodes[nodeID]
	if !exists {
		return
	}
	
	slog.Info("removing node from ring", "node_id", nodeID, "addr", node.Addr)
	
	// Remove from health tracker
	r.healthTracker.RemoveNode(nodeID)
	
	// Remove node
	delete(r.nodes, nodeID)
	
	// Remove virtual nodes from the ring
	newHashRing := make([]uint32, 0, len(r.hashRing))
	for _, hash := range r.hashRing {
		if r.ringMap[hash] != nodeID {
			newHashRing = append(newHashRing, hash)
		} else {
			delete(r.ringMap, hash)
		}
	}
	r.hashRing = newHashRing
}

// Shutdown cleanly shuts down the ring
func (r *Ring) Shutdown() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if r.memberlist != nil {
		slog.Info("leaving memberlist cluster")
		if err := r.memberlist.Leave(5 * 1000000000); err != nil { // 5 second timeout
			slog.Warn("error leaving memberlist", "error", err)
		}
		if err := r.memberlist.Shutdown(); err != nil {
			return fmt.Errorf("shutdown memberlist: %w", err)
		}
	}
	
	return nil
}

// eventDelegate implements memberlist.EventDelegate for node join/leave events
type eventDelegate struct {
	ring *Ring
}

// NotifyJoin is called when a node joins the cluster
func (e *eventDelegate) NotifyJoin(node *memberlist.Node) {
	slog.Info("node joined cluster", "node", node.Name, "addr", node.Addr.String())
	
	// Add node to ring
	ringNode := &Node{
		ID:      node.Name,
		Addr:    node.Addr.String(),
		IsLocal: node.Name == e.ring.cfg.Ring.InstanceID,
	}
	e.ring.AddNode(ringNode)
}

// NotifyLeave is called when a node leaves the cluster gracefully
func (e *eventDelegate) NotifyLeave(node *memberlist.Node) {
	slog.Info("node left cluster", "node", node.Name, "addr", node.Addr.String())
	e.ring.RemoveNode(node.Name)
}

// NotifyUpdate is called when a node's metadata is updated
func (e *eventDelegate) NotifyUpdate(node *memberlist.Node) {
	slog.Debug("node updated", "node", node.Name, "addr", node.Addr.String())
	// Currently we don't handle updates, but this could be extended
	// to support dynamic address changes or metadata updates
}
