package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	batchSize      = 200
	channelBuffer  = 1024
	maxPending     = 50000
	dropTarget     = maxPending / 2
	initialBackoff = 1 * time.Second
	maxBackoff     = 60 * time.Second
)

func (e *orderedEvent) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range e.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(e.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type Batcher struct {
	URL    string
	APIKey string
	Client *http.Client
	Size   int

	ch   chan *orderedEvent
	done chan struct{}
}

func NewBatcher(url, apiKey string, insecure bool) *Batcher {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Batcher{
		URL:    url,
		APIKey: apiKey,
		Client: &http.Client{Timeout: 30 * time.Second, Transport: tr},
		Size:   batchSize,
		ch:     make(chan *orderedEvent, channelBuffer),
		done:   make(chan struct{}),
	}
}

func (b *Batcher) Start()                  { go b.run() }
func (b *Batcher) Submit(ev *orderedEvent) { b.ch <- ev }
func (b *Batcher) Close()                  { close(b.ch); <-b.done }

type postResult struct {
	n   int
	err error
}

// run is G2 (manager). Its select only does O(1) work — never blocks on I/O —
// so b.ch is always drained promptly and Submit effectively never blocks.
func (b *Batcher) run() {
	defer close(b.done)

	var pending []*orderedEvent
	var inflight bool
	var droppedTotal int
	backoff := initialBackoff
	var retryC <-chan time.Time
	resultC := make(chan postResult, 1)
	closed := false

	dispatch := func() {
		if inflight || retryC != nil || len(pending) == 0 {
			return
		}
		n := min(len(pending), b.Size)
		batch := make([]*orderedEvent, n)
		copy(batch, pending[:n])
		inflight = true
		// G3: one-shot sender, dies after reporting result.
		go func() {
			resultC <- postResult{n: n, err: b.post(batch)}
		}()
	}

	for {
		// When closed, nil the channel so the select ignores it.
		var inC <-chan *orderedEvent
		if !closed {
			inC = b.ch
		}

		select {
		case ev, ok := <-inC:
			if !ok {
				closed = true
				dispatch()
				break
			}
			pending = append(pending, ev)
			if len(pending) > maxPending {
				drop := len(pending) - dropTarget
				pending = pending[:copy(pending, pending[drop:])]
				droppedTotal += drop
				printf("[sing2seq] buffer overflow: dropped %d oldest events (total dropped=%d)\n",
					drop, droppedTotal)
			}
			dispatch()

		case r := <-resultC:
			inflight = false
			if r.err == nil {
				pending = pending[:copy(pending, pending[r.n:])]
				backoff = initialBackoff
				dispatch()
			} else {
				printf("[sing2seq] post failed (pending=%d): %v; retry in %s\n", len(pending), r.err, backoff)
				retryC = time.After(backoff)
				backoff = min(backoff*2, maxBackoff)
			}

		case <-retryC:
			retryC = nil
			dispatch()
		}

		if closed && !inflight && retryC == nil && len(pending) == 0 {
			return
		}
	}
}

func (b *Batcher) post(events []*orderedEvent) error {
	var body bytes.Buffer
	for i, ev := range events {
		if i > 0 {
			body.WriteByte('\n')
		}
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		body.Write(data)
	}
	url := strings.TrimRight(b.URL, "/") + "/ingest/clef"
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/vnd.serilog.clef")
	if b.APIKey != "" {
		req.Header.Set("X-Seq-ApiKey", b.APIKey)
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("seq ingest failed: %d %q", resp.StatusCode, data)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func printf(format string, a ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format, a...)
}
