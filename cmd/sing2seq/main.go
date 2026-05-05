package main

import (
	"os"

	"github.com/spf13/cobra"
)

var version = "main"

func main() {
	pipe := &Pipe{}
	pipeRun := func(cmd *cobra.Command, args []string) { _ = pipe.Run(os.Stdin) }

	pipeCmd := &cobra.Command{
		Use:   "pipe",
		Short: "Read sing-box logs from stdin and forward to Seq",
		Run:   pipeRun,
	}
	pipe.Bind(pipeCmd.Flags())

	runOpts := &RunCmd{}
	runCmd := &cobra.Command{
		Use:   "run [flags] [-- sing-box-args...]",
		Short: "Spawn sing-box and forward its stderr logs to Seq",
		Run:   func(cmd *cobra.Command, args []string) { runOpts.Run(args) },
	}
	runOpts.Pipe.Bind(runCmd.Flags())
	runCmd.Flags().StringVarP(&runOpts.SingBox, "sing-box", "p", "sing-box", "sing-box command to spawn")
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
