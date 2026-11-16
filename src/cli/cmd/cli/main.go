// Command cli is the terminal client (../../contract.md). It runs cli.Run with ENGINE_URL,
// ENGINE_TOKEN and EDITOR from the environment and exits with its code. SIGINT and SIGTERM go to
// Run, which stops following and exits 130 or 143; they never cancel the work.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentic-platform/cli"
)

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	env := cli.Env{EngineURL: os.Getenv("ENGINE_URL"), EngineToken: os.Getenv("ENGINE_TOKEN"), Editor: os.Getenv("EDITOR"),
		Signals: sigs}
	code := cli.Run(context.Background(), os.Args[1:], env, os.Stdin, os.Stdout, os.Stderr)
	signal.Stop(sigs)
	os.Exit(code)
}
