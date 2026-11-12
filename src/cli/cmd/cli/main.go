// Command cli is the terminal client (../../contract.md). It runs cli.Run with ENGINE_URL,
// ENGINE_TOKEN and EDITOR from the environment and exits with its code.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentic-platform/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	env := cli.Env{EngineURL: os.Getenv("ENGINE_URL"), EngineToken: os.Getenv("ENGINE_TOKEN"), Editor: os.Getenv("EDITOR")}
	code := cli.Run(ctx, os.Args[1:], env, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
