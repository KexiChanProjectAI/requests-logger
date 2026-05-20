package logclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
)

// Client is an asynchronous, fail-open log client that buffers records
// in a bounded queue and POSTs them to the log server in the background.
type Client struct {
	cfg     config.ProxyConfig
	ch      chan *logschema.Record
	wg      sync.WaitGroup
	dropped int64
	stopCh  chan struct{}
	client  *http.Client
}

// NewClient creates a new log client with a bounded queue of the configured size.
func NewClient(cfg config.ProxyConfig) *Client {
	return &Client{
		cfg:    cfg,
		ch:     make(chan *logschema.Record, cfg.LogQueueSize),
		stopCh: make(chan struct{}),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Enqueue adds a record to the send queue. It never blocks and never fails.
// If the queue is full, the newest record is dropped and the dropped counter is incremented.
func (c *Client) Enqueue(record *logschema.Record) {
	select {
	case c.ch <- record:
		// enqueued successfully
	default:
		// queue full, drop newest and increment counter
		atomic.AddInt64(&c.dropped, 1)
	}
}

// DroppedCount returns the number of records that were dropped due to a full queue.
func (c *Client) DroppedCount() int64 {
	return atomic.LoadInt64(&c.dropped)
}

// Start launches the background worker goroutine that POSTs records to the log server.
func (c *Client) Start() {
	c.wg.Add(1)
	go c.worker()
}

// Stop gracefully shuts down the client. It closes the queue channel and waits
// for the worker to drain any remaining records before returning.
func (c *Client) Stop() {
	close(c.stopCh)
	close(c.ch)
	c.wg.Wait()
}

func (c *Client) worker() {
	defer c.wg.Done()

	for {
		select {
		case <-c.stopCh:
			// Drain remaining records before shutting down
			for record := range c.ch {
				c.sendWithRetry(record)
			}
			return
		case record := <-c.ch:
			c.sendWithRetry(record)
		}
	}
}

func (c *Client) sendWithRetry(record *logschema.Record) {
	const maxRetries = 3
	backoff := 100 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(time.Now().UnixNano()%int64(backoff / 2))
			sleep := backoff - (backoff / 2) + jitter
			time.Sleep(sleep)
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}

		err := c.send(record)
		if err == nil {
			return
		}
		lastErr = err

		// Check if error is permanent (4xx) - don't retry
		if isPermanentError(err) {
			log.Printf("logclient: permanent error sending record: %v", err)
			return
		}

		// Transient error (5xx, network) - retry
		log.Printf("logclient: transient error sending record (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	// All retries exhausted, fail-open: log error and continue
	log.Printf("logclient: failed to send record after %d retries: %v", maxRetries+1, lastErr)
}

func (c *Client) send(record *logschema.Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.cfg.LogServerURL+"/logs", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request creation error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.LogServerToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	// Read body to ensure connection is reused
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return &permanentError{statusCode: resp.StatusCode}
	}

	if resp.StatusCode >= 500 {
		return &transientError{statusCode: resp.StatusCode}
	}

	return nil
}

type permanentError struct {
	statusCode int
}

func (e *permanentError) Error() string {
	return fmt.Sprintf("permanent error: status %d", e.statusCode)
}

type transientError struct {
	statusCode int
}

func (e *transientError) Error() string {
	return fmt.Sprintf("transient error: status %d", e.statusCode)
}

func isPermanentError(err error) bool {
	_, ok := err.(*permanentError)
	return ok
}
