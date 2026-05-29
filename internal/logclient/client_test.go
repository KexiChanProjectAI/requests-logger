package logclient_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logclient"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
	"github.com/user/openai-go-proxy-logger/internal/testutil"
)

func TestEnqueueDequeuesAndSendsToServer(t *testing.T) {
	fls := testutil.NewFakeLogServer()
	defer fls.Server.Close()

	cfg := config.ProxyConfig{
		LogServerURL:    fls.URL(),
		LogClientWorkers: 1,
		LogServerToken: "test-token-123",
		LogQueueSize:   1024,
	}

	client := logclient.NewClient(cfg)
	client.Start()
	defer client.Stop()

	record := logschema.NewRecord()
	record.LogID = "log-abc"
	record.RequestID = "req-123"
	record.Route = "/v1/chat/completions"
	record.Method = "POST"
	record.URL = "https://api.openai.com/v1/chat/completions"
	record.TerminalStatus = logschema.TerminalStatusCompleted

	client.Enqueue(record)

	time.Sleep(100 * time.Millisecond)

	captures := fls.GetCaptures()
	if len(captures) != 1 {
		t.Fatalf("expected 1 capture, got %d", len(captures))
	}

	var received logschema.Record
	if err := json.Unmarshal(captures[0].Body, &received); err != nil {
		t.Fatalf("failed to unmarshal received body: %v", err)
	}

	if received.LogID != "log-abc" {
		t.Errorf("expected LogID 'log-abc', got '%s'", received.LogID)
	}
	if received.RequestID != "req-123" {
		t.Errorf("expected RequestID 'req-123', got '%s'", received.RequestID)
	}
	if received.Route != "/v1/chat/completions" {
		t.Errorf("expected Route '/v1/chat/completions', got '%s'", received.Route)
	}
	if received.TerminalStatus != logschema.TerminalStatusCompleted {
		t.Errorf("expected TerminalStatus 'completed', got '%s'", received.TerminalStatus)
	}
}

func TestBearerTokenSentInAuthorizationHeader(t *testing.T) {
	fls := testutil.NewFakeLogServer()
	defer fls.Server.Close()

	cfg := config.ProxyConfig{
		LogServerURL:    fls.URL(),
		LogClientWorkers: 1,
		LogServerToken: "my-secret-token",
		LogQueueSize:   1024,
	}

	client := logclient.NewClient(cfg)
	client.Start()
	defer client.Stop()

	record := logschema.NewRecord()
	client.Enqueue(record)

	time.Sleep(100 * time.Millisecond)

	captures := fls.GetCaptures()
	if len(captures) != 1 {
		t.Fatalf("expected 1 capture, got %d", len(captures))
	}

	authHeader := captures[0].Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		t.Errorf("expected Authorization header to start with 'Bearer ', got '%s'", authHeader)
	}
	if !strings.Contains(authHeader, "my-secret-token") {
		t.Errorf("expected Authorization header to contain token, got '%s'", authHeader)
	}
}

func TestEnqueueReturnsQuicklyWhenServerDown(t *testing.T) {
	cfg := config.ProxyConfig{
		LogServerURL:   "http://localhost:9999",
		LogServerToken: "test-token",
		LogQueueSize:   1024,
	}

	client := logclient.NewClient(cfg)
	client.Start()

	start := time.Now()
	client.Enqueue(logschema.NewRecord())
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("Enqueue took too long (%v), expected non-blocking behavior", elapsed)
	}

	client.Stop()
}

func TestQueueFullDropsNewestAndIncrementsCounter(t *testing.T) {
	fls := testutil.NewFakeLogServer()
	defer fls.Server.Close()

	cfg := config.ProxyConfig{
		LogServerURL:    fls.URL(),
		LogClientWorkers: 1,
		LogServerToken: "test-token",
		LogQueueSize:   2,
	}

	client := logclient.NewClient(cfg)
	client.Start()

	for i := 0; i < 5; i++ {
		record := logschema.NewRecord()
		record.LogID = "log-dropped"
		client.Enqueue(record)
	}

	time.Sleep(100 * time.Millisecond)

	dropped := client.DroppedCount()
	if dropped != 3 {
		t.Errorf("expected 3 dropped records, got %d", dropped)
	}
}

func TestBackgroundWorkerRetriesOnTransientFailures(t *testing.T) {
	var mu sync.Mutex
	attemptCount := 0
	failUntilAttempt := 3

	fls := testutil.NewFakeLogServer()
	originalHandler := fls.Server.Config.Handler

	fls.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attemptCount++
		currentAttempt := attemptCount
		mu.Unlock()

		if currentAttempt < failUntilAttempt {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		originalHandler.ServeHTTP(w, r)
	})

	cfg := config.ProxyConfig{
		LogServerURL:    fls.URL(),
		LogClientWorkers: 1,
		LogServerToken: "test-token",
		LogQueueSize:   1024,
	}

	client := logclient.NewClient(cfg)
	client.Start()
	defer client.Stop()

	record := logschema.NewRecord()
	client.Enqueue(record)

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if attemptCount < failUntilAttempt {
		t.Errorf("expected at least %d attempts, got %d", failUntilAttempt, attemptCount)
	}
}

func TestStopDrainsRemainingRecords(t *testing.T) {
	fls := testutil.NewFakeLogServer()
	defer fls.Server.Close()

	cfg := config.ProxyConfig{
		LogServerURL:    fls.URL(),
		LogClientWorkers: 1,
		LogServerToken: "test-token",
		LogQueueSize:   1024,
	}

	client := logclient.NewClient(cfg)
	client.Start()

	for i := 0; i < 5; i++ {
		record := logschema.NewRecord()
		record.LogID = "log-drain"
		client.Enqueue(record)
	}

	client.Stop()

	captures := fls.GetCaptures()
	if len(captures) != 5 {
		t.Errorf("expected 5 captured records after Stop(), got %d", len(captures))
	}
}

func TestEnqueueNeverBlocks(t *testing.T) {
	fls := testutil.NewFakeLogServer()
	defer fls.Server.Close()

	cfg := config.ProxyConfig{
		LogServerURL:    fls.URL(),
		LogClientWorkers: 1,
		LogServerToken: "test-token",
		LogQueueSize:   1,
	}

	client := logclient.NewClient(cfg)
	client.Start()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			client.Enqueue(logschema.NewRecord())
			if time.Since(start) > 100*time.Millisecond {
				t.Errorf("Enqueue blocked unexpectedly")
			}
		}()
	}

	wg.Wait()
	client.Stop()
}

func TestStopDoesNotPanicWhenWorkerReceivesClosedChannel(t *testing.T) {
	cfg := config.ProxyConfig{
		LogServerURL:   "http://localhost:9999",
		LogServerToken: "test-token",
		LogQueueSize:   1,
	}

	client := logclient.NewClient(cfg)
	client.Start()
	client.Enqueue(logschema.NewRecord())

	done := make(chan struct{})
	go func() {
		defer close(done)
		client.Stop()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}
}
