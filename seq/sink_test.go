package seq

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moonfruit/sing2seq/clef"
)

func newEvent(id string) *clef.Event {
	e := clef.NewEvent()
	e.Set("@t", "2026-05-05T00:00:00Z")
	e.Set("@l", "Information")
	e.Set("Source", "sing-box")
	e.Set("EventID", id)
	return e
}

// captureBus returns (bus, &events slice, mutex). Caller can read events under mu.
func captureBus(t *testing.T) (*clef.Bus, *[]*clef.Event, *sync.Mutex) {
	t.Helper()
	bus := clef.NewBus(64)
	t.Cleanup(bus.Close)
	var mu sync.Mutex
	got := make([]*clef.Event, 0)
	bus.Subscribe(clef.SubscriberFunc{
		MatchFn:   func(*clef.Event) bool { return true },
		DeliverFn: func(e *clef.Event) { mu.Lock(); got = append(got, e); mu.Unlock() },
	})
	return bus, &got, &mu
}

func TestSinkPostsBatch(t *testing.T) {
	var received int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/vnd.serilog.clef" {
			t.Errorf("content-type = %q", got)
		}
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := NewSink(Config{URL: srv.URL, BatchSize: 5})
	s.Start()
	for i := 0; i < 5; i++ {
		s.Submit(newEvent("e"))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if atomic.LoadInt32(&received) == 0 {
		t.Fatal("expected at least one POST")
	}
}

func TestSinkRetriesOnTransientFailure(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus, got, mu := captureBus(t)
	em := clef.NewEmitter(clef.EmitterConfig{Source: "sing2seq", Bus: bus})

	s := NewSink(Config{
		URL:            srv.URL,
		Emitter:        em,
		BatchSize:      1,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	})
	s.Start()
	s.Submit(newEvent("retry-me"))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&attempts) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	postFailed := 0
	for _, e := range *got {
		v, _ := e.Get("EventID")
		if v == "post_failed" {
			postFailed++
		}
	}
	if postFailed < 2 {
		t.Fatalf("expected >= 2 post_failed diagnostics, got %d", postFailed)
	}
}

func TestSinkBufferOverflowDropsOldest(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(block)

	bus, got, mu := captureBus(t)
	em := clef.NewEmitter(clef.EmitterConfig{Source: "sing2seq", Bus: bus})

	s := NewSink(Config{
		URL:           srv.URL,
		Emitter:       em,
		BatchSize:     10,
		ChannelBuffer: 16,
		MaxPending:    20,
		DropTarget:    10,
	})
	s.Start()
	for i := 0; i < 200; i++ {
		s.Submit(newEvent("flood"))
	}

	// Wait for at least one buffer_overflow diagnostic
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		hit := false
		for _, e := range *got {
			if v, _ := e.Get("EventID"); v == "buffer_overflow" {
				hit = true
				break
			}
		}
		mu.Unlock()
		if hit {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("did not observe buffer_overflow diagnostic event")
}

func TestSinkCloseDrainsPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := NewSink(Config{URL: srv.URL, BatchSize: 50})
	s.Start()
	for i := 0; i < 100; i++ {
		s.Submit(newEvent("drain"))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSinkCloseReturnsLastErrorOnFailedDrain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewSink(Config{
		URL:            srv.URL,
		BatchSize:      10,
		InitialBackoff: 10 * time.Millisecond,
	})
	s.Start()
	s.Submit(newEvent("fail-on-shutdown"))
	err := s.Close()
	if err == nil {
		t.Fatal("expected error from Close on shutdown post failure")
	}
	if !strings.Contains(err.Error(), "seq ingest failed") {
		t.Fatalf("error = %v, want seq ingest failed", err)
	}
}
