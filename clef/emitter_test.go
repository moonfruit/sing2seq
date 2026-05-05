package clef

import (
	"sync"
	"testing"
	"time"
)

func TestEmitterPublishesToBus(t *testing.T) {
	bus := NewBus(8)
	defer bus.Close()

	var mu sync.Mutex
	var got []*Event
	bus.Subscribe(SubscriberFunc{
		MatchFn:   func(*Event) bool { return true },
		DeliverFn: func(e *Event) { mu.Lock(); got = append(got, e); mu.Unlock() },
	})

	em := NewEmitter(EmitterConfig{Source: "sing2seq", MinLevel: LevelInfo, Bus: bus})
	em.Info("seq.sink", "buffer_overflow", "buffer overflow: dropped {Dropped}", map[string]any{"Dropped": 100})

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	ev := got[0]
	checkField(t, ev, "@l", "Information")
	checkField(t, ev, "Source", "sing2seq")
	checkField(t, ev, "Module", "seq.sink")
	checkField(t, ev, "EventID", "buffer_overflow")
	checkField(t, ev, "Dropped", 100)
}

func TestEmitterDropsBelowMinLevel(t *testing.T) {
	bus := NewBus(8)
	defer bus.Close()

	var mu sync.Mutex
	var got []*Event
	bus.Subscribe(SubscriberFunc{
		MatchFn:   func(*Event) bool { return true },
		DeliverFn: func(e *Event) { mu.Lock(); got = append(got, e); mu.Unlock() },
	})

	em := NewEmitter(EmitterConfig{Source: "sing2seq", MinLevel: LevelWarn, Bus: bus})
	em.Info("m", "skip", "should be dropped", nil)
	em.Warn("m", "kept", "should be kept", nil)

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("want 1 event after MinLevel filter, got %d", len(got))
	}
	checkField(t, got[0], "EventID", "kept")
}

func TestEmitterPublishExternalBypassesMinLevel(t *testing.T) {
	bus := NewBus(8)
	defer bus.Close()

	var mu sync.Mutex
	var got []*Event
	bus.Subscribe(SubscriberFunc{
		MatchFn:   func(*Event) bool { return true },
		DeliverFn: func(e *Event) { mu.Lock(); got = append(got, e); mu.Unlock() },
	})

	em := NewEmitter(EmitterConfig{Source: "daemon", MinLevel: LevelFatal, Bus: bus})
	external := NewEvent()
	external.Set("@t", "2026-05-05T00:00:00Z")
	external.Set("@l", "Information")
	external.Set("Source", "sing-box")
	em.PublishExternal(external)

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("want 1 external event, got %d", len(got))
	}
}

func checkField(t *testing.T, e *Event, key string, want any) {
	t.Helper()
	v, ok := e.Get(key)
	if !ok {
		t.Fatalf("key %q missing", key)
	}
	if v != want {
		t.Fatalf("key %q = %v, want %v", key, v, want)
	}
}
