package main

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var version = "main"

func main() {
	pipe := &Pipe{}
	run := func(cmd *cobra.Command, args []string) { pipe.Run() }

	pipeCmd := &cobra.Command{
		Use:   "pipe",
		Short: "Read sing-box logs from stdin and forward to Seq",
		Run:   run,
	}
	pipe.Bind(pipeCmd.Flags())

	rootCmd := &cobra.Command{
		Use:     "sing2seq",
		Short:   "Forward sing-box logs to Seq",
		Version: version,
		Run:     run,
	}
	rootCmd.Flags().AddFlagSet(pipeCmd.Flags())
	rootCmd.AddCommand(pipeCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

type Pipe struct {
	URL       string
	ApiKey    string
	Insecure  bool
	Timestamp bool
}

func (o *Pipe) Bind(flags *pflag.FlagSet) {
	flags.StringVar(&o.URL, "url", "", "Seq base URL; if empty, write CLEF JSON to stdout")
	flags.StringVar(&o.ApiKey, "api-key", "", "Seq API key")
	flags.BoolVar(&o.Insecure, "insecure", false, "skip TLS verification")
	flags.BoolVar(&o.Timestamp, "timestamp", false, "include timestamp in sing2seq's own log output")
}

func (o *Pipe) Run() {
	submit := o.newSink()
	defer submit(nil)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if ev := parseLine(scanner.Text()); ev != nil {
			submit(ev)
		}
	}
}

// newSink returns a function that accepts events; a nil event signals shutdown.
func (o *Pipe) newSink() func(*orderedEvent) {
	if o.URL == "" {
		return stdoutSink()
	}
	return o.batcherSink()
}

func stdoutSink() func(*orderedEvent) {
	w := bufio.NewWriter(os.Stdout)
	enc := json.NewEncoder(w)
	return func(ev *orderedEvent) {
		if ev == nil {
			_ = w.Flush()
			return
		}
		_ = enc.Encode(ev)
	}
}

func (o *Pipe) batcherSink() func(*orderedEvent) {
	b := NewBatcher(o.URL, o.ApiKey, o.Insecure, o.Timestamp)
	b.Start()
	return func(ev *orderedEvent) {
		if ev == nil {
			b.Close()
			return
		}
		b.Submit(ev)
	}
}
