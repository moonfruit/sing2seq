package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

var version = "main"

func main() {
	url := flag.String("url", "http://localhost:5341", "Seq base URL")
	apiKey := flag.String("api-key", "", "Seq API key")
	insecure := flag.Bool("insecure", false, "skip TLS verification")
	timestamp := flag.Bool("timestamp", false, "include timestamp in sing2seq's own log output (match sing-box log.timestamp)")
	showVersion := flag.Bool("version", false, "print version and exit")

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

	b := NewBatcher(*url, *apiKey, *insecure)
	b.Start()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if ev := parseLine(scanner.Text()); ev != nil {
			b.Submit(ev)
		}
	}
	b.Close()
}
