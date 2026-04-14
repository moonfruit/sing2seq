package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var version = "main"

func main() {
	pipe := &Pipe{}
	pipeRun := func(cmd *cobra.Command, args []string) { pipe.Run(os.Stdin) }

	pipeCmd := &cobra.Command{
		Use:   "pipe",
		Short: "Read sing-box logs from stdin and forward to Seq",
		Run:   pipeRun,
	}
	pipe.Bind(pipeCmd.Flags())

	runOpts := &RunCmd{}
	runCmd := &cobra.Command{
		Use:                "run [flags] [-- sing-box-args...]",
		Short:              "Spawn sing-box and forward its stderr logs to Seq",
		DisableFlagParsing: false,
		Run:                func(cmd *cobra.Command, args []string) { runOpts.Run(args) },
	}
	runOpts.Pipe.Bind(runCmd.Flags())
	runCmd.Flags().StringVar(&runOpts.SingBox, "sing-box", "sing-box", "sing-box command to spawn")
	runCmd.Flags().StringArrayVarP(&runOpts.Config, "config", "c", nil, "sing-box configuration file path")
	runCmd.Flags().StringArrayVarP(&runOpts.ConfigDirectory, "config-directory", "C", nil, "sing-box configuration directory path")
	runCmd.Flags().StringVarP(&runOpts.Directory, "directory", "D", "", "sing-box working directory")

	rootCmd := &cobra.Command{
		Use:     "sing2seq",
		Short:   "Forward sing-box logs to Seq",
		Version: version,
		Run:     pipeRun,
	}
	rootCmd.Flags().AddFlagSet(pipeCmd.Flags())
	rootCmd.AddCommand(pipeCmd, runCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

type Pipe struct {
	URL          string
	ApiKey       string
	Insecure     bool
	Timestamp    bool
	DisableColor bool

	logf logfFunc
}

func (o *Pipe) Logf(level, format string, a ...any) {
	if o.logf == nil {
		o.logf = newLogf(o.Timestamp, o.DisableColor)
	}
	o.logf(level, format, a...)
}

func (o *Pipe) Bind(flags *pflag.FlagSet) {
	flags.StringVar(&o.URL, "url", "", "Seq base URL; if empty, write CLEF JSON to stdout")
	flags.StringVar(&o.ApiKey, "api-key", "", "Seq API key")
	flags.BoolVar(&o.Insecure, "insecure", false, "skip TLS verification")
	flags.BoolVar(&o.Timestamp, "timestamp", false, "include timestamp in log output")
	flags.BoolVar(&o.DisableColor, "disable-color", false, "disable color output")
}

func (o *Pipe) Run(r io.Reader) {
	submit := o.newSink()
	defer submit(nil)

	scanner := bufio.NewScanner(r)
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
	b := NewBatcher(o.URL, o.ApiKey, o.Insecure, o.Logf)
	b.Start()
	return func(ev *orderedEvent) {
		if ev == nil {
			b.Close()
			return
		}
		b.Submit(ev)
	}
}

type RunCmd struct {
	Pipe
	SingBox         string
	Config          []string
	ConfigDirectory []string
	Directory       string
}

func (o *RunCmd) Run(args []string) {
	runArgs := o.buildRunArgs(args)
	cmd := exec.Command(o.SingBox, runArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	pr, pw := io.Pipe()
	cmd.Stderr = io.MultiWriter(os.Stderr, pw)

	if err := cmd.Start(); err != nil {
		o.Pipe.Logf("FATAL", "failed to start %s: %v", o.SingBox, err)
		os.Exit(1)
	}

	// Forward SIGINT/SIGTERM to the child and stay alive ourselves to flush pending events.
	// In a terminal foreground group the child already receives the signal, so this is a no-op
	// for Ctrl-C; it matters when we're launched from systemd / a pipeline where only our
	// PID is signalled.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)
	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
		_ = pw.Close()
	}()

	o.Pipe.Run(pr)

	err := <-waitErr
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		os.Exit(ee.ExitCode())
	}
	if err != nil {
		o.Pipe.Logf("ERROR", "%s exited: %v", o.SingBox, err)
		os.Exit(1)
	}
}

func (o *RunCmd) buildRunArgs(args []string) []string {
	runArgs := []string{"run"}
	if o.Timestamp {
		f, err := os.CreateTemp("", "sing2seq-timestamp-*.json")
		if err != nil {
			o.Pipe.Logf("FATAL", "failed to create timestamp config: %v", err)
			os.Exit(1)
		}
		if _, err := f.WriteString(`{"log":{"timestamp":true}}`); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			o.Pipe.Logf("FATAL", "failed to write timestamp config: %v", err)
			os.Exit(1)
		}
		_ = f.Close()
		defer func() { _ = os.Remove(f.Name()) }()
		runArgs = append(runArgs, "-c", f.Name())
	}
	for _, c := range o.Config {
		runArgs = append(runArgs, "-c", c)
	}
	for _, c := range o.ConfigDirectory {
		runArgs = append(runArgs, "-C", c)
	}
	if o.Directory != "" {
		runArgs = append(runArgs, "-D", o.Directory)
	}
	if o.DisableColor {
		runArgs = append(runArgs, "--disable-color")
	}
	return append(runArgs, args...)
}
