package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
)

var version = "main"

func main() {
	defaultURL := os.Getenv("SEQ_URL")
	if defaultURL == "" {
		defaultURL = "http://localhost:5341"
	}
	defaultKey := os.Getenv("SEQ_API_KEY")

	url := flag.String("url", defaultURL, "Seq base URL")
	apiKey := flag.String("api-key", defaultKey, "Seq API key")
	insecure := flag.Bool("insecure", false, "skip TLS verification")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

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
