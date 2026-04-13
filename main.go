package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var version = "main"

func main() {
	apiKey := flag.String("api-key", "", "Seq API key")
	insecure := flag.Bool("insecure", false, "skip TLS verification")
	timestamp := flag.Bool("timestamp", false, "include timestamp in sing2seq's own log output (match sing-box log.timestamp)")
	showVersion := flag.Bool("version", false, "print version and exit")

	var (
		urlVal    string
		stdoutVal = true
		lastMode  = "stdout"
	)
	flag.Func("url", "Seq base URL (cancels -stdout)", func(s string) error {
		urlVal = s
		lastMode = "url"
		return nil
	})
	flag.BoolFunc("stdout", "write CLEF JSON to stdout, one event per line (cancels -url) (default true)", func(s string) error {
		v, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		stdoutVal = v
		lastMode = "stdout"
		return nil
	})

	args := os.Args[1:]
	if opts := strings.TrimSpace(os.Getenv("SING2SEQ_OPTS")); opts != "" {
		args = append(strings.Fields(opts), args...)
	}
	_ = flag.CommandLine.Parse(args)

	logTimestamp = *timestamp

	if *showVersion {
		fmt.Printf("sing2seq %s\n", version)
		return
	}

	switch lastMode {
	case "stdout":
		urlVal = ""
	case "url":
		stdoutVal = false
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	if stdoutVal {
		w := bufio.NewWriter(os.Stdout)
		defer func() { _ = w.Flush() }()
		enc := json.NewEncoder(w)
		for scanner.Scan() {
			if ev := parseLine(scanner.Text()); ev != nil {
				_ = enc.Encode(ev)
			}
		}
		return
	}

	if urlVal == "" {
		_, _ = fmt.Fprintln(os.Stderr, "sing2seq: -url is required when -stdout=false")
		os.Exit(2)
	}

	b := NewBatcher(urlVal, *apiKey, *insecure)
	b.Start()
	for scanner.Scan() {
		if ev := parseLine(scanner.Text()); ev != nil {
			b.Submit(ev)
		}
	}
	b.Close()
}
