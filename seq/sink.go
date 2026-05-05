// Package seq 提供异步批量把 CLEF 事件投递到 Seq 的 sink。
package seq

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/moonfruit/sing2seq/clef"
)

const (
	defaultBatchSize      = 200
	defaultChannelBuffer  = 1024
	defaultMaxPending     = 50000
	defaultInitialBackoff = 1 * time.Second
	defaultMaxBackoff     = 60 * time.Second
)

// Config 配置 Sink。零值字段套默认。
type Config struct {
	URL        string
	APIKey     string
	Insecure   bool
	HTTPClient *http.Client
	Emitter    *clef.Emitter // 可选；用于发出 sink 自身诊断；nil 时 fallback 到 stderr

	BatchSize      int
	ChannelBuffer  int
	MaxPending     int
	DropTarget     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// Sink 是异步批量 HTTP sink。
type Sink struct {
	cfg Config

	ch        chan *clef.Event
	done      chan struct{}
	startOnce sync.Once

	closeMu  sync.Mutex
	closed   bool
	closeErr error
}

// NewSink 构造 Sink；不启动 manager。
func NewSink(cfg Config) *Sink {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.ChannelBuffer <= 0 {
		cfg.ChannelBuffer = defaultChannelBuffer
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = defaultMaxPending
	}
	if cfg.DropTarget <= 0 || cfg.DropTarget >= cfg.MaxPending {
		cfg.DropTarget = cfg.MaxPending / 2
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = defaultInitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaultMaxBackoff
	}
	if cfg.HTTPClient == nil {
		tr := &http.Transport{}
		if cfg.Insecure {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second, Transport: tr}
	}
	return &Sink{
		cfg:  cfg,
		ch:   make(chan *clef.Event, cfg.ChannelBuffer),
		done: make(chan struct{}),
	}
}

// Start 启动 manager goroutine。caller 应该只调用一次。
func (s *Sink) Start() {
	s.startOnce.Do(func() { go s.run() })
}

// Submit 投递事件；O(1) 不阻塞；nil 忽略；Close 之后丢弃。
// 持锁期间执行非阻塞 channel 发送，避免与 Close 竞态。
func (s *Sink) Submit(ev *clef.Event) {
	if ev == nil {
		return
	}
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- ev:
	default:
		// ChannelBuffer 满（罕见，因为 manager goroutine 是 O(1) 消费）→ 丢弃。
	}
}

// Close 停止接受新事件；阻塞直到 pending 排空；返回 drain 期间最后一个 post error。
func (s *Sink) Close() error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		<-s.done
		return s.closeErr
	}
	s.closed = true
	close(s.ch)
	s.closeMu.Unlock()
	<-s.done
	return s.closeErr
}

type postResult struct {
	n   int
	err error
}

// run 是 manager goroutine。其 select 只做 O(1) 工作，永不阻塞 I/O，
// 确保 ch 总能及时清空、Submit 实际上从不阻塞。
func (s *Sink) run() {
	defer close(s.done)

	var pending []*clef.Event
	var inflight bool
	var droppedTotal int
	backoff := s.cfg.InitialBackoff
	var retryC <-chan time.Time
	resultC := make(chan postResult, 1)
	closed := false

	dispatch := func() {
		if inflight || retryC != nil || len(pending) == 0 {
			return
		}
		n := min(len(pending), s.cfg.BatchSize)
		batch := make([]*clef.Event, n)
		copy(batch, pending[:n])
		inflight = true
		go func() {
			resultC <- postResult{n: n, err: s.post(batch)}
		}()
	}

	for {
		var inC <-chan *clef.Event
		if !closed {
			inC = s.ch
		}

		select {
		case ev, ok := <-inC:
			if !ok {
				closed = true
				retryC = nil
				dispatch()
				break
			}
			pending = append(pending, ev)
			if len(pending) > s.cfg.MaxPending {
				drop := len(pending) - s.cfg.DropTarget
				pending = pending[:copy(pending, pending[drop:])]
				droppedTotal += drop
				s.diag(clef.LevelWarn, "buffer_overflow",
					"buffer overflow: dropped {Dropped} oldest events (total dropped={TotalDropped})",
					map[string]any{"Dropped": drop, "TotalDropped": droppedTotal})
			}
			dispatch()

		case r := <-resultC:
			inflight = false
			if r.err == nil {
				pending = pending[:copy(pending, pending[r.n:])]
				backoff = s.cfg.InitialBackoff
				dispatch()
			} else if closed {
				s.closeErr = r.err
				s.diag(clef.LevelError, "shutdown_post_failed",
					"post failed during shutdown (pending={Pending}): {Error}; dropping remaining events",
					map[string]any{"Pending": len(pending), "Error": r.err.Error()})
				droppedTotal += len(pending)
				pending = pending[:0]
			} else {
				s.diag(clef.LevelWarn, "post_failed",
					"post failed (pending={Pending}): {Error}; retry in {RetryIn}",
					map[string]any{"Pending": len(pending), "Error": r.err.Error(), "RetryIn": backoff.String()})
				retryC = time.After(backoff)
				backoff = min(backoff*2, s.cfg.MaxBackoff)
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

func (s *Sink) post(events []*clef.Event) error {
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
	url := strings.TrimRight(s.cfg.URL, "/") + "/ingest/clef"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/vnd.serilog.clef")
	if s.cfg.APIKey != "" {
		req.Header.Set("X-Seq-ApiKey", s.cfg.APIKey)
	}
	resp, err := s.cfg.HTTPClient.Do(req)
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

func (s *Sink) diag(level clef.Level, eventID, mt string, fields map[string]any) {
	if s.cfg.Emitter != nil {
		switch level {
		case clef.LevelWarn:
			s.cfg.Emitter.Warn("seq.sink", eventID, mt, fields)
		case clef.LevelError:
			s.cfg.Emitter.Error("seq.sink", eventID, mt, fields)
		default:
			s.cfg.Emitter.Info("seq.sink", eventID, mt, fields)
		}
		return
	}
	rendered := mt
	for k, v := range fields {
		rendered = strings.ReplaceAll(rendered, "{"+k+"}", fmt.Sprintf("%v", v))
	}
	_, _ = fmt.Fprintf(os.Stderr, "[seq.sink] %s %s: %s\n", level.CLEFName(), eventID, rendered)
}
