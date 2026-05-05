package main

import (
	"bufio"
	"io"

	"github.com/moonfruit/sing2seq/clef"
	"github.com/moonfruit/sing2seq/seq"
	"github.com/spf13/pflag"
)

type Pipe struct {
	URL          string
	APIKey       string
	Insecure     bool
	Timestamp    bool
	DisableColor bool
}

func (p *Pipe) Bind(flags *pflag.FlagSet) {
	flags.StringVarP(&p.URL, "url", "u", "", "Seq base URL; if empty, write CLEF JSON to stdout")
	flags.StringVarP(&p.APIKey, "api-key", "k", "", "Seq API key")
	flags.BoolVar(&p.Insecure, "insecure", false, "skip TLS verification")
	flags.BoolVar(&p.Timestamp, "timestamp", false, "include timestamp in pretty stderr output")
	flags.BoolVar(&p.DisableColor, "disable-color", false, "disable color in pretty stderr output")
}

// Run reads sing-box stderr from r line-by-line, parses each line, and fans events
// out via a clef.Bus to:
//   - pretty renderer (always, stderr)
//   - either seq.Sink (when URL != "") or stdoutSink (URL == "")
//
// sing2seq's own diagnostics also flow through the same emitter+bus with
// Source="sing2seq". Returns the last sink error (if any) on shutdown.
func (p *Pipe) Run(r io.Reader) error {
	bus := clef.NewBus(256)
	em := clef.NewEmitter(clef.EmitterConfig{Source: "sing2seq", MinLevel: clef.LevelInfo, Bus: bus})

	bus.Subscribe(newPrettyRenderer(p.Timestamp, p.DisableColor))

	var sinkClose func() error
	if p.URL == "" {
		stdout := newStdoutSink()
		bus.Subscribe(stdout)
		sinkClose = func() error { stdout.Flush(); return nil }
	} else {
		sk := seq.NewSink(seq.Config{
			URL: p.URL, APIKey: p.APIKey, Insecure: p.Insecure,
			Emitter: em,
		})
		sk.Start()
		bus.Subscribe(clef.SubscriberFunc{
			MatchFn:   func(*clef.Event) bool { return true },
			DeliverFn: func(e *clef.Event) { sk.Submit(e) },
		})
		sinkClose = sk.Close
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if ev := clef.ParseSingBoxLine(scanner.Text()); ev != nil {
			em.PublishExternal(ev)
		}
	}

	err := sinkClose()
	bus.Close()
	return err
}
