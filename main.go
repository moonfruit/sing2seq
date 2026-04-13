package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "main"

var (
	apiKey    string
	insecure  bool
	timestamp bool
	url       string
	stdout    bool
)

var rootCmd *cobra.Command

func main() {
	pipeCmd := &cobra.Command{
		Use:   "pipe",
		Short: "Read sing-box logs from stdin and forward to Seq",
		Run:   runPipe,
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("sing2seq %s\n", version)
		},
	}

	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}

	// Register flags on pipeCmd
	pipeCmd.Flags().StringVar(&apiKey, "api-key", "", "Seq API key")
	pipeCmd.Flags().BoolVar(&insecure, "insecure", false, "skip TLS verification")
	pipeCmd.Flags().BoolVar(&timestamp, "timestamp", false, "include timestamp in sing2seq's own log output")
	pipeCmd.Flags().StringVar(&url, "url", "", "Seq base URL")
	pipeCmd.Flags().BoolVar(&stdout, "stdout", true, "write CLEF JSON to stdout, one event per line (default true)")

	rootCmd = &cobra.Command{
		Use:   "sing2seq",
		Short: "Forward sing-box logs to Seq",
		Run:   runPipe,
	}

	// Make pipeCmd's flags available to rootCmd
	rootCmd.Flags().AddFlagSet(pipeCmd.Flags())

	rootCmd.AddCommand(pipeCmd, versionCmd, completionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runPipe(cmd *cobra.Command, args []string) {
	logTimestamp = timestamp

	mode := "stdout"
	if stdout == false && url != "" {
		mode = "url"
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	if mode == "stdout" {
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

	if url == "" {
		_, _ = fmt.Fprintln(os.Stderr, "sing2seq: -url is required when -stdout=false")
		os.Exit(2)
	}

	b := NewBatcher(url, apiKey, insecure)
	b.Start()
	for scanner.Scan() {
		if ev := parseLine(scanner.Text()); ev != nil {
			b.Submit(ev)
		}
	}
	b.Close()
}
