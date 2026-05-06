package main

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"

	"github.com/moonfruit/sing2seq/clef"
)

type stdoutSink struct {
	mu  sync.Mutex
	bw  *bufio.Writer
	enc *json.Encoder
}

func newStdoutSink() *stdoutSink {
	bw := bufio.NewWriter(os.Stdout)
	return &stdoutSink{bw: bw, enc: json.NewEncoder(bw)}
}

func (s *stdoutSink) Match(*clef.Event) bool { return true }

func (s *stdoutSink) Deliver(e *clef.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.enc.Encode(e)
}

func (s *stdoutSink) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.bw.Flush()
}
