package client

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// BulkItem represents a single item in a bulk indexing operation.
type BulkItem struct {
	SQL          string
	PartitionKey string
}

// BulkIndexer performs async batch inserts with automatic flushing.
type BulkIndexer struct {
	client         *Client
	buffer         []BulkItem
	mu             sync.Mutex
	flushSize      int
	flushInterval  time.Duration
	workers        int
	errorHandler   func(error)
	successHandler func(*QueryResponse)
	stopCh         chan struct{}
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
}

// BulkIndexerConfig configures a BulkIndexer.
type BulkIndexerConfig struct {
	// FlushSize is the number of items to buffer before flushing.
	// Default: 1000
	FlushSize int

	// FlushInterval is the maximum time between flushes.
	// Default: 5 seconds
	FlushInterval time.Duration

	// Workers is the number of concurrent workers for flushing.
	// Default: 4
	Workers int

	// ErrorHandler is called when an item fails.
	// If nil, errors are silently dropped.
	ErrorHandler func(error)

	// SuccessHandler is called when an item succeeds.
	// If nil, successes are not tracked.
	SuccessHandler func(*QueryResponse)
}

// NewBulkIndexer creates a new BulkIndexer.
func (c *Client) NewBulkIndexer(cfg BulkIndexerConfig) *BulkIndexer {
	if cfg.FlushSize <= 0 {
		cfg.FlushSize = 1000
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}

	ctx, cancel := context.WithCancel(context.Background())

	bi := &BulkIndexer{
		client:         c,
		buffer:         make([]BulkItem, 0, cfg.FlushSize),
		flushSize:      cfg.FlushSize,
		flushInterval:  cfg.FlushInterval,
		workers:        cfg.Workers,
		errorHandler:   cfg.ErrorHandler,
		successHandler: cfg.SuccessHandler,
		stopCh:         make(chan struct{}),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Start periodic flush timer
	bi.wg.Add(1)
	go bi.periodicFlush()

	return bi
}

// Add adds an item to the bulk indexer.
// Returns an error if the indexer has been closed.
func (bi *BulkIndexer) Add(ctx context.Context, item BulkItem) error {
	select {
	case <-bi.stopCh:
		return fmt.Errorf("bulk indexer closed")
	default:
	}

	bi.mu.Lock()
	bi.buffer = append(bi.buffer, item)
	shouldFlush := len(bi.buffer) >= bi.flushSize
	bi.mu.Unlock()

	if shouldFlush {
		return bi.Flush(ctx)
	}

	return nil
}

// Flush immediately flushes all buffered items.
func (bi *BulkIndexer) Flush(ctx context.Context) error {
	bi.mu.Lock()
	if len(bi.buffer) == 0 {
		bi.mu.Unlock()
		return nil
	}

	items := bi.buffer
	bi.buffer = make([]BulkItem, 0, bi.flushSize)
	bi.mu.Unlock()

	return bi.processBatch(ctx, items)
}

// processBatch processes a batch of items using worker goroutines.
func (bi *BulkIndexer) processBatch(ctx context.Context, items []BulkItem) error {
	if len(items) == 0 {
		return nil
	}

	// Create work channel
	workCh := make(chan BulkItem, len(items))
	for _, item := range items {
		workCh <- item
	}
	close(workCh)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < bi.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				bi.processItem(ctx, item)
			}
		}()
	}

	wg.Wait()
	return nil
}

// processItem processes a single bulk item.
func (bi *BulkIndexer) processItem(ctx context.Context, item BulkItem) {
	resp, err := bi.client.Query(ctx, item.SQL, item.PartitionKey)
	if err != nil {
		if bi.errorHandler != nil {
			bi.errorHandler(fmt.Errorf("bulk insert failed for key %s: %w", item.PartitionKey, err))
		}
		return
	}

	if bi.successHandler != nil {
		bi.successHandler(resp)
	}
}

// periodicFlush runs in the background and flushes buffered items periodically.
func (bi *BulkIndexer) periodicFlush() {
	defer bi.wg.Done()

	ticker := time.NewTicker(bi.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_ = bi.Flush(bi.ctx)
		case <-bi.stopCh:
			return
		}
	}
}

// Close stops the bulk indexer and flushes any remaining items.
func (bi *BulkIndexer) Close(ctx context.Context) error {
	close(bi.stopCh)
	bi.wg.Wait()
	bi.cancel()

	// Final flush
	return bi.Flush(ctx)
}

// Stats returns statistics about the bulk indexer.
func (bi *BulkIndexer) Stats() BulkStats {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	return BulkStats{
		BufferedItems: len(bi.buffer),
	}
}

// BulkStats contains statistics about the bulk indexer.
type BulkStats struct {
	BufferedItems int
}
