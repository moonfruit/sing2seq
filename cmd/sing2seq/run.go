package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

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
		_, _ = io.WriteString(os.Stderr, "FATAL failed to start "+o.SingBox+": "+err.Error()+"\n")
		os.Exit(1)
	}

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

	_ = o.Pipe.Run(pr)

	err := <-waitErr
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	if err != nil {
		_, _ = io.WriteString(os.Stderr, "ERROR "+o.SingBox+" exited: "+err.Error()+"\n")
		os.Exit(1)
	}
}

func (o *RunCmd) buildRunArgs(args []string) []string {
	runArgs := []string{"run"}
	if o.Timestamp {
		f, err := os.CreateTemp("", "sing2seq-timestamp-*.json")
		if err != nil {
			_, _ = io.WriteString(os.Stderr, "FATAL failed to create timestamp config: "+err.Error()+"\n")
			os.Exit(1)
		}
		if _, err := f.WriteString(`{"log":{"timestamp":true}}`); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			_, _ = io.WriteString(os.Stderr, "FATAL failed to write timestamp config: "+err.Error()+"\n")
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
